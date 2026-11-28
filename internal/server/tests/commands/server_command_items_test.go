package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
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

// TestGiveAirNamesItWithoutHover checks that /give of air answers with the
// plain [Air] text, since the client refuses a show-item hover of air.
func TestGiveAirNamesItWithoutHover(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("give @s air")
	one := int64(1)
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.give.success.single",
		protocol.Text{Number: &one},
		protocol.Text{
			Translate: "chat.square_brackets",
			With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "block.minecraft.air"}}}},
		},
		servertest.PlayerDisplayComponent("Testificate"))
	c.Disconnect()
}
