package world

import (
	"github.com/golem-mc/golem/registry"
)

// Odds against a turtle egg breaking under a body: one in TurtleEggStepOdds
// per tick standing on it, one in TurtleEggLandOdds for a landing.
// vanilla: TurtleEggBlock.stepOn, TurtleEggBlock.fallOn
const (
	TurtleEggStepOdds = 100
	TurtleEggLandOdds = 3
)

// IsTurtleEgg reports a turtle egg state, without the physics lock.
func IsTurtleEgg(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockTurtleEgg
}

// TurtleEggBreak describes an egg decrement and its client effects.
type TurtleEggBreak struct {
	State      uint16
	EggsRemain bool
	Pitch      float32
	Changes    []BlockChange
}

// TurtleEggTrampled rolls one in odds for the egg block at (x, y, z) to lose an
// egg; the last one removes the block without a drop. Whether the body may
// break eggs is the caller's check.
// vanilla: TurtleEggBlock.destroyEgg, TurtleEggBlock.decreaseEggs
func (p *Physics) TurtleEggTrampled(x, y, z int32, odds int) (TurtleEggBreak, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockTurtleEgg {
		return TurtleEggBreak{}, false
	}
	if p.rng.Intn(odds) != 0 {
		return TurtleEggBreak{}, false
	}
	return p.decreaseTurtleEgg(x, y, z, state), true
}

// DecreaseTurtleEgg takes one egg from state, which must be a turtle egg and
// may already have been removed by mining.
// vanilla: TurtleEggBlock.decreaseEggs
func (p *Physics) DecreaseTurtleEgg(x, y, z int32, state uint16) TurtleEggBreak {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.decreaseTurtleEgg(x, y, z, state)
}

func (p *Physics) decreaseTurtleEgg(x, y, z int32, state uint16) TurtleEggBreak {
	out := TurtleEggBreak{State: state, Pitch: 0.9 + p.rng.Float32()*0.2}
	eggs, _ := registry.StateInt(state, registry.PropEggs)
	if eggs <= 1 {
		p.setShaped(x, y, z, airState, &out.Changes)
		return out
	}
	if next, found := registry.WithInt(state, registry.PropEggs, eggs-1); found {
		p.setClientShaped(x, y, z, next, &out.Changes)
		out.EggsRemain = true
	}
	return out
}
