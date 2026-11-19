package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/registry"
)

// TestSetblockWakesThePhysics checks that a block /setblock puts in the world
// is one the physics engine hears about, the way a player's placement is:
// sand the command left hanging over air falls, and a torch whose ground it
// removed breaks at once.
func TestSetblockWakesThePhysics(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	// The spawn the join puts the player at is read out of the join stream
	// itself, which ends at the teleport that puts them there; the rest of
	// the join is skipped past afterwards.
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}
	torch, ok := registry.FindState("minecraft:torch", nil)
	if !ok {
		t.Fatal("the registry has no torch")
	}

	// Sand put two blocks above the ground hangs over air, and the physics
	// engine's neighbour update schedules its fall, which the tick runs:
	// down to the ground, leaving air where it started.
	c.ChatCommand("setblock 5 66 3 sand")
	servertest.AwaitBlockUpdate(t, c, 5, 66, 3, sand)
	servertest.UntilTicks(t, srv, 100, "block at (5, 64, 3)", func() bool { return srv.BlockAt(5, 64, 3) == sand })
	if got := srv.BlockAt(5, 66, 3); got != 0 {
		t.Errorf("block the sand fell from = %d, want 0 (air)", got)
	}

	// A torch whose ground the command takes away breaks: the change the
	// command made is one its neighbours react to.
	c.ChatCommand("setblock 8 64 3 torch")
	servertest.AwaitBlockUpdate(t, c, 8, 64, 3, torch)
	c.ChatCommand("setblock 8 63 3 air")
	servertest.UntilTicks(t, srv, 100, "block at (8, 64, 3)", func() bool { return srv.BlockAt(8, 64, 3) == 0 })
	c.Disconnect()
}
