package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// itemLootTable is a one-pool table that always gives count of item.
func itemLootTable(item string, count int) string {
	return `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"` + item +
		`","functions":[{"function":"minecraft:set_count","count":` + strconv.Itoa(count) + `}]}]}]}`
}

// giveStack runs a /loot give command and returns the one stack it lands,
// once the command's feedback has been read too.
func giveStack(t *testing.T, c *servertest.Client, command string) protocol.ItemStack {
	t.Helper()
	c.ChatCommand(command)
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if len(chats) == 0 {
		c.ExpectPlay(protocol.IDPlaySystemChat)
	}
	return updates[0].Stack
}

func expectGave(t *testing.T, c *servertest.Client, command, item string, count int32) {
	t.Helper()
	got := giveStack(t, c, command)
	if got.ItemID != servertest.MustItem(item) || got.Count != count {
		t.Errorf("%q gave item %d x%d, want %s x%d", command, got.ItemID, got.Count, item, count)
	}
}

// TestPackLootTablesOverrideAndReload checks that a data pack's loot table
// replaces the built-in one for a block's drop and for the fishing table, adds
// a table of its own, that a file that does not decode leaves the built-in
// table in force, and that /reload reads a changed or removed file. The pack
// layer is process-wide, so the tests that load loot are not parallel with
// each other and roll blocks no other test of the package rolls.
// vanilla: ReloadableServerRegistries, LootDataType.TABLES
func TestPackLootTablesOverrideAndReload(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data")
	block := filepath.Join(data, "minecraft", "loot_table", "blocks", "netherite_block.json")
	writeFile(t, block, itemLootTable("minecraft:emerald", 2))
	writeFile(t, filepath.Join(data, "minecraft", "loot_table", "blocks", "ancient_debris.json"),
		itemLootTable("minecraft:not_an_item", 1))
	writeFile(t, filepath.Join(data, "minecraft", "loot_table", "gameplay", "fishing.json"),
		itemLootTable("minecraft:stick", 3))
	writeFile(t, filepath.Join(data, "pk", "loot_table", "custom.json"), itemLootTable("minecraft:gold_ingot", 4))
	writeFile(t, filepath.Join(data, "pk", "loot_table", "broken.json"), `{"pools":[`)
	_, c := joinPacks(t, cfg)

	c.ChatCommand("setblock 0 80 0 minecraft:netherite_block")
	servertest.FeedbackComponent(t, c)
	const mine = "loot give @s mine 0 80 0"
	expectGave(t, c, mine, "minecraft:emerald", 2)
	expectGave(t, c, "loot give @s loot pk:custom", "minecraft:gold_ingot", 4)
	expectGave(t, c, "loot give @s fish minecraft:gameplay/fishing 0 80 0 minecraft:fishing_rod", "minecraft:stick", 3)

	c.ChatCommand("loot give @s loot pk:broken")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got,
		servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", "pk:broken", "minecraft:loot_table")) {
		t.Errorf("a table that does not decode answered % X, want the unknown table refusal", got)
	}

	c.ChatCommand("setblock 0 81 0 minecraft:ancient_debris")
	servertest.FeedbackComponent(t, c)
	expectGave(t, c, "loot give @s mine 0 81 0", "minecraft:ancient_debris", 1)

	writeFile(t, block, itemLootTable("minecraft:diamond", 5))
	c.ChatCommand("reload")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.reload.success")
	expectGave(t, c, mine, "minecraft:diamond", 5)

	if err := os.Remove(block); err != nil {
		t.Fatal(err)
	}
	c.ChatCommand("reload")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.reload.success")
	expectGave(t, c, mine, "minecraft:netherite_block", 1)
}

// TestPackPredicatesAnswerExecuteAndSelectors checks that a data pack's
// predicate is what execute if predicate and a selector's predicate option
// test, and that a loot table's reference condition follows it.
// vanilla: ReloadableServerRegistries, LootDataType.PREDICATES
func TestPackPredicatesAnswerExecuteAndSelectors(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk")
	predicate := filepath.Join(data, "predicate", "gate.json")
	writeFile(t, predicate, `{"condition":"minecraft:random_chance","chance":1}`)
	writeFile(t, filepath.Join(data, "predicate", "never.json"), `[{"condition":"minecraft:random_chance","chance":0}]`)
	writeFile(t, filepath.Join(data, "loot_table", "gated.json"),
		`{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:clay_ball",`+
			`"conditions":[{"condition":"minecraft:reference","name":"pk:gate"}]}]}]}`)
	// A holder set names a single holder without a list.
	// vanilla: HolderSetCodec.directCodec
	writeFile(t, filepath.Join(data, "predicate", "always.json"), `{"condition":"minecraft:random_chance","chance":1}`)
	writeFile(t, filepath.Join(data, "loot_table", "single_term.json"),
		`{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:brick",`+
			`"conditions":[{"condition":"minecraft:all_of","terms":"pk:always"}]}]}]}`)
	_, c := joinPacks(t, cfg)

	c.ChatCommand("execute if predicate pk:gate run say hi")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("the passing predicate said % X, want the tail's say", got)
	}
	c.ChatCommand("execute if predicate pk:never")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the failing predicate answered % X, want the conditional failure", got)
	}
	c.ChatCommand("execute if entity @s[predicate=pk:gate] run say hi")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("the selector option said % X, want the tail's say", got)
	}
	expectGave(t, c, "loot give @s loot pk:gated", "minecraft:clay_ball", 1)
	expectGave(t, c, "loot give @s loot pk:single_term", "minecraft:brick", 1)

	writeFile(t, predicate, `{"condition":"minecraft:random_chance","chance":0}`)
	c.ChatCommand("reload")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.reload.success")
	c.ChatCommand("execute if predicate pk:gate")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the reloaded predicate answered % X, want the conditional failure", got)
	}
}

// TestPackItemModifiersRunFromItemModify checks that /item modify runs a data
// pack's item modifier by name, as one function or a list, and inside an
// inline list or a sequence's single holder, refuses one that
// does not decode, and follows /reload.
// vanilla: ReloadableServerRegistries, LootDataType.MODIFIERS
func TestPackItemModifiersRunFromItemModify(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "item_modifier")
	single := filepath.Join(data, "five.json")
	writeFile(t, single, `{"function":"minecraft:set_count","count":5}`)
	writeFile(t, filepath.Join(data, "chain.json"),
		`[{"function":"minecraft:set_count","count":4},{"function":"minecraft:set_count","count":2,"add":true}]`)
	writeFile(t, filepath.Join(data, "swap.json"),
		`{"function":"minecraft:sequence","functions":[{"function":"minecraft:set_item","item":"minecraft:diamond"},`+
			`{"function":"minecraft:set_name","name":{"text":"Gem"}},{"function":"minecraft:set_count","count":3}]}`)
	writeFile(t, filepath.Join(data, "gone.json"), `[{"function":"minecraft:discard"}]`)
	writeFile(t, filepath.Join(data, "refused.json"), `{"function":"minecraft:set_book_cover","generation":4}`)
	writeFile(t, filepath.Join(data, "named.json"), `{"function":"minecraft:copy_name","source":"block_entity"}`)
	writeFile(t, filepath.Join(data, "broken.json"), `{"function":"minecraft:set_count"`)
	writeFile(t, filepath.Join(data, "plus_two.json"), `{"function":"minecraft:set_count","count":2,"add":true}`)
	writeFile(t, filepath.Join(data, "one_holder.json"), `{"function":"minecraft:sequence","functions":"pk:plus_two"}`)
	_, c := joinPacks(t, cfg)

	modify := func(name string, want int32) protocol.ItemStack {
		t.Helper()
		// The slot update and the command's feedback arrive in either order.
		run := func(command string) []servertest.SlotUpdate {
			c.ChatCommand(command)
			updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
			if len(chats) == 0 {
				c.ExpectPlay(protocol.IDPlaySystemChat)
			}
			return updates
		}
		run("item replace entity @s weapon.mainhand with minecraft:stone 1")
		updates := run("item modify entity @s weapon.mainhand " + name)
		if got := updates[0].Stack.Count; got != want {
			t.Errorf("modifier %s left %d, want %d", name, got, want)
		}
		return updates[0].Stack
	}
	modify("pk:five", 5)
	modify("pk:chain", 6)
	diamond, _ := registry.ItemID("minecraft:diamond")
	if got := modify("pk:swap", 3).ItemID; got != diamond {
		t.Errorf("set_item from a pack left item %d, want %d", got, diamond)
	}
	modify("pk:gone", 0)
	// copy_name loads, and without a block entity leaves the stack as it was.
	modify("pk:named", 1)
	// An inline list is a holder set: it names a modifier as well as writing one.
	// vanilla: LootItemFunctions.DIRECT_CODEC
	modify(`["pk:plus_two"]`, 3)
	modify(`[{type:"minecraft:set_count",count:4},"pk:plus_two"]`, 6)
	modify("pk:one_holder", 3)

	// /item replace ... from reads the same modifier by name.
	c.ChatCommand("item replace entity @s weapon.mainhand with minecraft:stone 1")
	if _, chats := servertest.AwaitSlotUpdates(t, c, 1); len(chats) == 0 {
		c.ExpectPlay(protocol.IDPlaySystemChat)
	}
	c.ChatCommand("item replace entity @s hotbar.1 from entity @s weapon.mainhand pk:five")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if len(chats) == 0 {
		c.ExpectPlay(protocol.IDPlaySystemChat)
	}
	if got := updates[0].Stack.Count; got != 5 {
		t.Errorf("item replace from through pk:five left %d, want 5", got)
	}

	c.ChatCommand("item modify entity @s weapon.mainhand pk:refused")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got,
		servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", "pk:refused", "minecraft:item_modifier")) {
		t.Errorf("a modifier that does not decode answered % X, want the unknown modifier refusal", got)
	}
	c.ExpectPlay(protocol.IDPlaySystemChat) // the refusal's "here" context line

	c.ChatCommand("item modify entity @s weapon.mainhand pk:broken")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got,
		servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", "pk:broken", "minecraft:item_modifier")) {
		t.Errorf("a modifier that does not decode answered % X, want the unknown modifier refusal", got)
	}

	writeFile(t, single, `{"function":"minecraft:set_count","count":9}`)
	c.ChatCommand("reload")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.reload.success")
	modify("pk:five", 9)
}

// TestPackPredicatesSeeThePositionAndInlineForms checks that a location_check
// predicate reads the world at the execute position and at the selected
// entity, and that execute if predicate accepts an inline condition.
// vanilla: ExecuteCommand.checkCustomPredicate, EntitySelectorOptions' predicate option
func TestPackPredicatesSeeThePositionAndInlineForms(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk")
	writeFile(t, filepath.Join(data, "predicate", "onstone.json"),
		`{"condition":"minecraft:location_check","predicate":{"block":{"blocks":"minecraft:stone"}}}`)
	_, c := joinPacks(t, cfg)

	says := func(command string, want bool) {
		t.Helper()
		c.ChatCommand(command)
		if want {
			if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
				t.Errorf("%q said % X, want the tail's say", command, got)
			}
			return
		}
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
			t.Errorf("%q answered % X, want the conditional failure", command, got)
		}
	}
	c.ChatCommand("setblock 0 80 0 minecraft:stone")
	servertest.FeedbackComponent(t, c)
	says("execute positioned 0 80 0 if predicate pk:onstone run say hi", true)
	says("execute positioned 8 80 8 if predicate pk:onstone", false)

	c.ChatCommand("summon minecraft:pig 0.5 80 0.5 {NoAI:1b,NoGravity:1b,Invulnerable:1b}")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("summon minecraft:pig 8.5 80 8.5 {NoAI:1b,NoGravity:1b}")
	servertest.FeedbackComponent(t, c)
	says("execute if entity @e[type=pig,predicate=pk:onstone,x=0,y=80,z=0,distance=..2] run say hi", true)
	says("execute if entity @e[type=pig,predicate=pk:onstone,x=8,y=80,z=8,distance=..2]", false)

	says(`execute if predicate {condition:"minecraft:random_chance",chance:1} run say hi`, true)
	says(`execute if predicate {condition:"minecraft:random_chance",chance:0}`, false)
	says(`execute if predicate {condition:"minecraft:random_chance",chance:1b} run say hi`, true)
	says(`execute if predicate {condition:"minecraft:random_chance",chance:0b}`, false)
	says(`execute if predicate {condition:"minecraft:inverted",term:{condition:"minecraft:random_chance",chance:0b}} run say hi`, true)
	says(`execute if predicate {condition:"minecraft:entity_properties",entity:"this",predicate:{"minecraft:flags":{is_baby:0b}}} run say hi`, true)
}

// TestPackPredicateReadsTheSelectedEntitysDimension checks that a selector's
// predicate option reads the world of the entity's own dimension, inverted
// or not.
// vanilla: EntitySelectorOptions' predicate option
func TestPackPredicateReadsTheSelectedEntitysDimension(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	cfg.GameMode = "creative"
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk")
	writeFile(t, filepath.Join(data, "predicate", "onstone.json"),
		`{"condition":"minecraft:location_check","predicate":{"block":{"blocks":"minecraft:stone"}}}`)
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate", "Boltholer"))

	bolt := connect().Join("Boltholer")
	srv.MovePlayerToDimension("Boltholer", world.NetherName, 80.5, 40, -16.5)
	servertest.AwaitViewSent(t, bolt)
	servertest.UntilTicks(t, srv, 400, "condition", func() bool { return srv.PlayerDimension("Boltholer") == world.NetherName })
	servertest.UntilTicks(t, srv, 400, "condition", func() bool { return srv.ChunkLoadedInDimension(world.NetherName, 80, -30) })

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	srv.SetBlockInDimension(world.NetherName, 80, 40, -30, stone)
	c := connect().Join("Testificate")
	c.ChatCommand("execute in minecraft:the_nether run summon minecraft:pig 80.5 40 -29.5 {NoAI:1b,NoGravity:1b,Invulnerable:1b}")
	servertest.FeedbackComponent(t, c)

	for sel, want := range map[string]bool{
		"@e[type=pig,predicate=pk:onstone]":  true,
		"@e[type=pig,predicate=!pk:onstone]": false,
	} {
		if want {
			c.ChatCommand("execute if entity " + sel + " run say hi")
			if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
				t.Errorf("%q said % X, want the tail's say", sel, got)
			}
			continue
		}
		c.ChatCommand("execute if entity " + sel)
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
			t.Errorf("%q answered % X, want the conditional failure", sel, got)
		}
	}
}

// TestPackLootContextProviders checks that a pack table's set_count takes a
// 26.3 context int provider: add sums its inputs, weighted_list picks one of
// its values, and a name resolves against the pack's own context_int_provider
// entries, which load before the tables. The pack layers are process-wide, so
// this test is not parallel and uses names no other test does.
// vanilla: ints.Sum, ints.WeightedListValue, ContextIntProviders.CODEC
func TestPackLootContextProviders(t *testing.T) {
	t.Cleanup(func() {
		registry.ApplyPackLoot(nil, nil, nil)
		registry.ApplyPackContextProviders(nil, nil)
	})
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk")
	table := func(item, count string) string {
		return `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"` + item + `",` +
			`"functions":[{"function":"minecraft:set_count","count":` + count + `}]}]}]}`
	}
	writeFile(t, filepath.Join(data, "loot_table", "add.json"), table("minecraft:emerald", `{"type":"minecraft:add","inputs":[2,3]}`))
	writeFile(t, filepath.Join(data, "loot_table", "weighted.json"),
		table("minecraft:diamond", `{"type":"minecraft:weighted_list","distribution":[{"data":{"type":"minecraft:sub","left":9,"right":2},"weight":3}]}`))
	writeFile(t, filepath.Join(data, "loot_table", "named.json"), table("minecraft:gold_ingot", `"pk:loot_six"`))
	writeFile(t, filepath.Join(data, "context_int_provider", "loot_six.json"), `{"type":"minecraft:mul","inputs":[2,3]}`)
	_, c := joinPacks(t, cfg)

	expectGave(t, c, "loot give @s loot pk:add", "minecraft:emerald", 5)
	expectGave(t, c, "loot give @s loot pk:weighted", "minecraft:diamond", 7)
	expectGave(t, c, "loot give @s loot pk:named", "minecraft:gold_ingot", 6)
}

// TestPackLootChanceAndLimitTakeProviders checks that a pack table's
// random_chance reads its chance as a context float provider and limit_count
// its bounds as context int providers: a chance summing to 1 always passes,
// and an add bound clamps the count to its sum from either side. The pack
// layers are process-wide, so this test is not parallel and uses names no
// other test does.
// vanilla: LootItemRandomChanceCondition, IntLimit
func TestPackLootChanceAndLimitTakeProviders(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pl")
	table := func(item string, count int, function string) string {
		return `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"` + item + `",` +
			`"functions":[{"function":"minecraft:set_count","count":` + strconv.Itoa(count) + `},` + function + `]}]}]}`
	}
	writeFile(t, filepath.Join(data, "loot_table", "chance.json"), table("minecraft:lapis_lazuli", 3,
		`{"function":"minecraft:set_count","count":1,"add":true,"conditions":[{"condition":"minecraft:random_chance","chance":{"type":"minecraft:add","inputs":[0.5,0.5]}}]}`))
	writeFile(t, filepath.Join(data, "loot_table", "max.json"), table("minecraft:redstone", 64,
		`{"function":"minecraft:limit_count","limit":{"max":{"type":"minecraft:add","inputs":[2,3]}}}`))
	writeFile(t, filepath.Join(data, "loot_table", "min.json"), table("minecraft:quartz", 1,
		`{"function":"minecraft:limit_count","limit":{"min":{"type":"minecraft:add","inputs":[3,4]},"max":20}}`))
	_, c := joinPacks(t, cfg)

	expectGave(t, c, "loot give @s loot pl:chance", "minecraft:lapis_lazuli", 4)
	expectGave(t, c, "loot give @s loot pl:max", "minecraft:redstone", 5)
	expectGave(t, c, "loot give @s loot pl:min", "minecraft:quartz", 7)
}
