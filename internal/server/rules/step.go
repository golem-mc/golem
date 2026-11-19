package rules

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// stepScale turns a movement length into walk distance.
// vanilla: Entity.applyMovementEmissionAndPlaySound
const stepScale = 0.6

// StepTracker accumulates a body's walk distance and says when it has gone far
// enough to take a step. The zero value is a body that has not moved.
// vanilla: Entity.moveDist, Entity.nextStep
type StepTracker struct {
	moveDist float32
	nextStep float32
}

// Stride is one movement's inputs to StepTracker.Advance.
type Stride struct {
	// DX, DY and DZ are the movement made.
	DX, DY, DZ float64
	// Climbing is whether the supporting block is climbable.
	Climbing bool
	// InWater is whether the body is in water.
	InWater bool
	// SupportAir is whether the supporting block is air.
	SupportAir bool
	// OnGround, Crouching and Swimming gate the step.
	OnGround, Crouching, Swimming bool
}

// Advance adds a movement to the walk distance and reports whether a step
// event or a swim event is due. A step needs a supporting block, and the body on the ground,
// on a climbable, or crouched with no vertical movement, and not swimming.
// A body in water that does not step swims instead. The caller skips the
// movements that emit nothing (flying, or sneaking on the ground).
// vanilla: Entity.applyMovementEmissionAndPlaySound, vibrationAndSoundEffectsFromBlock
func (t *StepTracker) Advance(s Stride) (stepped, swam bool) {
	if t.nextStep == 0 {
		t.nextStep = 1
	}
	moved := math.Sqrt(s.DX*s.DX+s.DY*s.DY+s.DZ*s.DZ) * stepScale
	if !s.Climbing {
		moved = math.Hypot(s.DX, s.DZ) * stepScale
	}
	t.moveDist += float32(moved)
	if t.moveDist <= t.nextStep || s.SupportAir {
		return false, false
	}
	if (s.OnGround || s.Climbing || s.Crouching && s.DY == 0) && !s.Swimming {
		t.nextStep = float32(int(t.moveDist) + 1)
		return true, false
	}
	if s.InWater {
		t.nextStep = float32(int(t.moveDist) + 1)
		return false, true
	}
	return false, false
}

// IsClimbableSupport reports whether the block under a body counts as
// climbing for its walk distance: climbable tag or powder snow.
// vanilla: Entity.isStateClimbable
func IsClimbableSupport(state uint16) bool {
	return registry.BlockOf(state).InTag(climbableBlocks) || IsPowderSnow(state)
}

var (
	insideStepSoundBlocks      = registry.BlockTagOf("minecraft:inside_step_sound_blocks")
	combinationStepSoundBlocks = registry.BlockTagOf("minecraft:combination_step_sound_blocks")
	crystalSoundBlocks         = registry.BlockTagOf("minecraft:crystal_sound_blocks")
)

// StepSound is one sound a step plays; the zero value plays nothing.
type StepSound struct {
	Event         registry.Sound
	Volume, Pitch float32
}

// PlayerStepSounds is the block sounds of a player's step on on, with above
// the block over it. A carpet, snow layer or other combination block on top
// plays its step over on's muffled one; an inside-step block on top (petals,
// a lily pad) plays its step alone. In water only on's muffled step plays,
// beside the swim sound the caller plays.
// vanilla: Player.playStepSound, Entity.getPrimaryStepSoundBlockPos,
// playCombinationStepSounds, playMuffledStepSound
func PlayerStepSounds(on, above uint16, inWater bool) (step, muffled StepSound) {
	if inWater {
		return StepSound{}, scaledStep(on, 0.05, 0.8)
	}
	switch b := registry.BlockOf(above); {
	case b.InTag(combinationStepSoundBlocks):
		return scaledStep(above, 0.15, 1), scaledStep(on, 0.05, 0.8)
	case b.InTag(insideStepSoundBlocks):
		return scaledStep(above, 0.15, 1), StepSound{}
	}
	return scaledStep(on, 0.15, 1), StepSound{}
}

// scaledStep is the state's step sound with its sound type's volume and
// pitch scaled; zero for a block with none.
func scaledStep(state uint16, volume, pitch float32) StepSound {
	event, v, p, ok := BlockStepSound(state)
	if !ok {
		return StepSound{}
	}
	return StepSound{Event: event, Volume: v * volume, Pitch: p * pitch}
}

// SwimSoundVolume is a swim sound's volume for a body moving at (vx, vy, vz)
// blocks per tick. vanilla: Entity.waterSwimSound
func SwimSoundVolume(vx, vy, vz float64) float32 {
	return min(1, float32(math.Sqrt(vx*vx*0.2+vy*vy+vz*vz*0.2))*0.35)
}

// PlayerSplashSound is the sound of a player entering water at (vx, vy, vz)
// blocks per tick: the high-speed splash from volume 0.25.
// vanilla: Entity.doWaterSplashEffect, Player.getSwimSplashSound
func PlayerSplashSound(vx, vy, vz float64) (event registry.Sound, volume float32) {
	volume = min(1, float32(math.Sqrt(vx*vx*0.2+vy*vy+vz*vz*0.2))*0.2)
	if volume < 0.25 {
		return registry.SoundEntityPlayerSplash, volume
	}
	return registry.SoundEntityPlayerSplashHighSpeed, volume
}

// SwimSoundPitch is the swim and splash sounds' pitch for two rolls in [0, 1).
// vanilla: Entity.playSwimSound
func SwimSoundPitch(r1, r2 float32) float32 {
	return 1 + (r1-r2)*0.4
}

// crystalChimeTicks is the least gap between two amethyst step chimes.
const crystalChimeTicks = 20

// CrystalStep is the amethyst chime a body's steps ring. The zero value is a
// body that has not chimed, at tick 0.
// vanilla: Entity.lastCrystalSoundPlayTick, crystalSoundIntensity
type CrystalStep struct {
	last      int64
	intensity float32
}

// Chime returns a step's amethyst chime, louder and higher for closer chimes.
// It draws roll in [0, 1) only on crystal_sound_blocks after the 20-tick cooldown.
// vanilla: Entity.shouldPlayAmethystStepSound, playAmethystStepSound
func (c *CrystalStep) Chime(state uint16, now int64, roll func() float32) (StepSound, bool) {
	if !registry.BlockOf(state).InTag(crystalSoundBlocks) || now < c.last+crystalChimeTicks {
		return StepSound{}, false
	}
	c.intensity *= float32(math.Pow(0.997, float64(now-c.last)))
	c.intensity = min(1, c.intensity+0.07)
	c.last = now
	return StepSound{
		Event:  registry.SoundBlockAmethystBlockChime,
		Volume: 0.1 + c.intensity*1.2,
		Pitch:  0.5 + c.intensity*roll()*1.2,
	}, true
}
