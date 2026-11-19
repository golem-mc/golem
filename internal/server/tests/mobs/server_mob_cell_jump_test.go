package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// chaseFromCell sets block at (0, 64, 0), spawns an adult zombie at
// (0.5, y, 0.5) with a player six blocks east, and pumps up to 200 ticks
// until the zombie's X passes 1.5. It returns whether it did and the
// highest Y the zombie reached before then.
func chaseFromCell(t *testing.T, block string, props map[string]string, y float64) (bool, float64) {
	t.Helper()
	state, ok := registry.FindState(block, props)
	if !ok {
		t.Fatalf("the block registry has no %s", block)
	}
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Bait")
	srv.SetDayTime(18000) // midnight, whose dark is no daylight burn
	if !srv.SetBlock(0, 64, 0, state) {
		t.Fatalf("the world did not take the %s", block)
	}
	c.MoveToGround(6.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	zombie := srv.SpawnMob("minecraft:zombie", 0.5, y, 0.5)
	if zombie == 0 {
		t.Fatal("the zombie did not spawn")
	}
	srv.SetMobBaby(zombie, false)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	maxY, out := 0.0, false
	c.PumpTicks(srv, 200, func(int32, []byte) bool {
		x, y, _, ok := srv.MobPos(zombie)
		if !ok {
			return false
		}
		out = x > 1.5
		if !out {
			maxY = math.Max(maxY, y)
		}
		return out
	})
	if !out {
		x, y, z, _ := srv.MobPos(zombie)
		t.Fatalf("the zombie never left the %s's cell; it stands at %.2f,%.2f,%.2f", block, x, y, z)
	}
	return out, maxY
}

// TestAZombieInACauldronJumpsOut: the rim (0.75 above the floor) is above the
// zombie's 0.6 step, so it leaves only by the jump a shape rising above its
// feet orders, and by a path whose cauldron node is lifted a block.
// vanilla: MoveControl.tick, PathNavigation.trimPath
func TestAZombieInACauldronJumpsOut(t *testing.T) {
	t.Parallel()
	chaseFromCell(t, "minecraft:cauldron", nil, 64.25)
}
