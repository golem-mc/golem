package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestStoredNumericBookArgumentsSurviveReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Tester")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`setblock 4 64 4 chest{Items:[{Slot:0b,id:"minecraft:written_book",count:1,components:{"minecraft:written_book_content":{title:"Numeric",author:"Tester",pages:[{translate:"%s %s",with:[7,{text:"Good"}]}]}}}]}`)
	srv.RunLine(`item replace entity Tester hotbar.1 from block 4 64 4 container.0`)
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 2, protocol.IDPlayContainerSlot))
	r.ReadVarInt()
	r.ReadVarInt()
	if slot := r.ReadI16(); slot != player.HotbarSlot+1 {
		t.Fatalf("transferred slot = %d", slot)
	}
	stack, ok := r.ReadSlot()
	if !ok || r.Err() != nil {
		t.Fatalf("transferred book unreadable: %v", r.Err())
	}
	number := int64(7)
	want := protocol.Text{Translate: "%s %s", With: []protocol.Text{{Number: &number}, {Text: "Good"}}}
	check := func(stack protocol.ItemStack) {
		t.Helper()
		book, ok := stack.WrittenBookOf()
		if !ok || len(book.Pages) != 1 || !protocol.TextEqual(book.Pages[0].Raw, want) {
			t.Fatalf("book = %+v, readable %v, want numeric and text arguments", book, ok)
		}
	}
	check(stack)
	c.Disconnect()
	srv.Shutdown()

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Tester")
	again.Configure()
	check(again.Inventory()[player.HotbarSlot+1])
	again.Disconnect()
}
