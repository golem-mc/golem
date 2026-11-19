package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Chicken.aiStep, Turtle.ageBoundaryReached
func TestPackOverridesChickenAndTurtleGifts(t *testing.T) {
	// Pack resources are process-wide, so this test must not be parallel.
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	for _, table := range []string{"chicken_lay", "turtle_grow"} {
		servertest.WritePackFile(t, cfg, "gifts", "minecraft/loot_table/gameplay/"+table+".json",
			`{"type":"minecraft:gift","pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:diamond"}]}]}`)
	}
	srv, c, _ := servertest.JoinAs(t, cfg, "Farmer", servertest.OpsListing(t, "Farmer"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon chicken ~ ~ ~ {EggLayTime:1}")
	c.ChatCommand("summon turtle ~2 ~ ~ {Age:-1}")
	c.Settle()
	servertest.UntilTicks(t, srv, 5, "both gift tables drop diamonds", func() bool {
		return len(srv.ItemsInDimension(world.DimensionName)) >= 2
	})
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 2 {
		t.Fatalf("gifts = %+v, want two diamond entities", items)
	}
	for _, item := range items {
		if item.Item != "minecraft:diamond" || item.Count != 1 {
			t.Errorf("gift = %+v, want one diamond and no egg or scute", item)
		}
	}
}

func TestColdChickenLaysBlueEggWithoutPack(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Farmer", servertest.OpsListing(t, "Farmer"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon chicken ~2 ~ ~ {variant:"minecraft:cold",EggLayTime:1}`)
	c.Settle()
	servertest.UntilTicks(t, srv, 5, "the cold chicken lays", func() bool {
		return len(srv.ItemsInDimension(world.DimensionName)) > 0
	})
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Item != "minecraft:blue_egg" || items[0].Count != 1 {
		t.Fatalf("cold chicken laid %+v, want one blue egg", items)
	}
}

// vanilla: DataComponentMatchers, Chicken.aiStep
func TestPackChickenVariantChoosesGift(t *testing.T) {
	// Pack resources are process-wide, so this test must not be parallel.
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "gifts", "minecraft/loot_table/gameplay/chicken_lay.json",
		`{"type":"minecraft:gift","pools":[{"rolls":1,"conditions":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:components":{"minecraft:chicken/variant":"cold"}}}],"entries":[{"type":"minecraft:item","name":"minecraft:diamond"}]}]}`)
	srv, c, _ := servertest.JoinAs(t, cfg, "Farmer", servertest.OpsListing(t, "Farmer"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon chicken ~2 ~ ~ {variant:"minecraft:cold",EggLayTime:1}`)
	c.ChatCommand(`summon chicken ~4 ~ ~ {variant:"minecraft:warm",EggLayTime:1}`)
	c.Settle()
	servertest.RunTicks(t, srv, 5)
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Item != "minecraft:diamond" || items[0].Count != 1 {
		t.Fatalf("variant gifts = %+v, want only the cold chicken's diamond", items)
	}
}
