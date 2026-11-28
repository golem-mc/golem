package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LecternBlockEntity.loadAdditional, WrittenBookContent.resolveForItem
func TestSetblockLecternResolvesItsBook(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	ops := servertest.OpsListing(t, "Reader")
	srv, c := servertest.Join(t, cfg, "Reader", ops, func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, srv)

	c.ChatCommand(`setblock 2 63 1 lectern{Book:{id:"minecraft:written_book",count:1,components:{"minecraft:written_book_content":{title:"t",author:"a",pages:[{raw:{selector:"@p"}}]}}},Page:0}`)
	c.ExpectCommandFeedback("commands.setblock.success", "2", "63", "1")
	got := plainQuery(t, srv, `data get block 2 63 1 Book.components."minecraft:written_book_content"`, "commands.data.block.query")
	if !strings.Contains(got, "resolved: 1b") || !strings.Contains(got, "Reader") || strings.Contains(got, "@p") {
		t.Errorf("lectern book = %s, want resolved with the player's name", got)
	}
}

// vanilla: LecternBlockEntity.loadAdditional
func TestSavedLecternResolvesItsBookOnLoad(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	freeze := func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }
	ops := servertest.OpsListing(t, "Reader")
	srv, c := servertest.Join(t, cfg, "Reader", ops, freeze)
	servertest.HoldTicks(t, srv)

	c.ChatCommand("setblock 2 63 1 minecraft:lectern")
	c.ExpectCommandFeedback("commands.setblock.success", "2", "63", "1")
	book := servertest.ItemStack(t, "minecraft:written_book", 1)
	if !book.SetWrittenBook(protocol.WrittenBook{
		Title: "t", Author: "a",
		Pages: []protocol.WrittenPage{{Raw: protocol.Text{Raw: nbt.Compound{"selector": nbt.String("@p")}}}},
	}) {
		t.Fatal("SetWrittenBook refused")
	}
	srv.PutBlockEntityStack(world.KindLectern, 2, 63, 1, 0, book)
	c.Conn.Close()
	srv.Shutdown()

	restarted := *cfg
	back, _ := servertest.Join(t, &restarted, "Reader", ops, freeze)
	servertest.HoldTicks(t, back)
	servertest.RunTicks(t, back, 1)
	got := plainQuery(t, back, `data get block 2 63 1 Book.components."minecraft:written_book_content"`, "commands.data.block.query")
	if !strings.Contains(got, "resolved: 1b") || strings.Contains(got, "@p") {
		t.Errorf("reloaded lectern book = %s, want resolved", got)
	}
}

// vanilla: BlockItem.updateCustomBlockEntityTag, LecternBlockEntity.loadAdditional
func TestPlacedLecternItemResolvesItsBookData(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	ops := servertest.OpsListing(t, "Reader")
	srv, c := servertest.Join(t, cfg, "Reader", ops, func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, srv)

	tag, err := nbt.ParseOrderedSNBT(`{id:"minecraft:lectern",Book:{id:"minecraft:written_book",count:1,components:{"minecraft:written_book_content":{title:"t",author:"a",pages:[{raw:{selector:"@p"}}]}}},Page:0}`)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := registry.ItemID("minecraft:lectern")
	if !ok {
		t.Fatal("no lectern item")
	}
	stack := protocol.ItemStack{ItemID: id, Count: 1}
	typeID, _ := protocol.BlockEntityTypeID("minecraft:lectern")
	if !stack.SetBlockEntityData(typeID, tag) {
		t.Fatal("could not lay block_entity_data on the item")
	}
	c.SetCreativeSlot(player.HotbarSlot, stack)
	c.Settle()
	c.PlaceBlock(2, 63, 1, protocol.FaceTop)
	c.Settle()
	got := plainQuery(t, srv, `data get block 2 64 1 Book.components."minecraft:written_book_content"`, "commands.data.block.query")
	if !strings.Contains(got, "resolved: 1b") || !strings.Contains(got, "Reader") || strings.Contains(got, "@p") {
		t.Errorf("lectern book = %s, want resolved with the player's name", got)
	}
}
