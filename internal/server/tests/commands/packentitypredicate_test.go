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
		return `{"condition":"minecraft:entity_properties","entity":"this","predicate":{` + predicate + `}}`
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

// TestPackEntityPredicatesReadPlaceDistanceMovementAndSlots checks that pack
// predicates on the block under the entity, the distance from the origin, its
// known movement and its slots load and answer /execute if predicate.
// vanilla: SteppingOnPredicate, DistanceToPlayerPredicate, MovementPredicate, EntitySlotsPredicate
func TestPackEntityPredicatesReadPlaceDistanceMovementAndSlots(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "predicate")
	entity := func(predicate string) string {
		return `{"condition":"minecraft:entity_properties","entity":"this","predicate":{` + predicate + `}}`
	}
	gold := `{"blocks":"minecraft:gold_block"}`
	writeFile(t, filepath.Join(data, "on_gold.json"), entity(`"minecraft:stepping_on":{"block":`+gold+`}`))
	writeFile(t, filepath.Join(data, "in_gold.json"), entity(`"minecraft:location":{"block":`+gold+`}`))
	writeFile(t, filepath.Join(data, "affected_gold.json"), entity(`"minecraft:movement_affected_by":{"block":`+gold+`}`))
	writeFile(t, filepath.Join(data, "near.json"), entity(`"minecraft:distance":{"horizontal":{"max":2}}`))
	writeFile(t, filepath.Join(data, "hotbar_diamond.json"), entity(`"minecraft:slots":{"hotbar.*":{"items":"minecraft:diamond"}}`))
	writeFile(t, filepath.Join(data, "falling.json"), entity(`"minecraft:movement":{"y":{"max":-5}}`))
	writeFile(t, filepath.Join(data, "fast_down.json"), entity(`"minecraft:movement":{"vertical_speed":{"min":5},"fall_distance":{"min":5}}`))
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	says := func(command string, want bool) {
		t.Helper()
		if !want {
			c.ChatCommand("execute " + command)
			if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
				t.Errorf("%s answered % X, want the conditional failure", command, got)
			}
			return
		}
		c.ChatCommand("execute " + command + " run say hi")
		if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
			t.Errorf("%s said % X, want the say", command, got)
		}
	}
	for _, name := range []string{"on_gold", "in_gold", "affected_gold", "hotbar_diamond", "falling", "fast_down"} {
		says("if predicate pk:"+name, false)
	}
	says("positioned 1.5 64 0.5 if predicate pk:near", true)
	says("positioned 10 64 10 if predicate pk:near", false)

	block, ok := registry.FindState("minecraft:gold_block", nil)
	if !ok {
		t.Fatal("the registry has no gold block")
	}
	if !srv.SetBlock(0, 63, 0, block) {
		t.Fatal("could not lay the gold block")
	}
	c.Settle()
	says("if predicate pk:on_gold", true)
	says("if predicate pk:affected_gold", true)
	says("if predicate pk:in_gold", false)

	c.ChatCommand("item replace entity @s hotbar.3 with minecraft:diamond")
	if _, chats := servertest.AwaitSlotUpdates(t, c, 1); len(chats) == 0 {
		c.ExpectPlay(protocol.IDPlaySystemChat)
	}
	says("if predicate pk:hotbar_diamond", true)

	c.MoveToGround(0.5, 84, 0.5, false)
	c.MoveToGround(0.5, 79, 0.5, false)
	c.Settle()
	says("if predicate pk:falling", true)
	says("if predicate pk:fast_down", true)
}
