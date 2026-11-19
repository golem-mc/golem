package items

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestBookPagesSurviveASaveAndReload checks that a written and a writable
// book keep their pages, title and author across the player file.
func TestBookPagesSurviveASaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, connect := servertest.Start(t, cfg)

	filtered := "clean"
	writable := servertest.ItemStack(t, "minecraft:writable_book", 1)
	if !writable.SetWritableBook(protocol.WritableBook{Pages: []protocol.FilterablePage{
		{Raw: "first"}, {Raw: "second", Filtered: &filtered},
	}}) {
		t.Fatal("SetWritableBook refused")
	}
	written := servertest.ItemStack(t, "minecraft:written_book", 1)
	if !written.SetWrittenBook(protocol.WrittenBook{
		Title: "Title", Author: "Writer", Generation: 1, Resolved: true,
		Pages: []protocol.WrittenPage{{Raw: protocol.Text{Text: "page"}}},
	}) {
		t.Fatal("SetWrittenBook refused")
	}

	c := connect().Join("Scribe")
	srv.GiveSlot("Scribe", player.HotbarSlot, writable)
	srv.GiveSlot("Scribe", player.HotbarSlot+1, written)
	c.Disconnect()
	srv.Shutdown()

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Scribe")
	again.Configure()
	slots := again.Inventory()
	if got := slots[player.HotbarSlot]; !got.Equal(writable) {
		t.Errorf("writable book slot = %+v, want %+v", got, writable)
	}
	if got := slots[player.HotbarSlot+1]; !got.Equal(written) {
		t.Errorf("written book slot = %+v, want %+v", got, written)
	}
	again.Disconnect()
}

// vanilla: WrittenBookContent.CODEC, ContainerHelper.loadAllItems
func TestStoredBooksTransferPartialAndDynamicPages(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")
	servertest.HoldTicks(t, srv)
	pages := []string{`{nbt:"v"}`, `{nbt:"[",storage:"t:s"}`, `{text:"T",bold:"yes"}`, `{raw:"T",filtered:{nbt:"v"}}`, `{raw:{nbt:"v",storage:"t:s"},filtered:""}`, `{text:"Root",extra:[{text:"Good"},{nbt:"v"}]}`}
	var entries []string
	for i, page := range pages {
		entries = append(entries, fmt.Sprintf(`{Slot:%db,id:"minecraft:written_book",count:1,components:{"minecraft:written_book_content":{title:"T",author:"A",pages:[%s]}}}`, i, page))
	}
	srv.RunLine(`setblock 4 64 4 chest{Items:[` + strings.Join(entries, ",") + `]}`)
	for i := range pages {
		srv.RunLine(fmt.Sprintf("item replace entity Actor hotbar.%d from block 4 64 4 container.%d", i, i))
		// Read the clientbound component directly: untrusted ReadSlot deliberately
		// refuses unresolved dynamic pages arriving from clients.
		r := protocol.NewReader(c.ExpectWithinTicks(srv, 2, protocol.IDPlayContainerSlot))
		window := r.ReadVarInt()
		r.ReadVarInt()
		slot := r.ReadI16()
		count, item := r.ReadVarInt(), r.ReadVarInt()
		added, removed, component := r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
		if window != 0 || slot != int16(player.HotbarSlot+i) || count != 1 || registry.Item(item) != registry.ItemWrittenBook || added != 1 || removed != 0 || component != protocol.DataComponentWrittenBookContent {
			t.Fatalf("transfer header: window %d slot %d count %d item %d added %d removed %d component %d", window, slot, count, item, added, removed, component)
		}
		sent := protocol.ItemStack{Count: count, ItemID: item, Raw: []protocol.RawComponent{{Type: component, Data: r.Rest()}}}
		if want := srv.InventoryAt(4, 64, 4)[i]; !sent.Equal(want) {
			t.Fatalf("transferred stack changed: %+v, want %+v", sent, want)
		}
		book, ok := sent.WrittenBookOf()
		if i == 5 {
			if !ok || len(book.Pages) != 1 || book.Pages[0].Raw.Plain() != "RootGood" || book.Pages[0].Filtered != nil || book.Title != "T" || book.Author != "A" {
				t.Fatalf("partial child book=%+v, present=%v; want RootGood with title and author", book, ok)
			}
			if saved, ok := registry.StoredStack(registry.StackTag(sent)); !ok || !saved.Equal(sent) {
				t.Fatal("recovered children did not round-trip through strict stack decoding")
			}
			continue
		}
		if i < 4 {
			wantPages := 0
			if i >= 2 {
				wantPages = 1
			}
			if !ok || len(book.Pages) != wantPages || book.Title != "T" || book.Author != "A" {
				t.Fatalf("partial book=%+v, present=%v; want %d safe pages with title and author", book, ok, wantPages)
			}
			if wantPages == 1 && (book.Pages[0].Raw.Plain() != "T" || book.Pages[0].Filtered != nil) {
				t.Fatalf("partial page=%+v, want raw T without invalid styles or filtered content", book.Pages[0])
			}
			continue
		}
		if !ok || book.Resolved || len(book.Pages) != 1 || book.Pages[0].Filtered == nil || !nbt.Equal(protocol.TextTag(book.Pages[0].Raw), nbt.Compound{"nbt": nbt.String("v"), "storage": nbt.String("t:s")}) || book.Pages[0].Filtered.Plain() != "" {
			t.Fatalf("transferred book=%+v, present=%v", book, ok)
		}
	}
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayDisconnect))
}

// vanilla: ContainerHelper.loadAllItems, ComponentSerialization.CODEC
func TestStoredObjectBookPagesTransfer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")
	servertest.HoldTicks(t, srv)
	pages := []string{
		`{object:"player",player:{name:"Actor",id:[I;1,2,3,4],properties:[{name:"textures",value:"value"}],texture:"t:skin",model:"slim"}}`,
		`{raw:{object:"atlas",atlas:"minecraft:items",sprite:"minecraft:item/diamond"},filtered:{player:"Actor"}}`,
		`{object:"player",player:{name:"invalid name"}}`,
	}
	var entries []string
	for i, page := range pages {
		entries = append(entries, fmt.Sprintf(`{Slot:%db,id:"minecraft:written_book",count:1,components:{"minecraft:written_book_content":{title:"Objects",author:"A",pages:[%s]}}}`, i, page))
	}
	srv.RunLine(`setblock 4 64 4 chest{Items:[` + strings.Join(entries, ",") + `]}`)
	for i, page := range pages {
		srv.RunLine(fmt.Sprintf("item replace entity Actor hotbar.%d from block 4 64 4 container.%d", i, i))
		stack := srv.InventorySlot("Actor", player.HotbarSlot+i)
		book, ok := stack.WrittenBookOf()
		wantPages := 1
		if i == 2 {
			wantPages = 0
		}
		if !ok || len(book.Pages) != wantPages || book.Title != "Objects" || book.Author != "A" {
			t.Fatalf("object book %d did not transfer: %+v", i, stack)
		}
		if wantPages != 0 {
			want, _, err := nbt.ParseValueTag(page)
			if err != nil {
				t.Fatal(err)
			}
			raw, filtered := want, nbt.Tag(nil)
			if fields := want.(nbt.Compound); fields["raw"] != nil {
				raw, filtered = fields["raw"], fields["filtered"]
			}
			if !nbt.Equal(protocol.TextTag(book.Pages[0].Raw), raw) {
				t.Fatalf("raw page changed: %v", protocol.TextTag(book.Pages[0].Raw))
			}
			twin := book.Pages[0].Filtered
			if (twin != nil) != (filtered != nil) || (twin != nil && !nbt.Equal(protocol.TextTag(*twin), filtered)) {
				t.Fatalf("filtered page changed: %+v", twin)
			}
		}
		r := protocol.NewReader(c.ExpectWithinTicks(srv, 2, protocol.IDPlayContainerSlot))
		window := r.ReadVarInt()
		r.ReadVarInt()
		slot := r.ReadI16()
		count, item := r.ReadVarInt(), r.ReadVarInt()
		added, removed, component := r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
		if window != 0 || int(slot) != player.HotbarSlot+i || count != 1 || registry.Item(item) != registry.ItemWrittenBook || added != 1 || removed != 0 || component != protocol.DataComponentWrittenBookContent {
			t.Fatalf("transfer header: window %d slot %d count %d item %d added %d removed %d component %d", window, slot, count, item, added, removed, component)
		}
		sent := protocol.ItemStack{Count: count, ItemID: item, Raw: []protocol.RawComponent{{Type: component, Data: r.Rest()}}}
		if !sent.Equal(stack) {
			t.Fatalf("transferred stack changed: %+v, want %+v", sent, stack)
		}
	}
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayDisconnect))
}
