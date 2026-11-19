package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The powered shelf side chain: powered shelves of one facing in a row link up
// to three deep, so their three cells each swap with one hotbar slot.
// vanilla: SideChainPartBlock, SideChainPart

// sideChainPart is a shelf's place in a chain, named for its free side: a right
// part has the chain on its left, leaving its right free to grow into; center
// has the chain on both sides.
// vanilla: SideChainPart
type sideChainPart uint8

const (
	partUnconnected sideChainPart = 0
	partRight       sideChainPart = 1
	partCenter      sideChainPart = 2
	partLeft        sideChainPart = 3
)

// sideChainPartOf reads the side_chain block-state property.
func sideChainPartOf(state uint16) sideChainPart {
	switch registry.StateValue(state, registry.PropSideChain) {
	case "left":
		return partLeft
	case "center":
		return partCenter
	case "right":
		return partRight
	}
	return partUnconnected
}

// sideChainPartName writes the side_chain block-state property.
func sideChainPartName(part sideChainPart) string {
	switch part {
	case partLeft:
		return "left"
	case partCenter:
		return "center"
	case partRight:
		return "right"
	}
	return "unconnected"
}

func (part sideChainPart) isConnected() bool { return part != partUnconnected }

// connectsTowards is whether the chain may continue from this shelf toward
// the block the walk is heading for. vanilla: SideChainPart.connectsTowards
func (part sideChainPart) connectsTowards(end sideChainPart) bool {
	return part == partCenter || part == end
}

// isUnconnectableOrChainEnd stops a side-chain walk: only a center shelf
// lets the walk step past it. vanilla: SideChainPart.isUnconnectableOrChainEnd
func (part sideChainPart) isUnconnectableOrChainEnd() bool { return part != partCenter }

// The part transitions: a shelf gaining a chain neighbour on its left becomes
// free on the right, or not free at all once both sides are taken.
// vanilla: SideChainPart.whenConnectedToTheLeft, whenConnectedToTheRight,
// whenDisconnectedFromTheRight, whenDisconnectedFromTheLeft
func (part sideChainPart) whenConnectedToTheLeft() sideChainPart {
	switch part {
	case partUnconnected, partRight:
		return partRight
	default:
		return partCenter
	}
}

func (part sideChainPart) whenConnectedToTheRight() sideChainPart {
	switch part {
	case partUnconnected, partLeft:
		return partLeft
	default:
		return partCenter
	}
}

func (part sideChainPart) whenDisconnectedFromTheRight() sideChainPart {
	switch part {
	case partUnconnected, partLeft:
		return partUnconnected
	default:
		return partRight
	}
}

func (part sideChainPart) whenDisconnectedFromTheLeft() sideChainPart {
	switch part {
	case partUnconnected, partRight:
		return partUnconnected
	default:
		return partLeft
	}
}

// maxShelfChain is vanilla's SideChainPartBlock.getMaxChainLength.
const maxShelfChain = 3

// shelfNeighbor is one shelf of a chain next to another, with the part it
// is currently in.
type shelfNeighbor struct {
	pos   [3]int32
	part  sideChainPart
	empty bool
}

// shelfBlock marks the shelf blocks of every wood.
var shelfBlock = registry.BlocksOf(func(b registry.Block) bool {
	return b.Type() == registry.BlockTypeShelf
})

// shelfConnectable is whether the state is a shelf another powered shelf of
// facing can link to. vanilla: SideChainPartBlock.isConnectableToThisBlock
func shelfConnectable(state uint16, facing int) bool {
	return shelfBlock[registry.BlockOf(state)] &&
		registry.StatePowered(state) && registry.StateFacing(state) == facing
}

// shelfAt is the shelf at x, y, z if the powered shelf of facing can link to
// it, with its part.
func (p *Physics) shelfAt(x, y, z int32, facing int) shelfNeighbor {
	state := p.w.BlockAt(x, y, z)
	if !shelfConnectable(state, facing) {
		return shelfNeighbor{empty: true}
	}
	return shelfNeighbor{pos: [3]int32{x, y, z}, part: sideChainPartOf(state)}
}

// shelfNeighborAt is a shelf's neighbour along d, empty unless a powered shelf
// of the same facing.
func (p *Physics) shelfNeighborAt(x, y, z int32, facing, d int) shelfNeighbor {
	nx, ny, nz := pistonShift(x, y, z, d, 1)
	return p.shelfAt(nx, ny, nz, facing)
}

// shelfWalk steps along one chain side, adding each position: the left walk
// runs toward the far end and so must addFirst, the right addLast, for the
// chain to come out leftmost first.
// vanilla: SideChainPartBlock.getAllBlocksConnectedTo
func (p *Physics) shelfWalk(x, y, z int32, facing, d int, end sideChainPart, add func([3]int32)) {
	for step := 1; step < maxShelfChain; step++ {
		nx, ny, nz := pistonShift(x, y, z, d, step)
		neighbor := p.shelfAt(nx, ny, nz, facing)
		if neighbor.empty {
			return
		}
		if neighbor.part.connectsTowards(end) {
			add(neighbor.pos)
		}
		if neighbor.part.isUnconnectableOrChainEnd() {
			return
		}
	}
}

// shelfChainOf walks a powered shelf's chain, leftmost first from its facing;
// nil for an unpowered or non-shelf block.
func (p *Physics) shelfChainOf(x, y, z int32) [][3]int32 {
	state := p.w.BlockAt(x, y, z)
	facing := registry.StateFacing(state)
	if !shelfConnectable(state, facing) {
		return nil
	}
	chain := [][3]int32{{x, y, z}}
	p.shelfWalk(x, y, z, facing, blocklogic.Clockwise(facing), partLeft, func(pos [3]int32) {
		chain = append([][3]int32{pos}, chain...)
	})
	p.shelfWalk(x, y, z, facing, blocklogic.CounterClockwise(facing), partRight, func(pos [3]int32) {
		chain = append(chain, pos)
	})
	return chain
}

// ShelfChain is shelfChainOf for outside callers. It takes the engine's lock,
// which shelfChainOf cannot under the neighbour updates it runs in.
func (p *Physics) ShelfChain(x, y, z int32) [][3]int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shelfChainOf(x, y, z)
}

// shelfCanConnect reports whether existing and current shelves together fit the
// max chain length.
// vanilla: SideChainPartBlock.canConnect
func shelfCanConnect(existing, current int) bool {
	return existing > 0 && current+existing <= maxShelfChain
}

// shelfSetPart writes side_chain, skipping a missing shelf or an unchanged
// part.
func (p *Physics) shelfSetPart(x, y, z int32, part sideChainPart, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if !shelfBlocks[registry.BlockOf(state)] || sideChainPartOf(state) == part {
		return
	}
	want, _ := registry.WithProp(state, registry.PropSideChain, sideChainPartName(part))
	p.setClientShaped(x, y, z, want, changes)
}

// shelfPowerUp links a just-powered shelf with its powered neighbours, joining
// chains up to the max length. oldState is the shelf before power arrived; a
// shelf already chained, or already a powered member before, is left alone.
// vanilla: SideChainPartBlock.updateSelfAndNeighborsOnPoweringUp
func (p *Physics) shelfPowerUp(x, y, z int32, oldState uint16, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	facing := registry.StateFacing(state)
	if !shelfConnectable(state, facing) {
		return
	}
	part := sideChainPartOf(state)
	if part.isConnected() {
		// A shelf the write itself just linked needs no second link.
		return
	}
	if shelfBlocks[registry.BlockOf(oldState)] && registry.StatePowered(oldState) && sideChainPartOf(oldState).isConnected() {
		return
	}
	current := 1
	left := p.shelfNeighborAt(x, y, z, facing, blocklogic.Clockwise(facing))
	existing := 0
	if !left.empty {
		existing = len(p.shelfChainOf(left.pos[0], left.pos[1], left.pos[2]))
	}
	if shelfCanConnect(existing, current) {
		part = part.whenConnectedToTheLeft()
		p.shelfSetPart(left.pos[0], left.pos[1], left.pos[2], left.part.whenConnectedToTheRight(), changes)
		current += existing
	}
	right := p.shelfNeighborAt(x, y, z, facing, blocklogic.CounterClockwise(facing))
	existing = 0
	if !right.empty {
		existing = len(p.shelfChainOf(right.pos[0], right.pos[1], right.pos[2]))
	}
	if shelfCanConnect(existing, current) {
		part = part.whenConnectedToTheRight()
		p.shelfSetPart(right.pos[0], right.pos[1], right.pos[2], right.part.whenConnectedToTheLeft(), changes)
	}
	p.shelfSetPart(x, y, z, part, changes)
}

// shelfPowerDownNeighbors unlinks the powered neighbours of a shelf that lost
// power or is going; the transitions leave correct part names, so no re-walk is
// needed.
// vanilla: SideChainPartBlock.updateNeighborsAfterPoweringDown
func (p *Physics) shelfPowerDownNeighbors(x, y, z int32, facing int, changes *[]BlockChange) {
	left := p.shelfNeighborAt(x, y, z, facing, blocklogic.Clockwise(facing))
	if !left.empty {
		p.shelfSetPart(left.pos[0], left.pos[1], left.pos[2], left.part.whenDisconnectedFromTheRight(), changes)
	}
	right := p.shelfNeighborAt(x, y, z, facing, blocklogic.CounterClockwise(facing))
	if !right.empty {
		p.shelfSetPart(right.pos[0], right.pos[1], right.pos[2], right.part.whenDisconnectedFromTheLeft(), changes)
	}
}
