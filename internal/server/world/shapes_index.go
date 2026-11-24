package world

import "github.com/golem-mc/golem/registry"

// isShapeBlock marks the blocks the server's shape reconnect pass reads or
// rewrites: fences, panes, bars, walls, stairs, gates, growing plants and
// pale moss carpet.
var isShapeBlock = registry.BlocksOf(func(b registry.Block) bool {
	_, plant := growingPlants[b]
	return b.IsCrossCollision() || b.IsWall() || b.IsStairs() || b.IsFenceGate() || plant ||
		b == registry.BlockPaleMossCarpet
})

// ShapeBlocksIn reports whether a loaded section overlapping the inclusive
// cell box contains a block the shape reconnect pass reacts to.
// Near-cache reads follow BlockAt: an unloading chunk reads as it left.
func (w *World) ShapeBlocksIn(x0, y0, z0, x1, y1, z1 int32) bool {
	lo := max((y0-worldMinY)>>4, 0)
	hi := min((y1-worldMinY)>>4, worldHeight/sectionSize-1)
	if lo > hi {
		return false
	}
	for chx := x0 >> 4; chx <= x1>>4; chx++ {
		for chz := z0 >> 4; chz <= z1>>4; chz++ {
			pos := ChunkPos{X: chx, Z: chz}
			loaded := w.nearAt(pos)
			if loaded == nil {
				w.mu.RLock()
				loaded = w.chunks[pos]
				if loaded != nil {
					w.near[nearIndex(pos)].Store(loaded)
				}
				w.mu.RUnlock()
			}
			if loaded == nil {
				continue
			}
			for sec := lo; sec <= hi; sec++ {
				if loaded.chunk.sections[sec].shapes.Load() > 0 {
					return true
				}
			}
		}
	}
	return false
}
