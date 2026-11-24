package world

import (
	"github.com/golem-mc/golem/registry"
)

// Scaffolding: a piece's distance is the scaffolding below's, 0 over a sturdy
// top face, otherwise one past the closest piece beside it, up to the ceiling.
// A tick finding a piece at the ceiling sends it down, as a falling body if it
// already stood there, with its loot if a neighbour change pushed it out.
// bottom draws the plate under a piece not standing on scaffolding.
// vanilla: ScaffoldingBlock

// scaffoldingStabilityMax is STABILITY_MAX_DISTANCE.
const scaffoldingStabilityMax = 7

// scaffoldingTickDelay is TICK_DELAY, the game ticks out a placement and every
// neighbour change book the recompute.
const scaffoldingTickDelay = 1

const scaffoldingTickEvent = 1<<5 | 1<<6 | 1<<7 | 5

func (p *Physics) scheduleScaffolding(x, y, z int32, due int64) {
	p.scheduleBooked(p.scaffoldingBooked, x, y, z, due, scaffoldingTickEvent)
}

// scaffoldingDistanceOf reads a state's distance, the ceiling if missing.
func scaffoldingDistanceOf(state uint16) int {
	return stateIntOr(state, registry.PropDistance, scaffoldingStabilityMax)
}

// scaffoldingDistance is the distance the blocks around make: the piece below's
// (which a closer side, plus one, may undercut), 0 over a sturdy top, else the
// ceiling.
// vanilla: ScaffoldingBlock.getDistance
func scaffoldingDistance(w *World, x, y, z int32) int {
	below := w.BlockAt(x, y-1, z)
	d := scaffoldingStabilityMax
	if int(below) < registry.StateCount {
		switch {
		case registry.BlockOf(below) == registry.BlockScaffolding:
			d = scaffoldingDistanceOf(below)
		case registry.FaceSturdy(below, dirUp, registry.SupportFull):
			return 0
		}
	}
	for _, side := range horizontal4 {
		s := w.BlockAt(x+side[0], y, z+side[1])
		if registry.BlockOf(s) != registry.BlockScaffolding {
			continue
		}
		if v := scaffoldingDistanceOf(s) + 1; v < d {
			d = v
		}
	}
	return d
}

// scaffoldingBottom reports a piece with nonzero distance that does not stand
// on scaffolding.
// vanilla: ScaffoldingBlock.isBottom
func scaffoldingBottom(w *World, x, y, z int32, distance int) bool {
	if distance == 0 {
		return false
	}
	return registry.BlockOf(w.BlockAt(x, y-1, z)) != registry.BlockScaffolding
}

// ScaffoldingRefusesPlacement is the gate a scaffolding placement runs instead
// of canSurvive (ScaffoldingBlockItem.mustSurvive is false): a distance at the
// ceiling is refused, and a cell already holding a piece is left to the
// extension path, which does not test distance.
// vanilla: ScaffoldingBlockItem.updatePlacementContext
func (w *World) ScaffoldingRefusesPlacement(x, y, z int32) bool {
	if registry.BlockOf(w.BlockAt(x, y, z)) == registry.BlockScaffolding {
		return false
	}
	return scaffoldingDistance(w, x, y, z) == scaffoldingStabilityMax
}

// ScaffoldingStateForPlacement is a placement's state: the distance and bottom
// the blocks around make. Water is taken in the waterlogging pass that runs
// after.
// vanilla: ScaffoldingBlock.getStateForPlacement
func (w *World) ScaffoldingStateForPlacement(placed uint16, x, y, z int32) uint16 {
	if int(placed) >= registry.StateCount {
		return placed
	}
	distance := scaffoldingDistance(w, x, y, z)
	if s, ok := registry.WithInt(placed, registry.PropDistance, distance); ok {
		s, _ = registry.WithBool(s, registry.PropBottom, scaffoldingBottom(w, x, y, z, distance))
		return s
	}
	return placed
}

// scaffoldingTick writes the distance and bottom the blocks around now make,
// where they differ. A piece at the ceiling falls as a body if it already stood
// there (a command, piston or landing put it there), or drops with its loot if
// a neighbour change pushed it out.
// vanilla: ScaffoldingBlock.tick
func (p *Physics) scaffoldingTick(x, y, z int32, state uint16, changes *[]BlockChange, falls *[]FallingSpawn) {
	if registry.BlockOf(state) != registry.BlockScaffolding {
		return // not scaffolding any more; the queue entry is spent
	}
	stoodAt := scaffoldingDistanceOf(state)
	distance := scaffoldingDistance(p.w, x, y, z)
	newState, _ := registry.WithInt(state, registry.PropDistance, distance)
	newState, _ = registry.WithBool(newState, registry.PropBottom, scaffoldingBottom(p.w, x, y, z, distance))
	if distance == scaffoldingStabilityMax {
		if stoodAt == scaffoldingStabilityMax {
			p.fallSpawn(x, y, z, newState, changes, falls)
		} else {
			p.popBlock(x, y, z, state, changes)
		}
		return
	}
	if newState != state {
		p.setShaped(x, y, z, newState, changes)
	}
}
