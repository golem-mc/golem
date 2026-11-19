package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// An allay plans its flight through water at the water's price, where a bee
// refuses the cell. Swim only lifts it to the roof of a covered pool, so it
// leaves by the one hole at the far side only by a path through the water.
// vanilla: FlyNodeEvaluator.findAcceptedNode, PathType.WATER, AllayAi's Swim
func TestAnAllayFliesUpOutOfAPoolToItsPlayer(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "minecraft:cobblestone", 64)
	stone := registry.BlockStates["minecraft:stone"]
	water := registry.MustState("minecraft:water", map[string]string{"level": "0"})
	// A stone bank at y 63 with a 5x5 pool x 4..8, z -2..2, three deep
	// (y 61..63) on a stone floor at y 60.
	for x := int32(-3); x <= 11; x++ {
		for z := int32(-5); z <= 5; z++ {
			for y := int32(64); y <= 72; y++ {
				srv.SetBlock(x, y, z, 0)
			}
			for y := int32(60); y <= 63; y++ {
				srv.SetBlock(x, y, z, stone)
			}
		}
	}
	for x := int32(4); x <= 8; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(61); y <= 63; y++ {
				srv.SetBlock(x, y, z, water)
			}
		}
	}
	// The player steps down into the pool to hand the allay in its far corner
	// the stack, then back onto the bank 3 blocks from the pool's edge.
	c.MoveToGround(7.5, 61, 1.5, true)
	c.Settle()
	id := srv.SpawnMob(allay, 8.5, 61, 2.5)
	c.ExpectMobSpawn(allayType())
	c.UseOn(id)
	c.Settle()
	if held := srv.MobEquipment(id, "mainhand"); held.ItemID != servertest.MustItemID(t, "minecraft:cobblestone") {
		t.Fatalf("allay holds %v, want cobblestone", held)
	}
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	// A stone roof over the pool but for one hole at the edge by the player.
	for x := int32(4); x <= 8; x++ {
		for z := int32(-2); z <= 2; z++ {
			if x != 4 || z != 0 {
				srv.SetBlock(x, 64, z, stone)
			}
		}
	}
	// A matching stack it picks up gives it something to deliver.
	srv.SpawnItemEntity(8.5, 61, 2.5, "minecraft:cobblestone", 1)
	risen := false
	servertest.UntilTicks(t, srv, 200, "the allay out of the pool by its player", func() bool {
		x, y, z, ok := srv.MobPos(id)
		if !ok {
			return false
		}
		risen = risen || y > 64
		// The allay makes for the player's eyes (AllayAi.getLikedPlayerPositionTracker).
		return risen && math.Hypot(math.Hypot(x-0.5, y-(64+1.62)), z-0.5) <= 3
	})
}
