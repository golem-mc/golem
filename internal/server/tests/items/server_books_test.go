package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The book tests: the Edit Book packet's edits and signings of a book and
// quill, and the Open Book packet the use of a held written book sends. The
// books travel with their components built byte for byte here, so the tests
// read the wire shape and not the component writers under test.

// bookEditor is a survival player in the world with empty hands.
func bookEditor(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	return bookEditorIn(t, "survival")
}

// bookEditorIn is a player in the given game mode with empty hands.
func bookEditorIn(t *testing.T, mode string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = mode
	srv, c := servertest.Join(t, cfg, "Actor")
	return srv, c
}

// writablePagesValue builds a minecraft:writable_book_content value by hand:
// the pages, each with no filtered twin, the way an unfiltered server keeps
// them.
func writablePagesValue(pages ...string) []byte {
	var b protocol.Buffer
	b.PutVarInt(int32(len(pages)))
	for _, page := range pages {
		b.PutString(page)
		b.PutBool(false)
	}
	return b.Bytes()
}

// writtenValue builds a minecraft:written_book_content value by hand: the
// title, its absent filtered twin, the author, the first generation, the
// pages as plain text components and the resolved flag.
func writtenValue(title, author string, pages ...string) []byte {
	var b protocol.Buffer
	b.PutString(title)
	b.PutBool(false)
	b.PutString(author)
	b.PutVarInt(0)
	b.PutVarInt(int32(len(pages)))
	for _, page := range pages {
		b.PutText(protocol.Text{Text: page})
		b.PutBool(false)
	}
	b.PutBool(true)
	return b.Bytes()
}

// writableBook gives a book and quill whose pages the value holds.
func writableBook(t *testing.T, pages ...string) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, "minecraft:writable_book", 1)
	stack.Raw = append(stack.Raw, protocol.RawComponent{
		Type: protocol.DataComponentWritableBookContent,
		Data: writablePagesValue(pages...),
	})
	return stack
}

// writtenBook gives a written book whose contents the value holds.
func writtenBook(t *testing.T, title, author string, pages ...string) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, "minecraft:written_book", 1)
	stack.Raw = append(stack.Raw, protocol.RawComponent{
		Type: protocol.DataComponentWrittenBookContent,
		Data: writtenValue(title, author, pages...),
	})
	return stack
}

// testPagesOf decodes the minecraft:writable_book_content a stack keeps, so
// an edit's result is read off the wire and not through the writers under
// test. The bool reports the component's presence.
func testPagesOf(t *testing.T, stack protocol.ItemStack) ([]string, bool) {
	t.Helper()
	for _, raw := range stack.Raw {
		if raw.Type != protocol.DataComponentWritableBookContent {
			continue
		}
		r := protocol.NewReader(raw.Data)
		count := r.ReadVarInt()
		pages := make([]string, 0, count)
		for range count {
			pages = append(pages, r.ReadString(1024))
			if r.ReadBool() {
				r.ReadString(1024)
			}
		}
		if r.Err() != nil {
			t.Fatalf("the writable book's pages did not read back")
		}
		return pages, true
	}
	return nil, false
}

// testWrittenOf decodes the minecraft:written_book_content a stack keeps:
// the title, the author, each page's own text, the copy count and whether
// the server resolved the components. The bool reports the component's
// presence.
func testWrittenOf(t *testing.T, stack protocol.ItemStack) (title, author string, pages []string, generation int32, resolved bool, ok bool) {
	t.Helper()
	for _, raw := range stack.Raw {
		if raw.Type != protocol.DataComponentWrittenBookContent {
			continue
		}
		r := protocol.NewReader(raw.Data)
		title = r.ReadString(32)
		if r.ReadBool() {
			r.ReadString(32)
		}
		author = r.ReadString(32767)
		generation = r.ReadVarInt()
		count := r.ReadVarInt()
		for range count {
			text, ok := protocol.ReadText(r)
			if !ok {
				t.Fatalf("a written page did not read back as a text component")
			}
			pages = append(pages, text.Text)
			if r.ReadBool() {
				protocol.ReadText(r)
			}
		}
		resolved = r.ReadBool()
		if r.Err() != nil {
			t.Fatalf("the written book's contents did not read back")
		}
		return title, author, pages, generation, resolved, true
	}
	return "", "", nil, 0, false, false
}

// editBook sends the Edit Book packet as a client does: the hotbar or
// off-hand slot it edits, the pages the book then holds and, when it signs,
// the title.
func editBook(c *servertest.Client, slot int32, pages []string, title string, signing bool) {
	c.T.Helper()
	c.Send(protocol.IDPlayEditBook, func(b *protocol.Buffer) {
		b.PutVarInt(slot)
		b.PutVarInt(int32(len(pages)))
		for _, page := range pages {
			b.PutString(page)
		}
		if signing {
			b.PutBool(true)
			b.PutString(title)
		} else {
			b.PutBool(false)
		}
	})
}

// useItemHand sends the Use Item packet for one hand, with the look a click
// carries and the block change sequence it answers.
func useItemHand(c *servertest.Client, hand int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(hand)
		b.PutVarInt(0)
		b.PutF32(0)
		b.PutF32(0)
	})
}

// expectBookCorrection reads the Set Container Slot packet an accepted edit
// is answered with and returns the window slot and the stack it carries.
func expectBookCorrection(t *testing.T, c *servertest.Client) (int16, protocol.ItemStack) {
	t.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayContainerSlot))
	if window := r.ReadVarInt(); window != 0 {
		t.Fatalf("the correction named window %d, want the player's own", window)
	}
	r.ReadVarInt() // the state ID the correction rides in
	slot := r.ReadI16()
	stack, ok := r.ReadSlot()
	if !ok {
		t.Fatalf("the correction's stack did not read back")
	}
	return slot, stack
}

// expectNothingFromTheBooks drains what the server queued after an exchange
// that should have changed nothing, and fails if a correction, a book
// window or a swing says otherwise. The block change ack a use always sends
// is not one of them.
func expectNothingFromTheBooks(t *testing.T, c *servertest.Client) {
	t.Helper()
	c.SettleAndCollect(func(id int32, _ []byte) {
		switch id {
		case protocol.IDPlayContainerSlot:
			t.Error("an edit that changed nothing still corrected a slot")
		case protocol.IDPlayOpenBook:
			t.Error("a book the server would not open still opened")
		case protocol.IDPlayEntityAnimation:
			t.Error("a book use still swung the arm")
		case protocol.IDPlayAckBlockChange:
		}
	})
}

// expectEditBookRejected waits for the disconnect a malformed Edit Book
// packet is answered with.
func expectEditBookRejected(t *testing.T, c *servertest.Client) {
	t.Helper()
	servertest.ExpectDisconnect(t, c, protocol.Text{
		Translate: "disconnect.genericReason",
		With: []protocol.Text{{Text: "Internal Exception: io.netty.handler.codec.DecoderException: " +
			"Failed to decode packet 'serverbound/minecraft:edit_book'"}},
	})
}

// TestEditBookUpdatesPages: the pages a book and quill is edited to keep the
// item and count it had and come back with the sent pages, the server's own
// inventory agreeing with the correction it sent.
// vanilla: ServerGamePacketListenerImpl.handleEditBook
func TestEditBookUpdatesPages(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)

	srv.GiveSlot("Actor", player.HotbarSlot+3, writableBook(t, "old page"))
	c.Settle()

	editBook(c, 3, []string{"Hello", "Page two"}, "", false)
	slot, stack := expectBookCorrection(t, c)
	if slot != int16(player.HotbarSlot+3) {
		t.Errorf("the correction named slot %d, want the edited hotbar slot's %d", slot, player.HotbarSlot+3)
	}
	if stack.ItemID != servertest.ItemID(t, "minecraft:writable_book") || stack.Count != 1 {
		t.Errorf("the edited book = item %d count %d, want the book and quill it was", stack.ItemID, stack.Count)
	}
	if got, ok := testPagesOf(t, stack); !ok || !slices.Equal(got, []string{"Hello", "Page two"}) {
		t.Errorf("the edited pages = %v (present %v), want the sent two", got, ok)
	}
	got, ok := testPagesOf(t, srv.InventorySlot("Actor", player.HotbarSlot+3))
	if !ok || !slices.Equal(got, []string{"Hello", "Page two"}) {
		t.Errorf("the book the server keeps = %v (present %v), want the sent two", got, ok)
	}
}

// TestEditBookWritesAFreshBook: a book and quill straight from the creative
// menu carries its empty writable_book_content as an item default, not in its
// patch, and still takes the pages.
// vanilla: ServerGamePacketListenerImpl.updateBookContents
func TestEditBookWritesAFreshBook(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)

	srv.GiveSlot("Actor", player.HotbarSlot+8, servertest.ItemStack(t, "minecraft:writable_book", 1))
	c.Settle()

	editBook(c, 8, []string{"First"}, "", false)
	_, stack := expectBookCorrection(t, c)
	if got, ok := testPagesOf(t, stack); !ok || !slices.Equal(got, []string{"First"}) {
		t.Errorf("the fresh book's pages = %v (present %v), want the sent one", got, ok)
	}

	editBook(c, 8, nil, "Signed", true)
	_, stack = expectBookCorrection(t, c)
	if stack.ItemID != servertest.ItemID(t, "minecraft:written_book") {
		t.Errorf("the signed fresh book is item %d, want the written book", stack.ItemID)
	}
}

// TestEditBookSignsTheBook: a book and quill sent with a title becomes one
// written book of the first generation, the stack the client predicted: its
// item and count kept, its writable component gone and its contents carried
// by the written component, authored by the editing player.
// vanilla: ServerGamePacketListenerImpl.signBook
func TestEditBookSignsTheBook(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)

	srv.GiveSlot("Actor", player.HotbarSlot, writableBook(t, "Once upon"))
	c.Settle()

	editBook(c, 0, []string{"Once upon", "a time"}, "The Tale", true)
	slot, stack := expectBookCorrection(t, c)
	if slot != int16(player.HotbarSlot) {
		t.Errorf("the correction named slot %d, want the signed hotbar slot's %d", slot, player.HotbarSlot)
	}
	if stack.ItemID != servertest.ItemID(t, "minecraft:written_book") || stack.Count != 1 {
		t.Errorf("the signed book = item %d count %d, want one written book", stack.ItemID, stack.Count)
	}
	title, author, pages, generation, resolved, ok := testWrittenOf(t, stack)
	if !ok || title != "The Tale" || author != "Actor" ||
		!slices.Equal(pages, []string{"Once upon", "a time"}) || generation != 0 || !resolved {
		t.Errorf("the signed contents = %q by %q, pages %v, generation %d, resolved %v (present %v), "+
			"want The Tale by Actor, its two pages, the first generation, resolved",
			title, author, pages, generation, resolved, ok)
	}
	if _, hasPages := testPagesOf(t, stack); hasPages {
		t.Errorf("the signed book still carries a writable_book_content")
	}
	if _, _, _, _, _, ok := testWrittenOf(t, srv.InventorySlot("Actor", player.HotbarSlot)); !ok {
		t.Errorf("the book the server keeps carries no written_book_content")
	}
}

// TestEditBookSignsTheOffHandBook: the off-hand slot signs the same way, the
// correction naming the off-hand slot the packet's 40 names.
// vanilla: ServerGamePacketListenerImpl.handleEditBook, Inventory.isHotbarSlot
func TestEditBookSignsTheOffHandBook(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)

	srv.GiveSlot("Actor", player.OffHandSlot, writableBook(t))
	c.Settle()

	editBook(c, 40, nil, "Off Hand", true)
	slot, stack := expectBookCorrection(t, c)
	if slot != int16(player.OffHandSlot) {
		t.Errorf("the correction named slot %d, want the off hand's %d", slot, player.OffHandSlot)
	}
	title, author, pages, generation, resolved, ok := testWrittenOf(t, stack)
	if !ok || title != "Off Hand" || author != "Actor" || len(pages) != 0 || generation != 0 || !resolved {
		t.Errorf("the signed contents = %q by %q, %d pages, generation %d, resolved %v (present %v), "+
			"want Off Hand by Actor, no pages, the first generation, resolved",
			title, author, len(pages), generation, resolved, ok)
	}
}

// TestEditBookIgnoresWhatItCannotEdit: a stack that is no book and quill, a
// book already signed, a book outside the hotbar and the off hand, and a slot
// the packet's rule does not name all change nothing, and the server says
// nothing back.
// vanilla: ServerGamePacketListenerImpl.handleEditBook
func TestEditBookIgnoresWhatItCannotEdit(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)

	srv.GiveSlot("Actor", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stone", 1))
	srv.GiveSlot("Actor", player.HotbarSlot+2, writtenBook(t, "Tale", "Author", "text"))
	srv.GiveSlot("Actor", 9, writableBook(t, "quill")) // the main inventory's first slot
	c.Settle()

	editBook(c, 0, []string{"page"}, "", false) // the stone
	expectNothingFromTheBooks(t, c)
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != servertest.ItemID(t, "minecraft:stone") {
		t.Errorf("the stone turned into item %d", got.ItemID)
	}

	editBook(c, 2, []string{"page"}, "Again", true) // the signed book, which edits no more
	expectNothingFromTheBooks(t, c)

	editBook(c, 9, []string{"page"}, "", false) // past the hotbar
	expectNothingFromTheBooks(t, c)
	if got, _ := testPagesOf(t, srv.InventorySlot("Actor", 9)); !slices.Equal(got, []string{"quill"}) {
		t.Errorf("the book outside the hotbar = %v, want its own page", got)
	}

	editBook(c, 100, []string{"page"}, "", false)
	expectNothingFromTheBooks(t, c)
}

// TestEditBookRefusesWhatThePacketCannotCarry: a page list past its hundred,
// a page past its 1024 code units and a title past its 32 are a decode
// failure that drops the sender; the limits themselves are accepted. The
// 15-character title is the sign screen's own limit, which the server does
// not hold.
// vanilla: ServerboundEditBookPacket.STREAM_CODEC
func TestEditBookRefusesWhatThePacketCannotCarry(t *testing.T) {
	t.Parallel()
	t.Run("at the limits", func(t *testing.T) {
		t.Parallel()
		srv, c := bookEditor(t)
		srv.GiveSlot("Actor", player.HotbarSlot, writableBook(t))
		c.Settle()
		pages := make([]string, 100)
		for i := range pages {
			pages[i] = strings.Repeat("x", 1024)
		}
		editBook(c, 0, pages, strings.Repeat("t", 32), true)
		_, stack := expectBookCorrection(t, c)
		title, _, got, _, _, ok := testWrittenOf(t, stack)
		if !ok || title != strings.Repeat("t", 32) || len(got) != 100 || len(got[99]) != 1024 {
			t.Errorf("the book signed at the limits = title %q, %d pages (present %v), "+
				"want the 32-character title and 100 full pages", title, len(got), ok)
		}
	})
	t.Run("101 pages", func(t *testing.T) {
		t.Parallel()
		_, c := bookEditor(t)
		editBook(c, 0, make([]string, 101), "", false)
		expectEditBookRejected(t, c)
	})
	t.Run("a long page", func(t *testing.T) {
		t.Parallel()
		_, c := bookEditor(t)
		editBook(c, 0, []string{strings.Repeat("x", 1025)}, "", false)
		expectEditBookRejected(t, c)
	})
	t.Run("a long title", func(t *testing.T) {
		t.Parallel()
		_, c := bookEditor(t)
		editBook(c, 0, []string{"ok"}, strings.Repeat("t", 33), true)
		expectEditBookRejected(t, c)
	})
}

// TestOpenBookOpensWhatItsHandHolds: the use of a written book opens the
// book's own screen naming the hand that held it, and does not swing the
// arm - the vanilla server leaves a book's swing to the client.
// vanilla: WrittenBookItem.use, ServerPlayer.openItemGui
func TestOpenBookOpensWhatItsHandHolds(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)

	srv.GiveSlot("Actor", player.HotbarSlot, writtenBook(t, "Tale", "Author", "once"))
	srv.GiveSlot("Actor", player.OffHandSlot, writtenBook(t, "Other", "Author"))
	c.Settle()

	useItemHand(c, protocol.HandMain)
	if hand := protocol.NewReader(c.ExpectPlay(protocol.IDPlayOpenBook)).ReadVarInt(); hand != protocol.HandMain {
		t.Errorf("the open book named hand %d, want the main hand's %d", hand, protocol.HandMain)
	}

	useItemHand(c, protocol.HandOff)
	if hand := protocol.NewReader(c.ExpectPlay(protocol.IDPlayOpenBook)).ReadVarInt(); hand != protocol.HandOff {
		t.Errorf("the open book named hand %d, want the off hand's %d", hand, protocol.HandOff)
	}
}

// TestOpenBookResolvesSelectors: a written book that was given with a
// selector page and not yet resolved reads the player's name in the slot
// update that follows its use, and is marked resolved.
// vanilla: WrittenBookContent.resolveForItem
func TestOpenBookResolvesSelectors(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)

	var value protocol.Buffer
	value.PutString("Tale")
	value.PutBool(false)
	value.PutString("Author")
	value.PutVarInt(0)
	value.PutVarInt(1)
	value.PutText(protocol.Text{Raw: nbt.Compound{"selector": nbt.String("@p")}})
	value.PutBool(false)
	value.PutBool(false) // not resolved
	book := servertest.ItemStack(t, "minecraft:written_book", 1)
	book.Raw = append(book.Raw, protocol.RawComponent{Type: protocol.DataComponentWrittenBookContent, Data: value.Bytes()})
	srv.GiveSlot("Actor", player.HotbarSlot, book)
	c.Settle()

	useItemHand(c, protocol.HandMain)
	_, stack := expectBookCorrection(t, c)
	c.ExpectPlay(protocol.IDPlayOpenBook)
	content, ok := stack.WrittenBookOf()
	if !ok || !content.Resolved || len(content.Pages) != 1 {
		t.Fatalf("the used book = %+v (ok %v), want one resolved page", content, ok)
	}
	got := nbt.Stringify(protocol.TextTag(content.Pages[0].Raw))
	if !strings.Contains(got, "Actor") || strings.Contains(got, "selector") {
		t.Errorf("the page = %s, want the player's name in place of the selector", got)
	}
}

// TestOpenBookResolvesScores: a score page with `*` shows the reader's score.
// vanilla: ScoreContents.resolve
func TestOpenBookResolvesScores(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)
	srv.RunLine("scoreboard objectives add o dummy")
	srv.RunLine("scoreboard players set Actor o 42")

	var value protocol.Buffer
	value.PutString("Tale")
	value.PutBool(false)
	value.PutString("Author")
	value.PutVarInt(0)
	value.PutVarInt(1)
	value.PutText(protocol.Text{Raw: nbt.Compound{"score": nbt.Compound{"name": nbt.String("*"), "objective": nbt.String("o")}}})
	value.PutBool(false)
	value.PutBool(false)
	book := servertest.ItemStack(t, "minecraft:written_book", 1)
	book.Raw = append(book.Raw, protocol.RawComponent{Type: protocol.DataComponentWrittenBookContent, Data: value.Bytes()})
	srv.GiveSlot("Actor", player.HotbarSlot, book)
	c.Settle()

	useItemHand(c, protocol.HandMain)
	_, stack := expectBookCorrection(t, c)
	c.ExpectPlay(protocol.IDPlayOpenBook)
	content, ok := stack.WrittenBookOf()
	if !ok || len(content.Pages) != 1 {
		t.Fatalf("the used book = %+v (ok %v), want one page", content, ok)
	}
	got := nbt.Stringify(protocol.TextTag(content.Pages[0].Raw))
	if !strings.Contains(got, "42") || strings.Contains(got, "score") {
		t.Errorf("the page = %s, want the score", got)
	}
}

// TestOpenBookResolvesNbtAndHovers checks dynamic page contents and styles on use.
// vanilla: WrittenBookContent.resolvePage, ComponentUtils.resolveStyle
func TestOpenBookResolvesNbtAndHovers(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)
	srv.RunLine(`data modify storage t:s v set value "Hi"`)
	srv.RunLine(`data modify storage t:s parts set value [{selector:"@s"},{text:"!"}]`)
	pages := []nbt.Tag{
		nbt.Compound{"nbt": nbt.String("v"), "storage": nbt.String("t:s")},
		nbt.Compound{"text": nbt.String("Hover"), "hover_event": nbt.Compound{
			"action": nbt.String("show_text"), "value": nbt.Compound{"selector": nbt.String("@s")},
		}},
		nbt.Compound{"nbt": nbt.String("parts[]"), "storage": nbt.String("t:s"), "interpret": nbt.Byte(1), "separator": nbt.String("/")},
	}
	var value protocol.Buffer
	value.PutString("Tale")
	value.PutBool(false)
	value.PutString("Author")
	value.PutVarInt(0)
	value.PutVarInt(int32(len(pages)))
	for _, page := range pages {
		value.PutText(protocol.Text{Raw: page})
		value.PutBool(false)
	}
	value.PutBool(false)
	book := servertest.ItemStack(t, "minecraft:written_book", 1)
	book.Raw = append(book.Raw, protocol.RawComponent{Type: protocol.DataComponentWrittenBookContent, Data: value.Bytes()})
	srv.GiveSlot("Actor", player.HotbarSlot, book)
	c.Settle()

	useItemHand(c, protocol.HandMain)
	_, stack := expectBookCorrection(t, c)
	c.ExpectPlay(protocol.IDPlayOpenBook)
	content, ok := stack.WrittenBookOf()
	if !ok || !content.Resolved || len(content.Pages) != len(pages) {
		t.Fatalf("the used book = %+v (ok %v), want %d resolved pages", content, ok, len(pages))
	}
	for i, want := range []string{`"Hi"`, "Hover", "Actor/!"} {
		if got := content.Pages[i].Raw.Plain(); got != want {
			t.Errorf("page %d = %q, want %q", i, got, want)
		}
	}
	hover := content.Pages[1].Raw.HoverText
	if hover == nil || hover.Plain() != "Actor" {
		t.Errorf("hover = %+v, want Actor", hover)
	}
}

// TestOpenBookOpensOnlyWrittenBooks: a book and quill, a written book
// without its contents component and a written book in a spectator's hand
// all open nothing, and none of them swings the arm.
// vanilla: ServerPlayer.openItemGui, ServerPlayerGameMode.useItem
func TestOpenBookOpensOnlyWrittenBooks(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)

	srv.GiveSlot("Actor", player.HotbarSlot, writableBook(t, "draft"))
	srv.GiveSlot("Actor", player.OffHandSlot, servertest.ItemStack(t, "minecraft:written_book", 1))
	c.Settle()

	useItemHand(c, protocol.HandMain)
	expectNothingFromTheBooks(t, c)

	useItemHand(c, protocol.HandOff)
	expectNothingFromTheBooks(t, c)

	spectatorSrv, spectator := bookEditorIn(t, "spectator")
	spectatorSrv.GiveSlot("Actor", player.HotbarSlot, writtenBook(t, "Tale", "Author", "once"))
	spectator.Settle()
	useItemHand(spectator, protocol.HandMain)
	expectNothingFromTheBooks(t, spectator)
}

// TestOpenBookOpensABookAndQuillWithWrittenContent: a book and quill that
// carries written_book_content opens the book screen, since the check is on
// the component and not the item.
// vanilla: WritableBookItem.use, ServerPlayer.openItemGui
func TestOpenBookOpensABookAndQuillWithWrittenContent(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)

	stack := servertest.ItemStack(t, "minecraft:writable_book", 1)
	stack.Raw = append(stack.Raw, protocol.RawComponent{
		Type: protocol.DataComponentWrittenBookContent,
		Data: writtenValue("Tale", "Author", "once"),
	})
	srv.GiveSlot("Actor", player.HotbarSlot, stack)
	c.Settle()

	useItemHand(c, protocol.HandMain)
	if hand := protocol.NewReader(c.ExpectPlay(protocol.IDPlayOpenBook)).ReadVarInt(); hand != protocol.HandMain {
		t.Errorf("the open book named hand %d, want the main hand's %d", hand, protocol.HandMain)
	}
}

// vanilla: WritableBookItem.use, ServerPlayer.openItemGui
func TestPlainWritableBookUseCountsWithoutOpenPacket(t *testing.T) {
	t.Parallel()
	srv, c := bookEditor(t)
	book := servertest.ItemStack(t, "minecraft:writable_book", 1)
	srv.GiveSlot("Actor", player.HotbarSlot, book)
	srv.GiveSlot("Actor", player.OffHandSlot, book)
	c.Settle()

	before := itemStat(srv, registry.StatUsed, "minecraft:writable_book")
	for i, hand := range []int32{protocol.HandMain, protocol.HandOff} {
		useItemHand(c, hand)
		expectNothingFromTheBooks(t, c)
		if got, want := itemStat(srv, registry.StatUsed, "minecraft:writable_book"), before+int32(i)+1; got != want {
			t.Errorf("hand %d: used writable book = %d, want %d", hand, got, want)
		}
	}
}

// TestGiveWrittenBookContent gives a signed book through the component list
// and reads its title, author, generation and pages back from the slot.
func TestGiveWrittenBookContent(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	_, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Smith").String(), Name: "Smith", Level: 2}))
	c := connect()
	c.Login("Smith")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Smith")

	c.ChatCommand(`give @s written_book[written_book_content={title:"Tale",author:"Ann",generation:2,pages:["one","two"]}]`)
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	title, author, pages, generation, _, ok := testWrittenOf(t, updates[0].Stack)
	if !ok || title != "Tale" || author != "Ann" || generation != 2 ||
		len(pages) != 2 || pages[0] != "one" || pages[1] != "two" {
		t.Errorf("given book = %q by %q gen %d pages %q (present %v)", title, author, generation, pages, ok)
	}
}

// vanilla: WrittenBookContent.CODEC, WrittenBookContent.resolveForItem
func TestGiveDynamicWrittenBookOpensResolvedPages(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`data modify storage t:s v set value "Bye"`)
	c.ExpectSystemChat("commands.data.storage.modified", false, protocol.Text{Text: "t:s"})
	srv.RunLine(`scoreboard objectives add o dummy`)
	srv.RunLine(`scoreboard players set Actor o 42`)
	c.ChatCommand(`give @s minecraft:written_book[minecraft:written_book_content={title:"Review",author:"Tester",pages:[{nbt:"v",storage:"t:s"},{raw:{selector:"@s"},filtered:{nbt:"v",storage:"t:s"}},{score:{name:"*",objective:"o"}},{text:"Hover",hover_event:{action:"show_text",value:{nbt:"v",storage:"t:s"}}}]}]`)
	// Read the clientbound patch directly: ReadSlot intentionally refuses
	// dynamic pages in stacks received from untrusted clients.
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 2, protocol.IDPlayContainerSlot))
	window := r.ReadVarInt()
	r.ReadVarInt() // state ID
	slot := r.ReadI16()
	count, item := r.ReadVarInt(), r.ReadVarInt()
	added, removed := r.ReadVarInt(), r.ReadVarInt()
	component := r.ReadVarInt()
	if window != 0 || slot != player.HotbarSlot || count != 1 || registry.Item(item) != registry.ItemWrittenBook ||
		added != 1 || removed != 0 || component != protocol.DataComponentWrittenBookContent {
		t.Fatalf("given slot header = window %d slot %d count %d item %d added %d removed %d component %d", window, slot, count, item, added, removed, component)
	}
	given := protocol.ItemStack{ItemID: item, Count: count, Raw: []protocol.RawComponent{{Type: component, Data: r.Rest()}}}
	book, ok := given.WrittenBookOf()
	if !ok || book.Resolved || len(book.Pages) != 4 || book.Pages[1].Filtered == nil {
		t.Fatalf("given book = %+v, %v; want four unresolved pages and a filtered twin", book, ok)
	}
	if tag := protocol.TextTag(book.Pages[0].Raw); !nbt.Equal(tag, nbt.Compound{"nbt": nbt.String("v"), "storage": nbt.String("t:s")}) {
		t.Fatalf("given NBT page = %v, want the unresolved component", tag)
	}
	c.Settle()
	useItemHand(c, protocol.HandMain)
	_, stack := expectBookCorrection(t, c)
	if hand := protocol.NewReader(c.ExpectPlay(protocol.IDPlayOpenBook)).ReadVarInt(); hand != protocol.HandMain {
		t.Fatalf("opened hand %d, want main hand", hand)
	}
	book, ok = stack.WrittenBookOf()
	if !ok || !book.Resolved || len(book.Pages) != 4 {
		t.Fatalf("opened book = %+v, %v; want four resolved pages", book, ok)
	}
	for i, want := range []string{`"Bye"`, "Actor", "42", "Hover"} {
		if got := book.Pages[i].Raw.Plain(); got != want {
			t.Errorf("page %d = %q, want %q", i, got, want)
		}
	}
	if twin := book.Pages[1].Filtered; twin == nil || twin.Plain() != `"Bye"` {
		t.Errorf("filtered page = %+v, want quoted Bye", twin)
	}
	if hover := book.Pages[3].Raw.HoverText; hover == nil || hover.Plain() != `"Bye"` {
		t.Errorf("hover = %+v, want quoted Bye", hover)
	}
	if tag := protocol.TextTag(book.Pages[0].Raw); !strings.Contains(nbt.Stringify(tag), "green") {
		t.Errorf("resolved NBT page = %v, want green string styling", tag)
	}
}
