package world

import (
	"github.com/golem-mc/golem/registry"
)

// Leaves: the distance property, the scheduled tick that recomputes it, and the
// random tick that rots unreached leaves (LeavesBlock's three rules).
//
// Distance is how far a leaf is from the nearest block that holds leaves back:
// 1 beside a log, 2 beside that leaf, up to 7. A neighbour change only books a
// tick (updateShape writes nothing); the tick a game tick later takes the
// minimum of the neighbours' plus one. A leaf cut off from every log settles at
// 7, where a non-persistent leaf's random tick drops its loot and removes it.

// leafTickDelay is the delay of LeavesBlock.updateShape's scheduleTick, in game
// ticks.
const leafTickDelay = 1

// leafTickEvent tags a leaf's distance tick so a waterlogged leaf's fluid visit
// is not mistaken for it. Event bits: piston events the low five, survival
// five, leaf six, dried ghast seven.
const leafTickEvent = 1 << 6

// scheduleLeaf books the leaf's distance tick at due, one per position, in the
// leaf's own set so the fluid visit neither blocks nor is blocked by it.
func (p *Physics) scheduleLeaf(x, y, z int32, due int64) {
	p.scheduleBooked(p.leafBooked, x, y, z, due, leafTickEvent)
}

// leafDistanceOf: 0 for a block that holds leaves back, a leaf's own distance,
// and the ceiling for anything else.
// vanilla: LeavesBlock.getDistanceAt
func leafDistanceOf(state uint16) int {
	b := registry.BlockOf(state)
	if b.IsPreventsNearbyLeafDecay() {
		return 0
	}
	if b.IsLeaves() {
		if d, ok := registry.StateInt(state, registry.PropDistance); ok {
			return d
		}
	}
	return 7
}

// leafDistanceAround is the least neighbour distance plus one, starting from
// the ceiling, so it never writes past 7.
// vanilla: LeavesBlock.updateDistance
func leafDistanceAround(w *World, x, y, z int32) int {
	d := 7
	for _, delta := range [6][3]int32{
		{0, 0, -1}, {0, 0, 1}, {0, -1, 0}, {0, 1, 0}, {-1, 0, 0}, {1, 0, 0},
	} {
		if v := leafDistanceOf(w.BlockAt(x+delta[0], y+delta[1], z+delta[2])) + 1; v < d {
			d = v
		}
	}
	return d
}

// leafTick writes the distance the neighbours now make when it differs; an
// unchanged distance writes nothing, as setBlock declines an unchanged state.
// vanilla: LeavesBlock.tick
func (p *Physics) leafTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	block := registry.BlockOf(state)
	if !block.IsLeaves() {
		return // not a leaf any more; the queue entry is spent
	}
	want := leafDistanceAround(p.w, x, y, z)
	current, ok := registry.StateInt(state, registry.PropDistance)
	if !ok || want == current {
		return
	}
	if next, ok := registry.WithInt(state, registry.PropDistance, want); ok {
		p.setShaped(x, y, z, next, changes)
	}
}

// leafDecay is LeavesBlock.randomTick's dropResources and removeBlock for a
// leaf at distance 7, handing the leaf back beside the change so the server
// rolls the loot after the block update. removeBlock writes the cell's fluid,
// so a waterlogged leaf leaves its water.
func (p *Physics) leafDecay(x, y, z int32, state uint16, changes *[]BlockChange, decays *[]Decay) {
	if p.listens(HookLeaves) {
		d := HookDecision{Kind: HookLeaves, X: x, Y: y, Z: z, Old: state}
		if p.ask(&d) {
			return
		}
	}
	p.setShaped(x, y, z, fluidHolding(state).createLegacy(), changes)
	*decays = append(*decays, Decay{X: x, Y: y, Z: z, State: state})
}

// Decay is a leaf whose random tick found it rotting, at its position with its
// state, for the server to roll the loot and drop it there. RandomTick returns
// these; nothing else rots a block.
type Decay struct {
	X, Y, Z int32
	State   uint16
}

// LeafStateForPlacement is a player-placed leaf's state: persistent, at the
// neighbours' distance. Deliberately unlike vanilla, a placement is never
// waterlogged; the property stays false.
// vanilla: LeavesBlock.getStateForPlacement
func (w *World) LeafStateForPlacement(block registry.Block, x, y, z int32) uint16 {
	state, ok := registry.WithBool(block.DefaultState(), registry.PropPersistent, true)
	if !ok {
		return 0
	}
	state, _ = registry.WithInt(state, registry.PropDistance, leafDistanceAround(w, x, y, z))
	return state
}
