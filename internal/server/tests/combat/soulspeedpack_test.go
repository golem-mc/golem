package combat

import (
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// soulSpeedOverride is vanilla's soul_speed.json with the speed modifier's
// base amount raised to 0.2.
const soulSpeedOverride = `{"anvil_cost":8,"description":{"translate":"enchantment.minecraft.soul_speed"},
	"max_cost":{"base":25,"per_level_above_first":10},"max_level":3,"min_cost":{"base":10,"per_level_above_first":10},
	"slots":["feet"],"supported_items":"#minecraft:enchantable/foot_armor","weight":1,
	"effects":{"minecraft:location_changed":[{"effect":{"type":"minecraft:all_of","effects":[
		{"type":"minecraft:attribute","amount":{"type":"minecraft:linear","base":0.2,"per_level_above_first":0.0105},
		 "attribute":"minecraft:movement_speed","id":"minecraft:enchantment.soul_speed","operation":"add_value"},
		{"type":"minecraft:attribute","amount":1.0,"attribute":"minecraft:movement_efficiency",
		 "id":"minecraft:enchantment.soul_speed","operation":"add_value"}]},
	"requirements":{"type":"minecraft:all_of","terms":[
		{"type":"minecraft:inverted","term":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:vehicle":{}}}},
		{"type":"minecraft:any_of","terms":[
			{"type":"minecraft:all_of","terms":[
				{"type":"minecraft:enchantment_active_check","active":true},
				{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:flags":{"is_flying":false}}},
				{"type":"minecraft:any_of","terms":[
					{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:movement_affected_by":{"block":{"blocks":"#minecraft:soul_speed_blocks"}}}},
					{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:flags":{"is_on_ground":false}}}]}]},
			{"type":"minecraft:all_of","terms":[
				{"type":"minecraft:enchantment_active_check","active":false},
				{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:flags":{"is_flying":false},
					"minecraft:movement_affected_by":{"block":{"blocks":"#minecraft:soul_speed_blocks"}}}}]}]}]}}]}}`

// A data pack that overrides minecraft:soul_speed changes the modifier the
// boots put on, and it comes off on landing on stone.
// vanilla: EnchantmentHelper.runLocationChangedEffects, EnchantmentActiveCheck
func TestPackOverrideOfSoulSpeedChangesTheModifier(t *testing.T) {
	const name = "Soul"
	cfg := servertest.BowTestConfig()
	cfg.LevelName = packEnchantments(t, map[string]string{"data/minecraft/enchantment/soul_speed.json": soulSpeedOverride})
	srv, c, id := servertest.JoinAs(t, cfg, name)
	sand, _ := registry.FindState("minecraft:soul_sand", nil)
	srv.SetBlock(0, 63, 0, sand)
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot(name, player.ArmorSlot+3, servertest.EnchantedStack(t, "minecraft:iron_boots", "minecraft:soul_speed", 1))
	servertest.RunTicks(t, srv, 2)
	on := func(e servertest.AttrEntry) bool { return hasSuffixModifier(e, "minecraft:enchantment.soul_speed") }
	e := c.AwaitAttributes(id, servertest.AttrMovementSpeed, on)[servertest.AttrMovementSpeed]
	found := false
	for _, m := range e.Modifiers {
		if strings.HasPrefix(m.Id, "minecraft:enchantment.soul_speed") {
			found = true
			if float32(m.Amount) != 0.2 {
				t.Errorf("modifier amount = %v, want the pack's 0.2", m.Amount)
			}
		}
	}
	if !found {
		t.Fatal("no soul speed modifier on soul sand")
	}

	// Landing on stone takes it off.
	c.MoveToGround(1.5, 64, 0.5, true)
	// The step is read on the connection's goroutine; the tick that sends the
	// change must come after it.
	for srv.PlayerPosition(name).X != 1.5 {
		time.Sleep(time.Millisecond)
	}
	servertest.RunTicks(t, srv, 2)
	c.AwaitAttributes(id, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool { return !on(e) })
}

// A teleport off soul sand takes the Soul Speed modifier off too.
// vanilla: LivingEntity.baseTick, ServerPlayer.onChangedBlock
func TestTeleportOffSoulSandRemovesSoulSpeed(t *testing.T) {
	const name = "Soul"
	srv, c, id := servertest.JoinAs(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
	sand, _ := registry.FindState("minecraft:soul_sand", nil)
	srv.SetBlock(0, 63, 0, sand)
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot(name, player.ArmorSlot+3, servertest.EnchantedStack(t, "minecraft:iron_boots", "minecraft:soul_speed", 1))
	servertest.RunTicks(t, srv, 2)
	on := func(e servertest.AttrEntry) bool { return hasSuffixModifier(e, "minecraft:enchantment.soul_speed") }
	c.AwaitAttributes(id, servertest.AttrMovementSpeed, on)

	c.ChatCommand("tp @s 3.5 64 0.5")
	for srv.PlayerPosition(name).X != 3.5 {
		time.Sleep(time.Millisecond)
	}
	servertest.RunTicks(t, srv, 2)
	c.AwaitAttributes(id, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool { return !on(e) })
}
