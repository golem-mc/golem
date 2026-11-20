package commands

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestPackPlayerPredicateReadsGamemodeLevelRecipesAndInput checks that a pack
// predicate's type_specific/player answers /execute if predicate from the
// player's game mode, experience level, recipe book, food and last input, and
// that an exact components test reads a wolf's variant.
// vanilla: PlayerPredicate, EntityExactDataComponentsPredicate
func TestPackPlayerPredicateReadsGamemodeLevelRecipesAndInput(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "predicate")
	entity := func(predicate string) string {
		return `{"type":"minecraft:entity_properties","entity":"this","predicate":{` + predicate + `}}`
	}
	writeFile(t, filepath.Join(data, "creative.json"), entity(`"minecraft:type_specific/player":{"gamemode":["creative"]}`))
	writeFile(t, filepath.Join(data, "lvl5.json"), entity(`"minecraft:type_specific/player":{"level":{"min":5}}`))
	writeFile(t, filepath.Join(data, "knows_bread.json"), entity(`"minecraft:type_specific/player":{"recipes":{"minecraft:bread":true}}`))
	writeFile(t, filepath.Join(data, "jumping.json"), entity(`"minecraft:type_specific/player":{"input":{"jump":true,"sneak":false}}`))
	writeFile(t, filepath.Join(data, "starving.json"), entity(`"minecraft:type_specific/player":{"food":{"level":{"max":10}}}`))
	writeFile(t, filepath.Join(data, "ashen.json"), entity(`"minecraft:components":{"minecraft:wolf/variant":"minecraft:ashen"}`))
	writeFile(t, filepath.Join(data, "hook.json"), entity(`"minecraft:type_specific/fishing_hook":{"in_open_water":true}`))
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)

	says := func(name, as string, want bool) {
		t.Helper()
		selector := as + "[predicate=pk:" + name + "]"
		if before, ok := strings.CutSuffix(as, "]"); ok {
			selector = before + ",predicate=pk:" + name + "]"
		}
		command := "execute if entity " + selector
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

	// A fresh survival player: full food, level 0, no jump.
	for _, name := range []string{"creative", "lvl5", "knows_bread", "jumping", "starving", "hook"} {
		says(name, "@s", false)
	}
	run("gamemode creative")
	says("creative", "@s", true)
	run("xp set @s 5 levels")
	says("lvl5", "@s", true)
	run("recipe give @s minecraft:bread")
	servertest.RunTicks(t, srv, 1)
	says("knows_bread", "@s", true)
	c.SendInput(protocol.InputJump)
	servertest.RunTicks(t, srv, 2)
	says("jumping", "@s", true)
	c.SendInput(protocol.InputJump | protocol.InputSneak)
	servertest.RunTicks(t, srv, 2)
	says("jumping", "@s", false)

	run(`summon minecraft:wolf 4 64 4 {NoAI:1b,Tags:["ashen"],variant:"minecraft:ashen"}`)
	run(`summon minecraft:wolf 6 64 4 {NoAI:1b,Tags:["pale"],variant:"minecraft:pale"}`)
	says("ashen", "@e[tag=ashen]", true)
	says("ashen", "@e[tag=pale]", false)
	says("ashen", "@s", false)
}
