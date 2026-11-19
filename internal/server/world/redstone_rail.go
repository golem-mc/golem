package world

import "github.com/golem-mc/golem/registry"

// The powered and activator rails (one vanilla class registered twice): POWERED
// comes from adjacent redstone or a same-kind rail within eight along the line.

var poweredRailBlocks = func() *[registry.BlockCount]bool {
	t := new([registry.BlockCount]bool)
	t[registry.BlockPoweredRail] = true
	t[registry.BlockActivatorRail] = true
	return t
}()

// railPropagateLimit is the literal 8 in
// PoweredRailBlock.findPoweredRailSignal.
const railPropagateLimit = 8

// railFedLine reports whether redstone feeds a same-family rail within eight
// along the line, walking both ways.
// vanilla: PoweredRailBlock.updateState.
func (p *Physics) railFedLine(x, y, z int32, state uint16) bool {
	family := registry.BlockOf(state)
	if family == registry.BlockAir {
		return false
	}
	return p.railSignalWalk(x, y, z, state, family, true, 0) ||
		p.railSignalWalk(x, y, z, state, family, false, 0)
}

// railSignalWalk is one direction of the walk: a flat shape keeps the height, a
// slope steps down its low end or up its high end, and a flat shape also checks
// one below. distance counts rails walked, eight at most.
// vanilla: PoweredRailBlock.findPoweredRailSignal.
func (p *Physics) railSignalWalk(x, y, z int32, state uint16, family registry.Block, forward bool, distance int) bool {
	if distance >= railPropagateLimit {
		return false
	}
	shape := registry.StateValue(state, registry.PropShape)
	// An ascending shape has no axis of its own, so a slope reads as its flat
	// counterpart.
	from := shape
	i, j, k := x, y, z
	flat := true
	switch shape {
	case "north_south":
		if forward {
			k++
		} else {
			k--
		}
	case "east_west":
		if forward {
			i--
		} else {
			i++
		}
	case "ascending_east":
		from = "east_west"
		if forward {
			i--
		} else {
			i++
			j++
			flat = false
		}
	case "ascending_west":
		from = "east_west"
		if forward {
			i--
			j++
			flat = false
		} else {
			i++
		}
	case "ascending_north":
		from = "north_south"
		if forward {
			k++
		} else {
			k--
			j++
			flat = false
		}
	case "ascending_south":
		from = "north_south"
		if forward {
			k++
			j++
			flat = false
		} else {
			k--
		}
	default:
		return false
	}
	if p.railSameFamilyPowered(i, j, k, from, family, forward, distance) {
		return true
	}
	return flat && p.railSameFamilyPowered(i, j-1, k, from, family, forward, distance)
}

// railSameFamilyPowered checks a rail the walk found: same family, on the
// previous shape's axis, and POWERED, or the walk ends. One fed by adjacent
// redstone ends it powered; otherwise the walk continues.
// vanilla: PoweredRailBlock.isSameRailWithPower.
func (p *Physics) railSameFamilyPowered(x, y, z int32, from string, family registry.Block, forward bool, distance int) bool {
	id := p.w.BlockAt(x, y, z)
	if registry.BlockOf(id) != family {
		return false
	}
	if !railSameAxis(from, registry.StateValue(id, registry.PropShape)) || !registry.StatePowered(id) {
		return false
	}
	if p.hasNeighborSignal(x, y, z) {
		return true
	}
	return p.railSignalWalk(x, y, z, id, family, forward, distance+1)
}

// railSameAxis reports whether shape to continues a walk from shape from: a
// straight shape forbids the other axis.
// vanilla: PoweredRailBlock.isSameRailWithPower.
func railSameAxis(from, to string) bool {
	if from == "east_west" && (to == "north_south" || to == "ascending_north" || to == "ascending_south") {
		return false
	}
	if from == "north_south" && (to == "east_west" || to == "ascending_east" || to == "ascending_west") {
		return false
	}
	return true
}

// railUpdatePowered sets POWERED from the signal beside the rail or along its
// line. A change wakes the support below and, for a slope, the step above, two
// cells the write's own wake is too near to reach.
// vanilla: PoweredRailBlock.updateState.
func (p *Physics) railUpdatePowered(x, y, z int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if !poweredRailBlocks[registry.BlockOf(state)] {
		return
	}
	shape := registry.StateValue(state, registry.PropShape)
	powered := p.hasNeighborSignal(x, y, z) || p.railFedLine(x, y, z, state)
	if registry.StatePowered(state) == powered {
		return
	}
	next, ok := registry.WithBool(state, registry.PropPowered, powered)
	if !ok || !p.railWrite(x, y, z, next, changes) {
		return
	}
	p.wireWakeNeighbors(x, y-1, z, changes)
	if _, slope := ascendingTo[shape]; slope {
		p.wireWakeNeighbors(x, y+1, z, changes)
	}
}

// railWrite is a flags-3 write: clients, the six-neighbour update (a rail
// beside re-reads its power) and the shape pass an observer pulses on.
func (p *Physics) railWrite(x, y, z int32, id uint16, changes *[]BlockChange) bool {
	old := p.w.BlockAt(x, y, z)
	if id == old {
		return false
	}
	p.setClient(x, y, z, id, changes)
	p.wakeNeighbors(x, y, z, old, id, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
	return true
}

// The detector rail. A cart whose box crosses the search area sets POWERED, and
// a look 20 ticks out holds it or clears it once the cart has gone. Its signal
// is read above (weak 15, strong into its support).
// vanilla: DetectorRailBlock

// detectorRailCheckPressed compares the press against the carts in the search
// box, writes with flags 3, wakes the rails its shape connects (the powered
// lines it feeds) and books the next look while pressed.
// vanilla: DetectorRailBlock.checkPressed
func (p *Physics) detectorRailCheckPressed(x, y, z int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockDetectorRail {
		return
	}
	// An unsupported rail is left for the neighbour check: checkPressed returns
	// without dropping it.
	// vanilla: DetectorRailBlock.checkPressed
	if !canSurvive(p.w, x, y, z, state) {
		return
	}
	pressed := p.railCarts != nil && p.railCarts.CartCrossing(detectorRailSearchBox(x, y, z))
	if registry.StatePowered(state) == pressed {
		p.analogOutputChanged(x, y, z, p.now)
		if pressed {
			p.scheduleBooked(p.redstoneBooked, x, y, z, p.now+plateTickDelay, redstoneTickEvent)
		}
		return
	}
	next, ok := registry.WithBool(state, registry.PropPowered, pressed)
	if !ok || !p.railWrite(x, y, z, next, changes) {
		return
	}
	// The connected rails re-read their power, so the powered line a lit
	// detector stands in lights from it.
	// vanilla: DetectorRailBlock.updatePowerToConnected
	if r, ok := p.railAround(x, y, z, state); ok {
		for _, c := range r.conns {
			nb := p.w.BlockAt(c[0], c[1], c[2])
			p.redstoneNeighborChanged(c[0], c[1], c[2], nb, changes, false)
		}
	}
	// The rail's own cell wakes first, then its support.
	// vanilla: DetectorRailBlock.checkPressed
	p.wireWakeNeighbors(x, y, z, changes)
	p.wireWakeNeighbors(x, y-1, z, changes)
	p.analogOutputChanged(x, y, z, p.now)
	p.scheduleBooked(p.redstoneBooked, x, y, z, p.now+plateTickDelay, redstoneTickEvent)
}

// detectorRailTick looks again 20 ticks after a press, holding or clearing it.
// vanilla: DetectorRailBlock.tick
func (p *Physics) detectorRailTick(x, y, z int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockDetectorRail || registry.StateValue(state, registry.PropPowered) == "false" {
		return
	}
	p.detectorRailCheckPressed(x, y, z, changes)
}

// detectorRailSearchBox is the cell inset 0.2 from its sides and top.
// vanilla: DetectorRailBlock.getSearchBB
func detectorRailSearchBox(x, y, z int32) Box {
	const inset = 0.2
	return Box{
		MinX: float64(x) + inset, MinY: float64(y), MinZ: float64(z) + inset,
		MaxX: float64(x+1) - inset, MaxY: float64(y+1) - inset, MaxZ: float64(z+1) - inset,
	}
}

// RailCartSource reports whether a minecart crosses a box. The server owns the
// carts and sets it once they are live; tests can stub it.
type RailCartSource interface {
	// CartCrossing reports whether any live minecart's box intersects box.
	CartCrossing(box Box) bool
	// CartContainerSignal is the comparator signal of the first live container
	// minecart crossing box; ok is false when there is none.
	CartContainerSignal(box Box) (signal int, ok bool)
}

// SetRailCartSource sets the engine's minecart source; without one no cart
// crosses anything.
func (p *Physics) SetRailCartSource(c RailCartSource) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.railCarts = c
}

// RailCartsMissing reports whether no minecart source is set; the tick's setup
// checks it like the toucher's.
func (p *Physics) RailCartsMissing() bool { return p.railCarts == nil }
