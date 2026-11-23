package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Level.setBlock, LevelChunk.setBlockState
func (w pluginWorld) SetBlockState(p plugin.BlockPos, state plugin.BlockState, flags plugin.UpdateFlags) bool {
	if !pluginPositionFits(p) || int(state) >= registry.StateCount ||
		p.Y < w.MinY() || p.Y >= w.MinY()+w.Height() || !rules.InWorldBoundsHorizontal(int32(p.X), int32(p.Z)) {
		return false
	}
	updates := world.BlockUpdates{
		Clients: flags&plugin.UpdateClients != 0, Neighbors: flags&plugin.UpdateNeighbors != 0,
		Shapes: flags&plugin.UpdateKnownShape == 0, OnPlace: flags&plugin.UpdateSkipOnPlace == 0,
		EntityEffects: flags&plugin.UpdateSkipBlockEntitySideEffects == 0,
		MovedByPiston: flags&plugin.UpdateMoveByPiston != 0, SkipWireShape: flags&plugin.UpdateSkipShapeUpdateOnWire != 0,
	}
	// Flags 4 and 8 affect client rendering only. Flag 32 is cleared before
	// neighbour shapes; it does not suppress preRemoveSideEffects.
	x, y, z := int32(p.X), int32(p.Y), int32(p.Z)
	if !w.srv.loopRunning() {
		return w.srv.writeBlockWithUpdates(w.dim.w, x, y, z, uint16(state), nil, writeLight, &updates)
	}
	// A running loop relights once per chunk at the next flush.
	if !w.srv.writeBlockWithUpdates(w.dim.w, x, y, z, uint16(state), nil, 0, &updates) {
		return false
	}
	w.srv.deferRelight(w.dim, x, z)
	return true
}

// deferRelight grows the pending relight box of the column's chunk.
func (s *Server) deferRelight(dim *dimension, x, z int32) {
	key := [2]int32{world.ChunkOf(float64(x)), world.ChunkOf(float64(z))}
	dim.pendingRelightMu.Lock()
	defer dim.pendingRelightMu.Unlock()
	b, ok := dim.pendingRelight[key]
	if !ok {
		if dim.pendingRelight == nil {
			dim.pendingRelight = make(map[[2]int32]changeBox)
		}
		dim.pendingRelightOrder = append(dim.pendingRelightOrder, key)
		b = changeBox{x, z, x, z}
	}
	b.minX, b.minZ = min(b.minX, x), min(b.minZ, z)
	b.maxX, b.maxZ = max(b.maxX, x), max(b.maxZ, z)
	dim.pendingRelight[key] = b
}

// relightPending books the relights deferRelight collected.
func (s *Server) relightPending(dim *dimension) {
	dim.pendingRelightMu.Lock()
	if len(dim.pendingRelightOrder) == 0 {
		dim.pendingRelightMu.Unlock()
		return
	}
	boxes := make([]changeBox, 0, len(dim.pendingRelightOrder))
	for _, k := range dim.pendingRelightOrder {
		boxes = append(boxes, dim.pendingRelight[k])
	}
	clear(dim.pendingRelight)
	dim.pendingRelightOrder = dim.pendingRelightOrder[:0]
	dim.pendingRelightMu.Unlock()
	s.relightBoxesAsync(dim.w, boxes)
}

// BreakBlock breaks the block as if mined without a tool.
// vanilla: Level.destroyBlock
func (w pluginWorld) BreakBlock(p plugin.BlockPos, drops bool) bool {
	if !pluginPositionFits(p) || p.Y < w.MinY() || p.Y >= w.MinY()+w.Height() ||
		!rules.InWorldBoundsHorizontal(int32(p.X), int32(p.Z)) {
		return false
	}
	x, y, z := int32(p.X), int32(p.Y), int32(p.Z)
	lw := w.dim.w
	if !lw.ChunkLoaded(world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))}) {
		return false
	}
	old := lw.BlockAt(x, y, z)
	if !w.srv.destroyBlockContentsLoot(lw, x, y, z, old, "", drops) {
		return false
	}
	return w.SetBlockState(p, plugin.BlockState(world.RemovalLeaves(old)), plugin.UpdateAll)
}

func (s *Server) replacePluginBlockEntity(w *world.World, x, y, z int32, old, state uint16, effects bool) {
	if world.BlockEntityKept(old, state) {
		if w.BlockEntity(x, y, z) == nil {
			s.ensureBlockEntity(w, x, y, z, state)
		}
		return
	}
	if effects {
		s.breakBlockEntity(w, x, y, z, old, true, func(_ *world.BlockEntity, spills []protocol.ItemStack) {
			s.spillContainer(w, x, y, z, spills)
		})
	} else {
		w.RemoveBlockEntity(x, y, z)
	}
	s.replaceBlockEntity(w, x, y, z, old, state)
}

func (s *Server) applyBlockUpdates(w *world.World, x, y, z int32, old, state uint16, u world.BlockUpdates) {
	changes, effects, primes := s.physicsFor(w).NotifyBlockSet(x, y, z, old, u)
	s.finishBlockWake(w, x, y, z, old, changes, effects, primes, u.OnPlace, u.Clients, u.Shapes)
	if !u.Shapes || w.BlockAt(x, y, z) != state {
		return
	}
	s.applyBlockShapeUpdates(w, x, y, z, old, u, 512)
}

func (s *Server) applyBlockShapeUpdates(w *world.World, x, y, z int32, old uint16, u world.BlockUpdates, limit int) {
	if limit <= 0 {
		return
	} // vanilla: Block.UPDATE_LIMIT
	changes := rules.UpdateNeighborShapes(w, x, y, z)
	for i := range changes {
		changes[i].Silent = !u.Clients
	}
	s.tellBlockChanges(w, changes)
	for _, ch := range changes {
		s.applyBlockShapeUpdates(w, ch.X, ch.Y, ch.Z, 0, u, limit-1)
	}
	changes, effects, primes := s.physicsFor(w).BlockSetShapes(x, y, z, old, u.SkipWireShape, u.Clients)
	for _, e := range effects {
		s.tellEffect(w, e)
	}
	for _, t := range primes {
		s.primeTNT(w, t)
	}
	s.tellBlockChanges(w, changes)
}
