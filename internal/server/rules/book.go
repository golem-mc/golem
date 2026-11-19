package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// EditBookSlot maps the slot an Edit Book packet names to an inventory
// index: 0-8 the hotbar, 40 the off hand. Any other slot is refused.
// vanilla: ServerGamePacketListenerImpl.handleEditBook, Inventory.isHotbarSlot
func EditBookSlot(slot int32) (index int32, offHand, ok bool) {
	switch {
	case slot == 40:
		return 0, true, true
	case slot >= 0 && slot < 9:
		return slot, false, true
	}
	return 0, false, false
}

// EditBook applies an Edit Book packet to the stack in its slot: the pages
// replace a book and quill's, or, when signing, the stack becomes a written
// book by author with the title and pages. It reports false for a stack
// without writable_book_content, which the packet leaves alone.
// vanilla: ServerGamePacketListenerImpl.updateBookContents, signBook
func EditBook(stack protocol.ItemStack, pages []string, title string, signing bool, author string) (protocol.ItemStack, bool) {
	isWritable := registry.Item(stack.ItemID) == registry.ItemWritableBook && !stack.Empty()
	_, hasContent := stack.WritableBookOf()
	if !isWritable && !hasContent {
		return stack, false
	}
	if !signing {
		edited := stack
		if isWritable && len(pages) == 0 {
			// The empty content is the book and quill's default, which a
			// patch does not carry.
			edited.RemoveWritableBook()
			return edited, true
		}
		content := protocol.WritableBook{Pages: make([]protocol.FilterablePage, 0, len(pages))}
		for _, page := range pages {
			content.Pages = append(content.Pages, protocol.FilterablePage{Raw: page})
		}
		if !edited.SetWritableBook(content) {
			return stack, false
		}
		return edited, true
	}
	// transmuteCopy: the count and the rest of the patch carry over.
	signed := stack
	signed.ItemID = int32(registry.ItemWrittenBook)
	texts := make([]protocol.WrittenPage, 0, len(pages))
	for _, page := range pages {
		texts = append(texts, protocol.WrittenPage{Raw: protocol.Text{Text: page}})
	}
	if !signed.SetWrittenBook(protocol.WrittenBook{
		Title:    title,
		Author:   author,
		Pages:    texts,
		Resolved: true,
	}) {
		return stack, false
	}
	signed.RemoveWritableBook()
	return signed, true
}

// OpensBook reports whether using the stack opens the book screen: a written
// book, or a book and quill, that carries written_book_content.
// vanilla: WrittenBookItem.use, WritableBookItem.use, ServerPlayer.openItemGui
func OpensBook(stack protocol.ItemStack) bool {
	if _, ok := stack.WrittenBookOf(); stack.Empty() || !ok {
		return false
	}
	return registry.Item(stack.ItemID) == registry.ItemWrittenBook || registry.Item(stack.ItemID) == registry.ItemWritableBook
}

// ResolveBook runs every page of an unresolved written book through resolve
// and marks it resolved; false for a stack that is not one, or already is.
// vanilla: WrittenBookContent.resolveForItem
func ResolveBook(stack protocol.ItemStack, resolve func(protocol.Text) protocol.Text) (protocol.ItemStack, bool) {
	book, ok := stack.WrittenBookOf()
	if !ok || book.Resolved {
		return stack, false
	}
	pages := make([]protocol.WrittenPage, len(book.Pages))
	for i, page := range book.Pages {
		pages[i].Raw = resolve(page.Raw)
		if page.Filtered != nil {
			filtered := resolve(*page.Filtered)
			pages[i].Filtered = &filtered
		}
	}
	book.Pages, book.Resolved = pages, true
	resolved := stack
	if !resolved.SetWrittenBook(book) {
		return stack, false
	}
	return resolved, true
}
