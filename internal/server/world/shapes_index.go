package world

import (
	"sync/atomic"

	"github.com/golem-mc/golem/registry"
)

// isShapeBlock marks the blocks the server's shape reconnect pass reads or
// rewrites: fences, panes, bars, walls, stairs, gates, growing plants and
// pale moss carpet.
var isShapeBlock = registry.BlocksOf(func(b registry.Block) bool {
	_, plant := growingPlants[b]
	return b.IsCrossCollision() || b.IsWall() || b.IsStairs() || b.IsFenceGate() || plant ||
		b == registry.BlockPaleMossCarpet
})

// shapeTagTable is the tag snapshot checked for members isShapeBlock lacks.
type shapeTagTable struct {
	tags     *registry.BlockTagLookup
	extended bool
}

var shapeTagCache atomic.Pointer[shapeTagTable]

// shapeTagsExtended reports whether the live wall, stair or gate tags
// hold a block the built-in table does not count, as after a data pack
// expands one. The server then runs the pass unconditionally.
func shapeTagsExtended() bool {
	tags := registry.CurrentBlockTags()
	t := shapeTagCache.Load()
	if t == nil || t.tags != tags {
		ids := [...]registry.BlockTagID{
			registry.TagIDWalls, registry.TagIDStairs, registry.TagIDFenceGates,
		}
		t = &shapeTagTable{tags: tags}
		for b := range registry.Block(registry.BlockCount) {
			if isShapeBlock[b] {
				continue
			}
			for _, id := range ids {
				if tags.InTag(b, id) {
					t.extended = true
				}
			}
		}
		shapeTagCache.Store(t)
	}
	return t.extended
}

// ShapeBlocksIn reports whether a loaded section overlapping the inclusive
// cell box contains a block the shape reconnect pass reacts to. It is always
// true while a data pack adds members to the shape tags.
// Near-cache reads follow BlockAt: an unloading chunk reads as it left.
func (w *World) ShapeBlocksIn(x0, y0, z0, x1, y1, z1 int32) bool {
	return shapeTagsExtended() || w.sectionCountIn(x0, y0, z0, x1, y1, z1, func(s *section) int32 { return s.shapes.Load() })
}
