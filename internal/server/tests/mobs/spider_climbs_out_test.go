package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAWanderingSpiderClimbsOutOfAPit: a neutral spider on the floor of a
// 3x3 pit four blocks deep wanders to stroll spots on the rim and climbs the
// wall to them, though no path leads out.
// vanilla: WallClimberNavigation.createPath, WallClimberNavigation.tick
func TestAWanderingSpiderClimbsOutOfAPit(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "creative", goatOp, "", 0, servertest.OpsListing(t, goatOp))
	servertest.HoldTicks(t, srv)
	// A stone slab in chunk 0,0 with a pit 3 wide and 4 deep: floor at y=99,
	// rim at y=103, so standing on the rim is a feet height of 104.
	c.ChatCommand("fill 2 96 2 14 103 14 minecraft:stone")
	c.ChatCommand("fill 2 104 2 14 112 14 minecraft:air")
	c.ChatCommand("fill 7 100 7 9 103 9 minecraft:air")
	c.ChatCommand("tp Herder 14.5 104 14.5") // near enough that the spider does not despawn
	c.Settle()
	id := srv.SpawnMob("minecraft:spider", 8.5, 100, 8.5)
	servertest.UntilTicks(t, srv, 1200, "the spider climbs out", func() bool {
		_, y, _, ok := srv.MobPos(id)
		return ok && y >= 104
	})
}
