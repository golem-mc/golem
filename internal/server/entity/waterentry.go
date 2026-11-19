package entity

import (
	"math"
	"math/rand"
)

// WaterSplash is a body's water entry after its first tick.
// vanilla: Entity.doWaterSplashEffect
type WaterSplash struct {
	X, Y, Z       float64
	Volume, Pitch float32
}

// waterEntry keeps the unsaved fluid edge and firstTick flag of a body.
type waterEntry struct {
	inWater, ticked bool
	rng             *rand.Rand
}

// step checks the box before movement for bodies whose baseTick runs first.
// vanilla: Entity.updateFluidInteraction
func (e *waterEntry) step(w World, box AABB, vx, vy, vz float64) *WaterSplash {
	x, y, z := (box.minX+box.maxX)/2, box.minY, (box.minZ+box.maxZ)/2
	was := e.inWater
	e.inWater, _, _ = BoxInWater(w, x, y, z, box.maxX-box.minX, box.maxY-box.minY, y)
	splash := e.inWater && !was && e.ticked
	e.ticked = true
	if !splash {
		return nil
	}
	if e.rng == nil {
		e.rng = rand.New(rand.NewSource(rand.Int63()))
	}
	volume, pitch := splashSound(vx, vy, vz, e.rng)
	return &WaterSplash{X: x, Y: y, Z: z, Volume: volume, Pitch: pitch}
}

// Ridden rescales the splash to the controlling passenger's velocity, whose
// movement vanilla reads in place of the vehicle's, at 0.9 instead of 0.2.
func (s *WaterSplash) Ridden(vx, vy, vz float64) {
	s.Volume = min(1, float32(math.Sqrt(vx*vx*0.2+vy*vy+vz*vz*0.2))*0.9)
}
