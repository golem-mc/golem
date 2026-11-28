package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
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
