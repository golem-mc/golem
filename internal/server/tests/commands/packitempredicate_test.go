package commands

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestInlineItemPredicatesReadCountComponentsAndPredicates checks that an item
// predicate in an entity predicate reads count, exact components and the
// partial component predicates, answering /execute if predicate from the stack
// the player holds.
// vanilla: ItemPredicate.CODEC, DataComponentMatchers.CODEC
func TestInlineItemPredicatesReadCountComponentsAndPredicates(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	hold := func(stack string) {
		t.Helper()
		c.ChatCommand("item replace entity @s weapon.mainhand with " + stack)
		if _, chats := servertest.AwaitSlotUpdates(t, c, 1); len(chats) == 0 {
			c.ExpectPlay(protocol.IDPlaySystemChat)
		}
	}
	answers := func(item string, want bool) {
		t.Helper()
		const head = `execute if predicate {condition:"minecraft:entity_properties",entity:"this",predicate:{"minecraft:equipment":{mainhand:`
		command := head + item + `}}}`
		if want {
			command += " run say hi"
		}
		c.ChatCommand(command)
		if want {
			if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
				t.Errorf("%s said % X, want the say", item, got)
			}
			return
		}
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
			t.Errorf("%s answered % X, want the conditional failure", item, got)
		}
	}

	custom := `{items:"minecraft:stick",predicates:{"minecraft:custom_data":"{a:1b}"}}`
	hold("minecraft:stick[custom_data={a:1b}]")
	answers(custom, true)
	hold("minecraft:stick")
	answers(custom, false)

	count := `{items:"minecraft:stick",count:{min:2}}`
	hold("minecraft:stick 2")
	answers(count, true)
	hold("minecraft:stick")
	answers(count, false)

	exact := `{items:"minecraft:stick",components:{"minecraft:custom_data":{a:1}}}`
	hold("minecraft:stick[custom_data={a:1}]")
	answers(exact, true)
	hold("minecraft:stick[custom_data={a:2}]")
	answers(exact, false)

	wear := `{predicates:{"minecraft:damage":{damage:{min:5}}}}`
	hold("minecraft:iron_pickaxe[damage=10]")
	answers(wear, true)
	hold("minecraft:iron_pickaxe")
	answers(wear, false)

	durability := `{predicates:{"minecraft:damage":{durability:{min:5}}}}`
	hold("minecraft:iron_pickaxe")
	answers(durability, true)
	hold("minecraft:iron_pickaxe[damage=248]")
	answers(durability, false)

	potion := `{predicates:{"minecraft:potion_contents":{potions:"minecraft:swiftness"}}}`
	hold(`minecraft:potion[potion_contents={potion:"minecraft:swiftness"}]`)
	answers(potion, true)
	hold(`minecraft:potion[potion_contents={potion:"minecraft:healing"}]`)
	answers(potion, false)
}

// TestPackMatchToolReadsCustomData checks that a data pack's block loot table
// whose match_tool asks for a custom_data predicate drops its item only for
// that tool.
// vanilla: MatchTool, ItemPredicate
func TestPackMatchToolReadsCustomData(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table", "blocks", "netherite_block.json"),
		`{"pools":[{"rolls":1,"entries":[{"type":"minecraft:alternatives","children":[`+
			`{"type":"minecraft:item","name":"minecraft:emerald","conditions":[{"condition":"minecraft:match_tool",`+
			`"predicate":{"items":"minecraft:stick","predicates":{"minecraft:custom_data":"{a:1b}"}}}]},`+
			`{"type":"minecraft:item","name":"minecraft:dirt"}]}]}]}`)
	_, c := joinPacks(t, cfg)

	c.ChatCommand("setblock 0 80 0 minecraft:netherite_block")
	servertest.FeedbackComponent(t, c)
	hold := func(stack string) {
		t.Helper()
		c.ChatCommand("item replace entity @s weapon.mainhand with " + stack)
		if _, chats := servertest.AwaitSlotUpdates(t, c, 1); len(chats) == 0 {
			c.ExpectPlay(protocol.IDPlaySystemChat)
		}
	}
	const mine = "loot give @s mine 0 80 0 mainhand"
	hold("minecraft:stick[custom_data={a:1b}]")
	expectGave(t, c, mine, "minecraft:emerald", 1)
	hold("minecraft:stick")
	expectGave(t, c, mine, "minecraft:dirt", 1)
	// The second dirt stacks onto the first.
	hold("minecraft:stick[custom_data={a:2b}]")
	expectGave(t, c, mine, "minecraft:dirt", 2)
}
