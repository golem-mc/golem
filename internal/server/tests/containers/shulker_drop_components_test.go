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

// vanilla: CopyComponentsFunction.run, RandomizableContainerBlockEntity.collectImplicitComponents
func TestShulkerBoxNonPlayerDropsKeepDeferredLoot(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"destroy", "piston"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Breaker", servertest.OpsListing(t, "Breaker"))
			servertest.HoldTicks(t, srv)
			servertest.Setblock(t, c, 3, 64, 0, `shulker_box{lock:{items:"minecraft:tripwire_hook"},LootTable:"minecraft:chests/simple_dungeon",LootTableSeed:42L}`)
			if method == "piston" {
				servertest.Setblock(t, c, 2, 64, 0, "piston[facing=east]")
				servertest.Setblock(t, c, 1, 64, 0, "redstone_block")
				servertest.RunTicks(t, srv, 3)
			} else {
				c.ChatCommand("setblock 3 64 0 air destroy")
				c.ExpectCommandFeedback("commands.setblock.success", "3", "64", "0")
			}
			items := srv.ItemsInDimension(world.DimensionName)
			if len(items) != 1 || registry.Item(items[0].Stack.ItemID) != registry.ItemShulkerBox {
				t.Fatalf("drops = %+v, want one shulker box", items)
			}
			drop := items[0].Stack
			assertShulkerDropLock(t, drop)
			if drop.ContainerLoot != (protocol.ContainerLoot{Table: "minecraft:chests/simple_dungeon", Seed: 42}) {
				t.Errorf("drop deferred loot = %+v, want simple_dungeon with seed 42", drop.ContainerLoot)
			}
			if len(drop.Container) != 0 {
				t.Errorf("drop has %d generated slots, want no generated contents", len(drop.Container))
			}
			// Placing the drop must restore the unresolved table and lock.
			srv.GiveSlot("Breaker", player.HotbarSlot, drop)
			c.PlaceBlock(5, 63, 0, protocol.FaceTop)
			c.Settle()
			tag, ok := srv.BlockEntityTagAt(5, 64, 0)
			if !ok {
				t.Fatal("placed drop has no block entity")
			}
			if tag["LootTable"] != nbt.String("minecraft:chests/simple_dungeon") || tag["LootTableSeed"] != nbt.Long(42) {
				t.Errorf("placed drop lost deferred loot: %v", tag)
			}
			lock, _ := nbt.Get[nbt.CompoundTag](tag, "lock")
			if lock == nil || lock.Entries()["items"] != nbt.String("minecraft:tripwire_hook") {
				t.Errorf("placed drop lock = %v, want tripwire_hook", lock)
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

// vanilla: ShulkerBoxBlock.playerWillDestroy, RandomizableContainerBlockEntity.isEmpty
func TestShulkerBoxPlayerBreakUnpacksLoot(t *testing.T) {
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
			servertest.Setblock(t, c, 3, 64, 0, `shulker_box{lock:{items:"minecraft:tripwire_hook"},LootTable:"minecraft:chests/simple_dungeon",LootTableSeed:42L}`)
			srv.GiveSlot("Breaker", player.HotbarSlot, servertest.ItemStack(t, "minecraft:netherite_pickaxe", 1))
			c.BreakBlock(3, 64, 0)
			var drop protocol.ItemStack
			servertest.CollectDropsTicks(c, srv, 100, func(stack protocol.ItemStack) bool {
				if registry.Item(stack.ItemID) != registry.ItemShulkerBox {
					return false
				}
				drop = stack
				return true
			})
			if drop.Empty() {
				t.Fatal("broken box did not send a drop")
			}
			if len(drop.Container) == 0 {
				t.Error("drop has no generated contents")
			}
			if drop.ContainerLoot != (protocol.ContainerLoot{}) {
				t.Errorf("drop kept deferred loot %+v after unpacking", drop.ContainerLoot)
			}
			items := srv.ItemsInDimension(world.DimensionName)
			if len(items) != 1 {
				t.Fatalf("got %d drops, want one box", len(items))
			}
			assertShulkerDropLock(t, items[0].Stack)
		})
	}
}
