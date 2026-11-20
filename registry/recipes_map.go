package registry

import "github.com/golem-mc/golem/internal/protocol"

// The map extending recipe, which keeps the map's item and components (its
// map id). It also needs the map's saved data, which the caller checks.

// mapExtend is an #extendable_maps map ringed by eight paper: the map marked
// for scaling, which the take applies.
// vanilla: MapExtendingRecipe.matches, MapExtendingRecipe.assemble
func mapExtend(cells []protocol.ItemStack) (protocol.ItemStack, bool) {
	const centre = 4
	if len(cells) != 9 || !Item(cells[centre].ItemID).InTag(ItemTagIDExtendableMaps) {
		return protocol.ItemStack{}, false
	}
	for i, cell := range cells {
		if i != centre && (cell.Empty() || Item(cell.ItemID) != ItemPaper) {
			return protocol.ItemStack{}, false
		}
	}
	result := cells[centre]
	result.Count = 1
	result.SetMapPostProcessing(protocol.MapPostProcessingScale)
	return result, true
}
