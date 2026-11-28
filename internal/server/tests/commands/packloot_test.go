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

func TestPackLootUsesOnlyTheOuterSequence(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	cfg.LevelSeed = "12345"
	dir := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "loot_table")
	const inner = `{"random_sequence":"pk:inner","pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:gold_ingot",
		"modifier":[{"type":"minecraft:set_count","count":{"type":"minecraft:uniform","min":1,"max":20}}]}]}]}`
	const reference = `"pools":[{"rolls":1,"entries":[{"type":"minecraft:loot_table","value":"pk:inner"}]}]`
	writeFile(t, filepath.Join(dir, "inner.json"), inner)
	writeFile(t, filepath.Join(dir, "outer.json"), `{"random_sequence":"shared",`+reference+`}`)
	writeFile(t, filepath.Join(dir, "unsequenced.json"), "{"+reference+"}")
	_, c := joinPacks(t, cfg)

	resetAll := func(count int64) {
		c.ChatCommand("random reset *")
		servertest.ExpectFeedbackSoon(t, c, nil, "commands.random.reset.all.success", servertest.ItemNumber(count))
	}
	seq := world.NewRandomSequence(12345, "minecraft:shared")
	expectGave(t, c, "loot give @s loot pk:outer", "minecraft:gold_ingot", seq.NextIntBetween(1, 20))
	c.ChatCommand("random value 0..65535 shared")
	if got, want := expectSample(t, c), int64(seq.NextIntBetween(0, 65535)); got != want {
		t.Errorf("draw after loot = %d, want %d from the same sequence", got, want)
	}
	resetAll(1)

	for _, table := range []string{"pk:unsequenced", "{" + reference + "}"} {
		if got := giveStack(t, c, "loot give @s loot "+table); got.ItemID != servertest.MustItem("minecraft:gold_ingot") {
			t.Errorf("%s gave %+v, want gold ingots", table, got)
		}
	}
	resetAll(0)

	// Vanilla 26.3 on seed 12345 rolls four stacks, then draws 863 from the same sequence.
	c.ChatCommand(`loot give @s loot {random_sequence:"minecraft:inline_test",pools:[{rolls:{type:"minecraft:uniform",min:1,max:20},entries:[{type:"minecraft:item",name:"minecraft:gold_ingot"}]}]}`)
	_, chats := servertest.AwaitSlotUpdates(t, c, 4)
	servertest.ExpectFeedbackSoon(t, c, chats, "commands.drop.success.multiple", servertest.ItemNumber(4))
	c.ChatCommand("random value 0..1000 minecraft:inline_test")
	if got := expectSample(t, c); got != 863 {
		t.Errorf("draw after inline loot = %d, want 863 from minecraft:inline_test", got)
	}
}

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
// a table of its own, and that /reload reads a changed or removed file. The pack
// layer is process-wide, so the tests that load loot are not parallel with
// each other and roll blocks no other test of the package rolls.
// vanilla: ReloadableServerRegistries, LootDataType.TABLES
func TestPackLootTablesOverrideAndReload(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data")
	block := filepath.Join(data, "minecraft", "loot_table", "blocks", "netherite_block.json")
	writeFile(t, block, itemLootTable("minecraft:emerald", 2))
	writeFile(t, filepath.Join(data, "minecraft", "loot_table", "gameplay", "fishing.json"),
		itemLootTable("minecraft:stick", 3))
	writeFile(t, filepath.Join(data, "pk", "loot_table", "custom.json"), itemLootTable("minecraft:gold_ingot", 4))
	_, c := joinPacks(t, cfg)

	c.ChatCommand("setblock 0 80 0 minecraft:netherite_block")
	servertest.FeedbackComponent(t, c)
	const mine = "loot give @s mine 0 80 0"
	expectGave(t, c, mine, "minecraft:emerald", 2)
	expectGave(t, c, "loot give @s loot pk:custom", "minecraft:gold_ingot", 4)
	expectGave(t, c, "loot give @s fish minecraft:gameplay/fishing 0 80 0 minecraft:fishing_rod", "minecraft:stick", 3)

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
	writeFile(t, predicate, `{"type":"minecraft:random_chance","chance":1}`)
	writeFile(t, filepath.Join(data, "predicate", "never.json"), `[{"type":"minecraft:random_chance","chance":0}]`)
	writeFile(t, filepath.Join(data, "loot_table", "gated.json"),
		`{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:clay_ball",`+
			`"condition":{"type":"minecraft:reference","name":"pk:gate"}}]}]}`)
	// A holder set names a single holder without a list.
	// vanilla: HolderSetCodec.directCodec
	writeFile(t, filepath.Join(data, "predicate", "always.json"), `{"type":"minecraft:random_chance","chance":1}`)
	writeFile(t, filepath.Join(data, "loot_table", "single_term.json"),
		`{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:brick",`+
			`"condition":{"type":"minecraft:all_of","terms":"pk:always"}}]}]}`)
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

	writeFile(t, predicate, `{"type":"minecraft:random_chance","chance":0}`)
	c.ChatCommand("reload")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.reload.success")
	c.ChatCommand("execute if predicate pk:gate")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the reloaded predicate answered % X, want the conditional failure", got)
	}
}

// vanilla: ReloadableServerResources.loadResources, ServerFunctionLibrary.reload
func TestPackFunctionsResolveNewPredicates(t *testing.T) {
	for _, phase := range []string{"startup", "reload"} {
		t.Run(phase, func(t *testing.T) {
			t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
			cfg := packWorld(t)
			data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk")
			predicate := filepath.Join(data, "predicate", "function_gate.json")
			install := func() {
				writeFile(t, predicate, `{"type":"minecraft:random_chance","chance":1}`)
				writeFile(t, filepath.Join(data, "function", "check_gate.mcfunction"),
					"execute if predicate pk:function_gate run return 7\n")
			}
			if phase == "startup" {
				install()
			}
			_, c := joinPacks(t, cfg)
			if phase == "reload" {
				install()
				c.ChatCommand("reload")
				c.ExpectCommandFeedback("commands.reload.success")
			}
			const id = "pk:check_gate"
			c.ChatCommand("function " + id)
			if feedback := servertest.FeedbackComponent(t, c); feedback.Translate != "commands.function.scheduled.single" {
				t.Fatalf("/function %s answered %q, want the function to run", id, feedback.Plain())
			}
			expectFunctionResult(c, id, 7)

			if phase == "reload" {
				if err := os.Remove(predicate); err != nil {
					t.Fatal(err)
				}
				c.ChatCommand("reload")
				c.ExpectCommandFeedback("commands.reload.success")
				expectFunctionReturns(t, c, id, 0)
			}
		})
	}
}

// TestPackItemModifiersRunFromItemModify checks that /item modify runs a data
// pack's item modifier by name, as one function or a list, and inside an
// inline list or a sequence's single holder, and follows /reload.
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
	writeFile(t, filepath.Join(data, "named.json"), `{"function":"minecraft:copy_name","source":"block_entity"}`)
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

	writeFile(t, single, `{"function":"minecraft:set_count","count":9}`)
	c.ChatCommand("reload")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.reload.success")
	modify("pk:five", 9)
}

// TestPackLootIgnoresLegacyPluralConditions checks that a data pack loot table
// whose pool, entry or function carries only the legacy plural "conditions"
// field rolls as if it had no condition, while the singular "condition" field
// still gates it.
// vanilla: LootPool.CODEC, LootPoolEntryContainer.commonFields, LootItemConditionalFunction.commonFields
func TestPackLootIgnoresLegacyPluralConditions(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	dir := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "loot_table")
	const never = `{"type":"minecraft:random_chance","chance":0}`
	writeFile(t, filepath.Join(dir, "pool_legacy.json"),
		`{"pools":[{"rolls":1,"conditions":[`+never+`],"entries":[{"type":"minecraft:item","name":"minecraft:diamond"}]}]}`)
	writeFile(t, filepath.Join(dir, "entry_legacy.json"),
		`{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:emerald","conditions":[`+never+`]}]}]}`)
	writeFile(t, filepath.Join(dir, "func_legacy.json"),
		`{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:gold_ingot","modifier":{"type":"minecraft:set_count","count":3,"conditions":[`+never+`]}}]}]}`)
	writeFile(t, filepath.Join(dir, "pool_singular.json"),
		`{"pools":[{"rolls":1,"condition":`+never+`,"entries":[{"type":"minecraft:item","name":"minecraft:iron_ingot"}]}]}`)
	_, c := joinPacks(t, cfg)

	expectGave(t, c, "loot give @s loot pk:pool_legacy", "minecraft:diamond", 1)
	expectGave(t, c, "loot give @s loot pk:entry_legacy", "minecraft:emerald", 1)
	expectGave(t, c, "loot give @s loot pk:func_legacy", "minecraft:gold_ingot", 3)

	c.ChatCommand("loot give @s loot pk:pool_singular")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.drop.success.multiple", servertest.ItemNumber(0))
}
