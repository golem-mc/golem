package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAZombieWalksRoundAPuddleRatherThanWadingIt sinks a 5x5 pool one deep
// into the floor midway between a zombie and its prey: water and the land
// beside it cost a walker 8 each, so the hunt goes round the pool.
// vanilla: PathType WATER, WATER_BORDER, WalkNodeEvaluator.checkNeighbourBlocks
func TestAZombieWalksRoundAPuddleRatherThanWadingIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000) // midnight, so the sun does not burn the hunt off
	c := connect().Join("Prey")
	c.MoveToGround(6.5, 64, 0.5, true)
	c.Settle()

	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			srv.SetBlock(x, 63, z, water)
		}
	}

	zombieID := srv.SpawnMob("minecraft:zombie", -5.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	servertest.HoldTicks(t, srv)
	for tick := range 200 {
		servertest.RunTicks(t, srv, 1)
		x, y, z, ok := srv.MobPos(zombieID)
		if !ok {
			t.Fatalf("tick %d: the zombie is gone", tick)
		}
		bx, bz := int32(math.Floor(x)), int32(math.Floor(z))
		if y < 64 && bx >= -2 && bx <= 2 && bz >= -2 && bz <= 2 {
			t.Fatalf("tick %d: the zombie wades the pool at (%.2f, %.2f, %.2f)", tick, x, y, z)
		}
	}
	// The hunt did pass the pool, rather than keep off it by standing still.
	if x, _, _, _ := srv.MobPos(zombieID); x < 3 {
		t.Fatalf("the zombie stopped at x=%.2f, short of the far side of the pool", x)
	}
}
