package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A player's pearl, dead after one tick, still holds its chunk entity-ticking
// for the forty ticks its ender_pearl ticket lasts, then lets it freeze.
// vanilla: ServerPlayer.placeEnderPearlTicket, TicketType.ENDER_PEARL
func TestADeadPearlsTicketKeepsItsChunkTickingForFortyTicks(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = 10, 4
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Watcher"))
	c := connect()
	c.ViewAsk = 10
	c.Join("Watcher")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	x := 10*16 + 8.5
	pig := srv.SpawnMob("minecraft:pig", x, 270, 8.5)
	srv.ThrowEnderPearlOf("Watcher", x, 250, 8.5, 0, 0, 1)
	servertest.RunTicks(t, srv, 1)
	c.ChatCommand("kill @e[type=minecraft:ender_pearl]")
	servertest.RunTicks(t, srv, 5)
	early := mobY(t, srv, pig)
	if early >= 270 {
		t.Fatalf("pig y = %v after the pearl's tick, want it falling", early)
	}
	servertest.RunTicks(t, srv, 40)
	at45 := mobY(t, srv, pig)
	servertest.RunTicks(t, srv, 15)
	if at60 := mobY(t, srv, pig); at60 != at45 {
		t.Errorf("pig y = %v at tick 60 and %v at tick 45, want it frozen once the ticket ran out", at60, at45)
	}
	if at45 >= early {
		t.Errorf("pig y = %v at tick 45 vs %v at tick 6, want it to have kept falling", at45, early)
	}
}
