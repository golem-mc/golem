package world

import "github.com/golem-mc/golem/registry"

// TadpoleHatch is one persistent hatchling, positioned by the block's random stream.
// vanilla: FrogspawnBlock.spawnTadpoles
type TadpoleHatch struct {
	X, Y, Z float64
	Yaw     float32
}

// TakeTadpoleHatches returns and clears the hatchlings.
func (p *Physics) TakeTadpoleHatches() []TadpoleHatch {
	out := p.tadpoleHatches
	p.tadpoleHatches = nil
	return out
}

// vanilla: FrogspawnBlock.tick, hatchFrogspawn
func (p *Physics) frogspawnTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	survives := canSurvive(p.w, x, y, z, state)
	p.setShaped(x, y, z, airState, changes)
	p.effects = append(p.effects, Effect{LevelEvent: 2001, LevelEventData: int32(state), GameEvent: registry.GameEventBlockDestroy, X: x, Y: y, Z: z})
	if !survives {
		return
	}
	p.effects = append(p.effects, Effect{SoundID: registry.SoundBlockFrogspawnHatch, X: x, Y: y, Z: z, Volume: 1, Pitch: 1})
	count := 2 + p.rng.Intn(4)
	for range count {
		// vanilla: FrogspawnBlock.getRandomTadpolePositionOffset (float-promoted bounds).
		dx := max(float64(float32(0.2)), min(0.7999999970197678, p.rng.Float64()))
		dz := max(float64(float32(0.2)), min(0.7999999970197678, p.rng.Float64()))
		p.tadpoleHatches = append(p.tadpoleHatches, TadpoleHatch{float64(x) + dx, float64(y) - 0.5, float64(z) + dz, float32(1 + p.rng.Intn(360))})
	}
}

// frogspawnHatchEvent separates the hatch from stale fluid visits at the same cell.
const frogspawnHatchEvent = 1<<5 | 1<<6 | 1<<7 | 1

// scheduleFrogspawn books one hatch without delaying it on neighbour updates.
// vanilla: FrogspawnBlock.onPlace
func (p *Physics) scheduleFrogspawn(x, y, z int32) {
	p.scheduleBooked(p.frogspawnBooked, x, y, z, p.now+3600+int64(p.rng.Intn(8400)), frogspawnHatchEvent)
}
