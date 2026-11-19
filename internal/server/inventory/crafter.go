package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// CrafterCraft is one crafter visit's recipe, the item IDs of the non-empty
// cells, and the result and the spent ingredients' remainders in eject order;
// ok is false when no recipe matches.
// vanilla: CrafterBlock.dispenseFrom
func CrafterCraft(cells []protocol.ItemStack) (craft RecipeCraft, items []protocol.ItemStack, ok bool) {
	id, result := registry.CraftMatch(cells, 3)
	if result.Empty() {
		return RecipeCraft{}, nil, false
	}
	craft = newRecipeCraft(id, cells)
	items = append(items, result)
	for _, remainder := range registry.CraftRemainders(cells) {
		if !remainder.Empty() {
			items = append(items, remainder)
		}
	}
	return craft, items, true
}

// CrafterSpend takes one item out of every non-empty cell of the grid.
// vanilla: CrafterBlock.dispenseFrom
func CrafterSpend(b Block) {
	for slot, cell := range b.Stacks() {
		if cell.Empty() {
			continue
		}
		cell.Count--
		if cell.Count <= 0 {
			cell = protocol.ItemStack{}
		}
		b.Put(slot, cell)
	}
}

// containerStackLimit is a slot's getMaxStackSize: one for the jukebox,
// chiseled bookshelf and composter containers, else the item's own.
// vanilla: JukeboxBlockEntity.getMaxStackSize, ChiseledBookShelfBlockEntity.getMaxStackSize, ComposterBlock.InputContainer.getMaxStackSize
func containerStackLimit(b Block, stack protocol.ItemStack) int32 {
	switch b.Kind() {
	case KindJukebox, KindChiseledBookshelf, KindComposterInput, KindComposterOutput, KindComposterFull:
		return 1
	}
	return registry.StackMaxStackSize(stack)
}

// CrafterPour pours a crafted stack into the block in front and returns the
// rest. A crafter, or a block whose slots hold fewer than the stack, takes it
// one item at a time until the first refusal; others take it whole.
// intoEmpty is whether an empty block received something.
// vanilla: CrafterBlock.dispenseItem
func CrafterPour(dest Block, stack protocol.ItemStack, face int32) (left protocol.ItemStack, intoEmpty bool) {
	if dest.Kind() != KindCrafter && stack.Count <= containerStackLimit(dest, stack) {
		return AddItem(dest, stack, face, true)
	}
	for !stack.Empty() {
		one := stack
		one.Count = 1
		rest, into := AddItem(dest, one, face, true)
		intoEmpty = intoEmpty || into
		if !rest.Empty() {
			break
		}
		stack.Count--
	}
	return stack, intoEmpty
}
