package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/server/world"
)

// The firework rocket in flight, from the FireworkRocketEntity
// bytecode. A plain value like the thrown projectiles and primed TNT. A
// rocket used on a block rises alone; one used while gliding rides its
// user until its life runs out.

// FireworkRocketEntity and FireworkRocketItem numbers: the spawn's 0.05
// rise and sideways triangles of 0.002297 about 0; each tick's further 0.04
// rise with horizontals times 1.15 (not when a wall stopped them); a life of
// 10 ticks per flight count plus up to 11 more (rolls of 6 and 7). The box
// is sized(0.25F, 0.25F), and a rocket used on a block starts 0.15 off the
// face.
const (
	fireworkRise        = 0.04
	fireworkSideAccel   = 1.15
	fireworkSideHold    = 1.0
	fireworkSpawnRise   = 0.05
	fireworkSpawnSpread = 0.002297
	fireworkLifeBase    = 10
	fireworkLifeRollOne = 6
	fireworkLifeRollTwo = 7
	FireworkHalfWidth   = 0.125
	FireworkHeight      = 0.25
	FireworkPlaceOffset = 0.15
)

// FireworkCarrier is the body a boost rocket rides: position, motion and
// the yaw the rocket hangs beside.
type FireworkCarrier struct {
	X, Y, Z    float64
	VX, VY, VZ float64
	Yaw        float32
}

// FireworkEventKind is something a rocket's tick produced for the server.
type FireworkEventKind uint8

const (
	// FireworkBurst: the life ran out or a body was met; the rocket is
	// discarded and the server draws and applies the explosion at its position.
	FireworkBurst FireworkEventKind = iota
	// FireworkHitBlock: the flight met a block face with no body nearer;
	// BlockHit names it. A starred rocket's FireworkBurst follows.
	FireworkHitBlock
	// FireworkSplashed: the rocket entered water at X, Y, Z.
	FireworkSplashed
	// FireworkHitTarget reports a body collision before the burst.
	FireworkHitTarget
	// FireworkDeflected: the flight met a body that turns projectiles back;
	// the rocket flies on reversed, with no burst. Target is the body.
	// vanilla: Projectile.hitTargetOrDeflectSelf
	FireworkDeflected
)

// FireworkEvent is one event from a rocket's tick; X, Y, Z locate a burst,
// splash or body hit.
type FireworkEvent struct {
	Kind     FireworkEventKind
	X, Y, Z  float64
	BlockHit BlockHit
	// Target is the deflecting body of a FireworkDeflected.
	Target ArrowTarget
	// Volume and Pitch are the water-entry sound.
	Volume, Pitch float32
}

// Firework is one rocket in a world. OwnerID is its source (the spawn
// packet's data), 0 for a summoned rocket. AttachedTo is the gliding body a
// boost rocket rides, 0 for a free one. Explosions is the stack's star
// count, which scales the burst's damage; 0 means the burst draws and hurts
// nothing. Dead marks it discarded.
type Firework struct {
	waterEntry

	ID int32
	Place
	VX, VY, VZ float64
	Yaw, Pitch float32

	Life     int
	Lifetime int

	OwnerID    int32
	AttachedTo int32
	Explosions int
	Dead       bool

	// ShotAtAngle marks a crossbow rocket: it keeps its shot velocity, without
	// the rise and sideways acceleration.
	// vanilla: FireworkRocketEntity.isShotAtAngle
	ShotAtAngle bool

	// hitWall is the last free move's horizontal collision, which the next
	// tick's acceleration reads: against a wall the horizontals are kept, not
	// grown.
	// vanilla: FireworkRocketEntity.tick's horizontalCollision
	hitWall bool

	// leftOwner is Projectile.leftOwner: the shooter is skipped until the grown
	// box has left it, and stays hittable after.
	leftOwner bool

	// lastDeflectedBy is the body that last turned this rocket back, which
	// cannot do so twice in a row.
	// vanilla: Projectile.lastDeflectedBy
	lastDeflectedBy int32
}

// NewFirework places a rocket with its owner and a life from the flight
// count and two seeded rolls. flight is the stack's flight duration (the
// constructor counts one more), explosions its star count, attached the
// gliding body a boost rocket rides (0 for free).
// vanilla: FireworkRocketEntity.<init>
func NewFirework(id int32, x, y, z float64, ownerID, flight, explosions, attached int32, seed int64) *Firework {
	rng := NewDice(seed)
	return &Firework{
		rng: rng,
		ID:  id,
		X:   x, Y: y, Z: z,
		// The spawn motion: 0.05 up, two sideways triangles about 0.
		VX:         fireworkSpawnSpread * (rng.Float64() - rng.Float64()),
		VY:         fireworkSpawnRise,
		VZ:         fireworkSpawnSpread * (rng.Float64() - rng.Float64()),
		Lifetime:   FireworkLifetime(flight, rng.Intn),
		OwnerID:    ownerID,
		AttachedTo: attached,
		Explosions: int(explosions),
	}
}

// FireworkLifetime rolls a rocket's life: 10 ticks per flight count plus up
// to 11 more, with intn drawing from [0, n).
// vanilla: FireworkRocketEntity.<init>
func FireworkLifetime(flight int32, intn func(int) int) int {
	return fireworkLifeBase*(int(flight)+1) + intn(fireworkLifeRollOne) + intn(fireworkLifeRollTwo)
}

// LeftOwner reports Projectile.leftOwner, which the save keeps.
func (f *Firework) LeftOwner() bool { return f.leftOwner }

// Restore sets a saved rocket's age, total life, crossbow flag and owner
// exclusion.
func (f *Firework) Restore(life, lifetime int, shotAtAngle, leftOwner bool) {
	f.Life = life
	f.Lifetime = lifetime
	f.ShotAtAngle = shotAtAngle
	f.leftOwner = leftOwner
}

// Step advances the rocket one tick (FireworkRocketEntity.tick) against the
// targets, with the shooter as owner (nil if none) and the carrier for a
// boost rocket (nil if flying alone). A free rocket accelerates and rises,
// clips its flight against blocks and sweeps it for bodies, moves, and
// eases its angles. A boost rocket rides at its carrier's hand with the
// carrier's motion; if the carrier is gone it waits where it was. Meeting a
// body bursts it; a block face only bursts one with stars; the end of its
// life bursts it wherever it is.
// vanilla: FireworkRocketEntity.tick
func (f *Firework) Step(w World, owner *ArrowTarget, carrier *FireworkCarrier, targets []ArrowTarget) []FireworkEvent {
	if f.Dead {
		return nil
	}
	// vanilla: FireworkRocketEntity.tick calls super.tick before moving.
	splash := f.waterEntry.step(w, f.Box(), f.VX, f.VY, f.VZ)
	events := f.step(w, owner, carrier, targets)
	if splash != nil {
		events = append([]FireworkEvent{{Kind: FireworkSplashed, X: splash.X, Y: splash.Y, Z: splash.Z,
			Volume: splash.Volume, Pitch: splash.Pitch}}, events...)
	}
	return events
}

func (f *Firework) step(w World, owner *ArrowTarget, carrier *FireworkCarrier, targets []ArrowTarget) []FireworkEvent {
	// baseTick's below-world discard: 64 below the floor it is gone, without
	// a bang.
	if f.Y < float64(w.MinY())-64 {
		f.Dead = true
		return nil
	}

	// Projectile.checkLeftOwner: once the grown flight box stops touching
	// the shooter or its vehicle group the rocket may hit them, for good. A
	// rocket with no owner is clear at once.
	if !f.leftOwner {
		if owner == nil {
			f.leftOwner = true
		} else {
			box := f.ownerBox()
			f.leftOwner = !ownerGroupTouching(&box, owner, owner.RootVehicle, targets)
		}
	}

	x, y, z, vx, vy, vz := f.flight(carrier)
	if carrier != nil {
		f.SetPos(x, y, z)
	}
	f.VX, f.VY, f.VZ = vx, vy, vz

	if carrier != nil || f.AttachedTo == 0 {
		// The hit ray stops at the first block, while the box moves and slides
		// using the full velocity. A riding rocket only casts the ray.
		clip := clipProjectileFlight(w, f.X, f.Y, f.Z, f.VX, f.VY, f.VZ)
		blockHit := clip.ok
		fromX, fromY, fromZ := f.X, f.Y, f.Z
		toX, toY, toZ := clip.end(f.X, f.Y, f.Z, f.VX, f.VY, f.VZ)
		var hit BlockHit
		if blockHit {
			hit = BlockHit{X: clip.bx, Y: clip.by, Z: clip.bz, Face: clip.face,
				HitX: toX, HitY: toY, HitZ: toZ, State: clip.state, Border: clip.border}
		}
		if f.AttachedTo == 0 {
			f.move(w)
		}
		if hit, ok := sweepFireworkTargets(f, targets, owner, thrownHitMargin(f.Life+1), fromX, fromY, fromZ, toX, toY, toZ); ok {
			if deflectsProjectile(&hit.Target, false) {
				var events []FireworkEvent
				if hit.Target.EntityID != f.lastDeflectedBy {
					f.lastDeflectedBy = hit.Target.EntityID
					if f.rng == nil {
						f.rng = rand.New(rand.NewSource(rand.Int63()))
					}
					reverseDeflection(&f.VX, &f.VY, &f.VZ, &f.Yaw, f.rng, 1)
					events = append(events, FireworkEvent{Kind: FireworkDeflected, Target: hit.Target, X: hit.X, Y: hit.Y, Z: hit.Z})
				}
				return append(events, f.endTick()...)
			}
			f.Dead = true
			return []FireworkEvent{hit, {Kind: FireworkBurst, X: f.X, Y: f.Y, Z: f.Z}}
		}
		if blockHit {
			events := []FireworkEvent{{Kind: FireworkHitBlock, BlockHit: hit}}
			if f.Explosions > 0 {
				f.Dead = true
				events = append(events, FireworkEvent{Kind: FireworkBurst, X: f.X, Y: f.Y, Z: f.Z})
				return events
			}
			// A starless rocket lives on; its life may end this same tick.
			if tail := f.endTick(); tail != nil {
				return append(events, tail...)
			}
			return events
		}
	}

	return f.endTick()
}

// endTick eases the angles, ages the rocket and bursts it when its life ends.
func (f *Firework) endTick() []FireworkEvent {
	// vanilla: updateRotation, a fifth of the way toward the movement's
	// angles.
	f.Yaw = lerpRotation(f.Yaw, arrowTargetYaw(f.VX, f.VZ))
	f.Pitch = lerpRotation(f.Pitch, arrowTargetPitch(f.VY, f.VX, f.VZ))

	f.Life++
	if f.Life > f.Lifetime {
		f.Dead = true
		return []FireworkEvent{{Kind: FireworkBurst, X: f.X, Y: f.Y, Z: f.Z}}
	}
	return nil
}

// move clips the rocket's leading faces so it can leave shapes it spawns
// overlapping. It keeps the velocity for the next tick.
// vanilla: FireworkRocketEntity.tick, Entity.collideWithShapes
func (f *Firework) move(w World) {
	f.hitWall = false
	dx, dy, dz := f.VX, f.VY, f.VZ
	if dx == 0 && dy == 0 && dz == 0 {
		return
	}
	box := f.Box()
	cells := gatherMoveCells(w, box, dx, dy, dz, 0)
	defer cells.release()
	walls := borderWalls(w, box, dx, dz)
	// A gathered sweep with no candidates needs no per-axis clips.
	if cells.scratch != nil && len(cells.cells) == 0 && len(walls) == 0 {
		if _, hasBodies := w.(BodyWorld); !hasBodies {
			f.SetPos(f.X+dx, f.Y+dy, f.Z+dz)
			return
		}
	}
	ctx := collideCtx{moveCells: &cells, borders: true, walls: walls}
	box, _, _ = clipMove(w, ctx, box, axisY, dy)
	box, _, blockedX, blockedZ := collideHorizontal(w, ctx, box, dx, dz)
	x, z := box.centerXZ()
	f.SetPos(x, box.minY, z)
	f.hitWall = blockedX || blockedZ
}

// sweepFireworkTargets reports the nearest entry into a target's box.
// vanilla: ProjectileUtil.getEntityHitResult
func sweepFireworkTargets(f *Firework, targets []ArrowTarget, owner *ArrowTarget, margin, fromX, fromY, fromZ, toX, toY, toZ float64) (FireworkEvent, bool) {
	dx, dy, dz := toX-fromX, toY-fromY, toZ-fromZ
	ownerVehicle := int32(0)
	if owner != nil {
		ownerVehicle = owner.RootVehicle
	}
	bestT := math.Inf(1)
	var best *ArrowTarget
	for i := range targets {
		target := &targets[i]
		if target.Unhittable {
			continue // vanilla: canBeHitByProjectile, the spectator's no
		}
		if ownerExcluded(f.OwnerID, ownerVehicle, f.leftOwner, target) {
			continue
		}
		minX, minY, minZ := target.MinX-margin, target.MinY-margin, target.MinZ-margin
		maxX, maxY, maxZ := target.MaxX+margin, target.MaxY+margin, target.MaxZ+margin
		if pointWithin(fromX, fromY, fromZ, minX, minY, minZ, maxX, maxY, maxZ) {
			continue
		}
		if enter, _, ok := world.SegmentAABB(fromX, fromY, fromZ, dx, dy, dz,
			minX, minY, minZ, maxX, maxY, maxZ); ok && enter > 0 && enter < bestT {
			best, bestT = target, enter
		}
	}
	if best == nil {
		return FireworkEvent{}, false
	}
	return FireworkEvent{Kind: FireworkHitTarget, Target: *best, X: fromX + dx*bestT, Y: fromY + dy*bestT, Z: fromZ + dz*bestT}, true
}

func (f *Firework) flight(carrier *FireworkCarrier) (x, y, z, vx, vy, vz float64) {
	if carrier != nil {
		// vanilla: Entity.getHandHoldingItemAngle
		hx, _, hz := lookVector(carrier.Yaw+80, 0)
		return carrier.X + hx*0.5, carrier.Y, carrier.Z + hz*0.5, carrier.VX, carrier.VY, carrier.VZ
	}
	vx, vy, vz = f.VX, f.VY, f.VZ
	if f.AttachedTo == 0 && !f.ShotAtAngle {
		accel := fireworkSideAccel
		if f.hitWall {
			accel = fireworkSideHold
		}
		vx, vy, vz = vx*accel, vy+fireworkRise, vz*accel
	}
	return f.X, f.Y, f.Z, vx, vy, vz
}

// MoveBox covers the flight and, until clear of the owner, its pre-move exclusion check.
// vanilla: ProjectileUtil.getHitResult, Projectile.isOutsideOwnerCollisionRange
func (f *Firework) MoveBox(carrier *FireworkCarrier) AABB {
	box := fireworkMoveBox(f.flight(carrier))
	if !f.leftOwner && f.OwnerID != 0 && f.OwnerID != f.AttachedTo {
		old := fireworkMoveBox(f.X, f.Y, f.Z, f.VX, f.VY, f.VZ)
		box = AABB{
			min(box.minX, old.minX), min(box.minY, old.minY), min(box.minZ, old.minZ),
			max(box.maxX, old.maxX), max(box.maxY, old.maxY), max(box.maxZ, old.maxZ),
		}
	}
	return box
}

func fireworkMoveBox(x, y, z, vx, vy, vz float64) AABB {
	const margin = 1.0 // vanilla: ProjectileUtil.getHitResult
	const grow = FireworkHalfWidth + margin
	return AABB{
		min(x, x+vx) - grow, min(y, y+vy) - margin, min(z, z+vz) - grow,
		max(x, x+vx) + grow, max(y, y+vy) + FireworkHeight + margin, max(z, z+vz) + grow,
	}
}

func (f *Firework) ownerBox() ArrowTarget {
	b := fireworkMoveBox(f.X, f.Y, f.Z, f.VX, f.VY, f.VZ)
	return ArrowTarget{MinX: b.minX, MinY: b.minY, MinZ: b.minZ, MaxX: b.maxX, MaxY: b.maxY, MaxZ: b.maxZ}
}

// pointWithin reports whether the point is strictly inside the box: a start
// AABB.clip finds no crossing from (only the camera-pick overload counts
// it).
func pointWithin(x, y, z, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	return x > minX && x < maxX && y > minY && y < maxY && z > minZ && z < maxZ
}

// Box is the rocket's quarter-block body about its feet.
func (f *Firework) Box() AABB {
	return AABB{f.X - FireworkHalfWidth, f.Y, f.Z - FireworkHalfWidth,
		f.X + FireworkHalfWidth, f.Y + FireworkHeight, f.Z + FireworkHalfWidth}
}
