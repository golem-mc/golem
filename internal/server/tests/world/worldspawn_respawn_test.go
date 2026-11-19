package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: TeleportTransition.createDefault, PlayerSpawnFinder.findSpawn
func TestWorldSpawnRespawnKeepsRotation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		flooded bool
		wantY   float64
	}{
		{name: "search", wantY: 64},
		{name: "height_fixup", flooded: true, wantY: 65},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c, entityID := servertest.JoinAs(t, cfg, "Tester",
				servertest.OpsListing(t, "Tester"))
			servertest.HoldTicks(t, srv)
			srv.SetGameRuleValue("respawn_radius", 0)
			if tc.flooded {
				// The only candidate is water, so the search must fix up the
				// suggested height instead of finding a solid surface.
				if !srv.SetBlock(10, 64, 20, registry.BlockWater.DefaultState()) {
					t.Fatal("could not flood the spawn column")
				}
			}
			c.ChatCommand("setworldspawn 10 80 20 45 -30")
			servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success", "10", "80", "20", "45.0", "-30.0", "minecraft:overworld")

			pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Tester")
			if noBlock {
				t.Error("respawn without a personal spawn reported an invalid respawn block")
			}
			if pos.X != 10.5 || pos.Y != tc.wantY || pos.Z != 20.5 {
				t.Errorf("respawn position = (%v, %v, %v), want (10.5, %v, 20.5)", pos.X, pos.Y, pos.Z, tc.wantY)
			}
			if pos.Yaw != 45 || pos.Pitch != -30 {
				t.Errorf("respawn rotation = (%v, %v), want yaw 45 pitch -30", pos.Yaw, pos.Pitch)
			}
		})
	}
}
