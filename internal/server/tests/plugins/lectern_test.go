package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: LecternMenu.clickMenuButton
func TestPluginPlayerTakeLecternBook(t *testing.T) {
	t.Parallel()
	asked, cancel := 0, true
	var book plugin.ItemStack
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "lectern", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerTakeLecternBookEvent) {
			asked++
			book = e.Book
			if e.Player.Name() != "Reader" || e.Block.Pos != pos(4, 64, 0) {
				t.Errorf("player %s at %v", e.Player.Name(), e.Block.Pos)
			}
			e.SetCancelled(cancel)
		})
		return nil
	}}}})
	w := servertest.WindowedOf(t, "Reader", servertest.DialServer(t, srv))
	srv.SetBlockAndTell(4, 64, 0, stateOf(t, "minecraft:lectern", map[string]string{"facing": "north", "has_book": "false", "powered": "false"}))
	srv.GiveSlot("Reader", player.HotbarSlot, servertest.ItemStack(t, "minecraft:writable_book", 1))
	w.Settle()
	w.UseBlock(4, 64, 0)
	w.Settle()
	servertest.RunTicks(t, srv, 4)
	if nameAt(srv, 4, 64, 0) != "minecraft:lectern" {
		t.Fatalf("no lectern: %s", nameAt(srv, 4, 64, 0))
	}
	srv.GiveSlot("Reader", player.HotbarSlot, protocol.ItemStack{})
	w.Settle()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuLectern, false, 4, 64, 0)
	w.ClickButton(3)
	w.Settle()
	servertest.RunTicks(t, srv, 2)
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); asked != 1 || props["has_book"] != "true" || book.Empty() {
		t.Fatalf("a cancelled take: asked %d, props %v, book %v", asked, props, book)
	}
	cancel = false
	w.ClickButton(3)
	w.Settle()
	servertest.RunTicks(t, srv, 2)
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); asked != 2 || props["has_book"] != "false" {
		t.Errorf("an allowed take: asked %d, props %v", asked, props)
	}
}
