package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// PlacesOnWater reports whether the item is a PlaceOnWaterBlockItem: it
// refuses a click on a block and lays itself from Use Item instead.
func PlacesOnWater(item registry.Item) bool {
	switch item {
	case registry.ItemLilyPad, registry.ItemFrogspawn:
		return true
	}
	return false
}

// PlaceOnWaterTarget finds where a lily pad or frogspawn goes for a look ray:
// the ray stops at the first outline or water source, and the block goes in
// the cell above, or beside it when taken; false for no hit or no free cell.
// vanilla: PlaceOnWaterBlockItem.use, Item.getPlayerPOVHitResult
func PlaceOnWaterTarget(w *world.World, state uint16, fx, fy, fz, dx, dy, dz float64) (x, y, z int32, ok bool) {
	hit, found := w.ClipBlocks(fx, fy, fz, dx, dy, dz, world.HoldsFluidSource)
	if !found {
		return 0, 0, 0, false
	}
	x, y, z = hit.X, hit.Y+1, hit.Z
	ctx := PlacementContext{Face: hit.Face, ReplacingClickedOn: true}
	if !CanBeReplaced(w.BlockAt(x, y, z), registry.BlockOf(state), ctx) {
		off := protocol.FaceOffset[hit.Face]
		x, y, z = x+off[0], y+off[1], z+off[2]
		ctx.ReplacingClickedOn = false
		if !CanBeReplaced(w.BlockAt(x, y, z), registry.BlockOf(state), ctx) {
			return 0, 0, 0, false
		}
	}
	if !w.PlacementSurvives(state, x, y, z) {
		return 0, 0, 0, false
	}
	return x, y, z, true
}
