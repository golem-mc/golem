package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// Book cloning in the player's 2x2: a written book beside book and quills
// copies one generation up per book and quill, and stays in its cell.
// vanilla: BookCloningRecipe

func writtenBookAt(t *testing.T, generation int32) protocol.ItemStack {
	t.Helper()
	book := servertest.ItemStack(t, "minecraft:written_book", 1)
	if !book.SetWrittenBook(protocol.WrittenBook{
		Title: "Title", Author: "Writer", Generation: generation, Resolved: true,
		Pages: []protocol.WrittenPage{{Raw: protocol.Text{Text: "page"}}},
	}) {
		t.Fatal("SetWrittenBook refused")
	}
	return book
}

func TestBookAndQuillsCopyAWrittenBook(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, writtenBookAt(t, 0))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:writable_book", 1))
	w.SetCreativeSlot(38, servertest.ItemStack(t, "minecraft:writable_book", 1))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)
	w.PutInGrid(38, 3)

	want := writtenBookAt(t, 1)
	want.Count = 2
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: writtenBookAt(t, 0), 2: {}, 3: {}}, want)
}

func TestACopyOfACopyCannotBeCopied(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, writtenBookAt(t, 2))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:writable_book", 1))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)

	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})
}
