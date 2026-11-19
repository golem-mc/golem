package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: WalkNodeEvaluator.getMobJumpHeight, Camel constructor
func TestACamelLuredAcrossATallBarrierClimbsOver(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ kind, food string }{
		{"camel", "cactus"}, {"camel_husk", "rabbit_foot"},
	} {
		for _, barrier := range []string{"oak_fence", "cobblestone_wall", "oak_fence_gate"} {
			t.Run(tc.kind+"/"+barrier, func(t *testing.T) {
				t.Parallel()
				srv, c := servertest.BreedingPlayer(t, "creative", "Lure", "minecraft:"+tc.food, 1, func(s *server.Server) {
					s.SetWorldAge(1000)
					servertest.FreezeUntilHeld(t, s)
				}, servertest.OpsListing(t, "Lure"))
				servertest.HoldTicks(t, srv)
				// A single chunk holds the pen, with walls too high to go around.
				c.ChatCommand("fill 1 64 1 8 67 10 minecraft:stone")
				c.ChatCommand("fill 2 64 2 7 67 9 minecraft:air")
				c.ChatCommand("fill 8 64 2 8 67 9 minecraft:air")
				c.ChatCommand("fill 6 64 2 6 64 9 minecraft:" + barrier)
				c.MoveToGround(11.5, 64, 5.5, true)
				c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
				id := srv.SpawnMobSeeded("minecraft:"+tc.kind, 3.5, 64, 5.5, 1)
				servertest.UntilTicks(t, srv, 200, "the camel across the barrier", func() bool {
					x, _, _, _ := srv.MobPos(id)
					return x > 7.5
				})
			})
		}
	}
}
