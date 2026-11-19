package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: AbstractSkeleton.registerGoals
func TestASkeletonFleesAWolf(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Observer", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	c.Settle()
	srv.SetDayTime(6000)
	stone := registry.MustState("minecraft:stone", nil)
	air := registry.MustState("minecraft:air", nil)
	// Keep the entire escape path shaded, with room to run beyond six blocks.
	for x := int32(0); x <= 31; x++ {
		for z := int32(0); z <= 31; z++ {
			srv.SetBlock(x, 63, z, stone)
			srv.SetBlock(x, 67, z, stone)
			for y := int32(64); y < 67; y++ {
				state := air
				if x == 0 || x == 31 || z == 0 || z == 31 {
					state = stone
				}
				srv.SetBlock(x, y, z, state)
			}
		}
	}
	srv.RelightAt(16, 16)
	wolf := srv.SpawnMob("minecraft:wolf", 16.5, 64, 16.5)
	if !srv.SetMobNoAI(wolf) {
		t.Fatal("wolf is missing")
	}
	skeleton := srv.SpawnMobSeeded("minecraft:skeleton", 19.5, 64, 16.5, 3468)
	servertest.UntilTicks(t, srv, 100, "skeleton more than six blocks from the wolf", func() bool {
		x, _, z, ok := srv.MobPos(skeleton)
		wx, _, wz, wolfOK := srv.MobPos(wolf)
		return ok && wolfOK && (x-wx)*(x-wx)+(z-wz)*(z-wz) > 36
	})
}
