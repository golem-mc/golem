package commands

import (
	"bytes"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
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

// TestItemModifyTruncatesAndKeepsSpaces checks that a fractional number in a
// context int provider field truncates and that a quoted string keeps its
// runs of spaces.
// vanilla: ints.ConstantValue.INLINE_CODEC (Codec.INT)
func TestItemModifyTruncatesAndKeepsSpaces(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	stone := servertest.MustItem("minecraft:stone")
	sword := servertest.MustItem("minecraft:diamond_sword")

	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:stone 9")
	awaitBarrelSlot(t, srv, 0, "nine stone", func(s protocol.ItemStack) bool { return s.ItemID == stone && s.Count == 9 })
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_count",count:2.5}`)
	awaitBarrelSlot(t, srv, 0, "two stone", func(s protocol.ItemStack) bool { return s.Count == 2 })
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_count",count:9}`)
	awaitBarrelSlot(t, srv, 0, "nine stone", func(s protocol.ItemStack) bool { return s.Count == 9 })
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"limit_count",limit:{min:2.5,max:3.5}}`)
	awaitBarrelSlot(t, srv, 0, "three stone", func(s protocol.ItemStack) bool { return s.Count == 3 })

	c.ChatCommand("item replace block 2 64 3 container.1 with minecraft:diamond_sword")
	awaitBarrelSlot(t, srv, 1, "a diamond sword", func(s protocol.ItemStack) bool { return s.ItemID == sword })
	c.ChatCommand(`item modify block 2 64 3 container.1 {type:"set_enchantments",enchantments:{sharpness:2.5}}`)
	awaitBarrelSlot(t, srv, 1, "sharpness 2", func(s protocol.ItemStack) bool { return s.Enchantments[servertest.SharpnessID(t)] == 2 })
	c.ChatCommand(`item modify block 2 64 3 container.1 {type:"set_name",name:"a  b"}`)
	awaitBarrelSlot(t, srv, 1, "the name a  b", func(s protocol.ItemStack) bool {
		return s.CustomName != nil && s.CustomName.Text == "a  b"
	})
	c.Disconnect()
}

// TestItemReplaceFromWithModifier checks the modifier a from tail may end
// on: it runs over the copy, and the source keeps what it held.
func TestItemReplaceFromWithModifier(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	stone := servertest.MustItem("minecraft:stone")

	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:stone 2")
	awaitBarrelSlot(t, srv, 0, "two stone", func(s protocol.ItemStack) bool { return s.ItemID == stone && s.Count == 2 })
	c.ChatCommand(`item replace block 2 64 3 container.1 from block 2 64 3 container.0 {type:"set_count",count:5,add:true}`)
	awaitBarrelSlot(t, srv, 1, "seven stone", func(s protocol.ItemStack) bool { return s.ItemID == stone && s.Count == 7 })
	if got := srv.InventoryAt(2, 64, 3)[0]; got.Count != 2 {
		t.Errorf("source slot = %+v, want its two stone kept", got)
	}

	c.ChatCommand(`item replace entity @s weapon.mainhand from block 2 64 3 container.0 {type:"set_count",count:4}`)
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || updates[0].Stack.ItemID != stone || updates[0].Stack.Count != 4 {
		t.Errorf("main hand update = %+v, want four stone in slot 36", updates[0])
	}
	servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySingleReplaceKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"))
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

// vanilla: LootItemFunctions.TYPED_CODEC, CopyCustomDataFunction.MAP_CODEC
func TestItemModifyRequiresTypeDiscriminator(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`data merge storage review:clientitem {x:5}`)
	storageFeedback := servertest.PlainFeedbackTextsForTest("commands.data.storage.modified", "review:clientitem")
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, storageFeedback) {
		t.Fatalf("storage feedback = % X, want % X", got, storageFeedback)
	}

	const fields = `,source:{type:"minecraft:storage",source:"review:clientitem"},ops:[{source:"x",target:"a.b",op:"replace"}]}`
	var stack protocol.ItemStack
	for _, tc := range []struct {
		command string
		key     string
		with    []protocol.Text
	}{
		{"item replace entity @s weapon.mainhand with minecraft:stick", itemEntitySetSingleKey,
			[]protocol.Text{servertest.ItemNumber(1), servertest.PlayerDisplayComponent("Testificate"),
				servertest.ItemFeedbackText("minecraft:stick", servertest.ItemDisplayKey(t, "minecraft:stick"), 1)}},
		{`item modify entity @s weapon.mainhand {type:"minecraft:copy_custom_data"` + fields, itemEntitySingleModifyKey,
			[]protocol.Text{servertest.ItemNumber(1), servertest.PlayerDisplayComponent("Testificate")}},
	} {
		c.ChatCommand(tc.command)
		var updates []servertest.SlotUpdate
		var chats [][]byte
		c.ReadTicks(srv, 0, func(id int32, payload []byte) bool {
			switch id {
			case protocol.IDPlayContainerSlot:
				r := protocol.NewReader(payload)
				r.ReadVarInt() // window
				r.ReadVarInt() // state
				updates = append(updates, servertest.SlotUpdate{Slot: r.ReadI16(), Stack: servertest.MustReadSlot(t, r)})
			case protocol.IDPlaySystemChat:
				chats = append(chats, payload)
			}
			return false
		})
		if len(updates) != 1 || updates[0].Slot != 36 {
			t.Fatalf("%s: slot updates = %+v, want one main hand update", tc.command, updates)
		}
		stack = updates[0].Stack
		var feedback protocol.Buffer
		feedback.PutText(protocol.Text{Translate: tc.key, With: tc.with})
		feedback.PutBool(false)
		if len(chats) != 1 || !bytes.Equal(chats[0], feedback.Bytes()) {
			t.Fatalf("%s: feedback = % X, want % X", tc.command, chats, feedback.Bytes())
		}
	}
	data := stack.CustomData()
	inner, _ := data["a"].(nbt.Compound)
	if inner["b"] != nbt.Int(5) {
		t.Fatalf("copied custom data = %v, want a.b = 5", data)
	}
	c.ChatCommand(`data merge storage review:clientitem {x:9}`)
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, storageFeedback) {
		t.Fatalf("storage feedback = % X, want % X", got, storageFeedback)
	}
	const modifier = `{function:"minecraft:copy_custom_data"` + fields
	const command = "item modify entity @s weapon.mainhand " + modifier
	c.ChatCommand(command)
	const mapText = `{function:"minecraft:copy_custom_data",ops:[{op:"replace",source:"x",target:"a.b"}],source:{source:"review:clientitem",type:"minecraft:storage"}}`
	for _, want := range [][]byte{
		servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse",
			"Failed to parse either. First: No key type in MapLike["+mapText+"]; Second: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: "+mapText+"; Second: No key type in MapLike["+mapText+"]"),
		servertest.CommandErrorContextForTest(command, len(command)),
	} {
		if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, want) {
			t.Errorf("command error = % X, want % X", got, want)
		}
	}
	inner, _ = srv.InventorySlot("Testificate", 36).CustomData()["a"].(nbt.Compound)
	if inner["b"] != nbt.Int(5) {
		t.Errorf("refused modifier changed custom data to %v", inner)
	}
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

// TestItemModifyDataPackFunctions checks that an inline modifier reads the
// function types the data-pack loader reads: furnace_smelt turns raw iron
// into an ingot and set_potion names the potion.
func TestItemModifyDataPackFunctions(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	ingot := servertest.MustItem("minecraft:iron_ingot")
	potion := servertest.MustItem("minecraft:potion")
	swiftness, ok := registry.PotionID("minecraft:swiftness")
	if !ok {
		t.Fatal("the registry has no swiftness potion")
	}

	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:raw_iron 2")
	awaitBarrelSlot(t, srv, 0, "two raw iron", func(s protocol.ItemStack) bool { return s.Count == 2 })
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"minecraft:furnace_smelt"}`)
	awaitBarrelSlot(t, srv, 0, "two iron ingots", func(s protocol.ItemStack) bool { return s.ItemID == ingot && s.Count == 2 })

	c.ChatCommand("item replace block 2 64 3 container.2 with minecraft:raw_iron 2")
	awaitBarrelSlot(t, srv, 2, "two raw iron", func(s protocol.ItemStack) bool { return s.Count == 2 })
	c.ChatCommand(`item modify block 2 64 3 container.2 {type:"minecraft:furnace_smelt",use_input_count:0b}`)
	awaitBarrelSlot(t, srv, 2, "one iron ingot", func(s protocol.ItemStack) bool { return s.ItemID == ingot && s.Count == 1 })

	c.ChatCommand("item replace block 2 64 3 container.1 with minecraft:potion")
	awaitBarrelSlot(t, srv, 1, "a potion", func(s protocol.ItemStack) bool { return s.ItemID == potion })
	c.ChatCommand(`item modify block 2 64 3 container.1 {type:"minecraft:set_potion",id:"minecraft:swiftness"}`)
	awaitBarrelSlot(t, srv, 1, "a swiftness potion", func(s protocol.ItemStack) bool {
		contents, ok := s.PotionContentsOf()
		return ok && contents.Potion == swiftness
	})
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

// TestItemModifyCopyCustomDataAndSetContents checks copy_custom_data from
// command storage and from the running player, and set_contents.
func TestItemModifyCopyCustomDataAndSetContents(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	stone := servertest.MustItem("minecraft:stone")

	c.ChatCommand("data merge storage test:src {x:5,list:[1,2]}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:src")
	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:stick")
	awaitBarrelSlot(t, srv, 0, "a stick", func(s protocol.ItemStack) bool { return s.Count == 1 })
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"minecraft:copy_custom_data",source:{type:"minecraft:storage",source:"test:src"},ops:[{source:"x",target:"a.b",op:"replace"},{source:"list[]",target:"l",op:"append"},{source:"missing",target:"z",op:"replace"}]}`)
	awaitBarrelSlot(t, srv, 0, "custom data from storage", func(s protocol.ItemStack) bool {
		data := s.CustomData()
		inner, _ := data["a"].(nbt.Compound)
		l, _ := data["l"].(nbt.List)
		_, hasZ := data["z"]
		return inner["b"] == nbt.Int(5) && len(l.Items) == 2 && !hasZ
	})

	c.ChatCommand("item replace block 2 64 3 container.1 with minecraft:stick")
	awaitBarrelSlot(t, srv, 1, "a stick", func(s protocol.ItemStack) bool { return s.Count == 1 })
	c.ChatCommand(`item modify block 2 64 3 container.1 {type:"minecraft:copy_custom_data",source:"this",ops:[{source:"Air",target:"air",op:"replace"}]}`)
	awaitBarrelSlot(t, srv, 1, "custom data from this", func(s protocol.ItemStack) bool {
		_, ok := s.CustomData()["air"]
		return ok
	})

	c.ChatCommand("item replace block 2 64 3 container.2 with minecraft:shulker_box")
	awaitBarrelSlot(t, srv, 2, "a shulker box", func(s protocol.ItemStack) bool { return s.Count == 1 })
	c.ChatCommand(`item modify block 2 64 3 container.2 {type:"minecraft:set_contents",component:"minecraft:container",entries:[{type:"minecraft:item",name:"minecraft:stone",functions:[{type:"minecraft:set_count",count:3}]}]}`)
	awaitBarrelSlot(t, srv, 2, "three stone inside", func(s protocol.ItemStack) bool {
		return len(s.Container) == 1 && s.Container[0].ItemID == stone && s.Container[0].Count == 3
	})
	c.Disconnect()
}

// TestItemModifySetLoreResolvesEntity checks that a set_lore selector
// resolves against the entity field.
// vanilla: SetLoreFunction.run, ComponentUtils.updateForEntity
func TestItemModifySetLoreResolvesEntity(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)

	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:diamond_sword")
	awaitBarrelSlot(t, srv, 0, "a diamond sword", func(s protocol.ItemStack) bool { return s.Count == 1 })

	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_lore",lore:[{selector:"@s"}],entity:"this",mode:"replace_all"}`)
	awaitBarrelSlot(t, srv, 0, "the player's name in the lore", func(s protocol.ItemStack) bool {
		lore := s.LoreOf()
		return len(lore) == 1 && lore[0].Raw == nil && len(lore[0].Extra) == 1 && lore[0].Extra[0].Text == "Testificate"
	})
	c.Disconnect()
}

// TestItemModifyBookAndModelFunctions checks the inline SNBT forms of
// set_writable_book_pages, whose list operation edits the pages already
// there, set_custom_model_data, whose flags arrive as SNBT bytes, and
// set_loot_table with a long seed.
func TestItemModifyBookAndModelFunctions(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)

	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:writable_book")
	awaitBarrelSlot(t, srv, 0, "a writable book", func(s protocol.ItemStack) bool { return s.Count == 1 })
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_writable_book_pages",pages:["b"],mode:"append"}`)
	awaitBarrelSlot(t, srv, 0, "the page b", func(s protocol.ItemStack) bool {
		book, ok := s.WritableBookOf()
		return ok && len(book.Pages) == 1 && book.Pages[0].Raw == "b"
	})
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_writable_book_pages",pages:["a"],mode:"insert",offset:0}`)
	awaitBarrelSlot(t, srv, 0, "pages a then b", func(s protocol.ItemStack) bool {
		book, _ := s.WritableBookOf()
		return len(book.Pages) == 2 && book.Pages[0].Raw == "a" && book.Pages[1].Raw == "b"
	})

	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_custom_model_data",flags:{values:[1b,0b],mode:"append"},floats:{values:[{type:"uniform",min:2,max:2}],mode:"append"}}`)
	awaitBarrelSlot(t, srv, 0, "flags true, false and the float 2", func(s protocol.ItemStack) bool {
		d, ok := s.CustomModelDataOf()
		return ok && slices.Equal(d.Flags, []bool{true, false}) && slices.Equal(d.Floats, []float32{2})
	})

	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_loot_table",loot_table_id:"minecraft:chests/igloo_chest",seed:9007199254740993L}`)
	awaitBarrelSlot(t, srv, 0, "the igloo's container loot", func(s protocol.ItemStack) bool {
		return s.ContainerLoot == protocol.ContainerLoot{Table: "minecraft:chests/igloo_chest", Seed: 9007199254740993}
	})
	c.Disconnect()
}

// TestItemModifyKeepsComponentPosition replaces enchantments through
// set_components and expects them to keep their place in the patch.
// vanilla: SetComponentsFunction.run, PatchedDataComponentMap.set
func TestItemModifyKeepsComponentPosition(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	sword := servertest.MustItem("minecraft:diamond_sword")
	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:diamond_sword[enchantments={sharpness:1},damage=1]")
	awaitBarrelSlot(t, srv, 0, "a diamond sword", func(s protocol.ItemStack) bool { return s.ItemID == sword })
	c.ChatCommand(`item modify block 2 64 3 container.0 {type:"set_components",components:{"minecraft:enchantments":{sharpness:2}}}`)
	awaitBarrelSlot(t, srv, 0, "sharpness 2", func(s protocol.ItemStack) bool { return s.Enchantments[servertest.SharpnessID(t)] == 2 })
	got := srv.InventoryAt(2, 64, 3)[0].PatchOrdered([]int32{protocol.DataComponentDamage, protocol.DataComponentEnchantments})
	if want := []int32{protocol.DataComponentEnchantments, protocol.DataComponentDamage}; !slices.Equal(got, want) {
		t.Errorf("patch order = %v, want %v", got, want)
	}
}
