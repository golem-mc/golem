package commands

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// dialItemBarrel is an operator's connection with a barrel at 2 64 3 and
// the setblock's own answers drained.
func dialItemBarrel(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Dial(t, servertest.ItemCreativeConfig(), servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt()
	c.SkipJoin()
	c.ChatCommand("setblock 2 64 3 minecraft:barrel")
	barrel, ok := registry.FindState("minecraft:barrel", nil)
	if !ok {
		t.Fatal("the registry has no barrel")
	}
	servertest.AwaitBlockUpdate(t, c, 2, 64, 3, barrel)
	servertest.DrainBlockUpdates(t, c, 8)
	return srv, c
}

// awaitBarrelSlot waits for the barrel's slot to hold what ok accepts.
func awaitBarrelSlot(t *testing.T, srv *server.Server, slot int, what string, ok func(protocol.ItemStack) bool) {
	t.Helper()
	servertest.UntilTicks(t, srv, 200, fmt.Sprintf("barrel slot %d, want %s", slot, what),
		func() bool { return ok(srv.InventoryAt(2, 64, 3)[slot]) })
}

// TestItemModifyBlock checks /item modify block running inline modifiers
// over a barrel's slots, each outcome the one a vanilla server gave
// for the same command.
func TestItemModifyBlock(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	sword := servertest.MustItem("minecraft:diamond_sword")
	stone := servertest.MustItem("minecraft:stone")

	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:diamond_sword[enchantments={sharpness:2}]")
	awaitBarrelSlot(t, srv, 0, "a diamond sword", func(s protocol.ItemStack) bool { return s.ItemID == sword })

	// set_damage 0.5 leaves half of the sword's 1561 durability.
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"minecraft:set_damage",damage:0.5}`)
	awaitBarrelSlot(t, srv, 0, "damage 780", func(s protocol.ItemStack) bool { return s.Damage == 780 })

	// set_enchantments with add raises the level already there.
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_enchantments",enchantments:{sharpness:3},add:true}`)
	awaitBarrelSlot(t, srv, 0, "sharpness 5", func(s protocol.ItemStack) bool { return s.Enchantments[servertest.SharpnessID(t)] == 5 })

	// set_name, then set_components removing the name and setting damage,
	// spaces inside the compound included.
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_name",name:"Hi"}`)
	awaitBarrelSlot(t, srv, 0, "the custom name Hi", func(s protocol.ItemStack) bool {
		return s.CustomName != nil && s.CustomName.Text == "Hi"
	})
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_components", components:{"minecraft:damage":5,"!minecraft:custom_name":{}}}`)
	awaitBarrelSlot(t, srv, 0, "damage 5 and no name", func(s protocol.ItemStack) bool {
		return s.Damage == 5 && s.CustomName == nil
	})

	// An empty list changes nothing; a count past the stack size is capped
	// at it; a negative count empties the slot.
	c.ChatCommand("item replace block 2 64 3 container.1 with minecraft:stone 3")
	awaitBarrelSlot(t, srv, 1, "three stone", func(s protocol.ItemStack) bool { return s.ItemID == stone && s.Count == 3 })
	c.ChatCommand("item modify block 2 64 3 container.1 []")
	servertest.ExpectFeedbackSoon(t, c, nil, itemBlockModifyKey, servertest.ItemNumber(1),
		servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3))
	c.ChatCommand(`item modify block 2 64 3 container.1 [{type:"set_count",count:100}]`)
	awaitBarrelSlot(t, srv, 1, "64 stone", func(s protocol.ItemStack) bool { return s.ItemID == stone && s.Count == 64 })
	c.ChatCommand(`item modify block 2 64 3 container.1 {type:"set_count",count:-3}`)
	awaitBarrelSlot(t, srv, 1, "nothing", protocol.ItemStack.Empty)

	// An empty slot is no slot to modify: 26.3 counts only non-empty ones,
	// so the word is refused as having none.
	c.ChatCommand(`item modify block 2 64 3 container.5 [{type:"set_item",item:"diamond"},{type:"set_count",count:5}]`)
	awaitItemFailure(t, c, itemTargetNoSlotKey, protocol.Text{Text: "container.5"})
	if got := srv.InventoryAt(2, 64, 3)[5]; !got.Empty() {
		t.Errorf("barrel slot 5 = %+v, want the empty slot left empty", got)
	}

	// The target refusals of the replace forms answer here too.
	c.ChatCommand(`item modify block 2 64 3 container.27 {type:"discard"}`)
	awaitItemFailure(t, c, itemTargetNoSlotKey, protocol.Text{Text: "container.27"})
	c.ChatCommand(`item modify block 3 64 4 container.0 {type:"discard"}`)
	awaitItemFailure(t, c, itemTargetNoContainerKey, servertest.ItemNumber(3), servertest.ItemNumber(64), servertest.ItemNumber(4))
	c.Disconnect()
}

// TestItemModifyEntity checks the entity form: the slot a target has is
// rewritten and named in the feedback, and a slot no target has answers
// with the no-changes refusal by its raw slot id.
func TestItemModifyEntity(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")
	stone := servertest.MustItem("minecraft:stone")

	c.ChatCommand("item replace entity @s weapon.mainhand with minecraft:stone 3")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand(`item modify entity @s weapon.mainhand {type:"set_count",count:2,add:true}`)
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || updates[0].Stack.ItemID != stone || updates[0].Stack.Count != 5 {
		t.Errorf("main hand update = %+v, want five stone in slot 36", updates[0])
	}
	servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySingleModifyKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"))

	c.ChatCommand(`item modify entity @s mob.inventory.0 {type:"set_count",count:2}`)
	awaitItemFailure(t, c, itemTargetNoSlotKey, protocol.Text{Text: "mob.inventory.0"})
	c.Disconnect()
}

// TestItemModifierRefusals checks the inline modifiers the codec refuses:
// the message names the codec failure, and
// the cursor stands at the argument's end.
func TestItemModifierRefusals(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	for _, tc := range []struct {
		name     string
		modifier string
		message  string
	}{
		{"an unknown function type",
			`{type:"minecraft:nope"}`,
			`Failed to parse either. First: Unknown registry key in ResourceKey[minecraft:root / minecraft:loot_function_type]: minecraft:nope; Second: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: {type:"minecraft:nope"}; Second: Unknown registry key in ResourceKey[minecraft:root / minecraft:loot_function_type]: minecraft:nope`},
		{"a function without its required field",
			`{type:"set_count"}`,
			`Failed to parse either. First: No key count in MapLike[{type:"set_count"}]; Second: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: {type:"set_count"}; Second: No key count in MapLike[{type:"set_count"}]`},
		{"a compound without a function",
			`{}`,
			`Failed to parse either. First: No key type in MapLike[{}]; Second: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: {}; Second: No key type in MapLike[{}]`},
		{"a list element naming no modifier",
			`[{type:"discard"},"x"]`,
			`Failed to get element minecraft:x from registry minecraft:item_modifier`},
		{"a function type that is not a string",
			`{type:5}`,
			`Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: {type:5}; Second: Not a string`},
		{"a list element without a function",
			`[{}]`,
			`No key type in MapLike[{}]`},
		{"a list element with a non-string function",
			`[{type:5}]`,
			`Not a string`},
		{"a list element of an unknown type",
			`[{type:"nope"}]`,
			`Unknown registry key in ResourceKey[minecraft:root / minecraft:loot_function_type]: minecraft:nope`},
		{"a list element without its required field",
			`[{type:"set_count"}]`,
			`No key count in MapLike[{type:"set_count"}]`},
		{"a string",
			`"abc"`,
			`Failed to parse either. First: Not a map: "abc"; Second: Not a list: "abc"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := "item modify entity @s weapon.mainhand " + tc.modifier
			c.ChatCommand(command)
			c.ExpectCommandError(
				servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", tc.message),
				servertest.CommandErrorContextForTest(command, strings.LastIndex(command, tc.modifier)+len(tc.modifier)))
		})
	}
	c.Disconnect()
}

// TestItemModifyComponentFunctions checks set_lore, set_custom_data and
// toggle_tooltips over a barrel slot.
// vanilla: SetLoreFunction, SetCustomDataFunction, ToggleTooltips
func TestItemModifyComponentFunctions(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)

	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:diamond_sword")
	awaitBarrelSlot(t, srv, 0, "a diamond sword", func(s protocol.ItemStack) bool { return s.Count == 1 })

	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_lore",lore:["one"],mode:"append"}`)
	awaitBarrelSlot(t, srv, 0, "one lore line", func(s protocol.ItemStack) bool {
		lore := s.LoreOf()
		return len(lore) == 1 && lore[0].Text == "one"
	})
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_lore",lore:["zero"],mode:"insert",offset:0}`)
	awaitBarrelSlot(t, srv, 0, "zero then one", func(s protocol.ItemStack) bool {
		lore := s.LoreOf()
		return len(lore) == 2 && lore[0].Text == "zero" && lore[1].Text == "one"
	})
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_custom_data",tag:"{a:1}"}`)
	awaitBarrelSlot(t, srv, 0, "custom data a:1", func(s protocol.ItemStack) bool {
		a, ok := s.CustomData()["a"]
		return ok && a == nbt.Int(1)
	})
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_custom_data",tag:"{b:2}"}`)
	awaitBarrelSlot(t, srv, 0, "custom data a:1,b:2", func(s protocol.ItemStack) bool { return len(s.CustomData()) == 2 })

	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"toggle_tooltips",toggles:{"minecraft:enchantments":false}}`)
	awaitBarrelSlot(t, srv, 0, "enchantments hidden", func(s protocol.ItemStack) bool {
		_, hidden := s.HiddenTooltipOf()
		return slices.Contains(hidden, protocol.DataComponentEnchantments)
	})
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"toggle_tooltips",toggles:{"minecraft:enchantments":true}}`)
	awaitBarrelSlot(t, srv, 0, "enchantments shown", func(s protocol.ItemStack) bool {
		_, hidden := s.HiddenTooltipOf()
		return !slices.Contains(hidden, protocol.DataComponentEnchantments)
	})

	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_attributes",modifiers:[{id:"a:b",attribute:"minecraft:attack_damage",operation:"add_value",amount:2,slot:"mainhand"}]}`)
	awaitBarrelSlot(t, srv, 0, "one attribute modifier", func(s protocol.ItemStack) bool {
		entries, ok := s.AttributeModifiersOf()
		return ok && len(entries) == 1 && entries[0].Amount == 2 && entries[0].ID == "a:b"
	})
	c.Disconnect()
}
