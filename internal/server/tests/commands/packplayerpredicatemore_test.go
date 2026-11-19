package commands

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestPackPlayerPredicateReadsStatsAdvancementsAndLookingAt checks that the
// stats, advancements and looking_at tests of type_specific/player answer from
// the play-time counter, an advancement grant and the entity on the view ray.
// vanilla: PlayerPredicate
func TestPackPlayerPredicateReadsStatsAdvancementsAndLookingAt(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	root := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk")
	data := filepath.Join(root, "predicate")
	entity := func(predicate string) string {
		return `{"condition":"minecraft:entity_properties","entity":"this","predicate":{` + predicate + `}}`
	}
	writeFile(t, filepath.Join(data, "played.json"),
		entity(`"minecraft:type_specific/player":{"stats":[{"type":"minecraft:custom","stat":"minecraft:play_time","value":{"min":1000}}]}`))
	writeFile(t, filepath.Join(data, "has_adv.json"),
		entity(`"minecraft:type_specific/player":{"advancements":{"pk:adv":true}}`))
	writeFile(t, filepath.Join(data, "has_crit.json"),
		entity(`"minecraft:type_specific/player":{"advancements":{"pk:adv":{"c":true}}}`))
	writeFile(t, filepath.Join(data, "sees_pig.json"),
		entity(`"minecraft:type_specific/player":{"looking_at":{"minecraft:entity_type":"minecraft:pig"}}`))
	writeFile(t, filepath.Join(root, "advancement", "adv.json"),
		`{"criteria":{"c":{"trigger":"minecraft:impossible"}}}`)
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)

	says := func(name string, want bool) {
		t.Helper()
		command := "execute if entity @s[predicate=pk:" + name + "]"
		if want {
			c.ChatCommand(command + " run say hi")
			if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
				t.Errorf("%s said % X, want the say", name, got)
			}
			return
		}
		c.ChatCommand(command)
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
			t.Errorf("%s answered % X, want the conditional failure", name, got)
		}
	}
	run := func(command string) {
		t.Helper()
		c.ChatCommand(command)
		c.Settle()
	}

	for _, name := range []string{"played", "has_adv", "has_crit", "sees_pig"} {
		says(name, false)
	}
	servertest.RunTicks(t, srv, 1000)
	says("played", true)
	run("advancement grant @s only pk:adv")
	servertest.RunTicks(t, srv, 1)
	says("has_adv", true)
	says("has_crit", true)

	// The player looks along -x ...  when a pig stands in front.
	run("tp @s 0.5 64 0.5 -90 20")
	run(`summon minecraft:pig 3.5 64 0.5 {NoAI:1b}`)
	servertest.RunTicks(t, srv, 1)
	says("sees_pig", true)
	run("tp @s 0.5 64 0.5 90 20")
	servertest.RunTicks(t, srv, 1)
	says("sees_pig", false)
}

// TestPackFishingHookPredicateReadsOpenWater checks that type_specific/fishing_hook
// answers from the bobber's open-water flag, which starts true in flight.
// vanilla: FishingHookPredicate, FishingHook.isOpenWaterFishing
func TestPackFishingHookPredicateReadsOpenWater(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "predicate")
	entity := func(open string) string {
		return `{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:type_specific/fishing_hook":{"in_open_water":` + open + `}}}`
	}
	writeFile(t, filepath.Join(data, "open.json"), entity("true"))
	writeFile(t, filepath.Join(data, "closed.json"), entity("false"))
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Testificate", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	servertest.UseHeld(c, 0, 0)
	servertest.RunTicks(t, srv, 2)
	c.Settle()

	c.ChatCommand("execute if entity @e[type=minecraft:fishing_bobber,predicate=pk:open] run say hi")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("open said % X, want the say", got)
	}
	c.ChatCommand("execute if entity @e[type=minecraft:fishing_bobber,predicate=pk:closed]")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("closed answered % X, want the conditional failure", got)
	}
}
