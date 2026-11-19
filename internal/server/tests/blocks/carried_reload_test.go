package blocks

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: TheEndGatewayBlockEntity.loadAdditional, portalTick
func TestCarriedBlockEntitiesSurviveRegionReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	freeze := func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"), freeze)
	servertest.HoldTicks(t, srv)
	kinds := []string{"minecraft:end_gateway", "minecraft:structure_block", "minecraft:test_instance_block"}
	for i, kind := range kinds {
		c.ChatCommand(fmt.Sprintf("setblock %d 90 3 %s", i+3, kind))
		servertest.AwaitSystemChatSaying(t, c, "commands.setblock.success")
	}
	// Cross the beam interval on the first tick after reopening.
	srv.SetEndGatewayAge(3, 90, 3, rules.EndGatewayBeamInterval-1)
	c.Conn.Close()
	srv.Shutdown()
	restarted := *cfg
	back, again := servertest.Join(t, &restarted, "Keeper", servertest.OpsListing(t, "Keeper"), freeze)
	servertest.HoldTicks(t, back)
	servertest.RunTicks(t, back, 1)
	again.Settle()
	w := back.OverworldWorld()
	for i, kind := range kinds {
		pos := [3]int32{int32(i + 3), 90, 3}
		if _, ok := w.BlockEntityTag(pos[0], pos[1], pos[2]); !ok {
			t.Fatalf("%s block entity missing after reload", kind)
		}
		if got := w.CarriedOfKind(kind, pos, pos); len(got) != 1 || got[0].Pos != pos {
			t.Errorf("%s loaded lookup = %v, want %v", kind, got, pos)
		}
		if got := w.TickingCarriedOfKind(kind); len(got) != 1 || got[0].Pos != pos {
			t.Errorf("%s ticking lookup = %v, want %v", kind, got, pos)
		}
	}
	tag, _ := w.BlockEntityTag(3, 90, 3)
	if age, _ := nbt.Get[nbt.Long](tag, "Age"); age != rules.EndGatewayBeamInterval {
		t.Errorf("gateway age after one restart tick = %d, want %d", age, rules.EndGatewayBeamInterval)
	}
}
