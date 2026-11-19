package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
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
