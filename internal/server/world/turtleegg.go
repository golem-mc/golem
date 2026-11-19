package world

import (
	"github.com/golem-mc/golem/registry"
)

// tagSand is the minecraft:sand block tag an egg hatches on.
var tagSand = registry.BlockTagOf("minecraft:sand")

// The turtle egg hatches in three random-tick steps, quickly just before
// dawn and rarely at any other hour.
// vanilla: data/minecraft/timeline/day.json, TURTLE_EGG_HATCH_CHANCE
const (
	// turtleEggHatchChance is the attribute's default, held outside the window.
	turtleEggHatchChance = 0.002
	// turtleEggMaxHatch is the hatch stage whose tick breaks the egg.
	turtleEggMaxHatch = 2
)

// TurtleHatch is an egg block that hatched: its cell and the hatchling count.
// The server owns the bodies.
// vanilla: TurtleEggBlock.randomTick
type TurtleHatch struct {
	X, Y, Z int32
	Eggs    int
}

// TakeTurtleHatches returns and clears the hatches.
func (p *Physics) TakeTurtleHatches() []TurtleHatch {
	out := p.turtleHatches
	p.turtleHatches = nil
	return out
}

// turtleEggHatchChanceNow is the turtle-egg-hatch-chance attribute here; the
// nether and end have no track for it.
func (w *World) turtleEggHatchChanceNow() float64 {
	return float64(w.environmentGlobal(envTurtleEggHatchChance))
}

// turtleEggRandomTick cracks an egg on sand one stage on a passing roll; the
// third stage hatches.
// vanilla: TurtleEggBlock.randomTick
func (p *Physics) turtleEggRandomTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if p.rng.Float64() >= p.w.turtleEggHatchChanceNow() {
		return
	}
	if !stateInTag(p.w.BlockAt(x, y-1, z), tagSand) {
		return
	}
	hatch, ok := registry.StateInt(state, registry.PropHatch)
	if !ok {
		return
	}
	eggs, ok := registry.StateInt(state, registry.PropEggs)
	if !ok {
		return
	}
	pitch := 0.9 + float32(p.rng.Float64())*0.2
	if hatch < turtleEggMaxHatch {
		p.randomEffects = append(p.randomEffects, Effect{
			SoundID: registry.SoundEntityTurtleEggCrack, GameEvent: gameEventBlockChange, X: x, Y: y, Z: z, Volume: 0.7, Pitch: pitch,
		})
		if next, ok := registry.WithInt(state, registry.PropHatch, hatch+1); ok {
			p.setClientShaped(x, y, z, next, changes)
		}
		return
	}
	p.randomEffects = append(p.randomEffects, Effect{
		SoundID: registry.SoundEntityTurtleEggHatch, GameEvent: registry.GameEventBlockDestroy, X: x, Y: y, Z: z, Volume: 0.7, Pitch: pitch,
	})
	p.setShaped(x, y, z, airState, changes)
	for range eggs {
		p.randomEffects = append(p.randomEffects, Effect{LevelEvent: 2001, LevelEventData: int32(state), X: x, Y: y, Z: z})
	}
	p.turtleHatches = append(p.turtleHatches, TurtleHatch{X: x, Y: y, Z: z, Eggs: eggs})
}

// TurtleEggsIn reports whether a loaded section overlapping the inclusive box
// holds a turtle egg, so a search over the box can be skipped when none does.
func (w *World) TurtleEggsIn(x0, y0, z0, x1, y1, z1 int32) bool {
	lo := max((y0-worldMinY)>>4, 0)
	hi := min((y1-worldMinY)>>4, worldHeight/sectionSize-1)
	if lo > hi {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	for chx := x0 >> 4; chx <= x1>>4; chx++ {
		for chz := z0 >> 4; chz <= z1>>4; chz++ {
			loaded, found := w.loadedAt(ChunkPos{X: chx, Z: chz})
			if !found {
				continue
			}
			for sec := lo; sec <= hi; sec++ {
				if loaded.chunk.sections[sec].eggs > 0 {
					return true
				}
			}
		}
	}
	return false
}
