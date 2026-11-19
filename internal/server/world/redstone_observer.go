package world

import "github.com/golem-mc/golem/registry"

// The observer: a state change at the face FACING points to books a visit that
// powers it and books another that unpowers it, a strong one-redstone-tick
// pulse out the back.
// vanilla: ObserverBlock

// observerDelay is ObserverBlock's scheduleTick span in game ticks, to each
// edge of the pulse.
const observerDelay = 2

// observerTick flips powered, books the off edge if it just turned on, and
// wakes the front either way.
// vanilla: ObserverBlock.tick
func (p *Physics) observerTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	powered := !registry.StatePowered(state)
	next, _ := registry.WithBool(state, registry.PropPowered, powered)
	p.observerWrite(x, y, z, next, facingOfState(state), changes)
	if powered {
		p.schedulePriority(x, y, z, p.now+observerDelay, tickPriorityNormal)
	}
}

// observerWrite is the tick's flag-2 write, whose shape pass still runs, then
// the front wake.
func (p *Physics) observerWrite(x, y, z int32, state uint16, f int, changes *[]BlockChange) {
	p.setClientShaped(x, y, z, state, changes)
	p.repeaterWakeFront(x, y, z, f, changes)
}

// observerOnPlace stops a pulse an observer was written in with, when it has no
// visit booked, and wakes the front. Vanilla writes the stop with flag 18:
// clients hear it and no shape pass runs.
// vanilla: onPlace
func (p *Physics) observerOnPlace(x, y, z int32, state uint16, changes *[]BlockChange) {
	if !registry.StatePowered(state) || p.pending[blockKey{x, y, z}] {
		return
	}
	off, _ := registry.WithBool(state, registry.PropPowered, false)
	p.setClient(x, y, z, off, changes)
	p.repeaterWakeFront(x, y, z, facingOfState(state), changes)
}

// observerAfterRemoval wakes the front with the end of a pulse the break cut
// short.
// vanilla: affectNeighborsAfterRemoval
func (p *Physics) observerAfterRemoval(x, y, z int32, state uint16, changes *[]BlockChange) {
	if !registry.StatePowered(state) || !p.pending[blockKey{x, y, z}] {
		return
	}
	p.repeaterWakeFront(x, y, z, facingOfState(state), changes)
}
