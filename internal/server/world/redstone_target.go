package world

import "github.com/golem-mc/golem/registry"

// The target block: a projectile's hit powers it by how close it struck the
// face's centre, held for a span the projectile decides, out of every face
// weakly, until the booked tick drops it. The power is the rules'
// (internal/server/rules/redstone.go TargetHitPower); the engine owns the state and the
// visit.
// vanilla: TargetBlock

// TargetProjectiled writes a hit's power on the target at (x, y, z) and books
// its drop hold ticks out. A non-target, or a target whose visit is still
// booked, is left alone: the first hit's span stands. The six neighbours and
// their shape pass answer at once.
// vanilla: TargetBlock.onProjectileHit, TargetBlock.updateRedstoneOutput
func (p *Physics) TargetProjectiled(x, y, z int32, power, hold int) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockTarget || p.pending[blockKey{x, y, z}] {
		return nil, nil, nil
	}
	var changes []BlockChange
	p.effects, p.wakePrimes = nil, nil
	next, _ := registry.WithInt(state, registry.PropPower, power)
	p.setClient(x, y, z, next, &changes)
	p.wireWakeNeighbors(x, y, z, &changes)
	// The flag-3 write's shape pass is all an observer watching the target
	// reacts to.
	p.shapeUpdateNeighbors(x, y, z, &changes)
	p.schedule(x, y, z, p.now+int64(hold))
	effects, primes := p.takeWake()
	return changes, effects, primes
}

// targetOnPlace drops a power the target arrived with (a setblock state, a
// piston push): a hold belongs to a hit's own visit. The block's own state move
// keeps its power, being a ride with its own visit.
// vanilla: TargetBlock.onPlace
func (p *Physics) targetOnPlace(x, y, z int32, state, oldState uint16, changes *[]BlockChange) {
	if registry.BlockOf(oldState) == registry.BlockOf(state) {
		return
	}
	if registry.StatePower(state) == 0 || p.pending[blockKey{x, y, z}] {
		return
	}
	next, _ := registry.WithInt(state, registry.PropPower, 0)
	p.setClient(x, y, z, next, changes)
}

// targetTick drops a held power to 0 with the neighbour wake and shape pass;
// finding 0, it writes nothing.
// vanilla: TargetBlock.tick
func (p *Physics) targetTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if registry.StatePower(state) == 0 {
		return
	}
	next, _ := registry.WithInt(state, registry.PropPower, 0)
	p.setClient(x, y, z, next, changes)
	p.wireWakeNeighbors(x, y, z, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
}
