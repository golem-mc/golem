package commands

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestItemCommandsRemoveDefaultComponents(t *testing.T) {
	t.Parallel()
	const name = "Tester"
	srv, c := servertest.Join(t, servertest.CombatTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()

	for _, tc := range []struct {
		line, feedback string
		slot           int16
		item           registry.Item
		removed        []string
	}{
		{"give @s stone[!custom_name] 1", "commands.give.success.single", 36, registry.ItemStone, nil},
		{"give @s diamond_sword[!attribute_modifiers] 1", "commands.give.success.single", 37, registry.ItemDiamondSword, []string{"minecraft:attribute_modifiers"}},
		{"item replace entity @s hotbar.2 with diamond_sword[!enchantments,!lore,!rarity,!item_name,!tooltip_display,!item_model,!break_sound,!repair_cost,!damage]",
			itemEntitySetSingleKey, 38, registry.ItemDiamondSword, []string{
				"minecraft:break_sound", "minecraft:damage", "minecraft:enchantments", "minecraft:item_model",
				"minecraft:item_name", "minecraft:lore", "minecraft:rarity", "minecraft:repair_cost", "minecraft:tooltip_display",
			}},
		{"item replace entity @s hotbar.3 with stone[!damage,!container]", itemEntitySetSingleKey, 39, registry.ItemStone, nil},
	} {
		c.ChatCommand(tc.line)
		updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
		if !slices.ContainsFunc(chats, func(chat []byte) bool { return bytes.Contains(chat, []byte(tc.feedback)) }) {
			servertest.AwaitSystemChatSaying(t, c, tc.feedback)
		}
		received := updates[0].Stack
		if updates[0].Slot != tc.slot {
			t.Errorf("%s: slot = %d, want %d", tc.line, updates[0].Slot, tc.slot)
		}
		removed := received.RemovedNames()
		slices.Sort(removed)
		if received.ItemID != int32(tc.item) || received.Count != 1 || !slices.Equal(removed, tc.removed) {
			t.Errorf("%s: slot %d item/count/removals = %d/%d/%v, want %d/1/%v",
				tc.line, tc.slot, received.ItemID, received.Count, removed, tc.item, tc.removed)
		}
	}
	c.ChatCommand("data get entity @s Inventory")
	line := servertest.FeedbackComponent(t, c)
	if line.Translate != "commands.data.entity.query" || len(line.With) != 2 ||
		!strings.Contains(line.With[1].Plain(), `"!minecraft:attribute_modifiers": {}`) {
		t.Fatalf("Inventory feedback = %s, want the sword's attribute_modifiers removal", line.Plain())
	}

	c.SwapHotbarSlot(1)
	// Set armor to zero to measure the full attack before damage reduction.
	c.ChatCommand(`summon zombie ~ ~ ~2 {NoAI:1b,attributes:[{id:"minecraft:armor",base:0.0d}]}`)
	zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.Settle()
	servertest.AwaitFullSwing(t, srv, name)
	c.Attack(zombie.EntityID)
	c.Settle()
	if health, ok := srv.MobHealth(zombie.EntityID); !ok || health != 19 {
		t.Errorf("zombie health after a full hit = %v (present %t), want 19", health, ok)
	}

	c.ChatCommand("attribute @s attack_speed get")
	attribute := servertest.FeedbackComponent(t, c)
	if attribute.Translate != "commands.attribute.value.get.success" ||
		len(attribute.With) != 3 || attribute.With[2].Plain() != "4.0" {
		t.Errorf("attack speed feedback = %s, want base attack speed 4.0", attribute.Plain())
	}

	c.SwapHotbarSlot(2)
	c.ChatCommand("enchant @s sharpness 1")
	servertest.AwaitSystemChatSaying(t, c, "commands.enchant.success.single")
	c.ChatCommand(`summon zombie ~2 ~ ~ {NoAI:1b,attributes:[{id:"minecraft:armor",base:0.0d}]}`)
	zombie = c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.Settle()
	servertest.AwaitFullSwing(t, srv, name)
	c.Attack(zombie.EntityID)
	c.Settle()
	if health, ok := srv.MobHealth(zombie.EntityID); !ok || health != 13 {
		t.Errorf("zombie health after a hit with the non-enchanted sword = %v (present %t), want 13", health, ok)
	}
	c.ChatCommand("data get entity @s Inventory[{Slot:2b}].components")
	components := servertest.FeedbackComponent(t, c)
	if components.Translate != "commands.data.entity.query" || len(components.With) != 2 {
		t.Fatalf("held sword data = %s", components.Plain())
	}
	for _, removal := range []string{`"!minecraft:damage": {}`, `"!minecraft:enchantments": {}`} {
		if !strings.Contains(components.With[1].Plain(), removal) {
			t.Errorf("held sword components = %s, want %s", components.With[1].Plain(), removal)
		}
	}
}
