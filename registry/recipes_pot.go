package registry

import "github.com/golem-mc/golem/internal/protocol"

var (
	potItem = MustItem("minecraft:decorated_pot")
)

// decoratedPot makes a pot out of four bricks or sherds laid on the edge
// midpoints of the 3x3 grid, each side carrying the item beside it.
// vanilla: DecoratedPotRecipe.matches, assemble
func decoratedPot(cells []protocol.ItemStack) (protocol.ItemStack, bool) {
	if len(cells) != 9 {
		return protocol.ItemStack{}, false
	}
	// back, left, right, front
	sides := [4]int{1, 3, 5, 7}
	var items [4]protocol.ItemStack
	for _, cell := range sides {
		if !Item(cells[cell].ItemID).InTag(ItemTagIDDecoratedPotIngredients) {
			return protocol.ItemStack{}, false
		}
	}
	filled := 0
	for _, cell := range cells {
		if !cell.Empty() {
			filled++
		}
	}
	if filled != 4 {
		return protocol.ItemStack{}, false
	}
	for i, cell := range sides {
		items[i] = protocol.ItemStack{ItemID: cells[cell].ItemID, Count: 1}
	}
	result := protocol.ItemStack{ItemID: potItem, Count: 1}
	if !result.SetPotDecorations(items) {
		return protocol.ItemStack{}, false
	}
	return result, true
}
