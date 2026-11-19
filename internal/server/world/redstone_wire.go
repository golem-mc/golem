package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

const wireNone = 0

// wireCanSurvive tests the wire's floor support.
func (p *Physics) wireCanSurvive(x, y, z int32) bool {
	return blocklogic.WireCanSupport(p.w.BlockAt(x, y-1, z))
}

// wireUpdateShape checks support or applies the shared wire shape rule.
// vanilla: RedStoneWireBlock.updateShape
func (p *Physics) wireUpdateShape(x, y, z int32, d int, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if !isWire[registry.BlockOf(state)] {
		return
	}
	if d == dirDown {
		if !p.wireCanSurvive(x, y, z) {
			p.popBlock(x, y, z, state, changes)
		}
		return
	}
	sides := blocklogic.WireShapeUpdate(p.w, x, y, z, blocklogic.WireSides(state), d)
	ns := blocklogic.WireState(registry.StatePower(state), sides[2], sides[0], sides[1], sides[3])
	if ns != state {
		n := len(*changes)
		p.setClient(x, y, z, ns, changes)
		if p.silentShapes && len(*changes) > n {
			(*changes)[n].Silent = true
		}
		p.wireReshaped(x, y, z, state, ns, changes)
	}
}

// wireReshaped is the shape flow of a wire's own write (Level.setBlock, flag 16
// clear): the six-neighbour shape update, flanked by the slope wakes of the old
// and new connections.
func (p *Physics) wireReshaped(x, y, z int32, oldState, newState uint16, changes *[]BlockChange) {
	p.wireSlopeChanges(x, y, z, oldState, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
	if wireConnsDiffer(oldState, newState) {
		p.wireSlopeChanges(x, y, z, newState, changes)
	}
}

// wireConnsDiffer reports whether two wire states connect different sides, so
// an unchanged shape skips the second slope pass.
func wireConnsDiffer(a, b uint16) bool {
	if int(a) >= registry.StateCount || int(b) >= registry.StateCount {
		return true
	}
	for _, d := range []int{dirNorth, dirSouth, dirEast, dirWest} {
		if wireSideValue(a, d) != wireSideValue(b, d) {
			return true
		}
	}
	return false
}

// wireSlopeChanges updates the wires one step below or above each connected
// side, re-deriving their side facing the step cell; wires level with it are
// the six-neighbour pass's.
// vanilla: RedStoneWireBlock.updateIndirectNeighbourShapes.
func (p *Physics) wireSlopeChanges(x, y, z int32, state uint16, changes *[]BlockChange) {
	if p.skipWireShape {
		return
	}
	if !isWire[registry.BlockOf(state)] {
		return
	}
	for _, d := range []int{dirNorth, dirEast, dirSouth, dirWest} {
		if wireSideValue(state, d) == wireNone {
			continue
		}
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		if isWire[registry.BlockOf(p.w.BlockAt(nx, ny, nz))] {
			continue
		}
		for _, dy := range [2]int32{-1, 1} {
			p.wireUpdateShape(nx, ny+dy, nz, blocklogic.Opposite(d), changes)
		}
	}
}

// wireSideValue is the wire connection of a state on horizontal face d.
func wireSideValue(state uint16, d int) int {
	return int(registry.StateWireSide(state, blocklogic.WireSideIndex(d)))
}

func wireIsDot(state uint16) bool {
	for i := range 4 {
		if registry.StateWireSide(state, i) != registry.WireSideNone {
			return false
		}
	}
	return true
}

func wireIsCross(state uint16) bool {
	for i := range 4 {
		if registry.StateWireSide(state, i) == registry.WireSideNone {
			return false
		}
	}
	return true
}

// wireUpdatePower sets the target to the stronger of the blocks' signal and the
// neighbour wires' power minus one; when it differs, the wire writes it and
// wakes itself and its six neighbours, which carries power down a line.
// vanilla: DefaultRedstoneWireEvaluator.updatePowerStrength
func (p *Physics) wireUpdatePower(x, y, z int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if !isWire[registry.BlockOf(state)] {
		return
	}
	power := registry.StatePower(state)
	target := p.wireTargetStrength(x, y, z)
	if power == target {
		return
	}
	if target = p.redstonePower(x, y, z, power, target); target == power {
		return
	}
	c := blocklogic.WireSides(state)
	ns := blocklogic.WireState(target, c[2], c[0], c[1], c[3])
	p.setClient(x, y, z, ns, changes)
	// A flag-2 write still runs its shape pass: an observer on the wire sees
	// the change and slope partners re-derive their climb.
	p.wireReshaped(x, y, z, state, ns, changes)
	p.wireWake(x, y, z, changes)
}

// wireTargetStrength is the vanilla calculateTargetStrength.
func (p *Physics) wireTargetStrength(x, y, z int32) int {
	block := p.wireBlockSignal(x, y, z)
	if block == 15 {
		return 15
	}
	in := p.wireIncomingSignal(x, y, z)
	if in > block {
		return in
	}
	return block
}

// wireBlockSignal is the strongest direct signal of the neighbours, with the
// wire's should-signal gate off so wires answer nothing.
// vanilla: getBlockSignal
func (p *Physics) wireBlockSignal(x, y, z int32) int {
	prev := p.wireShouldSignal
	p.wireShouldSignal = false
	defer func() { p.wireShouldSignal = prev }()
	best := 0
	for _, d := range updateNeighborOrder {
		gx, gy, gz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		if s := p.signalThrough(gx, gy, gz, blocklogic.Opposite(d)); s > best {
			best = s
		}
	}
	return best
}

// wireIncomingSignal is the strongest neighbouring wire's power minus one,
// counting the two ways a wire reaches around a conductor.
// vanilla: getIncomingWireSignal
func (p *Physics) wireIncomingSignal(x, y, z int32) int {
	r := p.w.readBlocks()
	defer r.done()
	best := 0
	take := func(x, y, z int32) {
		state := r.at(x, y, z)
		if signalKindOf(state) != kindWire {
			return
		}
		if power := registry.StatePower(state); power > best {
			best = power
		}
	}
	for _, d := range [4]int{dirNorth, dirSouth, dirEast, dirWest} {
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		take(nx, ny, nz)
		if r.conductor(nx, ny, nz) {
			if !r.conductor(x, y+1, z) {
				take(nx, ny+1, nz)
			}
		} else {
			take(nx, ny-1, nz)
		}
	}
	if best-1 > 0 {
		return best - 1
	}
	return 0
}

// wireWake wakes the six neighbours of the wire's cell and of each of its six
// neighbours, so a block two steps away (beside the conductor under the wire)
// hears the change. Vanilla's HashSet iterates the seven in a fixed order, kept
// here as a list.
// vanilla: updatePowerStrength
func (p *Physics) wireWake(x, y, z int32, changes *[]BlockChange) {
	p.wireWakeNeighbors(x, y, z, changes)
	for _, d := range updateNeighborOrder {
		p.wireWakeNeighbors(x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2], changes)
	}
}

// wireWakeNeighbors gives one cell's six neighbours their neighbour update.
// vanilla: Level.updateNeighborsAt
func (p *Physics) wireWakeNeighbors(x, y, z int32, changes *[]BlockChange) {
	for _, d := range updateNeighborOrder {
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		p.redstoneNeighborChanged(nx, ny, nz, p.w.BlockAt(nx, ny, nz), changes, false)
	}
}

// wireOnPlace evaluates a placed wire's power, wakes the cells above and below,
// and wakes the wires stepping around its neighbours.
// vanilla: RedStoneWireBlock.onPlace
func (p *Physics) wireOnPlace(x, y, z int32, changes *[]BlockChange) {
	p.wireUpdatePower(x, y, z, changes)
	for _, d := range [2]int{dirDown, dirUp} {
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		p.redstoneNeighborChanged(nx, ny, nz, p.w.BlockAt(nx, ny, nz), changes, false)
	}
	p.wireCornerChanges(x, y, z, changes)
}

// wireAfterRemoval wakes the six neighbours of each cell around the removed
// wire (the two-step reach of a power wake), re-evaluates the old power at its
// cell so a lit wire wakes the line it fed, and wakes the corner wires.
// vanilla: affectNeighborsAfterRemoval
func (p *Physics) wireAfterRemoval(x, y, z int32, oldState uint16, changes *[]BlockChange) {
	for _, d := range updateNeighborOrder {
		p.wireWakeNeighbors(x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2], changes)
	}
	if isWire[registry.BlockOf(p.w.BlockAt(x, y, z))] {
		p.wireUpdatePower(x, y, z, changes)
	} else {
		oldPower := wirePowerOfState(oldState)
		target := p.wireTargetStrength(x, y, z)
		if oldPower != target {
			p.wireWake(x, y, z, changes)
		}
	}
	p.wireCornerChanges(x, y, z, changes)
}

func wirePowerOfState(state uint16) int {
	return registry.StatePower(state)
}

// wireCornerChanges wakes each horizontal neighbour of the wire and the wire
// climbing over or diving under it.
// vanilla: updateNeighborsOfNeighboringWires
func (p *Physics) wireCornerChanges(x, y, z int32, changes *[]BlockChange) {
	for _, d := range []int{dirNorth, dirSouth, dirEast, dirWest} {
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		p.wireCornerChangeAt(nx, ny, nz, changes)
	}
	for _, d := range []int{dirNorth, dirSouth, dirEast, dirWest} {
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		if p.w.ConductorAt(nx, ny, nz) {
			p.wireCornerChangeAt(nx, ny+1, nz, changes)
		} else {
			p.wireCornerChangeAt(nx, ny-1, nz, changes)
		}
	}
}

// wireCornerChangeAt wakes a wire at the position and its six neighbours.
// vanilla: checkCornerChangeAt
func (p *Physics) wireCornerChangeAt(x, y, z int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if !isWire[registry.BlockOf(state)] {
		return
	}
	p.redstoneNeighborChanged(x, y, z, state, changes, false)
	for _, d := range updateNeighborOrder {
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		p.redstoneNeighborChanged(nx, ny, nz, p.w.BlockAt(nx, ny, nz), changes, false)
	}
}

// clickWire toggles a cross or a dot between the two and gives the neighbours
// their shape updates. It reports whether it wrote; a click that re-derives the
// same state is a PASS the held item answers.
// vanilla: RedStoneWireBlock.useWithoutItem
func (p *Physics) clickWire(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	if !wireIsCross(state) && !wireIsDot(state) {
		return false
	}
	power := registry.StatePower(state)
	// wasDot is the template's dot, not the clicked state's: a cross flips to
	// the default dot and a dot to the cross.
	newConns := blocklogic.WireShape(p.w, x, y, z, wireIsCross(state))
	ns := blocklogic.WireState(power, newConns[2], newConns[0], newConns[1], newConns[3])
	if ns == state {
		return false
	}
	p.setClient(x, y, z, ns, changes)
	p.wireWakeNeighbors(x, y, z, changes)
	p.wireReshaped(x, y, z, state, ns, changes)
	p.wireUpdatesOnShapeChange(x, y, z, blocklogic.WireSides(state), newConns, changes)
	return true
}

// wireUpdatesOnShapeChange wakes the six neighbours (minus the face toward the
// wire) of each horizontal conductor whose side connectedness the click
// flipped.
// vanilla: RedStoneWireBlock.updatesOnShapeChange
func (p *Physics) wireUpdatesOnShapeChange(x, y, z int32, oldConns, newConns [4]int, changes *[]BlockChange) {
	for _, d := range []int{dirNorth, dirEast, dirSouth, dirWest} {
		if (oldConns[blocklogic.WireSideIndex(d)] != wireNone) == (newConns[blocklogic.WireSideIndex(d)] != wireNone) {
			continue
		}
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		if !p.w.ConductorAt(nx, ny, nz) {
			continue
		}
		for _, nd := range updateNeighborOrder {
			if nd == blocklogic.Opposite(d) {
				continue
			}
			qx, qy, qz := nx+dirDelta[nd][0], ny+dirDelta[nd][1], nz+dirDelta[nd][2]
			p.redstoneNeighborChanged(qx, qy, qz, p.w.BlockAt(qx, qy, qz), changes, false)
		}
	}
}
