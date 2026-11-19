package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BlockLootSubProvider.createShulkerBoxDrop, ShulkerBoxBlock.playerWillDestroy
func TestShulkerBoxPlayerDropsKeepLock(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"survival", "creative"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.GameMode = mode
			srv, c := servertest.Join(t, cfg, "Breaker", servertest.OpsListing(t, "Breaker"))
			servertest.HoldTicks(t, srv)
			c.MoveToGround(3.5, 64, 3.5, true)
			c.Settle()
			servertest.Setblock(t, c, 3, 64, 0, `purple_shulker_box{lock:{items:"minecraft:tripwire_hook"},CustomName:{text:"Locked box"},Items:[{Slot:0b,id:"minecraft:diamond",count:3}]}`)
			srv.GiveSlot("Breaker", player.HotbarSlot, servertest.ItemStack(t, "minecraft:netherite_pickaxe", 1))
			c.BreakBlock(3, 64, 0)
			var drop protocol.ItemStack
			servertest.CollectDropsTicks(c, srv, 100, func(stack protocol.ItemStack) bool {
				if registry.Item(stack.ItemID) != registry.ItemPurpleShulkerBox {
					return false
				}
				drop = stack
				return true
			})
			if drop.Empty() {
				t.Fatal("broken box did not send a drop")
			}
			// Locks have no network codec; inspect the server's dropped stack.
			items := srv.ItemsInDimension(world.DimensionName)
			if len(items) != 1 {
				t.Fatalf("got %d drops, want one box", len(items))
			}
			assertShulkerDropLock(t, items[0].Stack)
			if drop.CustomName == nil || drop.CustomName.Plain() != "Locked box" {
				t.Errorf("drop custom name = %v, want Locked box", drop.CustomName)
			}
			if len(drop.Container) != 1 || registry.Item(drop.Container[0].ItemID) != registry.ItemDiamond || drop.Container[0].Count != 3 {
				t.Errorf("drop contents = %+v, want three diamonds", drop.Container)
			}
		})
	}
}

func assertShulkerDropLock(t *testing.T, stack protocol.ItemStack) {
	t.Helper()
	if stack.Lock == nil || stack.Lock.Entries()["items"] != nbt.String("minecraft:tripwire_hook") {
		t.Errorf("drop lock = %v, want tripwire_hook", stack.Lock)
	}
}
