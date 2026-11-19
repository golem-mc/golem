package world

import "github.com/golem-mc/golem/registry"

// snifferEggHatchEvent separates the egg's own tick from stale fluid visits at the same cell.
const snifferEggHatchEvent = 1<<5 | 1<<6 | 1<<7 | 4

const (
	snifferEggHatchMax = 2 // vanilla: SnifferEggBlock.MAX_HATCH_LEVEL
	// Ticks for a whole hatch, split over its three stages, plus up to
	// snifferEggJitter more per stage.
	// vanilla: SnifferEggBlock.REGULAR_HATCH_TIME_TICKS, BOOSTED_HATCH_TIME_TICKS, RANDOM_HATCH_OFFSET_TICKS
	snifferEggRegularTime = 24000
	snifferEggBoostedTime = 12000
	snifferEggJitter      = 300
	// snifferEggBoostEvent is the level event of a boosted egg's steam.
	snifferEggBoostEvent = 3009
)

var snifferEggBoost = registry.BlockTagOf("minecraft:sniffer_egg_hatch_boost")

// SnifferHatch is a hatched egg: the block's cell, where the server stands a
// baby sniffer at the centre with Yaw.
// vanilla: SnifferEggBlock.tick
type SnifferHatch struct {
	X, Y, Z int32
	Yaw     float32
}

// TakeSnifferHatches returns and clears the hatchlings.
func (p *Physics) TakeSnifferHatches() []SnifferHatch {
	out := p.snifferHatches
	p.snifferHatches = nil
	return out
}

// scheduleSnifferEgg is the egg's onPlace: the boost's level event, the
// place game event and the next stage's tick.
// vanilla: SnifferEggBlock.onPlace
func (p *Physics) scheduleSnifferEgg(x, y, z int32) {
	hatchTime := snifferEggRegularTime
	if registry.BlockOf(p.w.BlockAt(x, y-1, z)).InTag(snifferEggBoost) {
		hatchTime = snifferEggBoostedTime
		p.effects = append(p.effects, Effect{LevelEvent: snifferEggBoostEvent, X: x, Y: y, Z: z})
	}
	p.effects = append(p.effects, Effect{GameEvent: registry.GameEventBlockPlace, X: x, Y: y, Z: z})
	p.scheduleBooked(p.snifferEggBooked, x, y, z, p.now+int64(hatchTime/3+p.rng.Intn(snifferEggJitter)), snifferEggHatchEvent)
}

// snifferEggTick cracks the egg a stage or, at the last, hatches it.
// vanilla: SnifferEggBlock.tick
func (p *Physics) snifferEggTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	hatch, ok := registry.StateInt(state, registry.PropHatch)
	if !ok {
		return
	}
	pitch := 0.9 + p.rng.Float32()*0.2
	if hatch < snifferEggHatchMax {
		p.effects = append(p.effects, Effect{SoundID: registry.SoundBlockSnifferEggCrack, X: x, Y: y, Z: z, Volume: 0.7, Pitch: pitch})
		if next, ok := registry.WithInt(state, registry.PropHatch, hatch+1); ok && p.setClientShaped(x, y, z, next, changes) {
			p.scheduleSnifferEgg(x, y, z)
		}
		return
	}
	p.effects = append(p.effects, Effect{SoundID: registry.SoundBlockSnifferEggHatch, X: x, Y: y, Z: z, Volume: 0.7, Pitch: pitch})
	p.setShaped(x, y, z, airState, changes)
	p.effects = append(p.effects, Effect{LevelEvent: 2001, LevelEventData: int32(state), GameEvent: registry.GameEventBlockDestroy, X: x, Y: y, Z: z})
	// vanilla: Mth.wrapDegrees(random.nextFloat() * 360)
	yaw := p.rng.Float32() * 360
	if yaw > 180 {
		yaw -= 360
	}
	p.snifferHatches = append(p.snifferHatches, SnifferHatch{X: x, Y: y, Z: z, Yaw: yaw})
}
