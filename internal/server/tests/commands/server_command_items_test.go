package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestGiveCommandPutsItemsInTheInventory checks /give: the items land in the
// hotbar the way a picked-up item does, the client is told about every slot
// that changed, and the feedback names the item, the count and the target.
func TestGiveCommandPutsItemsInTheInventory(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /give is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the registry has no diamond")
	}

	// Five diamonds land in the first hotbar slot, in one stack.
	c.ChatCommand("give @s minecraft:diamond 5")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || updates[0].Stack.ItemID != diamond || updates[0].Stack.Count != 5 {
		t.Errorf("give slot update = %+v, want the hotbar holding five diamonds", updates[0])
	}
	// The feedback names the count, the item and who it went to, in that
	// order; the item is named by its display name, which is the
	// translation the client renders in the language it plays in, in the
	// colour the item's rarity draws it in, with the show-item hover a
	// vanilla feedback points at the item with.
	five := int64(5)
	servertest.ExpectFeedbackSoon(t, c, chats, "commands.give.success.single",
		protocol.Text{Number: &five},
		protocol.Text{
			Translate: "chat.square_brackets",
			Color:     "white",
			With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "item.minecraft.diamond"}}}},
			HoverItem: &protocol.HoverItem{ID: "minecraft:diamond", Count: 1},
		},
		servertest.PlayerDisplayComponent("Testificate"))

	// Three more join the same stack, the way a pickup merges.
	c.ChatCommand("give @s minecraft:diamond 3")
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || updates[0].Stack.Count != 8 {
		t.Errorf("give slot update = %+v, want the same stack now holding eight", updates[0])
	}

	// A bare item name is the minecraft namespace's, and one item is the
	// count when none is given.
	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the registry has no stone")
	}
	c.ChatCommand("give @s stone")
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Stack.ItemID != stone || updates[0].Stack.Count != 1 {
		t.Errorf("give slot update = %+v, want one stone", updates[0])
	}
	c.Disconnect()
}

// TestGiveAcceptsEmptyNamespace checks that an item name whose namespace is
// empty reads as the minecraft namespace's.
// vanilla: Identifier.read
func TestGiveAcceptsEmptyNamespace(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the registry has no stone")
	}
	c.ChatCommand("give @s :stone")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || updates[0].Stack.ItemID != stone || updates[0].Stack.Count != 1 {
		t.Errorf("give slot update = %+v, want the hotbar holding one stone", updates[0])
	}
	one := int64(1)
	servertest.ExpectFeedbackSoon(t, c, chats, "commands.give.success.single",
		protocol.Text{Number: &one},
		protocol.Text{
			Translate: "chat.square_brackets",
			Color:     "white",
			With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "block.minecraft.stone"}}}},
			HoverItem: &protocol.HoverItem{ID: "minecraft:stone", Count: 1},
		},
		servertest.PlayerDisplayComponent("Testificate"))
	c.Disconnect()
}

// TestGiveTargets checks the form that names who receives: the other player
// is given the items, told about their own slots, and counted in the
// feedback.
func TestGiveTargets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /give is a gamemaster's command, so Alice, the giver, is listed as
	// an operator; Bob is only ever given to.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	apple, ok := registry.ItemID("minecraft:apple")
	if !ok {
		t.Fatal("the registry has no apple")
	}
	alice.ChatCommand("give Bob minecraft:apple 2")
	updates, _ := servertest.AwaitSlotUpdates(t, bob, 1)
	if updates[0].Stack.ItemID != apple || updates[0].Stack.Count != 2 {
		t.Errorf("give slot update = %+v, want Bob holding two apples", updates[0])
	}
	// The item in the feedback is named by its display name, which is the
	// translation the client renders in the language it plays in, in the
	// colour the item's rarity draws it in, with the show-item hover a
	// vanilla feedback points at the item with.
	two := int64(2)
	servertest.AwaitCommandFeedbackText(t, alice, "commands.give.success.single",
		protocol.Text{Number: &two},
		protocol.Text{
			Translate: "chat.square_brackets",
			Color:     "white",
			With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "item.minecraft.apple"}}}},
			HoverItem: &protocol.HoverItem{ID: "minecraft:apple", Count: 1},
		},
		servertest.PlayerDisplayComponent("Bob"))

	alice.Disconnect()
	bob.Disconnect()
}

// TestSetblockPlacesABlock checks /setblock: the block named, with or without
// its properties, is what the world ends up holding at the position named,
// and every client is told.
func TestSetblockPlacesABlock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /setblock is a gamemaster's command, so the runner is listed as an
	// operator.
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	// The spawn the join puts the player at is read out of the join stream
	// itself, which ends at the teleport that puts them there; the rest of
	// the join is skipped past afterwards. The relative setblock below is
	// measured from it.
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	// A plain block by its bare name, two blocks east of the player and one
	// above the ground, which is where the flat world has air to put it in.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	c.ChatCommand("setblock 2 64 3 stone")
	servertest.AwaitBlockUpdate(t, c, 2, 64, 3, stone)
	if got := srv.BlockAt(2, 64, 3); got != stone {
		t.Errorf("block at (2, 64, 3) = %d, want %d (stone)", got, stone)
	}

	// A block with its properties named, which is the state those
	// properties spell out.
	stairs, ok := registry.FindState("minecraft:oak_stairs", map[string]string{"facing": "east", "half": "top"})
	if !ok {
		t.Fatal("the registry has no oak stairs")
	}
	c.ChatCommand("setblock ~2 ~ ~ oak_stairs[facing=east,half=top]")
	servertest.AwaitBlockUpdate(t, c, 2, 64, 0, stairs)
	if got := srv.BlockAt(2, 64, 0); got != stairs {
		t.Errorf("block at (2, 64, 0) = %d, want %d (top-facing-east oak stairs)", got, stairs)
	}
	c.Disconnect()
}

// TestSetblockKeepSparesTheBlock checks the keep mode, which leaves a block
// that is already there alone, and the bare red failures the attempts that
// change nothing answer with.
func TestSetblockKeepSparesTheBlock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /setblock is a gamemaster's command, so the runner is listed as an
	// operator.
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	// The spawn the join puts the player at is read out of the join stream
	// itself, which ends at the teleport that puts them there; the rest of
	// the join is skipped past afterwards.
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	dirt, ok := registry.FindState("minecraft:dirt", nil)
	if !ok {
		t.Fatal("the registry has no dirt")
	}

	// The stone goes in one block above the ground, which is where the flat
	// world has air for it.
	c.ChatCommand("setblock 3 64 3 stone")
	servertest.AwaitBlockUpdate(t, c, 3, 64, 3, stone)

	// The keep mode finds the stone there and does nothing, which is what
	// the world still holds and what no second update says. The refusal
	// is the bare red failure a vanilla server answers the same command
	// with, waited for past whatever else is on the way, since a change's
	// block updates mail through and can land after the words a command
	// writes itself.
	c.ChatCommand("setblock 3 64 3 dirt keep")
	servertest.AwaitCommandFailure(t, c, "commands.setblock.failed")
	servertest.DrainBlockUpdates(t, c, 8)
	if got := srv.BlockAt(3, 64, 3); got != stone {
		t.Errorf("block at (3, 64, 3) = %d, want the stone the keep mode spared", got)
	}

	// Without the mode the same command replaces it.
	c.ChatCommand("setblock 3 64 3 dirt")
	servertest.AwaitBlockUpdate(t, c, 3, 64, 3, dirt)
	if got := srv.BlockAt(3, 64, 3); got != dirt {
		t.Errorf("block at (3, 64, 3) = %d, want %d (dirt)", got, dirt)
	}

	// Setting what is already there changes nothing, which the write
	// reports as not having happened, and the command refuses the same
	// way a keep does.
	c.ChatCommand("setblock 3 64 3 dirt")
	servertest.AwaitCommandFailure(t, c, "commands.setblock.failed")
	c.Disconnect()
}

// TestGiveRefusesMalformedComponents checks the refusals the item stack
// argument answers a component list with while the command is still being
// read, which are the ones the vanilla server's item parser words itself: a
// component the game has never heard of, and the values of the components
// whose codecs refuse with a message - each with the context line pointing
// where the vanilla parser's cursor stopped.
func TestGiveRefusesMalformedComponents(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, tc := range []struct {
		name    string
		command string // the command text, without the leading slash
		key     string // the translation key the first message carries
		with    []string
		cursor  int // where the "<--[HERE]" marker points
	}{
		{"unknown component",
			"give @s diamond_sword[nonsense_component=5] 1",
			"arguments.item.component.unknown",
			[]string{"minecraft:nonsense_component"},
			strings.Index("give @s diamond_sword[nonsense_component=5] 1", "nonsense_component")},
		{"unknown enchantment",
			"give @s diamond_sword[enchantments={nope:5}]",
			"arguments.item.component.malformed",
			[]string{"minecraft:enchantments", "Failed to get element minecraft:nope missed input: {nope:5}"},
			strings.Index("give @s diamond_sword[enchantments={nope:5}]", "{nope:5}")},
		{"name that is no text",
			"give @s diamond_sword[custom_name=5]",
			"arguments.item.component.malformed",
			[]string{"minecraft:custom_name", "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5"},
			strings.Index("give @s diamond_sword[custom_name=5]", "custom_name=5") + len("custom_name=")},
		{"enchantment level over the top",
			"give @s diamond_sword[enchantments={\"minecraft:sharpness\":256}]",
			"arguments.item.component.malformed",
			[]string{"minecraft:enchantments", "Value 256 outside of range [1:255] missed input: {\"minecraft:sharpness\":256}"},
			strings.Index("give @s diamond_sword[enchantments={\"minecraft:sharpness\":256}]", "{\"minecraft:sharpness\":256}")},
		{"damage that is no number",
			"give @s diamond_sword[damage=abc]",
			"arguments.item.component.malformed",
			[]string{"minecraft:damage", "Not a number"},
			strings.Index("give @s diamond_sword[damage=abc]", "abc")},
		{"damage below zero",
			"give @s diamond_sword[damage=-1]",
			"arguments.item.component.malformed",
			[]string{"minecraft:damage", "Value must be non-negative: -1"},
			strings.Index("give @s diamond_sword[damage=-1]", "-1")},
		{"damage that is a string",
			`give @s diamond_sword[damage="a b"]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:damage", "Not a number"},
			strings.Index(`give @s diamond_sword[damage="a b"]`, `"a b"`)},
		{"damage that is a list",
			"give @s diamond_sword[damage=[1,2]]",
			"arguments.item.component.malformed",
			[]string{"minecraft:damage", "Not a number"},
			strings.Index("give @s diamond_sword[damage=[1,2]]", "[1,2]")},
		{"unbreakable that is no map",
			"give @s diamond_sword[unbreakable=5]",
			"arguments.item.component.malformed",
			[]string{"minecraft:unbreakable", "Not a map: 5"},
			strings.Index("give @s diamond_sword[unbreakable=5]", "5")},
		{"enchantments that are no map",
			"give @s diamond_sword[enchantments=5]",
			"arguments.item.component.malformed",
			[]string{"minecraft:enchantments", "Not a map: 5"},
			strings.Index("give @s diamond_sword[enchantments=5]", "5]")},
		{"enchantment level that is no number",
			"give @s diamond_sword[enchantments={sharpness:[]}]",
			"arguments.item.component.malformed",
			[]string{"minecraft:enchantments", "Not a number missed input: {sharpness:[]}"},
			strings.Index("give @s diamond_sword[enchantments={sharpness:[]}]", "{sharpness:[]}")},
		{"name that is a byte",
			"give @s diamond_sword[custom_name=5b]",
			"arguments.item.component.malformed",
			[]string{"minecraft:custom_name", "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5b; Second: Not a map: 5b"},
			strings.Index("give @s diamond_sword[custom_name=5b]", "5b")},
		{"name that is a boolean",
			"give @s diamond_sword[custom_name=true]",
			"arguments.item.component.malformed",
			[]string{"minecraft:custom_name", "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1b; Second: Not a map: 1b"},
			strings.Index("give @s diamond_sword[custom_name=true]", "true")},
		{"name that is an empty list",
			"give @s diamond_sword[custom_name=[]]",
			"arguments.item.component.malformed",
			[]string{"minecraft:custom_name", "Failed to parse either. First: Failed to parse either. First: Not a string; Second: List must have contents; Second: Not a map: []"},
			strings.Index("give @s diamond_sword[custom_name=[]]", "[]")},
		{"name that is an empty map",
			"give @s diamond_sword[custom_name={}]",
			"arguments.item.component.malformed",
			[]string{"minecraft:custom_name", "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {}; Second: No matching codec found"},
			strings.Index("give @s diamond_sword[custom_name={}]", "{}")},
		{"name whose text is no string",
			"give @s diamond_sword[custom_name={text:5}]",
			"arguments.item.component.malformed",
			[]string{"minecraft:custom_name", "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:5}; Second: No matching codec found"},
			strings.Index("give @s diamond_sword[custom_name={text:5}]", "{text:5}")},
		{"item name that is a byte",
			"give @s diamond_sword[item_name=5b]",
			"arguments.item.component.malformed",
			[]string{"minecraft:item_name", "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5b; Second: Not a map: 5b"},
			strings.Index("give @s diamond_sword[item_name=5b]", "5b")},
		{"potion contents that is a number",
			"give @s potion[potion_contents=5]",
			"arguments.item.component.malformed",
			[]string{"minecraft:potion_contents", "Failed to parse either. First: Not a map: 5; Second: Not a string: 5"},
			strings.Index("give @s potion[potion_contents=5]", "5]")},
		{"damage past the int range",
			"give @s diamond_sword[damage=2147483648]",
			// A bare decimal past the int range is no tag the SNBT grammar
			// reads, so it is refused before the codec sees it.
			"argument.literal.incorrect",
			[]string{"."},
			strings.Index("give @s diamond_sword[damage=2147483648]", "2147483648") + len("2147483648")},
		{"profile name too long",
			`give @s player_head[minecraft:profile={name:"ThisNameIsWayTooLong"}]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:profile",
				`Failed to parse either. First: String "ThisNameIsWayTooLong" is too long: 20, expected range [0-16]; ` +
					`String "ThisNameIsWayTooLong" is too long: 20, expected range [0-16]; ` +
					`No key id in MapLike[{name:"ThisNameIsWayTooLong"}]; Second: Not a string`},
			strings.Index(`give @s player_head[minecraft:profile={name:"ThisNameIsWayTooLong"}]`, `{name:"ThisNameIsWayTooLong"}`)},
		{"profile name as a bare string too long",
			`give @s player_head[minecraft:profile="ThisNameIsWayTooLong"]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:profile",
				`Failed to parse either. First: Not a map: "ThisNameIsWayTooLong"; ` +
					`Second: String "ThisNameIsWayTooLong" is too long: 20, expected range [0-16]`},
			strings.Index(`give @s player_head[minecraft:profile="ThisNameIsWayTooLong"]`, `"ThisNameIsWayTooLong"`)},
		{"profile texture with a bad path",
			`give @s player_head[minecraft:profile={name:"Notch",texture:"Bad Id"}]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:profile",
				"Not a valid resource location: Bad Id " +
					"Non [a-z0-9/._-] character in path of location: minecraft:Bad Id"},
			strings.Index(`give @s player_head[minecraft:profile={name:"Notch",texture:"Bad Id"}]`, `{name:"Notch",texture:"Bad Id"}`)},
		{"profile model that is no model",
			`give @s player_head[minecraft:profile={name:"Notch",model:"foo"}]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:profile", "Unknown element name:foo"},
			strings.Index(`give @s player_head[minecraft:profile={name:"Notch",model:"foo"}]`, `{name:"Notch",model:"foo"}`)},
		{"note block sound that is no string",
			"give @s player_head[note_block_sound=5]",
			"arguments.item.component.malformed",
			[]string{"minecraft:note_block_sound", "Not a string"},
			strings.Index("give @s player_head[note_block_sound=5]", "5")},
		{"note block sound with a bad path",
			`give @s player_head[note_block_sound="minecraft:not a sound"]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:note_block_sound",
				"Not a valid resource location: minecraft:not a sound " +
					"Non [a-z0-9/._-] character in path of location: minecraft:not a sound"},
			strings.Index(`give @s player_head[note_block_sound="minecraft:not a sound"]`, `"minecraft:not a sound"`)},
		{"banner patterns that are no list",
			"give @s white_banner[banner_patterns=5]",
			"arguments.item.component.malformed",
			[]string{"minecraft:banner_patterns", "Not a list: 5"},
			strings.Index("give @s white_banner[banner_patterns=5]", "5")},
		{"banner pattern the registry lacks",
			`give @s white_banner[banner_patterns=[{pattern:"minecraft:nope",color:"red"}]]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:banner_patterns", "Failed to get element ResourceKey[minecraft:banner_pattern / minecraft:nope]"},
			strings.Index(`give @s white_banner[banner_patterns=[{pattern:"minecraft:nope",color:"red"}]]`, "[{pattern")},
		{"banner colour that is no colour",
			`give @s white_banner[banner_patterns=[{pattern:"minecraft:stripe_top",color:"nope"}]]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:banner_patterns", "Unknown element name:nope"},
			strings.Index(`give @s white_banner[banner_patterns=[{pattern:"minecraft:stripe_top",color:"nope"}]]`, "[{pattern")},
		{"banner layer with no colour",
			`give @s white_banner[banner_patterns=[{pattern:"minecraft:stripe_top"}]]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:banner_patterns", `No key color in MapLike[{pattern:"minecraft:stripe_top"}]`},
			strings.Index(`give @s white_banner[banner_patterns=[{pattern:"minecraft:stripe_top"}]]`, "[{pattern")},
		{"banner layer that is no map",
			"give @s white_banner[banner_patterns=[5]]",
			"arguments.item.component.malformed",
			[]string{"minecraft:banner_patterns", "Not a map: 5"},
			strings.Index("give @s white_banner[banner_patterns=[5]]", "[5")},
		{"banner inline pattern that is no map",
			`give @s white_banner[banner_patterns=[{pattern:5,color:"red"}]]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:banner_patterns", "Not a map: 5"},
			strings.Index(`give @s white_banner[banner_patterns=[{pattern:5,color:"red"}]]`, "[{pattern")},
		{"banner inline pattern with no asset",
			`give @s white_banner[banner_patterns=[{pattern:{translation_key:"k"},color:"red"}]]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:banner_patterns", `No key asset_id in MapLike[{translation_key:"k"}]`},
			strings.Index(`give @s white_banner[banner_patterns=[{pattern:{translation_key:"k"},color:"red"}]]`, "[{pattern")},
		{"banner inline asset with a bad path",
			`give @s white_banner[banner_patterns=[{pattern:{asset_id:"a b",translation_key:"k"},color:"red"}]]`,
			"arguments.item.component.malformed",
			[]string{"minecraft:banner_patterns",
				"Not a valid resource location: a b " +
					"Non [a-z0-9/._-] character in path of location: minecraft:a b"},
			strings.Index(`give @s white_banner[banner_patterns=[{pattern:{asset_id:"a b",translation_key:"k"},color:"red"}]]`, "[{pattern")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.ChatCommand(tc.command)
			c.ExpectCommandError(
				servertest.CommandErrorMessageForTest(tc.key, tc.with...),
				servertest.CommandErrorContextForTest(tc.command, tc.cursor))
		})
	}

	// A number the codec narrows is no refusal at all: the repair cost of
	// one and a half is a repair cost of one, and the item is given.
	c.ChatCommand("give @s diamond_sword[repair_cost=1.5]")
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the registry has no diamond sword")
	}
	if updates[0].Stack.ItemID != sword || updates[0].Stack.RepairCost != 1 {
		t.Errorf("give slot update = %+v, want a diamond sword costing one to repair", updates[0])
	}
	c.Disconnect()
}

// TestGiveRefusesRepeatedComponents checks the refusal the item stack
// argument answers a list that names one component twice, whether the two
// entries assign a value or remove it. The refusal carries no place in the
// input, so it travels as the message alone.
// vanilla: ItemParser.readComponents
func TestGiveRefusesRepeatedComponents(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, command := range []string{
		"give @s stone[damage=1,damage=2]",
		"give @s stone[!damage,!damage]",
		"give @s stone[!damage,minecraft:damage=1]",
	} {
		c.ChatCommand(command)
		want := servertest.CommandErrorMessageForTest("arguments.item.component.repeated", "minecraft:damage")
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want) {
			t.Errorf("%s: refusal = % X, want % X", command, got, want)
		}
	}
	c.Disconnect()
}

// TestGiveAcceptsAValidTextComponent checks a custom name written as the full
// component shape the game's codec reads — a text and a colour — reaches the
// stack, rather than the walk refusing it as a malformed component.
// vanilla: ComponentSerialization.CODEC
func TestGiveAcceptsAValidTextComponent(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the registry has no diamond sword")
	}
	c.ChatCommand("give @s diamond_sword[custom_name={text:'a',color:red}]")
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	name := updates[0].Stack.CustomName
	if updates[0].Stack.ItemID != sword || name == nil || name.Text != "a" || name.Color != "red" {
		t.Errorf("the given sword = %+v, want one diamond sword named a in red", updates[0].Stack)
	}
	c.Disconnect()
}

// TestGiveCarriesAMapID checks that a filled map's minecraft:map_id
// component rides a /give like any other this server models: the command
// hands out the map pointing at the id, which the stack's own reader reads
// back.
func TestGiveCarriesAMapID(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	mapItem, ok := registry.ItemID("minecraft:filled_map")
	if !ok {
		t.Fatal("the registry has no filled map")
	}
	c.ChatCommand("give @s filled_map[map_id=7]")
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	stack := updates[0].Stack
	id, hasID := stack.MapIDOf()
	if stack.ItemID != mapItem || !hasID || id != 7 {
		t.Errorf("the given map = %+v (map id %d, %v), want a filled map pointing at 7", stack, id, hasID)
	}
	c.Disconnect()
}

// TestGiveCarriesBannerPatterns checks that a banner's
// minecraft:banner_patterns component rides a /give like any other this server
// models: the pattern the codec names by identifier and the inline pattern it
// falls back to for a value that is no identifier, each read back by the
// stack's own reader.
// vanilla: BannerPatternLayers.CODEC
func TestGiveCarriesBannerPatterns(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	banner, ok := registry.ItemID("minecraft:white_banner")
	if !ok {
		t.Fatal("the registry has no white banner")
	}
	c.ChatCommand(`give @s white_banner[banner_patterns=[{pattern:"minecraft:stripe_top",color:"red"},{pattern:{asset_id:"minecraft:no_such",translation_key:"k"},color:"blue"}]]`)
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	stack := updates[0].Stack
	layers, has := stack.BannerPatternsOf()
	if stack.ItemID != banner || !has || len(layers) != 2 {
		t.Fatalf("the given banner = %+v (patterns %v, %v), want one white banner with two layers", stack, layers, has)
	}
	patterns, ok := registry.Get("minecraft:banner_pattern")
	if !ok {
		t.Fatal("the registry has no banner patterns")
	}
	if stripe := patterns.ID("minecraft:stripe_top"); layers[0].Pattern != stripe || layers[0].Dye != 14 {
		t.Errorf("the first layer = %+v, want stripe_top in red", layers[0])
	}
	if layers[1].Pattern != -1 || layers[1].AssetID != "minecraft:no_such" ||
		layers[1].TranslationKey != "k" || layers[1].Dye != 11 {
		t.Errorf("the second layer = %+v, want the inline pattern in blue", layers[1])
	}
	c.Disconnect()
}

// TestGiveAcceptsAProfileComponent gives a head whose profile is written the
// vanilla way, both as the compound shape the command argument takes and as
// its bare player-name shorthand, and checks the stack carries each back as
// the profile component.
func TestGiveAcceptsAProfileComponent(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		`give @s player_head[minecraft:profile={name:"Notch"}]`,
		`give @s player_head[minecraft:profile="Notch"]`,
	} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.GameMode = "creative"

			_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

			c.ChatCommand(command)
			updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
			var got []byte
			for _, raw := range updates[0].Stack.Raw {
				if raw.Type == protocol.DataComponentProfile {
					got = raw.Data
				}
			}
			if !bytes.Equal(got, servertest.PartialProfileValue("Notch")) {
				t.Errorf("the given head's profile = %x, want %x", got, servertest.PartialProfileValue("Notch"))
			}
			c.Disconnect()
		})
	}
}

// TestGiveAcceptsAMapFormProfileProperty gives a head whose profile names a
// player and writes its properties as one map of name to value list, the
// other shape the profile codec reads, and checks the property reaches the
// stack's profile component.
// vanilla: ExtraCodecs.PROPERTY_MAP
func TestGiveAcceptsAMapFormProfileProperty(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand(`give @s player_head[minecraft:profile={name:"Notch",properties:{textures:["abc"]}}]`)
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	var got []byte
	for _, raw := range updates[0].Stack.Raw {
		if raw.Type == protocol.DataComponentProfile {
			got = raw.Data
		}
	}
	var want protocol.Buffer
	want.PutBool(false) // not the complete shape
	want.PutBool(true)  // name
	want.PutString("Notch")
	want.PutBool(false) // no id
	want.PutVarInt(1)   // properties
	want.PutString("textures")
	want.PutString("abc")
	want.PutBool(false) // no signature
	want.PutBool(false) // body texture
	want.PutBool(false) // cape texture
	want.PutBool(false) // elytra texture
	want.PutBool(false) // slim model
	if !bytes.Equal(got, want.Bytes()) {
		t.Errorf("the given head's profile = %x, want %x", got, want.Bytes())
	}
	c.Disconnect()
}

// TestGiveAcceptsANoteBlockSoundComponent gives a head whose note block sound
// names an identifier and checks the stack carries the component back with
// the identifier a reader reads: the value is a bare resource location, not a
// sound event holder.
func TestGiveAcceptsANoteBlockSoundComponent(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand(`give @s player_head[note_block_sound="minecraft:block.note_block.pling"]`)
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	var got []byte
	for _, raw := range updates[0].Stack.Raw {
		if raw.Type == protocol.DataComponentNoteBlockSound {
			got = raw.Data
		}
	}
	var want protocol.Buffer
	want.PutString("minecraft:block.note_block.pling")
	if !bytes.Equal(got, want.Bytes()) {
		t.Errorf("the given head's note block sound = %x, want %x", got, want.Bytes())
	}
	c.Disconnect()
}

// TestGiveAcceptsARecipesComponent gives a knowledge book naming recipes; the
// component has no network codec, so the server's slot is read.
func TestGiveAcceptsARecipesComponent(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand(`give @s knowledge_book[recipes=["minecraft:oak_boat","stick"]]`)
	servertest.AwaitSlotUpdates(t, c, 1)
	got := srv.InventorySlot("Testificate", player.HotbarSlot).Recipes
	if want := []string{"minecraft:oak_boat", "minecraft:stick"}; !slices.Equal(got, want) {
		t.Errorf("the given book's recipes = %v, want %v", got, want)
	}
	c.Disconnect()
}

// TestClearAcceptsRepeatedPredicateComponents checks that an item predicate
// may name a component twice: the comma conjoins the terms and the pipe makes
// them alternatives, so naming one twice is allowed. The stone the tests give
// carries no damage, so the conjunction of two damage assignments finds
// nothing while two negations find it all.
// vanilla: ItemPredicateArgument, ComponentPredicateParser
func TestClearAcceptsRepeatedPredicateComponents(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		command string
		clears  bool
	}{
		{"conjoined assignments", "clear @s stone[damage=1,damage=2]", false},
		{"conjoined negations", "clear @s stone[!damage,!damage]", true},
		{"alternatives", "clear @s stone[damage|!damage]", true},
		{"repeated alternative", "clear @s stone[damage|damage]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.GameMode = "creative"
			srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

			c.ChatCommand("give @s stone 3")
			servertest.AwaitSlotUpdates(t, c, 1)
			c.ChatCommand(tc.command)
			c.Settle()
			empty := srv.InventorySlot("Testificate", player.HotbarSlot).Empty()
			if empty != tc.clears {
				t.Errorf("%q cleared=%v, want %v", tc.command, empty, tc.clears)
			}
			c.Disconnect()
		})
	}
}

// TestClearRefusesMalformedItemPredicates checks the refusals the item
// predicate argument answers a value its codec cannot decode, where the
// value's reading stopped: the message carries the codec's own wording.
// vanilla: ItemPredicateArgument, ComponentPredicateParser
func TestClearRefusesMalformedItemPredicates(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, tc := range []struct {
		name    string
		command string
		key     string
		with    []string
		cursor  int
	}{
		{"sub-predicate that is no map",
			"clear @s stone[damage~1]",
			"arguments.item.predicate.malformed",
			[]string{"minecraft:damage", "Not a map: 1"},
			strings.Index("clear @s stone[damage~1]", "1]") + 1},
		{"count that is no range",
			"clear @s stone[count=abc]",
			"arguments.item.component.malformed",
			[]string{"minecraft:count", `Failed to parse either. First: Not a map: "abc"; Second: Not a number`},
			strings.Index("clear @s stone[count=abc]", "abc") + 3},
		{"count sub-predicate that is no range",
			"clear @s stone[count~abc]",
			"arguments.item.predicate.malformed",
			[]string{"minecraft:count", `Failed to parse either. First: Not a map: "abc"; Second: Not a number`},
			strings.Index("clear @s stone[count~abc]", "abc") + 3},
		{"enchantment predicate that is no list",
			"clear @s stone[enchantments~5]",
			"arguments.item.predicate.malformed",
			[]string{"minecraft:enchantments", "Not a list: 5"},
			strings.Index("clear @s stone[enchantments~5]", "5]") + 1},
		{"damage predicate field that is no bound",
			"clear @s stone[damage~{damage:abc}]",
			"arguments.item.predicate.malformed",
			[]string{"minecraft:damage", `Failed to parse either. First: Not a map: "abc"; Second: Not a number`},
			strings.Index("clear @s stone[damage~{damage:abc}]", "{damage:abc}") + len("{damage:abc}")},
		{"count map with a bad bound",
			"clear @s stone[count={min:abc}]",
			"arguments.item.component.malformed",
			[]string{"minecraft:count", "Not a number"},
			strings.Index("clear @s stone[count={min:abc}]", "{min:abc}") + len("{min:abc}")},
		{"count map with swapped bounds",
			"clear @s stone[count={min:5,max:1}]",
			"arguments.item.component.malformed",
			[]string{"minecraft:count", "Swapped bounds in range: Optional[5] is higher than Optional[1]"},
			strings.Index("clear @s stone[count={min:5,max:1}]", "{min:5,max:1}") + len("{min:5,max:1}")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.ChatCommand(tc.command)
			c.ExpectCommandError(
				servertest.CommandErrorMessageForTest(tc.key, tc.with...),
				servertest.CommandErrorContextForTest(tc.command, tc.cursor))
		})
	}
	c.Disconnect()
}

// TestClearMatchesAnEmptyEnchantmentsPredicate checks an enchantments
// predicate whose list is empty: it demands nothing of the stack, so it
// matches every item through the enchantments component every item carries by
// default and the stone is cleared.
// vanilla: ItemPredicateArgument.lookupPredicateType, EnchantmentsPredicate
func TestClearMatchesAnEmptyEnchantmentsPredicate(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("give @s stone 3")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand("clear @s stone[enchantments~[]]")
	c.Settle()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hotbar = %+v, want the three stone the empty predicate clears", got)
	}
	c.Disconnect()
}

// TestClearMatchesEnchantmentPredicateEntries checks every shape an
// EnchantmentPredicate entry takes: a named enchantment matches when one of
// its names is present at a level the bounds take, a bare levels bound matches
// when any enchantment on the stack sits in it, and an entry with neither
// demands a non-empty map. An absent enchantment never counts as present, so
// an unenchanted stack is not cleared.
// vanilla: EnchantmentPredicate.containedIn
func TestClearMatchesEnchantmentPredicateEntries(t *testing.T) {
	t.Parallel()
	sharpness, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("the registry has no sharpness")
	}
	enchanted := protocol.ItemStack{
		ItemID:       servertest.MustItem("minecraft:diamond_sword"),
		Count:        1,
		Enchantments: map[int32]int32{sharpness: 3},
	}
	plain := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 1}
	for _, tc := range []struct {
		name    string
		stack   protocol.ItemStack
		command string
		clears  bool
	}{
		{"named present", enchanted, "clear @s diamond_sword[enchantments~[{enchantments:sharpness}]]", true},
		{"named absent", enchanted, "clear @s diamond_sword[enchantments~[{enchantments:mending}]]", false},
		{"named on plain stone", plain, "clear @s stone[enchantments~[{enchantments:sharpness}]]", false},
		{"any of several names", enchanted, "clear @s diamond_sword[enchantments~[{enchantments:[sharpness,mending]}]]", true},
		{"level in bounds", enchanted, "clear @s diamond_sword[enchantments~[{levels:3}]]", true},
		{"level out of bounds", enchanted, "clear @s diamond_sword[enchantments~[{levels:5}]]", false},
		{"level max below", enchanted, "clear @s diamond_sword[enchantments~[{levels:{max:2}}]]", false},
		{"named and level out", enchanted, "clear @s diamond_sword[enchantments~[{enchantments:sharpness,levels:1}]]", false},
		{"named and level in", enchanted, "clear @s diamond_sword[enchantments~[{enchantments:sharpness,levels:3}]]", true},
		{"neither non-empty", enchanted, "clear @s diamond_sword[enchantments~[{}]]", true},
		{"neither empty", plain, "clear @s *[enchantments~[{}]]", false},
		{"level on plain stone", plain, "clear @s *[enchantments~[{levels:1}]]", false},
		{"level max zero on plain stone", plain, "clear @s *[enchantments~[{levels:{max:0}}]]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.GameMode = "creative"
			srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

			srv.GiveSlot("Testificate", player.HotbarSlot, tc.stack)
			c.ChatCommand(tc.command)
			if tc.clears {
				c.Settle()
				if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
					t.Errorf("%q cleared=%v, hotbar=%+v", tc.command, tc.clears, got)
				}
			} else {
				servertest.AwaitCommandErrorText(t, c, "clear.failed.single", servertest.PlayerDisplayComponent("Testificate"))
				servertest.AwaitTicks(t, srv, 5)
				if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Empty() {
					t.Errorf("%q cleared the stack, want it left", tc.command)
				}
			}
			c.Disconnect()
		})
	}
}

// TestClearTestsComponentsThroughTheItemDefaults checks that /clear's
// component test reads the item's own default components beneath the stack's
// patch: a sword's damage default makes the bare damage term match where
// plain stone has none, and the enchantments and repair cost every item
// carries by default match it.
// vanilla: ItemStack.has, DataComponentMap
func TestClearTestsComponentsThroughTheItemDefaults(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// A fresh sword still carries the damage component by default, so the
	// bare damage term matches it.
	srv.GiveSlot("Testificate", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItem("minecraft:diamond_sword"), Count: 1})
	c.ChatCommand("clear @s diamond_sword[damage]")
	c.Settle()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the sword after the damage term = %+v, want the default damage to match", got)
	}

	// Stone carries no damage component, so an exact damage of zero misses.
	c.ChatCommand("give @s stone 3")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand("clear @s stone[damage=0]")
	servertest.AwaitCommandErrorText(t, c, "clear.failed.single", servertest.PlayerDisplayComponent("Testificate"))
	c.Settle()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 3 {
		t.Errorf("the stone after damage=0 = %+v, want it left", got)
	}

	// Every item carries enchantments and repair cost by default, so both
	// bare terms match the stone.
	c.ChatCommand("clear @s stone[enchantments]")
	c.Settle()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the stone after the enchantments term = %+v, want the default to match", got)
	}
	c.ChatCommand("give @s stone 3")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand("clear @s stone[repair_cost]")
	c.Settle()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the stone after the repair cost term = %+v, want the default to match", got)
	}

	// A plain rocket carries the default fireworks component.
	srv.GiveSlot("Testificate", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItem("minecraft:firework_rocket"), Count: 1})
	c.ChatCommand("clear @s firework_rocket[fireworks]")
	c.Settle()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the rocket after the fireworks term = %+v, want the default to match", got)
	}
	c.Disconnect()
}

// TestClearAcceptsWhitespaceBeforeItsPredicateBracket checks the item
// predicate grammar that skips whitespace before the bracket, and that the
// parted list filters the clear: a count of three clears the three stone,
// while the damage existence test, which plain stone fails, clears none and
// answers with the no-items-found refusal rather than stopping at the parted
// bracket.
// vanilla: ComponentPredicateParser
func TestClearAcceptsWhitespaceBeforeItsPredicateBracket(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		command string
		clears  bool
	}{
		{"named item matches", "clear @s stone [count=3]", true},
		{"any item matches", "clear @s * [count=3]", true},
		{"empty list matches", "clear @s stone [ ]", true},
		{"named item misses", "clear @s stone [damage]", false},
		{"any item misses", "clear @s * [damage]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.GameMode = "creative"
			srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

			c.ChatCommand("give @s stone 3")
			updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
			if updates[0].Stack.Count != 3 {
				t.Fatalf("given hotbar = %+v, want three stone", updates[0])
			}
			c.ChatCommand(tc.command)
			if tc.clears {
				c.Settle()
				if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
					t.Errorf("hotbar after %q = %+v, want the stone cleared", tc.command, got)
				}
				return
			}
			// The list was read, so the command ran and found nothing
			// rather than dying on the parted bracket.
			servertest.AwaitCommandErrorText(t, c, "clear.failed.single", servertest.PlayerDisplayComponent("Testificate"))
			servertest.AwaitTicks(t, srv, 5)
			if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 3 {
				t.Errorf("hotbar after %q = %+v, want the three stone left", tc.command, got)
			}
			c.Disconnect()
		})
	}
}

// TestClearMatchesPredicateValues checks the predicate terms /clear tests a
// stack against: a damage value test leaves a differently worn sword alone
// and takes it where the wear matches, and a count test misses a stack whose
// size differs.
// vanilla: ItemPredicate, ComponentPredicateParser
func TestClearMatchesPredicateValues(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// A diamond sword worn five.
	srv.GiveSlot("Testificate", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItem("minecraft:diamond_sword"), Count: 1, Damage: 5})

	// A wear of seven is not the sword's five, so the clear finds nothing.
	c.ChatCommand("clear @s diamond_sword[damage=7]")
	servertest.AwaitCommandErrorText(t, c, "clear.failed.single", servertest.PlayerDisplayComponent("Testificate"))
	c.Settle()
	sword := srv.InventorySlot("Testificate", player.HotbarSlot)
	if sword.Empty() || sword.Damage != 5 {
		t.Errorf("the sword after the miss = %+v, want the worn five left", sword)
	}

	// The sword's own wear matches, so it goes.
	c.ChatCommand("clear @s diamond_sword[damage=5]")
	c.Settle()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the sword after the matching wear = %+v, want it cleared", got)
	}

	// A count that is not the stack's size finds nothing.
	c.ChatCommand("give @s stone 3")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand("clear @s stone[count=2]")
	servertest.AwaitCommandErrorText(t, c, "clear.failed.single", servertest.PlayerDisplayComponent("Testificate"))
	c.Settle()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 3 {
		t.Errorf("the stone after the count miss = %+v, want the three left", got)
	}
	c.Disconnect()
}

// TestGiveSelectorWithSpacesInItsBrackets checks the targets argument read
// whole: the item lands for the player the selector names even when its
// options are parted by spaces.
func TestGiveSelectorWithSpacesInItsBrackets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the registry has no stone")
	}
	c.ChatCommand("give @s[ name=Testificate ] stone 2")
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Stack.ItemID != stone || updates[0].Stack.Count != 2 {
		t.Errorf("give slot update = %+v, want two stone", updates[0])
	}
	c.Disconnect()
}

// TestGiveThrowsWhatDoesNotFitLikeADrop checks the leftover of a /give: with
// every item slot full, it is thrown the way a dropped item is — from three
// tenths of a block under the standing eyes, moving away in the look
// direction at the throw's strength and pickable at once — rather than laid
// flat at the feet.
// vanilla: GiveCommand.giveItem, LivingEntity.createItemStackToDrop
func TestGiveThrowsWhatDoesNotFitLikeADrop(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// Every one of the 36 item slots holds stone, so none is free for the
	// give's dirt and all of it is left over.
	stone := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 64}
	for slot := player.MainSlot; slot < player.OffHandSlot; slot++ {
		srv.GiveSlot("Testificate", slot, stone)
	}

	c.ChatCommand("give @s minecraft:dirt 1")
	item := c.ExpectDroppedItem()
	if item.Stack.ItemID != servertest.MustItem("minecraft:dirt") || item.Stack.Count != 1 {
		t.Fatalf("the leftover = %+v, want one dirt", item.Stack)
	}
	// The thrower's own x and z, the feet three tenths of a block under
	// the standing eyes.
	if math.Abs(item.X-0.5) > 1e-9 || math.Abs(item.Z-0.5) > 1e-9 {
		t.Errorf("the leftover stood at (%v, %v), want the thrower's own x and z", item.X, item.Z)
	}
	if want := 64 + 1.62 - entity.ThrowSpawnBelow; math.Abs(item.Y-want) > 1e-9 {
		t.Errorf("the leftover's feet at y=%v, want %v under the eyes", item.Y, want)
	}
	// The velocity of a throw: the look's own 0.3 south (+Z) with the
	// drop's little spread, not the zero of an item laid on the ground.
	const nearEnough = 0.021
	if math.Abs(item.Vz-0.3) > nearEnough || math.Abs(item.Vx) > nearEnough {
		t.Errorf("the leftover's velocity = (%v, %v, %v), want the throw's 0.3 south", item.Vx, item.Vy, item.Vz)
	}
	if item.Vy <= 0 || item.Vy >= 0.2 {
		t.Errorf("the leftover's rise = %v, want the throw's tenth", item.Vy)
	}
	// A leftover waits out no pickup delay, unlike a throw's two seconds:
	// the receiver can take it back at once.
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].PickupDelay != 0 {
		t.Errorf("the leftover's pickup delay = %+v, want none", items)
	}
	c.Disconnect()
}

// TestGiveKeepsComponentsOnTheLeftover checks the stack a /give leaves on the
// ground when the inventory cannot hold it: the leftover is the whole stack,
// its enchantments included, not only the item's name.
// vanilla: GiveCommand.giveItem, LivingEntity.createItemStackToDrop
func TestGiveKeepsComponentsOnTheLeftover(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// Every item slot holds stone, so the sword has nowhere to go.
	stone := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 64}
	for slot := player.MainSlot; slot < player.OffHandSlot; slot++ {
		srv.GiveSlot("Testificate", slot, stone)
	}

	c.ChatCommand("give @s minecraft:diamond_sword[enchantments={sharpness:5}] 1")
	item := c.ExpectDroppedItem()
	if item.Stack.Count != 1 || item.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:diamond_sword") {
		t.Fatalf("the leftover = %+v, want one diamond sword", item.Stack)
	}
	if got := item.Stack.Enchantments[servertest.SharpnessID(t)]; got != 5 {
		t.Errorf("the leftover's sharpness = %d, want the five the stack was given with", got)
	}
	c.Disconnect()
}

// TestGiveThrowsEveryStackOfAHugeGive checks a /give larger than one stack
// with a full inventory: vanilla walks the count a maximum stack at a time,
// so every chunk that does not fit is its own leftover entity of one stack.
// vanilla: GiveCommand.giveItem
func TestGiveThrowsEveryStackOfAHugeGive(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// Every item slot holds stone, so none of the cobblestone fits.
	stone := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 64}
	for slot := player.MainSlot; slot < player.OffHandSlot; slot++ {
		srv.GiveSlot("Testificate", slot, stone)
	}

	// The give spawns its stacks on the connection's read goroutine, one
	// register at a time, so a count taken while the command is still
	// running sees only the handful already spawned. Stop the loop and
	// settle: the ping's answer is read after the command, which is every
	// stack spawned, and no tick runs to touch the items.
	srv.StopTick()
	c.ChatCommand("give @s minecraft:cobblestone 6400")
	c.Settle()

	// 6400 cobblestone is one hundred maximum stacks, each its own entity.
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 100 {
		t.Fatalf("the leftovers = %d entities, want 100", len(items))
	}
	for _, item := range items {
		if item.Item != "minecraft:cobblestone" || item.Count != 64 {
			t.Errorf("a leftover = %s x%d, want a stack of 64 cobblestone", item.Item, item.Count)
		}
	}
	c.Disconnect()
}

// TestGiveReservesTheLeftoverForTheReceiver checks that a /give's leftover is
// the receiver's alone: with the receiver's inventory full, a second player
// standing on the drop whose own inventory has room still cannot take it, and
// the receiver collects it once a slot frees up.
// vanilla: GiveCommand.giveItem, ItemEntity.setTarget
func TestGiveReservesTheLeftoverForTheReceiver(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	// Bob's 36 item slots are full, Alice's are empty, and both stand on
	// the same spawn spot: a leftover anyone may take would go to Alice.
	// Bob looks straight down so the throw lands by his feet.
	stone := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 64}
	for slot := player.MainSlot; slot < player.OffHandSlot; slot++ {
		srv.GiveSlot("Bob", slot, stone)
	}
	bob.MoveTo(0.5, 64, 0.5, 0, 90)
	alice.Settle()
	bob.Settle()

	alice.ChatCommand("give Bob minecraft:dirt 1")
	alice.ExpectDroppedItem()

	// Let the throw settle and its wait run out, then stand both players on
	// the drop: if it were anyone's, Alice — with room — would take it.
	servertest.AwaitTicks(t, srv, 60)
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("the leftover after the wait = %+v, want it still on the ground", items)
	}
	alice.MoveTo(items[0].X, 64, items[0].Z, 0, 90)
	bob.MoveTo(items[0].X, 64, items[0].Z, 0, 90)
	alice.Settle()
	bob.Settle()
	servertest.AwaitTicks(t, srv, 10)
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 1 {
		t.Fatalf("the leftover under the players = %+v, want it reserved for Bob", items)
	}
	if got := srv.InventorySlot("Alice", player.HotbarSlot); !got.Empty() {
		t.Errorf("Alice's hotbar = %+v, want her unable to take Bob's leftover", got)
	}

	// A cleared slot for Bob and the drop is his.
	alice.ChatCommand("clear Bob")
	servertest.AwaitTicks(t, srv, 10)
	if got := srv.InventorySlot("Bob", player.HotbarSlot); got.ItemID != servertest.MustItem("minecraft:dirt") {
		t.Errorf("Bob's hotbar after clearing = %+v, want the reserved dirt", got)
	}
	alice.Disconnect()
	bob.Disconnect()
}

// TestGiveShowsAFakeItemAndPlaysThePickupSound checks the shape of a give
// the inventory takes: for each maximum stack that lands, the receiver's
// client is shown a fake item of one that despawns at once, the receiver
// hears the pickup sound, and a player beyond the sound's earshot does not.
// The fake item is nobody's, matching vanilla's targetless drop for it.
// vanilla: GiveCommand.giveItem, ItemEntity.makeFakeItem
func TestGiveShowsAFakeItemAndPlaysThePickupSound(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	carol := connect().Join("Carol")

	// Carol stands a hundred blocks east, well outside the sixteen-block
	// earshot a volume-0.2 sound plays to.
	carol.MoveTo(100.5, 64, 0.5, 0, 0)
	bob.Settle()
	carol.Settle()

	// The tick stopped keeps the fake item, which despawns the tick after
	// it arrives, on the ground long enough to be read.
	srv.StopTick()
	alice.ChatCommand("give Bob minecraft:diamond 5")

	var spawned, heard bool
	for packetID, data := range bob.Stream() {
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItem {
				spawned = true
			}
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			if id := r.ReadVarInt(); id > 0 {
				if name, _ := servertest.SoundEventName(id - 1); name == "minecraft:entity.item.pickup" {
					heard = true
				}
			}
		}
		if spawned && heard {
			break
		}
	}
	if !spawned {
		t.Error("a fitting give showed no item")
	}
	if !heard {
		t.Error("the receiver heard no pickup sound")
	}

	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("the items after the give = %+v, want the one fake item", items)
	}
	if items[0].Item != "minecraft:diamond" || items[0].Count != 1 {
		t.Errorf("the fake item = %s x%d, want one diamond", items[0].Item, items[0].Count)
	}
	if items[0].PickupDelay != 32767 { // entity.pickupDelayInfinite
		t.Errorf("the fake item's pickup delay = %d, want the never-to-be-taken 32767", items[0].PickupDelay)
	}
	if items[0].Owner != "" {
		t.Errorf("the fake item is reserved for %q, want it nobody's", items[0].Owner)
	}

	// Carol is out of earshot: the sound never reaches her.
	faraway, _, _ := servertest.CollectSoundsTicks(carol, srv, 0)
	if n := servertest.CountSounds(faraway, "minecraft:entity.item.pickup", protocol.SoundCategoryPlayer); n != 0 {
		t.Errorf("a player a hundred blocks away heard %d pickup sounds, want the earshot to miss her", n)
	}
	alice.Disconnect()
	bob.Disconnect()
	carol.Disconnect()
}

// TestGiveKeepsComponentsOnTheFakeItem checks the item a fitting give shows
// the receiver for the pickup animation: it is the whole stack, its
// enchantments included, not only the item's name.
// vanilla: GiveCommand.giveItem, ItemEntity.makeFakeItem
func TestGiveKeepsComponentsOnTheFakeItem(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// The tick stopped keeps the fake item, which despawns the tick after
	// it arrives, on the ground long enough to be read.
	srv.StopTick()
	c.ChatCommand("give @s minecraft:diamond_sword[enchantments={sharpness:5}]")
	servertest.AwaitSlotUpdates(t, c, 1)
	item := c.ExpectDroppedItem()
	if item.Stack.Count != 1 || item.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:diamond_sword") {
		t.Fatalf("the fake item = %+v, want one diamond sword", item.Stack)
	}
	if got := item.Stack.Enchantments[servertest.SharpnessID(t)]; got != 5 {
		t.Errorf("the fake item's sharpness = %d, want the five the stack was given with", got)
	}
	c.Disconnect()
}

// TestGiveTooManyItemsIsAFailure checks that the give cap throws, so execute
// store success reads 0.
// vanilla: GiveCommand.giveItem
func TestGiveTooManyItemsIsAFailure(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t)
	if got := storedSuccess(t, srv, c, "give @s minecraft:diamond_sword 32767"); got != "0" {
		t.Errorf("an over-cap give stored success %s, want 0", got)
	}
	if got := storedSuccess(t, srv, c, "give @s minecraft:diamond_sword 1"); got != "1" {
		t.Errorf("a one-sword give stored success %s, want 1", got)
	}
}

// TestGiveRefusesMoreThanAHundredStacks checks the give cap: vanilla refuses
// more than a hundred maximum stacks of the item, naming the cap and the
// item, rather than spawning an entity per stack.
// vanilla: GiveCommand.giveItem
func TestGiveRefusesMoreThanAHundredStacks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// An unstackable item's cap is a hundred; 32767 is over it and would
	// otherwise be a leftover entity a stack.
	limit := int64(100)
	var want protocol.Buffer
	want.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{
		{Translate: "commands.give.failed.toomanyitems", With: []protocol.Text{
			{Number: &limit},
			{
				Translate: "chat.square_brackets",
				Color:     "white",
				With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "item.minecraft.diamond_sword"}}}},
				HoverItem: &protocol.HoverItem{ID: "minecraft:diamond_sword", Count: 1},
			},
		}},
	}})
	want.PutBool(false)
	c.ChatCommand("give @s minecraft:diamond_sword 32767")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("the refusal = % X, want the toomanyitems failure % X", got, want.Bytes())
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the items after the refused give = %+v, want none", items)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the inventory after the refused give = %+v, want it untouched", got)
	}
	c.Disconnect()
}

// TestGiveEmptyBannerPatternsOnlyDefaultsOnBanners checks that an empty
// banner_patterns list is dropped on a banner, whose default it is, and kept
// on any other item, where it is a patch that stops the stack merging.
// vanilla: PatchedDataComponentMap.applyPatch
func TestGiveEmptyBannerPatternsOnlyDefaultsOnBanners(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand(`give @s white_banner[banner_patterns=[]]`)
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	if _, has := updates[0].Stack.BannerPatternsOf(); has {
		t.Errorf("the given banner = %+v, want the empty list left off", updates[0].Stack)
	}
	c.ChatCommand(`give @s stone[banner_patterns=[]]`)
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	if layers, has := updates[0].Stack.BannerPatternsOf(); !has || len(layers) != 0 {
		t.Errorf("the given stone = %+v, want an explicit empty banner_patterns", updates[0].Stack)
	}
	c.Disconnect()
}

// TestClearTakesTagAndWildcardPredicates checks /clear's item predicate
// forms: a #tag takes every member, * takes every stack, and a component
// test takes only the stacks it names.
// vanilla: ItemPredicateArgument, ClearCommand
func TestClearTakesTagAndWildcardPredicates(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	fill := func() {
		srv.GiveSlot("Testificate", player.HotbarSlot,
			protocol.ItemStack{ItemID: servertest.MustItem("minecraft:oak_log"), Count: 3})
		srv.GiveSlot("Testificate", player.HotbarSlot+1,
			protocol.ItemStack{ItemID: servertest.MustItem("minecraft:diamond_sword"), Count: 1})
	}
	fill()
	three := int64(3)
	c.ChatCommand("clear @s #minecraft:logs")
	servertest.AwaitCommandFeedbackText(t, c, "commands.clear.success.single",
		protocol.Text{Number: &three}, servertest.PlayerDisplayComponent("Testificate"))
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the logs after the tag clear = %+v, want none", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot+1); got.Empty() {
		t.Error("the tag clear took the sword")
	}

	fill()
	c.ChatCommand("clear @s *[minecraft:damage=0]")
	one := int64(1)
	servertest.AwaitCommandFeedbackText(t, c, "commands.clear.success.single",
		protocol.Text{Number: &one}, servertest.PlayerDisplayComponent("Testificate"))
	if got := srv.InventorySlot("Testificate", player.HotbarSlot+1); !got.Empty() {
		t.Errorf("the sword after the damage clear = %+v, want none", got)
	}

	c.ChatCommand("clear @s *")
	servertest.AwaitCommandFeedbackText(t, c, "commands.clear.success.single",
		protocol.Text{Number: &three}, servertest.PlayerDisplayComponent("Testificate"))
	for slot := player.MainSlot; slot < player.OffHandSlot; slot++ {
		if got := srv.InventorySlot("Testificate", slot); !got.Empty() {
			t.Errorf("slot %d after clear * = %+v, want none", slot, got)
		}
	}
	c.Disconnect()
}

// TestGiveToACreativePlayerWithAFullInventoryVoidsTheLeftover checks that a
// creative player's give that fits nowhere counts as added: the fake item
// and the pickup sound, no leftover thrown.
// vanilla: Inventory.add, GiveCommand.giveItem
func TestGiveToACreativePlayerWithAFullInventoryVoidsTheLeftover(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	stone := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 64}
	for slot := player.MainSlot; slot < player.OffHandSlot; slot++ {
		srv.GiveSlot("Testificate", slot, stone)
	}

	srv.StopTick()
	c.ChatCommand("give @s minecraft:dirt 1")
	item := c.ExpectDroppedItem()
	if item.Stack.ItemID != servertest.MustItem("minecraft:dirt") {
		t.Fatalf("the dropped item = %+v, want the dirt", item.Stack)
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].PickupDelay != 32767 || items[0].Owner != "" {
		t.Errorf("the items after the give = %+v, want only the unpickable fake item", items)
	}
	c.Disconnect()
}

// TestGiveCountIsNotCappedBelowTheStackLimit checks that a count above
// 32767 reaches the toomanyitems check, which names the hundred stacks.
// vanilla: GiveCommand.giveItem
func TestGiveCountIsNotCappedBelowTheStackLimit(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	limit := int64(6400)
	var want protocol.Buffer
	want.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{
		{Translate: "commands.give.failed.toomanyitems", With: []protocol.Text{
			{Number: &limit},
			{
				Translate: "chat.square_brackets",
				Color:     "white",
				With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "block.minecraft.stone"}}}},
				HoverItem: &protocol.HoverItem{ID: "minecraft:stone", Count: 1},
			},
		}},
	}})
	want.PutBool(false)
	c.ChatCommand("give @s minecraft:stone 40000")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("the refusal = % X, want the toomanyitems failure % X", got, want.Bytes())
	}
	c.Disconnect()
}

// TestGiveCarriesTheModelledComponents checks that /give takes every
// component the stored-stack reader models, rather than giving nothing and
// saying nothing: the stack lands with the component and the feedback is sent.
// vanilla: ItemParser
func TestGiveCarriesTheModelledComponents(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	cases := []struct {
		command   string
		component int32
	}{
		{"firework_rocket[fireworks={flight_duration:3}]", protocol.DataComponentFireworks},
		{"firework_star[firework_explosion={shape:'small_ball',colors:[I;255]}]", protocol.DataComponentFireworkExplosion},
		{"leather_chestplate[dyed_color=255]", protocol.DataComponentDyedColor},
		{"ominous_bottle[ominous_bottle_amplifier=4]", protocol.DataComponentOminousBottleAmplifier},
		{"crossbow[charged_projectiles=[{id:'arrow'}]]", protocol.DataComponentChargedProjectiles},
		{"shulker_box[container=[{slot:0,item:{id:'stone'}}]]", protocol.DataComponentContainer},
		{"bundle[bundle_contents=[{id:'stone'}]]", protocol.DataComponentBundleContents},
		{"suspicious_stew[suspicious_stew_effects=[{id:'speed',duration:100}]]", protocol.DataComponentSuspiciousStew},
		{"compass[lodestone_tracker={tracked:0b}]", protocol.DataComponentLodestoneTracker},
		{"stone[item_name='x']", protocol.DataComponentItemName},
		{"shield[base_color='red']", protocol.DataComponentBaseColor},
		{`stone[can_place_on={blocks:"minecraft:dirt"}]`, protocol.DataComponentCanPlaceOn},
		{`diamond_pickaxe[can_break=[{blocks:"#minecraft:logs"}]]`, protocol.DataComponentCanBreak},
		{`armor_stand[entity_data={id:"minecraft:armor_stand",Invisible:1b}]`, protocol.DataComponentEntityData},
		{`chest[block_entity_data={id:"minecraft:chest",CustomName:"x"}]`, protocol.DataComponentBlockEntityData},
		{"iron_chestplate[trim={material:'minecraft:gold',pattern:'minecraft:coast'}]", protocol.DataComponentTrim},
		{"goat_horn[instrument='minecraft:sing_goat_horn']", protocol.DataComponentInstrument},
		{"painting[painting/variant='minecraft:kebab']", protocol.DataComponentPaintingVariant},
		{"axolotl_bucket[axolotl/variant='blue']", protocol.DataComponentAxolotlVariant},
		{"salmon_bucket[salmon/size='large']", protocol.DataComponentSalmonSize},
		{"tropical_fish_bucket[tropical_fish/pattern='sunstreak']", protocol.DataComponentTropicalFishPattern},
		{"tropical_fish_bucket[tropical_fish/base_color='lime']", protocol.DataComponentTropicalFishBaseColor},
		{"tropical_fish_bucket[tropical_fish/pattern_color='blue']", protocol.DataComponentTropicalFishPatternColor},
		{"rabbit_spawn_egg[rabbit/variant='black']", protocol.DataComponentRabbitVariant},
		{"potion[potion_duration_scale=0.5f]", protocol.DataComponentPotionDurationScale},
		{"stick[item_model=\"minecraft:diamond_sword\"]", protocol.DataComponentItemModel},
		{"paper[tooltip_style=\"minecraft:custom\"]", protocol.DataComponentTooltipStyle},
		{"paper[rarity=\"epic\"]", protocol.DataComponentRarity},
		{"stone[tooltip_display={hidden_components:[\"minecraft:enchantments\"]}]", protocol.DataComponentTooltipDisplay},
		{"stick[break_sound=\"minecraft:item.shield.break\"]", protocol.DataComponentBreakSound},
		{`stick[break_sound={sound_id:"minecraft:x",range:16}]`, protocol.DataComponentBreakSound},
	}
	for _, tc := range cases {
		c.ChatCommand("give @s " + tc.command)
		updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
		// The container rides its own field rather than the raw components.
		has := tc.component == protocol.DataComponentContainer && len(updates[0].Stack.Container) == 1
		for _, raw := range updates[0].Stack.Raw {
			has = has || raw.Type == tc.component
		}
		if !has {
			t.Errorf("/give %s: the stack %+v carries no component %d", tc.command, updates[0].Stack, tc.component)
		}
		sent := slices.ContainsFunc(chats, func(chat []byte) bool {
			return bytes.Contains(chat, []byte("commands.give.success.single"))
		})
		for packetID, data := range c.Packets(servertest.ExpectTimeout) {
			if sent {
				break
			}
			sent = packetID == protocol.IDPlaySystemChat && bytes.Contains(data, []byte("commands.give.success.single"))
		}
		if !sent {
			t.Errorf("/give %s sent no success feedback", tc.command)
		}
	}

	// The goat horn's own instrument is its default and no patch.
	// vanilla: Items.GOAT_HORN
	c.ChatCommand("give @s goat_horn[instrument='minecraft:ponder_goat_horn']")
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	for _, raw := range updates[0].Stack.Raw {
		if raw.Type == protocol.DataComponentInstrument {
			t.Errorf("a ponder goat horn carries the instrument component %v", raw.Data)
		}
	}
	c.Disconnect()
}

// TestExecuteIfSlotsCountsTheSlotsThatExist checks the slots test: it counts
// the named slots the body or container has, filled or empty, refuses a block
// that is no container, and turns over under unless.
// vanilla: ExecuteCommand.countSlots
func TestExecuteIfSlotsCountsTheSlotsThatExist(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand("execute if slots entity @s hotbar.*")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(9)) {
		t.Errorf("hotbar.* answered % X, want nine slots counted", got)
	}

	c.ChatCommand("setblock 2 64 3 minecraft:chest")
	c.ChatCommand("setblock 4 64 3 minecraft:stone")
	c.Settle()
	c.ChatCommand("execute if slots block 2 64 3 container.*")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(27)) {
		t.Errorf("the chest's container.* answered % X, want 27 slots counted", got)
	}

	// A slot past the chest's 27 does not exist, so unless passes.
	c.ChatCommand("execute unless slots block 2 64 3 container.30")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, plainFeedbackTextsForTest("commands.execute.conditional.pass")) {
		t.Errorf("the missing chest slot answered % X, want the conditional pass", got)
	}

	// Stone is no container: vanilla's getSlots throws, under unless too.
	c.ChatCommand("execute unless slots block 4 64 3 container.*")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, commandErrorNumbersForTest("commands.item.source.not_a_container", 4, 64, 3)) {
		t.Errorf("the stone answered % X, want the source not-a-container refusal", got)
	}
	c.Disconnect()
}

// TestExecuteItemsAndSlotsCommandTree checks the items and slots conditionals
// the client is sent against 26.3's commands.json: names, parsers, the
// executable leaves and their redirect to execute.
func TestExecuteItemsAndSlotsCommandTree(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.ItemCreativeConfig(), servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	nodes := servertest.DecodeCommands(t, c.AwaitPlay(protocol.IDPlayCommands))
	c.SkipJoin()

	parsers := map[int32]string{6: "entity", 8: "block_pos", 15: "item_predicate", 57: "slot_source"}
	var execute int32 = -1
	for _, i := range nodes[0].Children {
		if nodes[i].Name == "execute" {
			execute = i
		}
	}
	if execute < 0 {
		t.Fatal("the tree has no execute")
	}
	// describe renders a subtree one node per line, in tree order.
	var describe func(i int32, depth int) string
	describe = func(i int32, depth int) string {
		n := nodes[i]
		line := strings.Repeat(" ", depth) + n.Name
		if n.Typ == 2 {
			line += " <" + parsers[n.Parser] + ">"
		}
		if n.Executable {
			line += " exec"
		}
		if n.Redirect == execute {
			line += " ->execute"
		} else if n.Redirect >= 0 {
			line += " ->?"
		}
		line += "\n"
		for _, child := range n.Children {
			line += describe(child, depth+1)
		}
		return line
	}
	child := func(parent int32, name string) int32 {
		for _, i := range nodes[parent].Children {
			if nodes[i].Name == name {
				return i
			}
		}
		t.Fatalf("node %q has no child %q", nodes[parent].Name, name)
		return -1
	}
	want := map[string]string{
		"items": "items\n" +
			" entity\n  source <entity>\n   slots <slot_source>\n    item_predicate <item_predicate> exec ->execute\n" +
			" block\n  source <block_pos>\n   slots <slot_source>\n    item_predicate <item_predicate> exec ->execute\n",
		"slots": "slots\n" +
			" entity\n  source <entity>\n   slots <slot_source> exec ->execute\n" +
			" block\n  source <block_pos>\n   slots <slot_source> exec ->execute\n",
	}
	for _, mode := range []string{"if", "unless"} {
		for _, test := range []string{"items", "slots"} {
			if got := describe(child(child(execute, mode), test), 0); got != want[test] {
				t.Errorf("execute %s %s tree:\n%s\nwant:\n%s", mode, test, got, want[test])
			}
		}
	}
	c.Disconnect()
}

// TestClearMatchesMoreComponentTests checks the predicate terms beyond the
// few /clear modelled by hand: existence and exact value of any serialised
// component, the damage predicate and the potions predicate, and the same
// filter under /execute if items.
// vanilla: ItemPredicateArgument, DamagePredicate, PotionsPredicate
func TestClearMatchesMoreComponentTests(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, give := range []string{
		`give @s potion[potion_contents={potion:"minecraft:healing"}]`,
		`give @s stick[custom_name="Bob"]`,
		`give @s leather_helmet[dyed_color=255]`,
		`give @s diamond_sword[damage=1555]`,
	} {
		c.ChatCommand(give)
		servertest.AwaitSlotUpdates(t, c, 1)
	}

	found := int64(1)
	hit := func(command string) {
		t.Helper()
		c.ChatCommand(command)
		servertest.AwaitCommandFeedbackText(t, c, "commands.clear.test.single",
			protocol.Text{Number: &found}, servertest.PlayerDisplayComponent("Testificate"))
	}
	miss := func(command string) {
		t.Helper()
		c.ChatCommand(command)
		servertest.AwaitCommandErrorText(t, c, "clear.failed.single", servertest.PlayerDisplayComponent("Testificate"))
	}
	hit(`clear @s *[potion_contents] 0`)
	hit(`clear @s *[custom_name="Bob"] 0`)
	hit(`clear @s *[dyed_color=255] 0`)
	miss(`clear @s *[dyed_color=254] 0`)
	hit(`clear @s diamond_sword[damage~{durability:{max:10}}] 0`)
	miss(`clear @s diamond_sword[damage~{durability:{min:10}}] 0`)
	hit(`clear @s potion[potion_contents~{potions:"minecraft:healing"}] 0`)
	miss(`clear @s potion[potion_contents~{potions:"minecraft:swiftness"}] 0`)
	hit(`clear @s potion[potion_contents~{}] 0`)

	c.ChatCommand(`execute if items entity @s container.* *[custom_name="Bob"]`)
	servertest.AwaitCommandFeedbackText(t, c, "commands.execute.conditional.pass_count", protocol.Text{Number: &found})
	c.Disconnect()
}
