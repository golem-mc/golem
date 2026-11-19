package world

import (
	"github.com/golem-mc/golem/registry"
)

// BlockUpdates selects the independent server-side effects of Level.setBlock.
type BlockUpdates struct {
	Clients, Neighbors, Shapes, OnPlace, EntityEffects, MovedByPiston, SkipWireShape bool
}

// NotifyBlockSet runs removal, placement and neighbour hooks, without the shape pass.
// vanilla: LevelChunk.setBlockState, Level.setBlock
func (p *Physics) NotifyBlockSet(x, y, z int32, old uint16, u BlockUpdates) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	var changes []BlockChange
	removed := registry.BlockOf(old) != registry.BlockOf(state) || railBlocks[registry.BlockOf(state)]
	u.EntityEffects = u.EntityEffects && !BlockEntityKept(old, state)
	oldBlock := registry.BlockOf(old)
	movedSource := u.MovedByPiston && (isWire[oldBlock] || isTorch[oldBlock])
	if removed && (u.Neighbors || u.MovedByPiston) && !movedSource {
		p.redstoneAfterRemoval(x, y, z, old, u.MovedByPiston, &changes)
	}
	if u.EntityEffects {
		p.containerRemoved(x, y, z, old, state)
	}
	if u.OnPlace {
		p.redstoneOnPlace(x, y, z, old, &changes)
		p.updateAsNeighbor(x, y, z, &changes, true, old, state, [3]int32{})
	}
	if u.Neighbors && p.w.BlockAt(x, y, z) == state {
		for _, d := range [6][3]int32{{0, -1, 0}, {0, 1, 0}, {-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}} {
			p.blockNeighborChanged(x+d[0], y+d[1], z+d[2], old, &changes)
		}
		p.blockAnalogOutputChanged(x, y, z)
	}
	effects, primes := p.takeWake()
	return changes, effects, primes
}

func (p *Physics) blockAnalogOutputChanged(x, y, z int32) {
	cur := p.w.BlockAt(x, y, z)
	// Merely testing for analog output must not roll a container's loot.
	if isFullnessContainer(registry.BlockOf(cur)) {
		p.analogOutputChanged(x, y, z, p.now)
	} else if _, has := p.analogOutputOf(cur, x, y, z, dirDown); has {
		p.analogOutputChanged(x, y, z, p.now)
	}
}

// blockNeighborChanged excludes updateShape's survival and scheduling work.
func (p *Physics) blockNeighborChanged(x, y, z int32, old uint16, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	if fs := fluidHolding(state); fs.kind != fluidNone && !fluidContainer(state) {
		if fs.kind != fluidLava || p.shouldSpreadLiquid(x, y, z, changes) {
			p.schedule(x, y, z, p.now+p.tickDelayOf(fs.kind))
		}
		return
	}
	if p.spongeChanged(x, y, z, block, false, old, changes) {
		return
	}
	if block == registry.BlockFrostedIce {
		if registry.BlockOf(old) == block && p.frostedIceFewerNeighboursThan(x, y, z, frostedIceNeighboursToMelt) {
			p.meltIce(x, y, z, state, changes, &p.effects)
		}
		return
	}
	if railBlocks[block] {
		p.railBlockUpdate(x, y, z, state, false, old, changes)
		return
	}
	p.redstoneNeighborChanged(x, y, z, state, changes, false)
}

// BlockSetShapes runs the old/new indirect and direct neighbour shape passes.
func (p *Physics) BlockSetShapes(x, y, z int32, old uint16, skipWire, clients bool) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.skipWireShape, p.silentShapes = skipWire, !clients
	defer func() { p.skipWireShape, p.silentShapes = false, false }()
	var changes []BlockChange
	state := p.w.BlockAt(x, y, z)
	p.wireSlopeChanges(x, y, z, old, &changes)
	for _, d := range updateShapeOrder {
		nx, ny, nz := x+d[0], y+d[1], z+d[2]
		if skipWire && isWire[registry.BlockOf(p.w.BlockAt(nx, ny, nz))] {
			continue
		}
		n, e := len(changes), len(p.effects)
		p.updateAsNeighborParts(nx, ny, nz, &changes, false, old, state, [3]int32{-d[0], -d[1], -d[2]}, false)
		p.shapeWriteVisibility(nx, ny, nz, changes[n:], p.effects[e:])
		p.flaggedShapeUpdate(nx, ny, nz, d, &changes)
	}
	p.wireSlopeChanges(x, y, z, state, &changes)
	effects, primes := p.takeWake()
	return changes, effects, primes
}

// shapeWriteVisibility leaves destruction and its independent writes visible.
func (p *Physics) shapeWriteVisibility(x, y, z int32, changes []BlockChange, effects []Effect) {
	if !p.silentShapes {
		return
	}
	for _, e := range effects {
		if e.X == x && e.Y == y && e.Z == z && e.BreakDrop != 0 {
			return
		}
	}
	for i, ch := range changes {
		if ch.X == x && ch.Y == y && ch.Z == z {
			changes[i].Silent = !IsKnownAirState(ch.State)
			return
		}
	}
}

// BlockEntityKept reports whether a state change retains its block entity.
// vanilla: LevelChunk.setBlockState, CopperChestBlock, CopperGolemStatueBlock
func BlockEntityKept(old, state uint16) bool {
	a, b := registry.BlockOf(old), registry.BlockOf(state)
	return a == b || a.InTag(registry.TagIDCopperChests) && b.InTag(registry.TagIDCopperChests) ||
		a.InGroup(registry.GroupCopperGolemStatue) && b.InGroup(registry.GroupCopperGolemStatue)
}

// flaggedShapeUpdate keeps flag bookkeeping outside ordinary shape dispatch.
func (p *Physics) flaggedShapeUpdate(x, y, z int32, d [3]int32, changes *[]BlockChange) {
	if p.skipWireShape && isWire[registry.BlockOf(p.w.BlockAt(x, y, z))] {
		return
	}
	n, effects := len(*changes), len(p.effects)
	p.shapeUpdate(x, y, z, d, changes)
	p.shapeWriteVisibility(x, y, z, (*changes)[n:], p.effects[effects:])
}
