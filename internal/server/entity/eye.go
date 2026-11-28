package entity

import (
	"math"
	"math/rand"
)

// The eye of ender's entity type: sized(0.25F, 0.25F),
// clientTrackingRange(4), updateInterval(4).
// vanilla: EntityTypes.EYE_OF_ENDER
const (
	EyeTrackingRange = 4
	EyeInterval      = 4
	EyeHalfWidth     = 0.125
	EyeHeight        = 0.25
	// EyeSpawnHeightFraction is how far up its body a player releases the
	// eye. vanilla: EnderEyeItem.use, Entity.getY(0.5)
	EyeSpawnHeightFraction = 0.5

	eyeLifeTicks    = 80
	eyeFarDistance  = 12.0
	eyeFarRise      = 8.0
	eyeSpeedLerp    = 0.0025
	eyeVerticalEase = 0.015
	eyeSurviveRolls = 5
)

// EyeTargets finds the structure an eye of ender points to.
type EyeTargets interface {
	// NearestEyeTarget returns the lower corner of the nearest structure
	// the eye locates within the given radius in chunks of the block
	// position, and false when there is none.
	// vanilla: ServerLevel.findNearestMapStructure(EYE_OF_ENDER_LOCATED, 100)
	NearestEyeTarget(x, y, z int32, radius int) (tx, ty, tz int32, ok bool)
}

// EyeOfEnder is a thrown eye of ender in flight.
type EyeOfEnder struct {
	waterEntry

	ID int32
	Place
	VX, VY, VZ float64
	Dead       bool

	targetX, targetY, targetZ float64
	hasTarget                 bool
	life                      int
	survive                   bool
}

// NewEyeOfEnder puts an eye at rest at the position given.
func NewEyeOfEnder(id int32, x, y, z float64) *EyeOfEnder {
	return &EyeOfEnder{ID: id, X: x, Y: y, Z: z}
}

// SignalTo aims the eye: a target over 12 blocks away horizontally is
// replaced by the point 12 blocks toward it and 8 above the eye. A 1-in-5
// roll decides whether the eye survives.
// vanilla: EyeOfEnder.signalTo
func (e *EyeOfEnder) SignalTo(tx, ty, tz float64, rng *rand.Rand) {
	dx, dz := tx-e.X, tz-e.Z
	horizontal := math.Hypot(dx, dz)
	if horizontal > eyeFarDistance {
		e.targetX = e.X + dx/horizontal*eyeFarDistance
		e.targetY = e.Y + eyeFarRise
		e.targetZ = e.Z + dz/horizontal*eyeFarDistance
	} else {
		e.targetX, e.targetY, e.targetZ = tx, ty, tz
	}
	e.hasTarget = true
	e.life = 0
	e.survive = rng.Intn(eyeSurviveRolls) > 0
	e.rng = rng
}

// Survives reports whether the eye drops as an item when its flight ends
// rather than shattering.
func (e *EyeOfEnder) Survives() bool { return e.survive }

// Step advances the eye one tick and reports whether its flight has ended;
// the eye is then Dead and the caller drops or shatters it. A splash is
// recorded before movement, where baseTick checks the fluid edge.
// vanilla: EyeOfEnder.tick
func (e *EyeOfEnder) Step(w World) (ended bool, splash *WaterSplash) {
	if e.Dead {
		return false, nil
	}
	splash = e.step(w, e.Box(), e.VX, e.VY, e.VZ)
	nx, ny, nz := e.X+e.VX, e.Y+e.VY, e.Z+e.VZ
	if e.hasTarget {
		e.VX, e.VY, e.VZ = e.steer(nx, ny, nz)
	}
	e.SetPos(nx, ny, nz)
	e.life++
	if e.life > eyeLifeTicks {
		e.Dead = true
		return true, splash
	}
	return false, splash
}

// steer is the velocity for an eye about to stand at the position: the
// horizontal speed eased toward the remaining distance, the vertical toward
// a climb or sink around the target height.
// vanilla: EyeOfEnder.updateDeltaMovement
func (e *EyeOfEnder) steer(px, py, pz float64) (vx, vy, vz float64) {
	hx, hz := e.targetX-px, e.targetZ-pz
	hLen := math.Hypot(hx, hz)
	speed := math.Hypot(e.VX, e.VZ)
	speed += (hLen - speed) * eyeSpeedLerp
	vy = e.VY
	if hLen < 1 {
		speed *= 0.8
		vy *= 0.8
	}
	want := -1.0
	if py-e.VY < e.targetY {
		want = 1.0
	}
	vy += (want - vy) * eyeVerticalEase
	// An eye standing exactly over its target has no heading left; vanilla
	// divides by zero here and loses the eye's position to NaN.
	if hLen == 0 {
		return 0, vy, 0
	}
	return hx * speed / hLen, vy, hz * speed / hLen
}

// Box is the eye's quarter-block body about its feet.
func (e *EyeOfEnder) Box() AABB {
	return AABB{e.X - EyeHalfWidth, e.Y, e.Z - EyeHalfWidth,
		e.X + EyeHalfWidth, e.Y + EyeHeight, e.Z + EyeHalfWidth}
}

// BlastBox is the eye's box for ServerExplosion.getSeenPercent.
func (e *EyeOfEnder) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	b := e.Box()
	return b.Corners()
}

// EyePosition is getEyePosition at the default eye height, 0.85 of the box.
func (e *EyeOfEnder) EyePosition() (x, y, z float64) {
	return e.X, e.Y + float64(float32(EyeHeight)*0.85), e.Z
}

// Push is Entity.push for an explosion's knockback.
func (e *EyeOfEnder) Push(vx, vy, vz float64) {
	e.VX += vx
	e.VY += vy
	e.VZ += vz
}
