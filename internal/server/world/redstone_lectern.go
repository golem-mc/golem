package world

import "github.com/golem-mc/golem/registry"

// lecternPulseTicks is how long a page turn powers the lectern.
// vanilla: LecternBlock.PAGE_CHANGE_IMPULSE_TICKS
const lecternPulseTicks = 2

// LecternPageChanged powers a lectern whose book turned a page and books the
// tick that releases it; the sound is the caller's.
// vanilla: LecternBlock.signalPageChange
func (p *Physics) LecternPageChanged(x, y, z int32) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockLectern {
		return nil, nil, nil
	}
	var changes []BlockChange
	p.effects, p.wakePrimes = nil, nil
	p.lecternSetPowered(x, y, z, state, true, &changes)
	p.schedule(x, y, z, p.now+lecternPulseTicks)
	// The page is the comparator's reading even when powered already held.
	p.analogOutputChanged(x, y, z, p.now)
	effects, primes := p.takeWake()
	return changes, effects, primes
}

// LecternBookChanged writes a lectern's has_book and clears its powered
// state, then wakes what both reach.
// vanilla: LecternBlock.resetBookState
func (p *Physics) LecternBookChanged(x, y, z int32, hasBook bool) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockLectern {
		return nil, nil, nil
	}
	var changes []BlockChange
	p.effects, p.wakePrimes = nil, nil
	next, _ := registry.WithBool(state, registry.PropPowered, false)
	next, _ = registry.WithBool(next, registry.PropHasBook, hasBook)
	p.setClientShaped(x, y, z, next, &changes)
	p.wireWakeNeighbors(x, y, z, &changes)
	p.wireWakeNeighbors(x, y-1, z, &changes)
	p.analogOutputChanged(x, y, z, p.now)
	effects, primes := p.takeWake()
	return changes, effects, primes
}

// lecternSetPowered writes powered and wakes the neighbours of the lectern and
// of the block below, which it strongly powers.
// vanilla: LecternBlock.changePowered
func (p *Physics) lecternSetPowered(x, y, z int32, state uint16, powered bool, changes *[]BlockChange) {
	next, _ := registry.WithBool(state, registry.PropPowered, powered)
	p.setClientShaped(x, y, z, next, changes)
	p.wireWakeNeighbors(x, y, z, changes)
	p.wireWakeNeighbors(x, y-1, z, changes)
}
