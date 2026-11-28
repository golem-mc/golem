package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestFireSpreadRadiusAroundPlayer checks the gate on lava lighting the
// planks beside it: only a non-spectator player within the radius lets fire
// appear.
// vanilla: ServerLevel.canSpreadFireAround
func TestFireSpreadRadiusAroundPlayer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		dist     float64
		mode     int32
		wantFire bool
	}{
		{"near survival", 4, protocol.GameModeSurvival, true},
		{"far survival", 40, protocol.GameModeSurvival, false},
		{"near spectator", 4, protocol.GameModeSpectator, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
			servertest.HoldTicks(t, srv)
			c := connect()
			c.Join("Tester")
			srv.SetGameRuleValue("fire_spread_radius_around_player", 8)
			srv.SetGameRuleValue("random_tick_speed", 1000)
			srv.SetPlayerGameMode("Tester", tc.mode)
			// A tick the join left running may have read the mode before it
			// changed and the position after the move below.
			servertest.RunTicks(t, srv, 1)

			planks := servertest.BlockState(t, "minecraft:oak_planks")
			lava := servertest.BlockState(t, "minecraft:lava")
			const lx, ly, lz = 0, 64, 70
			if !srv.SetBlock(lx, ly, lz, lava) {
				t.Fatal("the lava's chunk was not loaded")
			}
			for _, d := range [][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}} {
				srv.SetBlock(lx+d[0], ly+d[1], lz+d[2], planks)
			}
			c.MoveToGround(0.5, 64, lz+0.5-tc.dist, true)
			c.Settle()

			fire := func() bool {
				for dx := int32(-2); dx <= 2; dx++ {
					for dy := int32(0); dy <= 2; dy++ {
						for dz := int32(-2); dz <= 2; dz++ {
							if name, _ := servertest.StateProps(srv.BlockAt(lx+dx, ly+dy, lz+dz)); name == "minecraft:fire" {
								return true
							}
						}
					}
				}
				return false
			}
			if tc.wantFire {
				servertest.UntilTicks(t, srv, 200, "fire beside the lava", fire)
				return
			}
			servertest.RunTicks(t, srv, 200)
			if fire() {
				t.Error("fire appeared with no player in range")
			}
		})
	}
}
