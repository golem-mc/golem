package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ItemStack.releaseUsing, BowItem.releaseUsing, CrossbowItem.releaseUsing,
// TridentItem.releaseUsing, SpyglassItem.releaseUsing
func TestAGivenUseCooldownStartsOnRelease(t *testing.T) {
	t.Parallel()
	for _, item := range []string{"bow", "crossbow", "trident", "spyglass"} {
		t.Run(item, func(t *testing.T) {
			t.Parallel()
			const name = "Archer"
			srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
			giveHeld(t, srv, c, name, item+`[use_cooldown={seconds:1}]`)
			srv.GiveSlot(name, player.HotbarSlot+1, protocol.ItemStack{ItemID: int32(registry.ItemArrow), Count: 5})
			servertest.HoldTicks(t, srv)
			servertest.UseHeld(c, -90, 0)
			c.ExpectNoneWithinTicks(srv, 30, isUseCooldown)
			c.ReleaseUseItem()
			expectUseCooldown(t, srv, c, "minecraft:"+item, 20)
			servertest.UseHeld(c, -90, 0)
			c.ExpectNoneWithinTicks(srv, 1, isUseCooldown)
			if srv.BowTicks(name) != 0 {
				t.Fatal("another use started inside the cooldown")
			}
			if item == "crossbow" && len(srv.InventorySlot(name, player.HotbarSlot).ChargedProjectiles()) == 0 {
				t.Fatal("the loaded crossbow fired inside the cooldown")
			}
		})
	}
}

// vanilla: ItemStack.use, FireworkRocketItem.use
func TestAGivenUseCooldownOnFireworkStartsInAir(t *testing.T) {
	t.Parallel()
	const name = "Glider"
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
	giveHeld(t, srv, c, name, `firework_rocket[use_cooldown={seconds:1}] 2`)
	srv.GiveSlot(name, player.ArmorSlot+1, protocol.ItemStack{ItemID: int32(registry.ItemElytra), Count: 1})
	servertest.HoldTicks(t, srv)
	servertest.UseHeld(c, -90, 0)
	c.ExpectNoneWithinTicks(srv, 1, isUseCooldown)
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 2 {
		t.Fatalf("grounded rocket use left %d rockets, want 2", got)
	}
	c.MoveToGrounded(0.5, 66, 0.5, -90, 10, false)
	servertest.StartFallFlying(c)
	c.ExpectNoneWithinTicks(srv, 1, isUseCooldown)
	if !srv.PlayerFallFlying(name) {
		t.Fatal("the wings did not open")
	}
	servertest.UseHeld(c, -90, 0)
	expectUseCooldown(t, srv, c, "minecraft:firework_rocket", 20)
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 1 {
		t.Fatalf("rocket boost left %d rockets, want 1", got)
	}
	servertest.UseHeld(c, -90, 0)
	c.ExpectNoneWithinTicks(srv, 1, isUseCooldown)
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 1 {
		t.Fatalf("use inside the cooldown left %d rockets, want 1", got)
	}
}

// vanilla: ItemStack.use, ItemStack.applyAfterUseComponentSideEffects
func TestAGivenUseCooldownOnInstantUses(t *testing.T) {
	t.Parallel()
	for _, item := range []string{
		"fishing_rod", "map", "writable_book", "diamond_helmet",
		"bucket", "water_bucket", "sulfur_cube_bucket", "glass_bottle",
		"oak_boat", "cow_spawn_egg", "lily_pad",
	} {
		t.Run(item, func(t *testing.T) {
			t.Parallel()
			const name = "User"
			srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
			giveHeld(t, srv, c, name, item+`[use_cooldown={seconds:1,cooldown_group:"test:instant"}]`)
			servertest.HoldTicks(t, srv)
			// The air-use ray sees a pool wide enough for a boat's hull.
			for x := int32(-2); x <= 2; x++ {
				for z := int32(0); z <= 4; z++ {
					srv.SetBlock(x, 63, z, registry.BlockWater.DefaultState())
				}
			}
			servertest.UseHeld(c, 0, 45)
			expectUseCooldown(t, srv, c, "test:instant", 20)
			c.ExpectNoneWithinTicks(srv, 0, isUseCooldown)
		})
	}
}

// vanilla: BowItem.releaseUsing, CrossbowItem.releaseUsing, TridentItem.releaseUsing
func TestAGivenUseCooldownDoesNotStartOnShortDraw(t *testing.T) {
	t.Parallel()
	for _, item := range []string{"bow", "crossbow", "trident"} {
		t.Run(item, func(t *testing.T) {
			t.Parallel()
			const name = "Archer"
			srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
			giveHeld(t, srv, c, name, item+`[use_cooldown={seconds:1}]`)
			srv.GiveSlot(name, player.HotbarSlot+1, protocol.ItemStack{ItemID: int32(registry.ItemArrow), Count: 5})
			servertest.HoldTicks(t, srv)
			servertest.UseHeld(c, -90, 0)
			c.ExpectNoneWithinTicks(srv, 0, isUseCooldown)
			if srv.BowTicks(name) == 0 {
				t.Fatal("the draw did not start")
			}
			c.ReleaseUseItem()
			c.ExpectNoneWithinTicks(srv, 1, isUseCooldown)
		})
	}
}

// vanilla: ItemStack.use applies side effects only for InteractionResult.Success.
func TestAGivenUseCooldownDoesNotStartOnFailedInstantUse(t *testing.T) {
	t.Parallel()
	for _, item := range []string{"stick", "bucket", "oak_boat", "carrot_on_a_stick", "knowledge_book"} {
		t.Run(item, func(t *testing.T) {
			t.Parallel()
			const name = "User"
			srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
			giveHeld(t, srv, c, name, item+`[use_cooldown={seconds:1}]`)
			servertest.HoldTicks(t, srv)
			servertest.UseHeld(c, 0, 0)
			c.ExpectNoneWithinTicks(srv, 1, isUseCooldown)
		})
	}
}

func isUseCooldown(id int32, _ []byte) bool { return id == protocol.IDPlayCooldown }

func expectUseCooldown(t *testing.T, srv *server.Server, c *servertest.Client, group string, ticks int32) {
	t.Helper()
	if !c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		if !isUseCooldown(id, data) {
			return false
		}
		r := protocol.NewReader(data)
		gotGroup, gotTicks := r.ReadString(256), r.ReadVarInt()
		if r.Err() != nil || gotGroup != group || gotTicks != ticks {
			t.Fatalf("Set Cooldown = %q %d (%v), want %q %d", gotGroup, gotTicks, r.Err(), group, ticks)
		}
		return true
	}) {
		t.Fatalf("no Set Cooldown for %s", group)
	}
}

// TestAGivenUseCooldownStartsItsGroup: a snowball given its own use_cooldown
// with a group of its own starts that group's 40 ticks when thrown, and a
// throw inside them throws nothing.
// vanilla: UseCooldown, ItemCooldowns.getCooldownGroup, ServerPlayerGameMode.useItem
func TestAGivenUseCooldownStartsItsGroup(t *testing.T) {
	t.Parallel()
	const name = "Thrower"
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
	giveHeld(t, srv, c, name, `snowball[use_cooldown={seconds:2,cooldown_group:"test:g"}] 16`)
	servertest.HoldTicks(t, srv)

	servertest.UseHeld(c, 0, 0)
	ticks := int32(-1)
	c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		if id != protocol.IDPlayCooldown {
			return false
		}
		r := protocol.NewReader(data)
		if group := r.ReadString(256); group != "test:g" {
			t.Errorf("Set Cooldown names group %q, want test:g", group)
		}
		ticks = r.ReadVarInt()
		return true
	})
	if ticks != 40 {
		t.Fatalf("Set Cooldown ticks = %d, want 40", ticks)
	}
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 15 {
		t.Fatalf("snowballs after the first throw = %d, want 15", got)
	}

	servertest.RunTicks(t, srv, 5)
	servertest.UseHeld(c, 0, 0)
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 15 {
		t.Errorf("snowballs after a throw inside the cooldown = %d, want 15", got)
	}
}

// TestAGivenUseCooldownOnFoodStartsWhenEaten: an apple given a use_cooldown
// without a group starts the apple's own when the meal finishes.
// vanilla: ItemStack.applyAfterUseComponentSideEffects
func TestAGivenUseCooldownOnFoodStartsWhenEaten(t *testing.T) {
	t.Parallel()
	const name = "Eater"
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
	giveHeld(t, srv, c, name, `apple[use_cooldown={seconds:1}]`)
	srv.SetFood(name, 10)
	servertest.HoldTicks(t, srv)

	servertest.UseHeld(c, 0, 0)
	ticks := int32(-1)
	// An apple is eaten in 32 ticks.
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayCooldown {
			return false
		}
		r := protocol.NewReader(data)
		if group := r.ReadString(256); group != "minecraft:apple" {
			t.Errorf("Set Cooldown names group %q, want minecraft:apple", group)
		}
		ticks = r.ReadVarInt()
		return true
	})
	if ticks != 20 {
		t.Fatalf("Set Cooldown ticks = %d, want 20", ticks)
	}
	if !srv.InventorySlot(name, player.HotbarSlot).Empty() {
		t.Error("the apple was not eaten")
	}
}
