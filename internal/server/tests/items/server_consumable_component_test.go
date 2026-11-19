package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// consumableWorld joins a survival operator named Eater at food level 10
// with the stack given by /give @s.
func consumableWorld(t *testing.T, stack string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	mealedPlayerFile(t, cfg, "Eater")
	srv, c := servertest.Join(t, cfg, "Eater", servertest.OpsListing(t, "Eater"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("give @s " + stack)
	c.Settle()
	c.DrainSurvivalTraffic()
	return srv, c
}

func hotbar(srv *server.Server) bool {
	return !srv.InventorySlot("Eater", player.HotbarSlot).Empty()
}

// TestAStackWithConsumableAndFoodIsEaten: a stick given consumable and food
// is eaten over the default 1.6 seconds and restores its own nutrition.
// vanilla: Consumable.startConsuming, FoodProperties.onConsume
func TestAStackWithConsumableAndFoodIsEaten(t *testing.T) {
	t.Parallel()
	srv, c := consumableWorld(t, "stick[consumable={},food={nutrition:4,saturation:2.4}]")
	if !hotbar(srv) {
		t.Fatal("/give put nothing in the hotbar")
	}
	c.UseItemRightClicks(0)
	c.Settle()
	if !srv.Eating("Eater") {
		t.Fatal("the use of an edible stick starts no meal")
	}
	servertest.RunTicks(t, srv, 20)
	if !hotbar(srv) {
		t.Fatal("the stick was eaten before its 32 ticks")
	}
	servertest.UntilTicks(t, srv, 15, "the stick is eaten", func() bool { return !hotbar(srv) })
	if got := c.ExpectHealthish(); got.Food != 14 {
		t.Errorf("food after the meal = %d, want 14", got.Food)
	}
}

// TestAConsumableTakesItsOwnSeconds: consume_seconds sets the length of the
// use, 0 consuming at once.
// vanilla: Consumable.consumeTicks
func TestAConsumableTakesItsOwnSeconds(t *testing.T) {
	t.Parallel()
	srv, c := consumableWorld(t, "stick[consumable={consume_seconds:0.25}]")
	c.UseItemRightClicks(0)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	if !hotbar(srv) {
		t.Fatal("the stick was consumed before its 5 ticks")
	}
	servertest.UntilTicks(t, srv, 6, "the stick is consumed", func() bool { return !hotbar(srv) })

	srv, c = consumableWorld(t, "stick[consumable={consume_seconds:0}]")
	c.UseItemRightClicks(0)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if hotbar(srv) {
		t.Fatal("a consumable of 0 seconds is not consumed on the use")
	}
}

// TestFoodWithoutConsumableIsNotEdible: a stack with food but no consumable
// starts no use.
// vanilla: Item.use reads the stack's CONSUMABLE
func TestFoodWithoutConsumableIsNotEdible(t *testing.T) {
	t.Parallel()
	srv, c := consumableWorld(t, "stick[food={nutrition:4,saturation:2.4}]")
	c.UseItemRightClicks(0)
	c.Settle()
	if srv.Eating("Eater") {
		t.Fatal("a stick with food but no consumable starts a use")
	}
	servertest.RunTicks(t, srv, 40)
	if !hotbar(srv) {
		t.Fatal("the stick was consumed")
	}
}

// TestAnAppleWithOwnConsumeEffectsAppliesThem: the stack's on_consume_effects
// replace the apple's.
// vanilla: Consumable.onConsume
func TestAnAppleWithOwnConsumeEffectsAppliesThem(t *testing.T) {
	t.Parallel()
	srv, c := consumableWorld(t, `apple[consumable={on_consume_effects:[{type:"minecraft:apply_effects",effects:[{id:"minecraft:speed",duration:200}]}]}]`)
	c.UseItemRightClicks(0)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the apple is eaten", func() bool { return !hotbar(srv) })
	if duration, _, ok := srv.PlayerEffect("Eater", "minecraft:speed"); !ok || duration <= 0 {
		t.Fatal("eating the apple gave no Speed")
	}
}

// TestConsumableAndFoodSurviveARejoin: both components are saved with the
// stack and read back.
func TestConsumableAndFoodSurviveARejoin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Eater"))
	c := connect()
	c.Join("Eater")
	c.ChatCommand("give @s stick[consumable={consume_seconds:0.5},food={nutrition:4,saturation:2.4}]")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool { return srv.PlayerCount() == 0 })
	c = connect()
	c.Join("Eater")
	stack := srv.InventorySlot("Eater", player.HotbarSlot)
	if cons, ok := stack.ConsumableOf(); !ok || cons.Seconds != 0.5 {
		t.Errorf("consumable after rejoin = %+v, %v", cons, ok)
	}
	if food, ok := stack.FoodOf(); !ok || food.Nutrition != 4 || food.Saturation != 2.4 {
		t.Errorf("food after rejoin = %+v, %v", food, ok)
	}
}

// rejoinedWith gives Eater the /give-n stack, has them leave and join again
// so the stack goes through the player file, and holds the loop.
func rejoinedWith(t *testing.T, stack string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	mealedPlayerFile(t, cfg, "Eater")
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Eater"))
	c := connect()
	c.Join("Eater")
	c.ChatCommand("give @s " + stack)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool { return srv.PlayerCount() == 0 })
	c = connect().Join("Eater")
	servertest.HoldTicks(t, srv)
	return srv, c
}

// TestAnAppleWithoutConsumableStartsNoUse: an apple whose patch removes
// consumable keeps the removal through a rejoin and is not eaten.
// vanilla: Item.use, stack.get(DataComponents.CONSUMABLE)
func TestAnAppleWithoutConsumableStartsNoUse(t *testing.T) {
	t.Parallel()
	srv, c := rejoinedWith(t, "apple[!consumable]")
	stack := srv.InventorySlot("Eater", player.HotbarSlot)
	if !stack.Removes(protocol.DataComponentConsumable) {
		t.Fatalf("the reloaded apple %+v lost its consumable removal", stack)
	}
	c.UseItemRightClicks(0)
	c.Settle()
	if srv.Eating("Eater") {
		t.Fatal("an apple without consumable starts a use")
	}
	servertest.RunTicks(t, srv, 40)
	if !hotbar(srv) {
		t.Fatal("the apple without consumable was eaten")
	}
}

// TestAnAppleWithoutFoodIsEatenAtFullHungerAndFeedsNothing: an apple whose
// patch removes food keeps the removal through a rejoin, is eaten with a
// full bar, and a second one eaten at food 10 leaves it at 10.
// vanilla: Consumable.canConsume, Consumable.onConsume
func TestAnAppleWithoutFoodIsEatenAtFullHungerAndFeedsNothing(t *testing.T) {
	t.Parallel()
	srv, c := rejoinedWith(t, "apple[!food] 2")
	stack := srv.InventorySlot("Eater", player.HotbarSlot)
	if !stack.Removes(protocol.DataComponentFood) {
		t.Fatalf("the reloaded apple %+v lost its food removal", stack)
	}
	count := func() int32 { return srv.InventorySlot("Eater", player.HotbarSlot).Count }

	srv.SetFood("Eater", 20)
	c.UseItemRightClicks(0)
	c.Settle()
	if !srv.Eating("Eater") {
		t.Fatal("an apple without food starts no meal at a full bar")
	}
	servertest.UntilTicks(t, srv, 40, "the first apple is eaten", func() bool { return count() == 1 })

	srv.SetFood("Eater", 10)
	c.UseItemRightClicks(0)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the second apple is eaten", func() bool { return count() == 0 })
	if got := srv.PlayerFood("Eater"); got != 10 {
		t.Errorf("food after eating an apple without food = %d, want 10", got)
	}
}
