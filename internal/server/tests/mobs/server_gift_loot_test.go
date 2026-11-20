package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: DataComponentMatchers, Chicken.aiStep
func TestPackChickenVariantChoosesGift(t *testing.T) {
	// Pack resources are process-wide, so this test must not be parallel.
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "gifts", "minecraft/loot_table/gameplay/chicken_lay.json",
		`{"type":"minecraft:gift","pools":[{"rolls":1,"conditions":[{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:components":{"minecraft:chicken/variant":"cold"}}}],"entries":[{"type":"minecraft:item","name":"minecraft:diamond"}]}]}`)
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
