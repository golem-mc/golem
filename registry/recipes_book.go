package registry

import "github.com/golem-mc/golem/internal/protocol"

// The book cloning special recipe: its result is the source book with a
// higher generation, so it is matched here instead of read from
// recipes_gen.go.

var writtenBookItem = MustItem("minecraft:written_book")

// bookCloneMaxGeneration is the highest generation that can still be copied.
// vanilla: BookCloningRecipe.DEFAULT_BOOK_GENERATION_RANGES
const bookCloneMaxGeneration = 1

// bookClone matches one written book and at least one item of the
// book_cloning_target tag (the book and quill): one copy per book and
// quill, one generation up. The returned cell is the source's index.
// vanilla: BookCloningRecipe.matches, BookCloningRecipe.assemble
func bookClone(cells []protocol.ItemStack) (result protocol.ItemStack, sourceCell int, ok bool) {
	copies := int32(0)
	sourceCell = -1
	for i, cell := range cells {
		switch {
		case cell.Empty():
		case cell.ItemID == writtenBookItem:
			book, has := cell.WrittenBookOf()
			if !has || book.Generation > bookCloneMaxGeneration || sourceCell >= 0 {
				return protocol.ItemStack{}, 0, false
			}
			sourceCell = i
		case Item(cell.ItemID).InTag(ItemTagIDBookCloningTarget):
			copies++
		default:
			return protocol.ItemStack{}, 0, false
		}
	}
	if sourceCell < 0 || copies == 0 {
		return protocol.ItemStack{}, 0, false
	}
	result = cells[sourceCell]
	result.Count = copies
	book, _ := result.WrittenBookOf()
	book.Generation++
	if !result.SetWrittenBook(book) {
		return protocol.ItemStack{}, 0, false
	}
	return result, sourceCell, true
}

// bookCloneResult is bookClone in the shape specialCraftResult tries.
func bookCloneResult(cells []protocol.ItemStack) (protocol.ItemStack, bool) {
	result, _, ok := bookClone(cells)
	return result, ok
}
