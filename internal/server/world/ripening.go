package world

import (
	"github.com/golem-mc/golem/registry"
)

// Nether wart, cocoa and berry bush aging, each its class's randomTick with a
// flag-2 write (clients told, no neighbour woken, observers still see the shape
// pass).

// The age ceilings from each block's isRandomlyTicking: a state at its ceiling
// is never rolled.
const (
	netherWartMaxAge = 3
	cocoaMaxAge      = 2
	berryBushMaxAge  = 3
)

// netherWartTick ages a nether wart on a one-in-ten roll, age read before the
// die.
// vanilla: NetherWartBlock.randomTick
func (p *Physics) netherWartTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok || age >= netherWartMaxAge {
		return
	}
	if p.rng.Intn(10) != 0 {
		return
	}
	p.ripen(x, y, z, state, age, changes)
}

// cocoaTick ages a cocoa pod on a one-in-five roll, the die before the age.
// vanilla: CocoaBlock.randomTick
func (p *Physics) cocoaTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if p.rng.Intn(5) != 0 {
		return
	}
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok || age >= cocoaMaxAge {
		return
	}
	p.ripen(x, y, z, state, age, changes)
}

// berryBushTick ages a sweet berry bush on a one-in-five roll with raw light 9
// or more above (getRawBrightness(pos.above(), 0)), posting block_change.
// vanilla: SweetBerryBushBlock.randomTick
func (p *Physics) berryBushTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok || age >= berryBushMaxAge {
		return
	}
	if p.rng.Intn(5) != 0 {
		return
	}
	if rawLightAt(p.w, x, y+1, z) < 9 {
		return
	}
	p.ripen(x, y, z, state, age, changes)
	p.randomEffects = append(p.randomEffects, Effect{GameEvent: gameEventBlockChange, X: x, Y: y, Z: z})
}

// ripen writes the next age, other properties kept.
func (p *Physics) ripen(x, y, z int32, state uint16, age int, changes *[]BlockChange) {
	if next, found := registry.WithInt(state, registry.PropAge, age+1); found {
		p.setClientShaped(x, y, z, next, changes)
	}
}
