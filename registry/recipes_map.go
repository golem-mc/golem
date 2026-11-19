package registry

import "github.com/golem-mc/golem/internal/protocol"

// The map special recipes, which keep the map's item and components (its map
// id): cloning and extending. Extending also needs the map's saved data,
// which the caller checks.

// mapCloneMaterials is how many empty maps one clone takes at most.
// vanilla: TransmuteRecipe.MAX_MATERIAL_COUNT
const mapCloneMaterials = 8

// mapClone is one #clonable_maps map and one to eight empty maps: that many
// plus one copies of the map, same item and map id.
// vanilla: TransmuteRecipe (map_cloning)
func mapClone(cells []protocol.ItemStack) (protocol.ItemStack, bool) {
	var source protocol.ItemStack
	materials := int32(0)
	for _, cell := range cells {
		switch {
		case cell.Empty():
		case Item(cell.ItemID).InTag(ItemTagIDClonableMaps):
			if !source.Empty() {
				return protocol.ItemStack{}, false
			}
			source = cell
		case Item(cell.ItemID) == ItemMap:
			if materials++; materials > mapCloneMaterials {
				return protocol.ItemStack{}, false
			}
		default:
			return protocol.ItemStack{}, false
		}
	}
	if source.Empty() || materials == 0 {
		return protocol.ItemStack{}, false
	}
	source.Count = materials + 1
	return source, true
}

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
