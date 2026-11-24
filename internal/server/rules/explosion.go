package rules

import (
	"cmp"
	"math"
	"math/bits"
	"math/rand"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Explosions: which blocks a blast destroys, how much an entity is exposed,
// and the damage and knockback that follow, as plain functions over a block
// reader and caller-supplied randomness. Applying them is internal/server/'s.
// vanilla: ServerExplosion.calculateExplodedPositions, getSeenPercent,
// ExplosionDamageCalculator.getEntityDamageAmount
//
// A ray's intensity is radius * (0.7 + nextFloat*0.6), rolled once per ray;
// each 0.3-block step costs (resistance + 0.3) * 0.3 plus 0.225, and a block
// is destroyed while the intensity reaching it is above 0. Entity damage is
// ((e² + e) / 2) * 7 * radius * 2 + 1, where e is the seen share times the
// falloff (1 - d / (radius*2)). Knockback is the blast-to-eye direction
// scaled by the same falloff and seen share, the vector Entity.push gets and
// a player's Explosion packet carries.

// BlastBlocks are the block coordinates a blast destroyed, the cells the
// caller sets to air, each once (vanilla collects them in a HashSet of
// BlockPos) and sorted by X, then Y, then Z.
type BlastBlocks [][3]int32

type blastScratch struct {
	seen        []uint64
	resistances []float64
}

// Pools are keyed by side; unusually large, command-created blasts are not retained.
var blastScratchPools [64]sync.Pool

// BlastResistance returns a cell's blast resistance, absent for empty air or
// a state outside the registry.
// vanilla: ExplosionDamageCalculator.getBlockExplosionResistance
func BlastResistance(state uint16) (float64, bool) {
	if int(state) >= registry.StateCount {
		return 0, false
	}
	block := registry.BlockOf(state)
	fluid := blocklogic.FluidOf(state).Kind != blocklogic.FluidNone
	if !fluid && (block == registry.BlockAir || block == registry.BlockCaveAir || block == registry.BlockVoidAir) {
		return 0, false
	}
	resistance := block.ExplosionResistance()
	if fluid {
		// vanilla: FluidState.getExplosionResistance (water and lava)
		resistance = max(resistance, 100)
	}
	return resistance, true
}

// The grid directions never depend on the blast or its RNG. Preserve the
// float-rounded components, normalization and widened float step separately.
// vanilla: ServerExplosion.calculateExplodedPositions
var explosionSteps = func() [16*16*16 - 14*14*14][3]float64 {
	var steps [16*16*16 - 14*14*14][3]float64
	i := 0
	for gx := range 16 {
		for gy := range 16 {
			for gz := range 16 {
				if gx != 0 && gx != 15 && gy != 0 && gy != 15 && gz != 0 && gz != 15 {
					continue
				}
				dx := float64(float32(gx)/15*2 - 1)
				dy := float64(float32(gy)/15*2 - 1)
				dz := float64(float32(gz)/15*2 - 1)
				length := math.Sqrt(dx*dx + dy*dy + dz*dz)
				steps[i] = [3]float64{dx / length * 0.30000001192092896, dy / length * 0.30000001192092896, dz / length * 0.30000001192092896}
				i++
			}
		}
	}
	return steps
}()

// ExplosionBlocks casts 16 rays per side from the faces of the 16³ grid
// around the centre, each walked in 0.3 steps while its intensity lasts,
// collecting the blocks it reaches. The intensity rolls on rng, not the
// level random, so a seeded blast replays; an unloaded cell is crossed
// without loss.
//
// override, when non-nil, replaces a cell's resistance (false falls back to
// BlastResistance), so a cell already set to air still braces the blast.
// The world and override must remain unchanged during the ray walk.
func ExplosionBlocks(w BlockReader, rng *rand.Rand, x, y, z, radius float64, override func(bx, by, bz int32) (float64, bool)) BlastBlocks {
	if override == nil && radius > 0 && radius <= 4 {
		return explosionBlocksSmall(w, rng, x, y, z, radius)
	}
	return explosionBlocksGeneral(w, rng, x, y, z, radius, override)
}

// explosionBlocksGeneral keeps a bounded resistance cache and rereads cells
// beyond it: negative overrides can carry a ray outside the radius bound.
func explosionBlocksGeneral(w BlockReader, rng *rand.Rand, x, y, z, radius float64, override func(bx, by, bz int32) (float64, bool)) BlastBlocks {
	// Every step costs at least 0.22500001 and moves 0.30000001, so a ray
	// starting at 1.3·radius stays within reach of the centre; the extra
	// cells cover the floor and first step. A resistance below -0.3 would
	// break the bound, so such a cell goes to a side list instead of off
	// the bitset.
	reach := int32(radius*1.3/0.22500001*0.30000001) + 3
	side := 2*reach + 1
	cx, cy, cz := int32(math.Floor(x))-reach, int32(math.Floor(y))-reach, int32(math.Floor(z))-reach
	cells := int(side) * int(side) * int(side)
	var scratch *blastScratch
	if side < int32(len(blastScratchPools)) {
		scratch, _ = blastScratchPools[side].Get().(*blastScratch)
	}
	if scratch == nil {
		scratch = &blastScratch{make([]uint64, (cells+63)/64), make([]float64, cells)}
	}
	if side < int32(len(blastScratchPools)) {
		defer blastScratchPools[side].Put(scratch)
	}
	seen, resistances := scratch.seen, scratch.resistances
	clear(seen)
	// NaN is unread; -Inf is no resistance, distinct from a zero-resistance block.
	noResistance := math.Inf(-1)
	for i := range resistances {
		resistances[i] = math.NaN()
	}
	var outside [][3]int32
	for gx := range 16 {
		for gy := range 16 {
			for gz := range 16 {
				// Only the faces of the grid fire rays; the interior is
				// covered by the rays that pass through it.
				if gx != 0 && gx != 15 && gy != 0 && gy != 15 && gz != 0 && gz != 15 {
					continue
				}
				// Java widens each float-rounded grid component before
				// normalizing. vanilla: ServerExplosion.calculateExplodedPositions
				dx := float64(float32(gx)/15*2 - 1)
				dy := float64(float32(gy)/15*2 - 1)
				dz := float64(float32(gz)/15*2 - 1)
				length := math.Sqrt(dx*dx + dy*dy + dz*dz)
				dx, dy, dz = dx/length, dy/length, dz/length
				// Power and every attenuation round to float, including
				// the products before adding or subtracting them.
				intensity := float32(radius) * (0.7 + float32(rng.Float32()*0.6))
				px, py, pz := x, y, z
				for intensity > 0 {
					bx, by, bz := int32(math.Floor(px)), int32(math.Floor(py)), int32(math.Floor(pz))
					ox, oy, oz := bx-cx, by-cy, bz-cz
					i := -1
					resistance := math.NaN()
					if ox >= 0 && oy >= 0 && oz >= 0 && ox < side && oy < side && oz < side {
						i = (int(ox)*int(side)+int(oy))*int(side) + int(oz)
						resistance = resistances[i]
					}
					if math.IsNaN(resistance) {
						state := w.BlockAt(bx, by, bz)
						hasResistance := false
						if override != nil {
							resistance, hasResistance = override(bx, by, bz)
						}
						if !hasResistance {
							resistance, hasResistance = BlastResistance(state)
						}
						if !hasResistance {
							resistance = noResistance
						}
						if i >= 0 {
							resistances[i] = resistance
						}
					}
					if resistance != noResistance {
						intensity -= float32((float32(resistance) + 0.3) * 0.3)
					}
					if intensity > 0 {
						if i < 0 {
							outside = append(outside, [3]int32{bx, by, bz})
						} else {
							seen[i>>6] |= 1 << (i & 63)
						}
					}
					// The step and the per-step loss of intensity, the
					// 0.3 of the walk and the 0.22500001 of the f -=.
					px += dx * 0.30000001192092896
					py += dy * 0.30000001192092896
					pz += dz * 0.30000001192092896
					intensity -= 0.22500001
				}
			}
		}
	}
	// Walking the bitset in index order yields the cells sorted by X, Y, Z.
	var destroyed BlastBlocks
	for wi, word := range seen {
		for ; word != 0; word &= word - 1 {
			i := wi<<6 | bits.TrailingZeros64(word)
			oz := int32(i % int(side))
			oy := int32(i / int(side) % int(side))
			ox := int32(i / int(side) / int(side))
			destroyed = append(destroyed, [3]int32{cx + ox, cy + oy, cz + oz})
		}
	}
	if len(outside) > 0 {
		destroyed = append(destroyed, outside...)
		slices.SortFunc(destroyed, func(a, b [3]int32) int {
			return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]), cmp.Compare(a[2], b[2]))
		})
		destroyed = slices.Compact(destroyed)
	}
	return destroyed
}

// smallBlastScratch uses a power-of-two cube covering every radius-four ray
// with nonnegative resistance, including the floor and first-step margin.
type smallBlastScratch struct {
	losses    [32 * 32 * 32]float32
	destroyed [32 * 32 * 32 / 64]uint64
	touched   []uint16
}

var smallBlastPool = sync.Pool{New: func() any {
	s := new(smallBlastScratch)
	for i := range s.losses {
		s.losses[i] = float32(math.NaN())
	}
	return s
}}

func (s *smallBlastScratch) release() {
	for _, i := range s.touched {
		s.losses[i] = float32(math.NaN())
	}
	s.touched = s.touched[:0]
	clear(s.destroyed[:])
	smallBlastPool.Put(s)
}

func (s *smallBlastScratch) loss(w BlockReader, i int, x, y, z int32) float32 {
	resistance, present := BlastResistance(w.BlockAt(x, y, z))
	loss := float32(0)
	if present {
		loss = float32((float32(resistance) + 0.3) * 0.3)
	}
	s.losses[i] = loss
	s.touched = append(s.touched, uint16(i))
	return loss
}

func explosionBlocksSmall(w BlockReader, rng *rand.Rand, x, y, z, radius float64) BlastBlocks {
	s := smallBlastPool.Get().(*smallBlastScratch)
	defer s.release()
	bx, by, bz := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	cx, cy, cz := bx-16, by-16, bz-16
	const origin = ((16<<5)|16)<<5 | 16
	originLoss := float32(math.NaN())
	// Every direction's second step stays in the origin only inside this band.
	const stepSize = 0.30000001192092896
	extraOrigin := math.Floor(x-stepSize) == float64(bx) && math.Floor(x+stepSize) == float64(bx) &&
		math.Floor(y-stepSize) == float64(by) && math.Floor(y+stepSize) == float64(by) &&
		math.Floor(z-stepSize) == float64(bz) && math.Floor(z+stepSize) == float64(bz)
	for _, step := range explosionSteps {
		intensity := float32(radius) * (0.7 + float32(rng.Float32()*0.6))
		if intensity <= 0 {
			continue
		}
		// Read the origin after the first positive roll, never ahead of the RNG.
		if math.IsNaN(float64(originLoss)) {
			originLoss = s.loss(w, origin, bx, by, bz)
		}
		intensity -= originLoss
		if intensity <= 0 {
			continue
		}
		s.destroyed[origin>>6] |= 1 << (origin & 63)
		intensity -= 0.22500001
		if extraOrigin {
			if intensity <= 0 {
				continue
			}
			intensity -= originLoss
			if intensity <= 0 {
				continue
			}
			s.destroyed[origin>>6] |= 1 << (origin & 63)
			intensity -= 0.22500001
		}
		px, py, pz := x+step[0], y+step[1], z+step[2]
		if extraOrigin {
			// Preserve both rounded double additions, even for the common cell.
			px += step[0]
			py += step[1]
			pz += step[2]
		}
		for intensity > 0 {
			bx, by, bz := int32(math.Floor(px)), int32(math.Floor(py)), int32(math.Floor(pz))
			i := int((((bx-cx)<<5)|(by-cy))<<5 | (bz - cz))
			loss := s.losses[i]
			if math.IsNaN(float64(loss)) {
				loss = s.loss(w, i, bx, by, bz)
			}
			intensity -= loss
			if intensity > 0 {
				s.destroyed[i>>6] |= 1 << (i & 63)
			}
			px += step[0]
			py += step[1]
			pz += step[2]
			intensity -= 0.22500001
		}
	}
	var destroyed BlastBlocks
	for wi, word := range s.destroyed {
		for ; word != 0; word &= word - 1 {
			i := wi<<6 | bits.TrailingZeros64(word)
			destroyed = append(destroyed, [3]int32{cx + int32(i>>10), cy + int32(i>>5&31), cz + int32(i&31)})
		}
	}
	return destroyed
}

// ExplosionExposure is the share of the entity's box that sees the blast
// centre. The box is sampled on a grid of 1/(2·span+1) steps, offset half
// the remainder in X and Z, and each sample's ray to the centre is clipped
// against blocks. The share is a float32 division, the precision damage and
// knockback read. Any collision shape blocks (the registry's full-block
// collision set).
// vanilla: ServerExplosion.getSeenPercent
func ExplosionExposure(w BlockReader, minX, minY, minZ, maxX, maxY, maxZ, cx, cy, cz float64) float64 {
	dx := 1 / ((maxX-minX)*2 + 1)
	dy := 1 / ((maxY-minY)*2 + 1)
	dz := 1 / ((maxZ-minZ)*2 + 1)
	if dx < 0 || dy < 0 || dz < 0 {
		return 0
	}
	// The half-division remainder of the X and Z samplings, the
	// (1 - floor(1/d) * d) / 2 the vanilla walk nudges its samples by.
	offX := (1 - math.Floor(1/dx)*dx) / 2
	offZ := (1 - math.Floor(1/dz)*dz) / 2
	seen, total := 0, 0
	if p, ok := w.(*PrefilledBlocks); ok {
		for f := 0.0; f <= 1; f += dx {
			for g := 0.0; g <= 1; g += dy {
				for h := 0.0; h <= 1; h += dz {
					sx := lerp(f, minX, maxX) + offX
					sy := lerp(g, minY, maxY)
					sz := lerp(h, minZ, maxZ) + offZ
					if !sightBlockedPrefilled(p, sx, sy, sz, cx, cy, cz) {
						seen++
					}
					total++
				}
			}
		}
		return float64(float32(seen) / float32(total))
	}
	for f := 0.0; f <= 1; f += dx {
		for g := 0.0; g <= 1; g += dy {
			for h := 0.0; h <= 1; h += dz {
				sx := lerp(f, minX, maxX) + offX
				sy := lerp(g, minY, maxY)
				sz := lerp(h, minZ, maxZ) + offZ
				if !sightBlocked(w, sx, sy, sz, cx, cy, cz) {
					seen++
				}
				total++
			}
		}
	}
	return float64(float32(seen) / float32(total))
}

// lerp is Mth.lerp: the share f of the way from the low to the high.
func lerp(f, low, high float64) float64 {
	return low + f*(high-low)
}

// sightBlocked walks the segment cell by cell (3D voxel traversal) and
// reports whether any cell, start and end included, holds a collision
// shape. Exact for full blocks; a shape reaching half the cell (a slab)
// blocks its whole cell, and a lower plate (carpet, lily pad) lets the ray through.
func sightBlocked(w BlockReader, x0, y0, z0, x1, y1, z1 float64) bool {
	cx, cy, cz := int32(math.Floor(x0)), int32(math.Floor(y0)), int32(math.Floor(z0))
	endX, endY, endZ := int32(math.Floor(x1)), int32(math.Floor(y1)), int32(math.Floor(z1))
	dx, dy, dz := x1-x0, y1-y0, z1-z0
	stepX, stepY, stepZ := sign(dx), sign(dy), sign(dz)
	tMaxX, tDeltaX := raySpan(x0, dx)
	tMaxY, tDeltaY := raySpan(y0, dy)
	tMaxZ, tDeltaZ := raySpan(z0, dz)
	for {
		if sightSolid(w.BlockAt(cx, cy, cz)) {
			return true
		}
		if cx == endX && cy == endY && cz == endZ {
			return false
		}
		switch {
		case tMaxX <= tMaxY && tMaxX <= tMaxZ:
			if tMaxX > 1 {
				return false
			}
			cx += stepX
			tMaxX += tDeltaX
		case tMaxY <= tMaxZ:
			if tMaxY > 1 {
				return false
			}
			cy += stepY
			tMaxY += tDeltaY
		default:
			if tMaxZ > 1 {
				return false
			}
			cz += stepZ
			tMaxZ += tDeltaZ
		}
	}
}

// sightBlockedPrefilled is sightBlocked specialized for PrefilledBlocks to
// avoid interface dispatch and per-step registry lookups in the inner loop.
func sightBlockedPrefilled(p *PrefilledBlocks, x0, y0, z0, x1, y1, z1 float64) bool {
	cx, cy, cz := int32(math.Floor(x0)), int32(math.Floor(y0)), int32(math.Floor(z0))
	endX, endY, endZ := int32(math.Floor(x1)), int32(math.Floor(y1)), int32(math.Floor(z1))
	dx, dy, dz := x1-x0, y1-y0, z1-z0
	stepX, stepY, stepZ := sign(dx), sign(dy), sign(dz)
	tMaxX, tDeltaX := raySpan(x0, dx)
	tMaxY, tDeltaY := raySpan(y0, dy)
	tMaxZ, tDeltaZ := raySpan(z0, dz)
	for {
		if p.solidAt(cx, cy, cz) {
			return true
		}
		if cx == endX && cy == endY && cz == endZ {
			return false
		}
		switch {
		case tMaxX <= tMaxY && tMaxX <= tMaxZ:
			if tMaxX > 1 {
				return false
			}
			cx += stepX
			tMaxX += tDeltaX
		case tMaxY <= tMaxZ:
			if tMaxY > 1 {
				return false
			}
			cy += stepY
			tMaxY += tDeltaY
		default:
			if tMaxZ > 1 {
				return false
			}
			cz += stepZ
			tMaxZ += tDeltaZ
		}
	}
}

// sign is the ray walk's step direction: toward the negative for a
// negative component, toward the positive otherwise.
func sign(v float64) int32 {
	if v < 0 {
		return -1
	}
	return 1
}

// raySpan returns, for one axis, the unit-segment t of the first cell-wall
// crossing and the spacing of later ones; an axis without movement never
// crosses.
func raySpan(from, delta float64) (tMax, tDelta float64) {
	if delta == 0 {
		return math.Inf(1), math.Inf(1)
	}
	if delta > 0 {
		first := (math.Floor(from) + 1 - from) / delta
		return first, 1 / delta
	}
	first := (from - math.Floor(from)) / -delta
	return first, 1 / -delta
}

// ExplosionDamage is ((e² + e) / 2) * 7 * radius * 2 + 1 for e the falloff
// (1 - dist/(radius*2)) times exposure. The caller measured dist; vanilla's
// hurtEntities skips an entity past the falloff's edge.
// vanilla: ExplosionDamageCalculator.getEntityDamageAmount
func ExplosionDamage(dist, exposure, radius float64) float64 {
	falloff := 1 - dist/(radius*2)
	seen := falloff * exposure
	return ((seen*seen+seen)/2)*7*(radius*2) + 1
}

// ExplosionInBlastRange reports whether an entity whose centre stands dist
// from the blast is one the blast reaches at all: vanilla's hurtEntities
// skips everything past the double radius before any exposure is asked.
func ExplosionInBlastRange(dist, radius float64) bool {
	return dist <= radius*2
}

// ExplosionKnockback is the blast-to-eye direction scaled by the damage's
// falloff-and-exposure share and the calculator's knockback multiplier
// (EntityBasedExplosionDamageCalculator's, for a source-entity blast): the
// vector Entity.push adds and a player's Explosion packet carries.
func ExplosionKnockback(ex, ey, ez, cx, cy, cz, dist, exposure, radius float64) (vx, vy, vz float64) {
	dx, dy, dz := ex-cx, ey-cy, ez-cz
	length := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if length == 0 {
		return 0, 0, 0
	}
	strength := (1 - dist/(radius*2)) * exposure
	return dx / length * strength, dy / length * strength, dz / length * strength
}

// ExplosionAfterDifficulty scales an explosion's raw damage before armour.
// minecraft:explosion's scaling is "always" (damage_type/explosion.json), so
// Player.actuallyHurt scales it whatever caused it: peaceful 0, easy
// min(damage/2 + 1, damage), normal unchanged, hard 1.5 times.
// MobAttackAfterDifficulty is the same shape for its own case.
func ExplosionAfterDifficulty(damage float64, difficulty uint8) float64 {
	switch difficulty {
	case DifficultyPeaceful:
		return 0
	case DifficultyEasy:
		return math.Min(damage/2+1, damage)
	case DifficultyHard:
		return damage * 3 / 2
	}
	return damage
}

// ExplosionSmall reports a radius under 2, which picks the packet's small
// burst over the emitter. Drops are unaffected: every destroyed block rolls
// its loot whatever the radius.
// vanilla: ServerExplosion.isSmall
func ExplosionSmall(radius float64) bool {
	return radius < 2
}

// TNTShortFuse is the fuse of TNT primed by a blast: nextInt(max(1, fuse/4))
// plus fuse/8, 10 to 29 ticks for the default 80, so a chain runs at its own
// pace. fuse is the DEFAULT_FUSE_TIME the caller passes.
// vanilla: PrimedTnt.getRandomShortFuse
func TNTShortFuse(rng *rand.Rand, fuse int) int {
	return rng.Intn(max(1, fuse/4)) + fuse/8
}

// ExplosionFireCells are a fiery blast's burning cells: each destroyed cell
// rolls one in three, in order, and burns if it is air over a solid-render
// block, read after the blast's blocks are gone.
// vanilla: ServerExplosion.createFire
func ExplosionFireCells(w FireWorld, rng *rand.Rand, cells [][3]int32) [][3]int32 {
	var lit [][3]int32
	for _, c := range cells {
		if rng.Intn(3) != 0 || w.BlockAt(c[0], c[1], c[2]) != 0 ||
			!registry.IsSolidRender(w.BlockAt(c[0], c[1]-1, c[2])) {
			continue
		}
		lit = append(lit, c)
	}
	return lit
}

// ExplosionInteraction is what a blast does to the blocks its rays reach:
// keep removes none, the ray-cast's count still riding the packet; destroy
// drops their loot whole; decay rolls the loot tables' 1/radius survival;
// trigger removes none, toggling the doors, levers and buttons the rays
// reach instead. vanilla: Explosion.BlockInteraction
type ExplosionInteraction uint8

const (
	ExplosionKeep ExplosionInteraction = iota
	ExplosionDestroy
	ExplosionDecay
	ExplosionTrigger
)

// MobExplosionInteraction is a mob's (a creeper's) blast's: mob_griefing
// vetoes it, mob_explosion_drop_decay picks decay. Callers pass the
// overworld, which holds the server's one rule set.
// vanilla: ServerLevel.explode
func MobExplosionInteraction(rulesOf *world.World) ExplosionInteraction {
	if !rulesOf.BoolRule(world.RuleMobGriefing) {
		return ExplosionKeep
	}
	return decayIf(rulesOf.BoolRule(world.RuleMobExplosionDropDecay))
}

// TNTExplosionInteraction is a primed TNT's blast's, by
// tnt_explosion_drop_decay.
func TNTExplosionInteraction(rulesOf *world.World) ExplosionInteraction {
	return decayIf(rulesOf.BoolRule(world.RuleTNTExplosionDropDecay))
}

// BlockExplosionInteraction is a block's blast's (a bed or an anchor that
// cannot work in its dimension), by block_explosion_drop_decay.
func BlockExplosionInteraction(rulesOf *world.World) ExplosionInteraction {
	return decayIf(rulesOf.BoolRule(world.RuleBlockExplosionDropDecay))
}

func decayIf(decay bool) ExplosionInteraction {
	if decay {
		return ExplosionDecay
	}
	return ExplosionDestroy
}

// RemovesBlocks reports whether the blast takes the blocks it reaches,
// which is also whether it takes down the blocklike bodies: paintings,
// frames, knots and armor stands.
// vanilla: BlockInteraction.shouldAffectBlocklikeEntities
func (i ExplosionInteraction) RemovesBlocks() bool {
	return i == ExplosionDestroy || i == ExplosionDecay
}

// SparesHanging reports whether the blast leaves a painting, frame or knot
// it reaches: one that removes no blocks does, and so does any blast whose
// carrier (the primed TNT, the creeper) stood in water.
// vanilla: BlockAttachedEntity.ignoreExplosion
func (i ExplosionInteraction) SparesHanging(carrierInWater bool) bool {
	return !i.RemovesBlocks() || carrierInWater
}

// LootRadius is the EXPLOSION_RADIUS of a destroyed block's loot context:
// the blast radius under decay, else 0 (absent).
// vanilla: BlockBehaviour.onExplosionHit
func (i ExplosionInteraction) LootRadius(radius float64) float64 {
	if i == ExplosionDecay {
		return radius
	}
	return 0
}

// ExplodedBlock is what a block a blast removed leaves behind.
type ExplodedBlock uint8

const (
	// ExplodedDrops rolls the block's loot table.
	ExplodedDrops ExplodedBlock = iota
	// ExplodedInfested releases the block's silverfish and rolls its loot:
	// a blast has no tool, so no silk touch keeps them in.
	// vanilla: InfestedBlock.spawnAfterBreak
	ExplodedInfested
	// ExplodedPrimed is a TNT block primed on a short fuse (TNTShortFuse),
	// dropping nothing. vanilla: TntBlock.wasExploded
	ExplodedPrimed
	// ExplodedNothing is a TNT block tnt_explodes keeps from priming.
	ExplodedNothing
)

// ExplodedBlockOf is what the block leaves when a blast in w removes
// it. vanilla: BlockBehaviour.onExplosionHit
func ExplodedBlockOf(b registry.Block, w *world.World) ExplodedBlock {
	switch {
	case b == registry.BlockTnt && w.TNTExplodes():
		return ExplodedPrimed
	case b == registry.BlockTnt:
		return ExplodedNothing
	case IsInfestedBlock(b):
		return ExplodedInfested
	}
	return ExplodedDrops
}

// ExplosionPushesPlayer reports whether a blast's knockback reaches a
// player of the game mode: a spectator and a flying creative player are
// left out of the hit-players map the packet's push comes from.
// vanilla: ServerExplosion.hurtEntities
func ExplosionPushesPlayer(gameMode int32, flying bool) bool {
	return gameMode != protocol.GameModeSpectator &&
		(gameMode != protocol.GameModeCreative || !flying)
}

// Blast is one explosion as its pass over the bodies measures it.
type Blast struct {
	X, Y, Z, Radius float64
	// KnockbackScale multiplies every push, 0 meaning 1.
	// vanilla: SimpleExplosionDamageCalculator
	KnockbackScale float64
}

// BlastHit is a body's share of a blast: the damage before any difficulty
// scaling, and the push.
type BlastHit struct {
	Damage     float64
	KX, KY, KZ float64
}

// Reach is the distance from the centre to (x, y, z), the point a kind's
// body is measured at, and whether a body there is in reach.
// vanilla: ServerExplosion.hurtEntities
func (b Blast) Reach(x, y, z float64) (dist float64, ok bool) {
	dx, dy, dz := x-b.X, y-b.Y, z-b.Z
	dist = math.Sqrt(dx*dx + dy*dy + dz*dz)
	return dist, ExplosionInBlastRange(dist, b.Radius)
}

// Hit measures a body Reach found dist from the centre: box is the box its
// exposure samples, aim the point its push aims along, the eyes (a primed
// TNT's and a falling block's feet).
func (b Blast) Hit(w BlockReader, dist float64, box func() (minX, minY, minZ, maxX, maxY, maxZ float64), aim func() (x, y, z float64)) BlastHit {
	minX, minY, minZ, maxX, maxY, maxZ := box()
	exposure := ExplosionExposure(w, minX, minY, minZ, maxX, maxY, maxZ, b.X, b.Y, b.Z)
	ax, ay, az := aim()
	kx, ky, kz := ExplosionKnockback(ax, ay, az, b.X, b.Y, b.Z, dist, exposure, b.Radius)
	if b.KnockbackScale != 0 {
		kx, ky, kz = kx*b.KnockbackScale, ky*b.KnockbackScale, kz*b.KnockbackScale
	}
	return BlastHit{Damage: ExplosionDamage(dist, exposure, b.Radius), KX: kx, KY: ky, KZ: kz}
}

// Resisted is the hit with its push cut by an explosion knockback
// resistance (ExplosionKnockbackResistanceAttribute), 0 to 1.
func (h BlastHit) Resisted(resist float64) BlastHit {
	h.KX, h.KY, h.KZ = h.KX*(1-resist), h.KY*(1-resist), h.KZ*(1-resist)
	return h
}
