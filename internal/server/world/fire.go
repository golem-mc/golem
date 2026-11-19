package world

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The fire half of the physics engine: FireBlock.tick on the scheduled-tick
// queue, one visit per fire, with the dice rolled in vanilla's order so a
// seeded run replays vanilla's distribution. A fire ages, is doused by rain,
// burns its flammable neighbours (priming TNT instead) and spreads into empty
// cells, at odds from the generated flammability table.
//
// State calculations are shared with placement through blocklogic; scheduled
// visits, writes and random draw order stay in the engine.
//
// gameplay/increased_fire_burnout (halved spread odds, faster burn-out) is read
// off the fire's biome from the table below. The
// fire_spread_radius_around_player rule gating every visit is answered by the
// world's fire spreader once the server sets it.

// The vanilla FireBlock's numbers, read off its bytecode.
const (
	// fireDelayBase and fireDelayDice are getFireTickDelay's 30 +
	// random.nextInt(10).
	fireDelayBase = 30
	fireDelayDice = 10

	// fireMaxAge is AGE's top, past which a fire starts looking for a reason to
	// go out.
	fireMaxAge = 15

	// fireLingerAge is the age below which a fire on a sturdy floor with no
	// fuel lingers instead of going out.
	fireLingerAge = 3

	// fireDouseBase and fireDousePerAge are rain's per-visit douse chance: 0.2
	// plus 0.03 per age.
	fireDouseBase   = 0.2
	fireDousePerAge = 0.03

	// fireBurnSideChance and fireBurnCapChance are the neighbours' burn-out
	// dice: 300 for the horizontal four, 250 above and below.
	fireBurnSideChance = 300
	fireBurnCapChance  = 250

	// fireSpreadOddsBase and fireSpreadPerDifficulty: spread odds are the
	// cell's ignite odds plus 40 plus 7 per difficulty step, over 30 plus the
	// age.
	fireSpreadOddsBase      = 40
	fireSpreadPerDifficulty = 7

	// fireSpreadRate is a spread candidate's die: 100 at the fire's level and
	// the two above, 100 more per level past that.
	fireSpreadRate = 100

	// fireOutDice is the one-in-four die a top-aged fire rolls to go out while
	// nothing flammable is under it.
	fireOutDice = 4
)

// increasedFireBurnoutBiomes are the biomes with
// gameplay/increased_fire_burnout true; no dimension sets it.
// vanilla: OverworldBiomes (jungle, bambooJungle, mushroomFields, swamp,
// mangroveSwamp, basePeaks, snowySlopes)
var increasedFireBurnoutBiomes = [registry.BiomeCount]bool{
	registry.BiomeJungle:         true,
	registry.BiomeBambooJungle:   true,
	registry.BiomeMushroomFields: true,
	registry.BiomeSwamp:          true,
	registry.BiomeMangroveSwamp:  true,
	registry.BiomeFrozenPeaks:    true,
	registry.BiomeJaggedPeaks:    true,
	registry.BiomeSnowySlopes:    true,
}

// fireBurnoutHastenedBy is how much the attribute lowers both burn-out dice.
const fireBurnoutHastenedBy = 50

// fireAgeOf reads a fire state's AGE, zero if it has none.
func fireAgeOf(state uint16) int {
	age, _ := registry.StateInt(state, registry.PropAge)
	return age
}

// TNTPrime is a TNT block a fire's burn-out primed: checkBurnOut removes it and
// TntBlock.prime spawns a body with the default fuse. The server spawns it
// where the block stood and plays the priming sound.
type TNTPrime struct {
	X, Y, Z int32
}

// fireTick is one fire visit in FireBlock.tick's dice order: book the next
// visit first (the fire keeps ticking whatever it decides), then survival,
// rain, aging, the two fuel-loss endings, the six neighbours' burn-out and the
// spread.
//
// Deliberately unlike vanilla, it stops after removing a fire that cannot
// survive; vanilla goes on to age and rewrite the state, which the same tick's
// fuel check then removes again, so the fire is gone either way.
func (p *Physics) fireTick(x, y, z int32, state uint16, changes *[]BlockChange, primes *[]TNTPrime) {
	// The next visit is due 30-39 ticks out regardless.
	p.schedule(x, y, z, p.now+int64(fireDelayBase+p.rng.Intn(fireDelayDice)))

	// The fire_spread_radius_around_player gate sits where vanilla's does, with
	// only the scheduling before it: with no player in range, the fire sits
	// unaged, unburnt and unspread.
	if p.spreader != nil && !p.spreader.FireCanSpread(x, y, z) {
		return
	}

	age := fireAgeOf(state)
	if !p.fireCanSurvive(x, y, z) {
		p.setShaped(x, y, z, airState, changes)
		return
	}

	// Rain douses a fire where the sky reaches it or a horizontal neighbour,
	// more readily with age; never on an infiniburn block.
	infiniburn := p.burnsForever(p.w.BlockAt(x, y-1, z))
	if !infiniburn && p.fireNearRain(x, y, z) &&
		p.rng.Float64() < fireDouseBase+fireDousePerAge*float64(age) {
		p.fireGoesOut(x, y, z, changes)
		return
	}

	// The aging write is flag 260, so the shape pass runs and an observer hears
	// the age change.
	// vanilla: FireBlock.tick
	newAge := min(age+p.rng.Intn(3)/2, fireMaxAge)
	if newAge != age {
		p.writeBlockShaped(x, y, z, fireStateWithAge(state, newAge), changes)
	}

	if !infiniburn {
		// A fire with no fuel past the linger age, or with nothing below, goes
		// at once and the visit ends; a young one on a sturdy floor stays a
		// while.
		if !blocklogic.FireHasFuel(p.w, x, y, z) {
			if !blocklogic.FireSupported(p.w.BlockAt(x, y-1, z), y, p.w.MinY()) || age > fireLingerAge {
				p.fireGoesOut(x, y, z, changes)
			}
			return
		}
		// An aged-out fire over nothing flammable rolls one in four to go out.
		if age == fireMaxAge && !blocklogic.FireCanBurn(p.w.BlockAt(x, y-1, z)) &&
			p.rng.Intn(fireOutDice) == 0 {
			p.fireGoesOut(x, y, z, changes)
			return
		}
	}

	// Burn-out: each neighbour rolls against its burn odds (out of 300
	// sideways, 250 above and below). A loser is consumed, replaced by fire
	// more often the younger the fire, otherwise air; TNT primes either way.
	// vanilla: FireBlock.tick (the attribute is read at the fire's position)
	b := p.w.NoiseBiome(x, y, z)
	increased := b >= 0 && b < registry.BiomeCount && increasedFireBurnoutBiomes[b]
	side, capChance := fireBurnSideChance, fireBurnCapChance
	if increased {
		side -= fireBurnoutHastenedBy
		capChance -= fireBurnoutHastenedBy
	}
	src := [3]int32{x, y, z}
	p.burnOut(x+1, y, z, side, age, src, changes, primes)
	p.burnOut(x-1, y, z, side, age, src, changes, primes)
	p.burnOut(x, y-1, z, capChance, age, src, changes, primes)
	p.burnOut(x, y+1, z, capChance, age, src, changes, primes)
	p.burnOut(x, y, z-1, side, age, src, changes, primes)
	p.burnOut(x, y, z+1, side, age, src, changes, primes)

	// Spread: every empty cell within 1 sideways, 4 up and 1 down (excluding
	// the fire's own) is a candidate, with the highest ignite odds of its six
	// neighbours. Odds fall with age and rise with difficulty; the die is 100
	// at the fire's level and 100 more per level past two; a rained-on
	// candidate is never taken. A spread takes the state the cell makes, a
	// little older than its source.
	difficulty := int(p.w.Difficulty())
	for xx := int32(-1); xx <= 1; xx++ {
		for zz := int32(-1); zz <= 1; zz++ {
			for yy := int32(-1); yy <= 4; yy++ {
				if xx == 0 && yy == 0 && zz == 0 {
					continue
				}
				tx, ty, tz := x+xx, y+yy, z+zz
				rate := fireSpreadRate
				if yy > 1 {
					rate += int(yy-1) * fireSpreadRate
				}
				igniteOdds := p.spreadIgniteOdds(tx, ty, tz)
				if igniteOdds <= 0 {
					continue
				}
				odds := (igniteOdds + fireSpreadOddsBase + difficulty*fireSpreadPerDifficulty) / (age + fireDelayBase)
				if increased {
					odds /= 2
				}
				if odds <= 0 || p.rng.Intn(rate) > odds {
					continue
				}
				if p.fireNearRain(tx, ty, tz) {
					continue
				}
				spreadAge := min(age+p.rng.Intn(5)/4, fireMaxAge)
				p.igniteTo(tx, ty, tz, p.fireStateFor(tx, ty, tz, spreadAge), HookCauseSpread, [3]int32{x, y, z}, changes)
			}
		}
	}
}

// burnOut is one neighbour's burn-out roll: on a loss the cell becomes a
// younger fire where no rain falls and the dice allow, else air, and TNT primes
// either way.
func (p *Physics) burnOut(x, y, z int32, chance, age int, from [3]int32, changes *[]BlockChange, primes *[]TNTPrime) {
	if p.rng.Intn(chance) >= registry.FireBurnOdds(p.w.BlockAt(x, y, z)) {
		return
	}
	// Decide before writing, in vanilla's order: the survive die, then the
	// rain, then the new age.
	keeps := p.rng.Intn(age+10) < 5 && !p.w.RainingAt(x, y, z)
	burned := p.w.BlockAt(x, y, z)
	if p.listens(HookBurn) {
		d := HookDecision{Kind: HookBurn, X: x, Y: y, Z: z, Old: burned, From: from}
		if p.ask(&d) {
			return
		}
	}
	if p.w.BlockEntity(x, y, z) != nil {
		// vanilla: LevelChunk.setBlockState (preRemoveSideEffects)
		p.effects = append(p.effects, Effect{RemovedEntity: burned, X: x, Y: y, Z: z})
	}
	if keeps {
		newAge := min(age+p.rng.Intn(5)/4, fireMaxAge)
		p.igniteTo(x, y, z, p.fireStateFor(x, y, z, newAge), HookCauseSpread, from, changes)
	} else {
		p.setShaped(x, y, z, airState, changes)
	}
	if registry.BlockOf(burned) == registry.BlockTnt {
		*primes = append(*primes, TNTPrime{X: x, Y: y, Z: z})
	}
}

// spreadIgniteOdds is a candidate's ignite odds: zero unless the cell is air
// (air, cave air or void air), else the highest of its six neighbours'
// (getIgniteOdds).
func (p *Physics) spreadIgniteOdds(x, y, z int32) int {
	if state := p.w.BlockAt(x, y, z); !IsKnownAirState(state) {
		return 0
	}
	odds := 0
	for _, d := range [6][3]int32{
		{0, -1, 0}, {0, 1, 0}, {-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1},
	} {
		if n := registry.FireIgniteOdds(p.w.BlockAt(x+d[0], y+d[1], z+d[2])); n > odds {
			odds = n
		}
	}
	return odds
}

// fireCanSurvive applies the shared rule with the physics floor bounds.
func (p *Physics) fireCanSurvive(x, y, z int32) bool {
	return blocklogic.FireSurvivesAt(p.w, x, y, z, p.w.VoidY(), p.w.MinY())
}

// fireNearRain reports rain on the position or a horizontal neighbour
// (isNearRain).
func (p *Physics) fireNearRain(x, y, z int32) bool {
	return p.w.RainingAt(x, y, z) || p.w.RainingAt(x-1, y, z) || p.w.RainingAt(x+1, y, z) ||
		p.w.RainingAt(x, y, z-1) || p.w.RainingAt(x, y, z+1)
}

// burnsForever reports the dimension's infiniburn set, whose fire never ages
// out or rains out.
func (p *Physics) burnsForever(state uint16) bool {
	return registry.BlockOf(state).IsInfiniburn(p.w.DimensionName())
}

// fireStateFor is placement's state with the world's floor bound.
func (p *Physics) fireStateFor(x, y, z int32, age int) uint16 {
	return blocklogic.FireStateAt(p.w, x, y, z, p.w.MinY(), age)
}

// fireStateWithAge sets the age, lean flags kept (the aging's setValue); an age
// the fire does not take gives the default.
func fireStateWithAge(state uint16, age int) uint16 {
	if registry.BlockOf(state) != registry.BlockFire {
		return state
	}
	if aged, ok := registry.WithInt(state, registry.PropAge, age); ok {
		return aged
	}
	return blocklogic.Fire
}

// scheduleFire books a fire's next visit 30-39 ticks out unless one is waiting,
// the visit placement and neighbour updates both promise.
func (p *Physics) scheduleFire(x, y, z int32) {
	p.schedule(x, y, z, p.now+int64(fireDelayBase+p.rng.Intn(fireDelayDice)))
}

// lavaTick is the lava's fire-lighting dice, in vanilla's order so a seeded run
// replays.
// vanilla: LavaFluid.randomTick
func (p *Physics) lavaTick(x, y, z int32, changes *[]BlockChange) {
	// The fire_spread_radius_around_player gate, before the die as in the
	// vanilla tick.
	if p.spreader != nil && !p.spreader.FireCanSpread(x, y, z) {
		return
	}
	if passes := p.rng.Intn(3); passes > 0 {
		tx, ty, tz := x, y, z
		for range passes {
			tx += int32(p.rng.Intn(3) - 1)
			ty++
			tz += int32(p.rng.Intn(3) - 1)
			// Level.isLoaded is false outside the build height too, and the
			// walk returns there.
			if !p.w.ChunkLoadedAt(tx, tz) || p.outsideBuildHeight(ty) {
				return
			}
			if state := p.w.BlockAt(tx, ty, tz); p.airAt(tx, ty, tz) {
				if !p.lavaFlammableNeighbours(tx, ty, tz) {
					continue
				}
				p.igniteTo(tx, ty, tz, p.fireStateFor(tx, ty, tz, 0), HookCauseLava, [3]int32{x, y, z}, changes)
				return
			} else if !registry.BlockOf(state).InTag(registry.TagIDBlocksLavaFireSpread) {
				continue // past a fluid, a fire, anything the walk moves through
			} else {
				return
			}
		}
	} else {
		for range 3 {
			tx, tz := x+int32(p.rng.Intn(3)-1), z+int32(p.rng.Intn(3)-1)
			if !p.w.ChunkLoadedAt(tx, tz) {
				return
			}
			if !p.airAt(tx, y+1, tz) || !p.lavaFlammable(tx, y, tz) {
				continue
			}
			p.igniteTo(tx, y+1, tz, p.fireStateFor(tx, y, tz, 0), HookCauseLava, [3]int32{x, y, z}, changes)
		}
	}
}

// lavaFlammable reports whether lava can light a block (its ignitedByLava
// flag); false outside the build height or an unloaded chunk.
// vanilla: LavaFluid.randomTick
func (p *Physics) lavaFlammable(x, y, z int32) bool {
	if p.outsideBuildHeight(y) {
		return false
	}
	if !p.w.ChunkLoadedAt(x, z) {
		return false
	}
	return registry.IgnitedByLava(p.w.BlockAt(x, y, z))
}

// lavaFlammableNeighbours reports a neighbour lava can light
// (hasFlammableNeighbours).
func (p *Physics) lavaFlammableNeighbours(x, y, z int32) bool {
	for _, d := range [6][3]int32{
		{0, -1, 0}, {0, 1, 0}, {-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1},
	} {
		if p.lavaFlammable(x+d[0], y+d[1], z+d[2]) {
			return true
		}
	}
	return false
}

// SetSeed reseeds the fire's dice so a test's burns replay identically.
func (p *Physics) SetSeed(seed int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rng = rand.New(rand.NewSource(seed))
}

// fireGoesOut writes air over the fire at x, y, z unless a listener vetoes the
// fade.
func (p *Physics) fireGoesOut(x, y, z int32, changes *[]BlockChange) {
	if state, ok := p.veto(HookFade, x, y, z, airState, [3]int32{}); ok {
		p.setShaped(x, y, z, state, changes)
	}
}

// igniteTo writes a fire state a spread or lava lit at x, y, z, asking the
// ignite and spread listeners first. A vetoed ignite leaves the cell as it is
// (air for a spread).
func (p *Physics) igniteTo(x, y, z int32, fire uint16, cause uint8, from [3]int32, changes *[]BlockChange) {
	if p.listens(HookIgnite) {
		d := HookDecision{Kind: HookIgnite, X: x, Y: y, Z: z, Old: p.w.BlockAt(x, y, z), New: fire, From: from, Cause: cause}
		if p.ask(&d) {
			if d.Cancel && cause == HookCauseSpread && !IsKnownAirState(p.w.BlockAt(x, y, z)) {
				p.setShaped(x, y, z, airState, changes) // a burned block is gone either way
			}
			return
		}
	}
	if cause == HookCauseSpread {
		var ok bool
		if fire, ok = p.veto(HookSpread, x, y, z, fire, from); !ok {
			return
		}
	}
	p.setShaped(x, y, z, fire, changes)
}
