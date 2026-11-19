package world

import (
	"math/rand"

	"github.com/golem-mc/golem/registry"
)

// The big dripleaf's tilt steps unstable, partial, full, then back to none on
// scheduled visits; a grounded body standing on it starts at unstable, a
// projectile sends it straight to full.

// dripleafDelay is each tilt's wait in game ticks.
// vanilla: BigDripleafBlock.DELAY_UNTIL_NEXT_TILT_STATE
var dripleafDelay = map[string]int64{"unstable": 10, "partial": 10, "full": 100}

// DripleafProjectiled tilts the big dripleaf at (x, y, z) fully and books its
// righting, with the tilt sound as an effect; a non-dripleaf is left alone.
// vanilla: BigDripleafBlock.onProjectileHit
func (p *Physics) DripleafProjectiled(x, y, z int32) ([]BlockChange, []Effect) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockBigDripleaf {
		return nil, nil
	}
	var changes []BlockChange
	p.effects = nil
	p.dripleafSetTilt(x, y, z, state, "full", registry.SoundBlockBigDripleafTiltDown, &changes)
	effects := p.effects
	p.effects = nil
	return changes, effects
}

// dripleafStoodOn tilts an untilted, unpowered leaf after a grounded body's
// feet have cleared its top.
// vanilla: BigDripleafBlock.entityInside
func (p *Physics) dripleafStoodOn(x, y, z int32, changes *[]BlockChange, effects *[]Effect) {
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockBigDripleaf || registry.StateValue(state, registry.PropTilt) != "none" || p.hasNeighborSignal(x, y, z) {
		return
	}
	p.effects = nil
	p.dripleafSetTilt(x, y, z, state, "unstable", registry.SoundNone, changes)
	*effects = append(*effects, p.effects...)
	p.effects = nil
}

// dripleafSetTilt writes the tilt, plays sound if not empty and books the next
// step.
// vanilla: BigDripleafBlock.setTiltAndScheduleTick
func (p *Physics) dripleafSetTilt(x, y, z int32, state uint16, tilt string, sound registry.Sound, changes *[]BlockChange) {
	p.dripleafWriteTilt(x, y, z, state, tilt, changes)
	if sound != registry.SoundNone {
		p.dripleafSound(x, y, z, sound)
	}
	if delay, ok := dripleafDelay[tilt]; ok {
		p.scheduleBooked(p.redstoneBooked, x, y, z, p.now+delay, redstoneTickEvent)
	}
}

func (p *Physics) dripleafSound(x, y, z int32, sound registry.Sound) {
	// vanilla: BigDripleafBlock.playTiltSound
	p.effects = append(p.effects, Effect{
		SoundID: sound, X: x, Y: y, Z: z, Volume: 1, Pitch: 0.8 + rand.Float32()*0.4,
	})
}

// dripleafTick rights a powered leaf, otherwise steps the tilt.
// vanilla: BigDripleafBlock.tick, resetTilt
func (p *Physics) dripleafTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if p.hasNeighborSignal(x, y, z) {
		p.dripleafReset(x, y, z, state, changes)
		return
	}
	switch registry.StateValue(state, registry.PropTilt) {
	case "unstable":
		p.dripleafSetTilt(x, y, z, state, "partial", registry.SoundBlockBigDripleafTiltDown, changes)
	case "partial":
		p.dripleafSetTilt(x, y, z, state, "full", registry.SoundBlockBigDripleafTiltDown, changes)
	case "full":
		p.dripleafReset(x, y, z, state, changes)
	}
}

func (p *Physics) dripleafReset(x, y, z int32, state uint16, changes *[]BlockChange) {
	p.dripleafWriteTilt(x, y, z, state, "none", changes)
	if registry.StateValue(state, registry.PropTilt) != "none" {
		p.dripleafSound(x, y, z, registry.SoundBlockBigDripleafTiltUp)
	}
}

// vanilla: BigDripleafBlock.setTilt, Tilt.causesVibration
func (p *Physics) dripleafWriteTilt(x, y, z int32, state uint16, tilt string, changes *[]BlockChange) {
	next, _ := registry.WithProp(state, registry.PropTilt, tilt)
	p.setClientShaped(x, y, z, next, changes)
	if registry.StateValue(state, registry.PropTilt) != tilt && tilt != "unstable" {
		p.effects = append(p.effects, Effect{GameEvent: gameEventBlockChange, X: x, Y: y, Z: z})
	}
}
