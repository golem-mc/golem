package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// mapLootContext binds exploration-map searches to the merchant or container's origin.
// vanilla: ExplorationMapFunction.run, MapItemSavedData.addTargetDecoration
func (s *Server) mapLootContext(w *world.World, x, y, z int32) *rules.LootContext {
	return &rules.LootContext{Level: s.lootLevelAtBlock(w, x, y, z), ExplorationMap: func(fn registry.LootExplorationMap, stack *protocol.ItemStack) bool {
		tx, tz, _, found := w.FindMapStructure(fn.Destination, x, y, z, int(fn.SearchRadius), fn.SkipExistingChunks)
		if !found {
			return false
		}
		dim := s.dimensionOfWorld(w).name
		id, data := s.maps.Create(dim, tx, tz, int8(fn.Zoom))
		data.UnlimitedTracking = true
		s.maps.RenderBiomePreview(id, dim, w.MapBiomeReader())
		stack.SetMapDecoration("+", protocol.MapDecoration{Type: fn.Decoration, X: float64(tx), Z: float64(tz), Rotation: 180})
		stack.SetMapID(id)
		return true
	}}
}
