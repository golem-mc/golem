package world

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// The eyeblossom: the overworld's hour decides its openness, the random tick
// and booked visits carry it there, and each flip wakes its kind in the box
// around it.
// vanilla: EyeblossomBlock, EyeblossomBlock$Type

// eyeblossomOpen reads the tri-state attribute; default leaves flowers alone.
// vanilla: EnvironmentAttributes.EYEBLOSSOM_OPEN, TriState.toBoolean
func (w *World) eyeblossomOpen() (open, ok bool) {
	v := w.environmentGlobal(envEyeblossomOpen)
	return v == 1, v != -1
}

// eyeblossomFlip is the state the eyeblossom should have by the hour and
// whether it differs, the gate tryChangingState opens with.
// vanilla: EyeblossomBlock.tryChangingState
func (p *Physics) eyeblossomFlip(state uint16) (uint16, bool) {
	b := registry.BlockOf(state)
	if int(state) >= registry.StateCount || (b != registry.BlockClosedEyeblossom && b != registry.BlockOpenEyeblossom) {
		return state, false
	}
	open, ok := p.w.eyeblossomOpen()
	if !ok {
		return state, false
	}
	want := registry.BlockClosedEyeblossom
	if open {
		want = registry.BlockOpenEyeblossom
	}
	if b == want {
		return state, false
	}
	return want.DefaultState(), true
}

// isEyeblossom reports an eyeblossom, open or closed, not potted.
func isEyeblossom(state uint16) bool {
	b := registry.BlockOf(state)
	return b == registry.BlockClosedEyeblossom || b == registry.BlockOpenEyeblossom
}

// eyeblossomOpenState reports an open eyeblossom, potted or not.
func eyeblossomOpenState(state uint16) bool {
	b := registry.BlockOf(state)
	return b == registry.BlockOpenEyeblossom || b == registry.BlockPottedOpenEyeblossom
}

// eyeblossomRandomTick flips by the hour with the long switch sound and books
// visits for its kind in the box around it.
// vanilla: EyeblossomBlock.randomTick
func (p *Physics) eyeblossomRandomTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if next, flipped := p.eyeblossomFlip(state); flipped {
		p.eyeblossomTakeState(x, y, z, state, next, true, changes)
	}
}

// eyeblossomScheduledTick is a booked visit from a neighbour's flip: the same
// flip with the short switch sound.
// vanilla: EyeblossomBlock.tick
func (p *Physics) eyeblossomScheduledTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if next, flipped := p.eyeblossomFlip(state); flipped {
		p.eyeblossomTakeState(x, y, z, state, next, false, changes)
	}
}

// eyeblossomTakeState writes the flip, plays the long (random tick) or short
// (booked) sound, and books every eyeblossom in the box still in the old state
// for dist*5 to dist*10 ticks out, dist the Euclidean block distance.
// vanilla: EyeblossomBlock.tryChangingState
func (p *Physics) eyeblossomTakeState(x, y, z int32, from, to uint16, long bool, changes *[]BlockChange) {
	p.setShaped(x, y, z, to, changes)
	p.eyeblossomTell(x, y, z, to, long)
	for dx := int32(-3); dx <= 3; dx++ {
		for dy := int32(-2); dy <= 2; dy++ {
			for dz := int32(-3); dz <= 3; dz++ {
				nx, ny, nz := x+dx, y+dy, z+dz
				if p.w.BlockAt(nx, ny, nz) != from {
					continue
				}
				// dist is at least 1: the only zero offset is the flipped
				// block, which no longer matches, so the roll's span is never
				// empty.
				dist := math.Sqrt(float64(dx*dx + dy*dy + dz*dz))
				lo, hi := int(dist*5.0), int(dist*10.0)
				p.schedule(nx, ny, nz, p.now+int64(lo+p.rng.Intn(hi-lo+1)))
			}
		}
	}
}

// eyeblossomSound is the new type's switch sound, long for the random tick
// and short for booked visits.
// vanilla: EyeblossomBlock$Type.longSwitchSound, shortSwitchSound
func eyeblossomSound(state uint16, long bool) registry.Sound {
	if eyeblossomOpenState(state) {
		if long {
			return registry.SoundBlockEyeblossomOpenLong
		}
		return registry.SoundBlockEyeblossomOpen
	}
	if long {
		return registry.SoundBlockEyeblossomCloseLong
	}
	return registry.SoundBlockEyeblossomClose
}

// eyeblossomTell queues the trail particle, then the switch sound.
func (p *Physics) eyeblossomTell(x, y, z int32, to uint16, long bool) {
	effect := Effect{
		SoundID: eyeblossomSound(to, long), X: x, Y: y, Z: z, Volume: 1, Pitch: 1,
		Trail: p.eyeblossomTrail(x, y, z, to),
	}
	// Random-pass effects go to the pass's sink, which the server drains beside
	// it; booked visits' go to the wake.
	if long {
		p.randomEffects = append(p.randomEffects, effect)
	} else {
		p.effects = append(p.effects, effect)
	}
}

// eyeblossomTrail rolls a flip's particle: from the block's centre to a point
// up and to the side, in the type's colour.
// vanilla: EyeblossomBlock$Type.spawnTransformParticle
func (p *Physics) eyeblossomTrail(x, y, z int32, state uint16) *TrailParticle {
	color := int32(0x5F5F5F)
	if eyeblossomOpenState(state) {
		color = 16545810
	}
	lifetime := 0.5 + p.rng.Float64()
	vx, vy, vz := p.rng.Float64()-0.5, p.rng.Float64()+1.0, p.rng.Float64()-0.5
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	return &TrailParticle{
		X: cx, Y: cy, Z: cz,
		TargetX: cx + vx*lifetime, TargetY: cy + vy*lifetime, TargetZ: cz + vz*lifetime,
		Color: color, Duration: int32(20.0 * lifetime),
	}
}

// pottedEyeblossomRandomTick flips by the hour with the long sound and the
// particle, without spreading.
// vanilla: FlowerPotBlock.randomTick
func (p *Physics) pottedEyeblossomRandomTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	open, ok := p.w.eyeblossomOpen()
	if !ok || open == (registry.BlockOf(state) == registry.BlockPottedOpenEyeblossom) {
		return
	}
	to := registry.BlockPottedClosedEyeblossom.DefaultState()
	if open {
		to = registry.BlockPottedOpenEyeblossom.DefaultState()
	}
	p.setShaped(x, y, z, to, changes)
	p.eyeblossomTell(x, y, z, to, true)
}
