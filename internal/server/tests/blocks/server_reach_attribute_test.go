package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestARaisedBlockInteractionRangeLetsAFarDigLand: a dig is verified
// against the player's minecraft:block_interaction_range, not a fixed
// reach, so a base of 10 (10.5 with the creative modifier, plus the 1.0
// buffer) reaches a block 8.5 from the eyes and the default 4.5 does not.
// vanilla: Player.blockInteractionRange, ServerPlayerGameMode.handleBlockBreakAction
func TestARaisedBlockInteractionRangeLetsAFarDigLand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Reacher", servertest.OpsListing(t, "Reacher"))
	c.MoveToGround(0.5, 64, 0.5, true)
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	// Eyes at (0.5, 65.62, 0.5); the block's near face is 8.5 away on x.
	const x, y, z = 9, 65, 0

	t.Run("raised", func(t *testing.T) {
		c.ChatCommand("attribute @s minecraft:block_interaction_range base set 10")
		srv.SetBlock(x, y, z, stone)
		c.Settle()
		c.StartDigging(x, y, z)
		got := c.WaitBlockChanged()
		if got.X != x || got.Y != y || got.Z != z || got.State != 0 {
			t.Errorf("block change = %+v, want air (0) at (%d, %d, %d)", got, x, y, z)
		}
		servertest.ExpectAir(t, srv, [3]int32{x, y, z})
	})

	t.Run("default", func(t *testing.T) {
		c.ChatCommand("attribute @s minecraft:block_interaction_range base set 4.5")
		srv.SetBlock(x, y, z, stone)
		c.Settle()
		c.StartDigging(x, y, z)
		c.ExpectBlockAck()
		c.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlayBlockUpdate || id == protocol.IDPlaySectionBlocks {
				t.Errorf("a refused dig sent block packet %#x", id)
			}
		})
		if state := srv.BlockAt(x, y, z); state != stone {
			t.Errorf("block after the refused dig = %d, want stone (%d)", state, stone)
		}
	})
}

// TestARaisedBlockInteractionRangeLetsABucketFillFromFar: an item's ray is
// as long as the player's minecraft:block_interaction_range, so a bucket
// aimed at water whose near face is 7.5 from the eyes scoops it at a base
// of 10 and comes back empty at the default 4.5.
// vanilla: Item.getPlayerPOVHitResult, BucketItem.use
func TestARaisedBlockInteractionRangeLetsABucketFillFromFar(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Scooper", servertest.OpsListing(t, "Scooper"))
	c.MoveToGround(0.5, 64, 0.5, true)
	bucket := servertest.MustItemID(t, "minecraft:bucket")
	waterBucket := servertest.MustItemID(t, "minecraft:water_bucket")
	water := registry.MustState("minecraft:water", nil)
	srv.GiveSlot("Scooper", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})
	// Eyes at (0.5, 65.62, 0.5), looking along +x at the source's west face.
	const x, y, z = 8, 65, 0
	if !srv.SetBlock(x, y, z, water) {
		t.Fatal("the world did not take the water")
	}
	c.Settle()

	servertest.UseHeld(c, -90, 0)
	c.Settle()
	if got := srv.BlockAt(x, y, z); got != water {
		t.Errorf("the water after a use at the default range = %d, want it still there", got)
	}
	if held := srv.InventorySlot("Scooper", player.HotbarSlot); held.ItemID != bucket {
		t.Errorf("the hand after a use at the default range = %+v, want the empty bucket", held)
	}

	c.ChatCommand("attribute @s minecraft:block_interaction_range base set 10")
	c.Settle()
	servertest.UseHeld(c, -90, 0)
	got := c.ExpectBucketUseResult()
	if got.Update.X != x || got.Update.Y != y || got.Update.Z != z || got.Update.State != 0 {
		t.Errorf("the scoop's block change = %+v, want air at (%d, %d, %d)", got.Update, x, y, z)
	}
	if held := srv.InventorySlot("Scooper", player.HotbarSlot); held.ItemID != waterBucket || held.Count != 1 {
		t.Errorf("the hand after the scoop = %+v, want one water bucket", held)
	}
}
