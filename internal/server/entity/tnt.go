package entity

import (
	"math"
	"math/rand"
)

// Primed TNT, from the PrimedTnt bytecode: a plain value like items and
// arrows, returning events. X, Y, Z is the box's bottom centre; the box is
// EntityTypes.TNT's sized(0.98F, 0.98F).

// PrimedTnt's numbers: gravity 0.04 (getDefaultGravity), air drag 0.98
// (getAirDrag), ground friction 0.7 per horizontal axis and -0.5 on the
// vertical (a small bounce); the fuse is DEFAULT_FUSE_TIME and the
// constructor's setFuse(80); the blast is explosionPower 4.0F, centred at
// explode's getY(0.0625), that share of the height above the feet.
const (
	tntGravity        = 0.04
	tntAirDrag        = 0.98
	tntGroundDrag     = 0.7
	tntGroundBounce   = -0.5
	TNTDefaultFuse    = 80
	TNTExplosionPower = 4.0
	TNTHalfWidth      = 0.49
	TNTHeight         = 0.98
	tntBlastYShare    = 0.0625
	tntEyeHeight      = 0.15 // vanilla: EntityTypes.TNT eyeHeight(0.15f)
)

// TNTNudgeVelocity is the PrimedTnt constructor's push: a random angle
// (nextDouble() times 6.2831854820251465, the float-widened two pi), 0.02 a
// tick horizontally along it, and 0.2 up.
func TNTNudgeVelocity(rng *rand.Rand) (vx, vy, vz float64) {
	const twoPi = 6.2831854820251465 // (double)(float)(Math.PI * 2)
	angle := rng.Float64() * twoPi
	return -math.Sin(angle) * 0.02, 0.20000000298023224, -math.Cos(angle) * 0.02
}

// TNTEventKind is one thing a primed TNT's tick produced that the server may
// want to tell the world about or act on.
type TNTEventKind uint8

const (
	// TntExploded: the fuse ran out and explode ran; the body is discarded and
	// the server runs the blast at X, Y, Z (the position plus the height share)
	// with Radius the explosionPower.
	TntExploded TNTEventKind = iota
	// TntSteppedOnRedstoneOre: the body rests on the unlit redstone ore at the
	// cell X, Y, Z (whole numbers).
	TntSteppedOnRedstoneOre
)

// TNTEvent is one event out of a primed TNT's tick. The X, Y, Z and the
// Radius ride only a TntExploded.
type TNTEvent struct {
	Kind    TNTEventKind
	X, Y, Z float64
	Radius  float64
}

// TNT is one primed TNT in a world. OwnerID is the constructor's igniter
// (the player who lit it, the mob whose blast set it off), whom the blast's
// damage and death message credit; summoned and unowned chain TNT have
// none. BlockState is the drawn block, TNT for every one this server
// lights. Dead marks one whose fuse ran out, awaiting removal.
type TNT struct {
	ID int32
	Place
	VX, VY, VZ float64
	OnGround   bool
	// bounced marks a landing that sent the body back up; TakeBounce reads it.
	bounced bool
	stuck   stuckFactor
	// fallDistance is the fall banked since the body last stood on the ground.
	fallDistance float64
	// LandedFall is the fall a landing this tick ended, 0 when none.
	LandedFall float64

	Fuse int
	// Power is the blast radius. vanilla: PrimedTnt.explosionPower
	Power      float32
	BlockState uint16
	OwnerID    int32
	Dead       bool
	// vanilla: Entity.isNoGravity
	NoGravity bool
	// Silent mutes the blast's sound. vanilla: Entity.isSilent
	Silent bool

	water, lava fluidTracker
}

// NewTNT places primed TNT with the velocity (a lit one's nudge, zero when
// summoned), fuse, drawn block state and owner.
func NewTNT(id int32, x, y, z, vx, vy, vz float64, fuse int, blockState uint16, ownerID int32) *TNT {
	return &TNT{
		ID: id,
		X:  x, Y: y, Z: z,
		VX: vx, VY: vy, VZ: vz,
		Fuse:       fuse,
		Power:      TNTExplosionPower,
		BlockState: blockState,
		OwnerID:    ownerID,
	}
}

// Step advances the TNT one tick (PrimedTnt.tick): gravity, move, drag,
// ground friction and bounce, then the fuse; the tick it reaches zero
// discards the body and returns the blast event.
func (t *TNT) Step(w World) []TNTEvent {
	if t.Dead {
		return nil
	}
	// No below-world discard: PrimedTnt.tick never calls Entity.baseTick, so
	// a body in the void keeps falling and still explodes on its fuse.

	// Gravity, move, then drag, as the vanilla tick does; the move's
	// on-ground result drives the friction below.
	if !t.NoGravity {
		t.VY -= tntGravity
	}
	fx, fy, fz, ax, az := t.X, t.Y, t.Z, t.VX, t.VZ
	t.OnGround = t.move(w)
	var events []TNTEvent
	if t.OnGround {
		if x, y, z, ok := unlitOreUnder(w, t.X, t.Y, t.Z, TNTHalfWidth); ok {
			events = append(events, TNTEvent{Kind: TntSteppedOnRedstoneOre, X: float64(x), Y: float64(y), Z: float64(z)})
		}
	}
	var spots []bubbleSpot
	walkInside(w, []bodyMove{{fx, fy, fz, t.X, t.Y, t.Z, ax, az, true}}, AABB{-TNTHalfWidth, 0, -TNTHalfWidth, TNTHalfWidth, TNTHeight, TNTHalfWidth},
		func(x, y, z int32, state uint16, _ int, from AABB, dx, dy, dz float64) bool {
			t.VY, _ = projectileBubblePush(w, x, y, z, state, from, dx, dy, dz, t.VY, true, &spots)
			return true
		})
	t.VX *= tntAirDrag
	t.VY *= tntAirDrag
	t.VZ *= tntAirDrag
	if t.OnGround {
		t.VX *= tntGroundDrag
		t.VY *= tntGroundBounce
		t.VZ *= tntGroundDrag
	}

	t.Fuse--
	if t.Fuse > 0 {
		t.pushByFluids(w)
	}
	if t.Fuse <= 0 {
		t.Dead = true
		return append(events, TNTEvent{
			Kind:   TntExploded,
			X:      t.X,
			Y:      t.Y + TNTHeight*tntBlastYShare,
			Z:      t.Z,
			Radius: float64(t.Power),
		})
	}
	return events
}

// BlastOrigin is where a blast measures and pushes this body from: its
// feet, the one body hurtEntities uses the position for instead of the
// eyes.
func (t *TNT) BlastOrigin() (x, y, z float64) {
	return t.X, t.Y, t.Z
}

// CapFall is Entity.checkFallDistanceAccumulation.
func (t *TNT) CapFall() { CapFallAccumulation(t.VY, &t.fallDistance) }

// BlastBox is the bounding box the exposure of a blast is measured over, the
// shape ServerExplosion.getSeenPercent samples.
func (t *TNT) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return t.X - TNTHalfWidth, t.Y, t.Z - TNTHalfWidth,
		t.X + TNTHalfWidth, t.Y + TNTHeight, t.Z + TNTHalfWidth
}

// Push is Entity.push, the velocity an explosion's knockback adds onto; the
// body's own motion check hears of it the tick after the blast.
func (t *TNT) Push(vx, vy, vz float64) {
	t.VX += vx
	t.VY += vy
	t.VZ += vz
}

// move moves the body along its velocity as far as block shapes allow,
// vertical first, zeroing stopped axes. It reports whether it ended on the
// ground, for the friction.
func (t *TNT) move(w World) bool {
	oldY, landingVY := t.Y, t.VY
	// A consumed stuck multiplier zeroes the velocity before the collision,
	// so nothing is left to restitute.
	// vanilla: Entity.move
	spent := t.stuck
	if _, _, _, slowed := spent.scale(0, 0, 0); slowed {
		landingVY = 0
	}
	x, y, z := t.X, t.Y, t.Z
	onGround := moveBox(w, fallCtx(t.fallDistance), &x, &y, &z, &t.VX, &t.VY, &t.VZ, &t.stuck, TNTHalfWidth, TNTHeight)
	t.SetPos(x, y, z)
	t.LandedFall = bankFall(&t.fallDistance, oldY, t.Y, onGround)
	if onGround && landingVY < 0 {
		// moveBox zeroed the velocity; restitute it instead.
		// vanilla: Entity.restituteMovementAfterCollisions
		g := 0.0
		if !t.NoGravity {
			g = tntGravity
		}
		t.VY = bounceOffGround(w, t.X, t.Y, t.Z, TNTHalfWidth, landingVY, t.Y-oldY, g, airDragFloat, false)
		if t.VY > 0 {
			t.bounced = true
		}
	}
	return onGround
}

// pushByFluids adds the push a flowing fluid gives the body onto its
// velocity, run only on a tick the fuse survives.
// vanilla: PrimedTnt.tick, Entity.updateFluidInteraction
func (t *TNT) pushByFluids(w World) {
	box := AABB{
		minX: t.X - TNTHalfWidth, minY: t.Y, minZ: t.Z - TNTHalfWidth,
		maxX: t.X + TNTHalfWidth, maxY: t.Y + TNTHeight, maxZ: t.Z + TNTHalfWidth,
	}
	updateFluid(w, box, t.X, t.Y+tntEyeHeight, t.Z, true, &t.water, &t.lava)
	if t.water.height > 0 {
		if dx, dy, dz, ok := t.water.currentImpulse(waterCurrent, t.VX, t.VZ); ok {
			t.VX += dx
			t.VY += dy
			t.VZ += dz
		}
	}
	if t.lava.height > 0 {
		scale := lavaCurrent
		if w.FastLava() {
			scale = fastLavaCurrent
		}
		if dx, dy, dz, ok := t.lava.currentImpulse(scale, t.VX, t.VZ); ok {
			t.VX += dx
			t.VY += dy
			t.VZ += dz
		}
	}
}

// FallDistance is the fall banked since the body last stood on the ground.
func (b *TNT) FallDistance() float64 { return b.fallDistance }

// SetFallDistance replaces the banked fall.
func (b *TNT) SetFallDistance(d float64) { b.fallDistance = d }

// TakeBounce reports and clears a bounce since the last call.
// vanilla: Entity.restituteMovementAfterCollisions
func (t *TNT) TakeBounce() bool {
	n := t.bounced
	t.bounced = false
	return n
}
