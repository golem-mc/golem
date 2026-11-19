package world

import (
	"github.com/golem-mc/golem/registry"
)

// Rail shapes: a rail landing (placed, set by command or moved by piston)
// derives its shape from the neighbouring rails and re-links them, and a plain
// rail with three rail columns around it re-derives when a signal source
// changes beside it. This file is the engine's twin of vanilla's RailState; the
// powered family's POWERED re-read is power, not shape, and lives in
// redstone_rail.go.
// vanilla: BaseRailBlock.onPlace, RailBlock.updateState, RailState

// blockSet marks the given blocks in a table indexed by Block.
func blockSet(blocks ...registry.Block) *[registry.BlockCount]bool {
	t := new([registry.BlockCount]bool)
	for _, b := range blocks {
		t[b] = true
	}
	return t
}

var (
	// railBlocks are the four rails, the BaseRailBlock family sharing the
	// minecraft:rails tag.
	// vanilla: BaseRailBlock.isRail
	railBlocks = blockSet(registry.BlockRail, registry.BlockPoweredRail,
		registry.BlockDetectorRail, registry.BlockActivatorRail)
	// straightRailBlocks are the powered, detector and activator rails, whose
	// shape carries no curves (vanilla's isStraight).
	straightRailBlocks = blockSet(registry.BlockPoweredRail,
		registry.BlockDetectorRail, registry.BlockActivatorRail)
)

// railConnOffsets is each shape's two ends as offsets from the rail; an
// ascending shape's raised end reaches over its neighbour.
// vanilla: RailState.updateConnections
var railConnOffsets = map[string][2][3]int32{
	"north_south":     {{0, 0, -1}, {0, 0, 1}},
	"east_west":       {{-1, 0, 0}, {1, 0, 0}},
	"ascending_east":  {{-1, 0, 0}, {1, 1, 0}},
	"ascending_west":  {{-1, 1, 0}, {1, 0, 0}},
	"ascending_north": {{0, 1, -1}, {0, 0, 1}},
	"ascending_south": {{0, 0, -1}, {0, 1, 1}},
	"south_east":      {{1, 0, 0}, {0, 0, 1}},
	"south_west":      {{-1, 0, 0}, {0, 0, 1}},
	"north_west":      {{-1, 0, 0}, {0, 0, -1}},
	"north_east":      {{1, 0, 0}, {0, 0, -1}},
}

// railState is a rail and the connections its shape reaches, at the heights it
// meets them. The state a placement first lands on is rules.PlacementOverrides'
// axis choice.
// vanilla: RailState
type railState struct {
	p     *Physics
	state uint16
	// shape is the shape the rail carries or is about to be written with.
	shape string
	// straight is false only for the plain rail, which has curves.
	straight bool
	x, y, z  int32
	conns    [][3]int32
}

// railAround builds the rail state at (x, y, z) from its shape; a non-rail
// returns false.
func (p *Physics) railAround(x, y, z int32, state uint16) (railState, bool) {
	block := registry.BlockOf(state)
	if !railBlocks[block] {
		return railState{}, false
	}
	r := railState{p: p, state: state, shape: registry.StateValue(state, registry.PropShape),
		straight: straightRailBlocks[block], x: x, y: y, z: z}
	r.resetConnections()
	return r, true
}

// resetConnections re-reads the connections off r.shape.
func (r *railState) resetConnections() {
	r.conns = r.conns[:0]
	for _, off := range railConnOffsets[r.shape] {
		r.conns = append(r.conns, [3]int32{r.x + off[0], r.y + off[1], r.z + off[2]})
	}
}

// railAtColumn finds the rail a connection reaches: the cell, then one above,
// then one below.
// vanilla: RailState.getRail
func (p *Physics) railAtColumn(x, y, z int32) (railState, bool) {
	for _, dy := range [3]int32{0, 1, -1} {
		if r, ok := p.railAround(x, y+dy, z, p.w.BlockAt(x, y+dy, z)); ok {
			return r, true
		}
	}
	return railState{}, false
}

// railInCell reports a rail in exactly the cell, the ascending shapes' read one
// up.
// vanilla: BaseRailBlock.isRail
func (p *Physics) railInCell(x, y, z int32) bool {
	return railBlocks[registry.BlockOf(p.w.BlockAt(x, y, z))]
}

// hasConn reports whether a connection stands in column (x, z); only the column
// decides, not the height.
// vanilla: RailState.hasConnection
func (r *railState) hasConn(x, z int32) bool {
	for _, c := range r.conns {
		if c[0] == x && c[2] == z {
			return true
		}
	}
	return false
}

// removeSoftConnections drops connections no rail answers and re-points the
// rest at the cell the answering rail is in.
// vanilla: RailState.removeSoftConnections
func (r *railState) removeSoftConnections() {
	kept := r.conns[:0]
	for _, c := range r.conns {
		nb, ok := r.p.railAtColumn(c[0], c[1], c[2])
		if !ok || !nb.hasConn(r.x, r.z) {
			continue
		}
		kept = append(kept, [3]int32{nb.x, nb.y, nb.z})
	}
	r.conns = kept
}

// canConnectTo reports whether this rail takes the other's connection: it
// already reaches that column, or its shape is not held to the two ends it has.
// vanilla: RailState.canConnectTo
func (r *railState) canConnectTo(other *railState) bool {
	return r.hasConn(other.x, other.z) || len(r.conns) != 2
}

// neighbourConnects is place()'s gate on one column: a rail there (level, one
// up or one down) that takes the connection.
// vanilla: RailState.hasNeighborRail
func (r *railState) neighbourConnects(dx, dz int32) bool {
	nb, ok := r.p.railAtColumn(r.x+dx, r.y, r.z+dz)
	if !ok {
		return false
	}
	nb.removeSoftConnections()
	return nb.canConnectTo(r)
}

// updateDir re-derives the shape from the rails around and re-links the
// neighbours it reaches, when it differs from the cell's (always, on the
// placement's first call). The signal preference picks a three-way junction's
// curve.
// vanilla: BaseRailBlock.updateDir, RailState.place
func (r *railState) updateDir(first bool, hasSignal bool, changes *[]BlockChange) {
	n, s := r.neighbourConnects(0, -1), r.neighbourConnects(0, 1)
	w, e := r.neighbourConnects(-1, 0), r.neighbourConnects(1, 0)
	ns, we := n || s, w || e
	shape := ""
	switch {
	case ns && !we:
		shape = "north_south"
	case we && !ns:
		shape = "east_west"
	}
	if !r.straight {
		switch {
		case s && e && !n && !w:
			shape = "south_east"
		case s && w && !n && !e:
			shape = "south_west"
		case n && w && !s && !e:
			shape = "north_west"
		case n && e && !s && !w:
			shape = "north_east"
		}
	}
	if shape == "" {
		// Both or neither axis answers: keep the cell's shape, or with rails on
		// both axes take the curve the signal preference orders.
		switch {
		case ns && we:
			shape = r.shape
		case ns:
			shape = "north_south"
		case we:
			shape = "east_west"
		}
		if !r.straight {
			if hasSignal {
				if s && e {
					shape = "south_east"
				}
				if s && w {
					shape = "south_west"
				}
				if n && e {
					shape = "north_east"
				}
				if n && w {
					shape = "north_west"
				}
			} else {
				if n && w {
					shape = "north_west"
				}
				if n && e {
					shape = "north_east"
				}
				if s && w {
					shape = "south_west"
				}
				if s && e {
					shape = "south_east"
				}
			}
		}
	}
	shape = r.ascend(shape)
	if shape == "" {
		shape = r.shape
	}
	r.shape = shape
	if id, ok := registry.WithProp(r.state, registry.PropShape, r.shape); ok {
		r.state = id
	}
	if !first && r.p.w.BlockAt(r.x, r.y, r.z) == r.state {
		return
	}
	r.resetConnections()
	r.p.setShaped(r.x, r.y, r.z, r.state, changes)
	for _, c := range r.conns {
		nb, ok := r.p.railAtColumn(c[0], c[1], c[2])
		if !ok {
			continue
		}
		nb.removeSoftConnections()
		if !nb.canConnectTo(r) {
			continue
		}
		nb.link(r, changes)
	}
}

// ascend raises a straight shape where a rail is one block up beyond its end,
// the later end winning when both are; curves and blanks pass through.
// vanilla: RailState.place, RailState.connectTo
func (r *railState) ascend(shape string) string {
	switch shape {
	case "north_south":
		if r.p.railInCell(r.x, r.y+1, r.z-1) {
			shape = "ascending_north"
		}
		if r.p.railInCell(r.x, r.y+1, r.z+1) {
			shape = "ascending_south"
		}
	case "east_west":
		if r.p.railInCell(r.x+1, r.y+1, r.z) {
			shape = "ascending_east"
		}
		if r.p.railInCell(r.x-1, r.y+1, r.z) {
			shape = "ascending_west"
		}
	}
	return shape
}

// link re-derives the shape counting rail among the connections (a neighbour's
// placement or re-derivation) and records the result.
// vanilla: RailState.connectTo
func (r *railState) link(rail *railState, changes *[]BlockChange) {
	r.conns = append(r.conns, [3]int32{rail.x, rail.y, rail.z})
	n, s := r.hasConn(r.x, r.z-1), r.hasConn(r.x, r.z+1)
	w, e := r.hasConn(r.x-1, r.z), r.hasConn(r.x+1, r.z)
	shape := ""
	if n || s {
		shape = "north_south"
	}
	if w || e {
		shape = "east_west"
	}
	if !r.straight {
		switch {
		case s && e && !n && !w:
			shape = "south_east"
		case s && w && !n && !e:
			shape = "south_west"
		case n && w && !s && !e:
			shape = "north_west"
		case n && e && !s && !w:
			shape = "north_east"
		}
	}
	shape = r.ascend(shape)
	if shape == "" {
		shape = "north_south"
	}
	if shape == r.shape {
		return
	}
	r.shape = shape
	if id, ok := registry.WithProp(r.state, registry.PropShape, r.shape); ok {
		r.p.setShaped(r.x, r.y, r.z, id, changes)
	}
}

// railPotentialConnections counts the four columns around with a rail in them,
// one up or down included.
// vanilla: RailState.countPotentialConnections
func (p *Physics) railPotentialConnections(x, y, z int32) int {
	count := 0
	for _, d := range horizontal4 {
		if _, ok := p.railAtColumn(x+d[0], y, z+d[1]); ok {
			count++
		}
	}
	return count
}

// railBlockUpdate answers a rail's own landing (the shape decision, skipped
// when a rail rewrites its own kind) or a neighbour's change (only a plain rail
// with three rail columns re-derives, and only on a signal source's change).
// The powered family reads POWERED on both, before the plain-rail gate returns.
// vanilla: BaseRailBlock.onPlace, BaseRailBlock.neighborChanged,
// RailBlock.updateState, PoweredRailBlock.updateState
func (p *Physics) railBlockUpdate(x, y, z int32, state uint16, placed bool, old uint16, changes *[]BlockChange) {
	if placed {
		if registry.BlockOf(old) == registry.BlockOf(state) {
			return
		}
		if r, ok := p.railAround(x, y, z, state); ok {
			r.updateDir(true, p.hasNeighborSignal(x, y, z), changes)
		}
		// Only straight rails run neighborChanged on themselves after the shape
		// pass, so only they pop when unsupported; a plain rail waits for a
		// neighbour change.
		// vanilla: BaseRailBlock.updateState
		if straightRailBlocks[registry.BlockOf(state)] && p.supportCheck(x, y, z, p.w.BlockAt(x, y, z), anyDirection, changes) {
			return
		}
		p.railUpdatePowered(x, y, z, changes)
		// A detector rail landing under a cart takes its press, as its onPlace
		// does after the shape pass.
		// vanilla: DetectorRailBlock.onPlace
		p.detectorRailCheckPressed(x, y, z, changes)
		return
	}
	if poweredRailBlocks[registry.BlockOf(state)] {
		p.railUpdatePowered(x, y, z, changes)
	}
	if registry.BlockOf(state) != registry.BlockRail ||
		!registry.IsRedstoneSignalSourceState(old) ||
		p.railPotentialConnections(x, y, z) != 3 {
		return
	}
	if r, ok := p.railAround(x, y, z, state); ok {
		r.updateDir(false, p.hasNeighborSignal(x, y, z), changes)
	}
}
