package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestMixedBookPageSurvivesSaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Scribe")
	srv.RunLine(`give Scribe written_book[written_book_content={title:"Mixed",author:"Writer",resolved:true,pages:[{text:"",extra:["prefix",{text:"suffix",bold:true}]}]}]`)
	r := protocol.NewReader(c.ExpectWithinTicks(nil, 0, protocol.IDPlayContainerSlot))
	r.ReadVarInt() // Window ID
	r.ReadVarInt() // State ID
	r.ReadI16()    // Slot
	stack, ok := r.ReadSlot()
	if !ok || r.Err() != nil {
		t.Fatalf("set container slot did not carry a stack: %v", r.Err())
	}
	want, ok := stack.WrittenBookOf()
	if !ok || len(want.Pages) != 1 {
		t.Fatalf("given book = %+v, present %v, want one page", want, ok)
	}
	c.Disconnect()
	srv.Shutdown()

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Scribe")
	again.Configure()
	slots := again.Inventory()
	got := slots[player.HotbarSlot]
	book, ok := got.WrittenBookOf()
	if !ok || len(book.Pages) != 1 {
		t.Fatalf("reloaded book = %+v, present %v, want one readable page", book, ok)
	}
	if !protocol.TextEqual(book.Pages[0].Raw, want.Pages[0].Raw) {
		t.Errorf("reloaded page = %#v, want the original mixed styled page", protocol.TextTag(book.Pages[0].Raw))
	}
	again.Disconnect()
}
