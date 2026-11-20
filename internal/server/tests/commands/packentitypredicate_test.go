package commands

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestPackEntityPredicatesReadFlagsEquipmentTagsAndEffects checks that pack
// predicates on flags, equipment, entity tags and effects load and answer
// /execute if predicate from the player's state.
// vanilla: EntityFlagsPredicate, EntityEquipmentPredicate, EntityTagPredicate, EntityEffectsPredicate
func TestPackEntityPredicatesReadFlagsEquipmentTagsAndEffects(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "predicate")
	entity := func(predicate string) string {
		return `{"type":"minecraft:entity_properties","entity":"this","predicate":{` + predicate + `}}`
	}
	writeFile(t, filepath.Join(data, "sneaking.json"), entity(`"minecraft:flags":{"is_sneaking":true}`))
	writeFile(t, filepath.Join(data, "helmet.json"), entity(`"minecraft:equipment":{"head":{"items":"minecraft:iron_helmet"}}`))
	writeFile(t, filepath.Join(data, "tagged.json"), entity(`"minecraft:entity_tags":{"any_of":["x"]}`))
	writeFile(t, filepath.Join(data, "speedy.json"), entity(`"minecraft:effects":{"minecraft:speed":{"amplifier":1}}`))
	writeFile(t, filepath.Join(data, "named.json"), entity(`"minecraft:nbt":"{Tags:[\"x\"]}","minecraft:periodic_tick":1`))
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)

	says := func(name string, want bool) {
		t.Helper()
		if !want {
			c.ChatCommand("execute if predicate pk:" + name)
		} else {
			c.ChatCommand("execute if predicate pk:" + name + " run say hi")
		}
		if want {
			if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
				t.Errorf("%s said % X, want the say", name, got)
			}
			return
		}
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
			t.Errorf("%s answered % X, want the conditional failure", name, got)
		}
	}
	run := func(command string) {
		t.Helper()
		c.ChatCommand(command)
		servertest.FeedbackComponent(t, c)
	}

	for _, name := range []string{"sneaking", "helmet", "tagged", "speedy", "named"} {
		says(name, false)
	}
	c.SendSneak(true)
	servertest.RunTicks(t, srv, 2)
	says("sneaking", true)
	c.ChatCommand("item replace entity @s armor.head with minecraft:iron_helmet")
	if _, chats := servertest.AwaitSlotUpdates(t, c, 1); len(chats) == 0 {
		c.ExpectPlay(protocol.IDPlaySystemChat)
	}
	says("helmet", true)
	run("tag @s add x")
	says("tagged", true)
	says("named", true)
	c.ChatCommand("effect give @s minecraft:speed 10 1")
	c.ExpectPlay(protocol.IDPlayUpdateMobEffect)
	servertest.FeedbackComponent(t, c)
	says("speedy", true)
}
