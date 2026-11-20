package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestInlineLocationPredicateReadsEveryPart checks that /execute if predicate
// answers a location predicate's position, dimension, light, sky, fluid,
// smoke, structure and block nbt parts, and that an empty one
// matches a loaded position.
// vanilla: LocationPredicate.matches
func TestInlineLocationPredicateReadsEveryPart(t *testing.T) {
	cfg := packWorld(t)
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)

	state := func(name string, props map[string]string) uint16 {
		t.Helper()
		s, ok := registry.FindState(name, props)
		if !ok {
			t.Fatalf("no state %s %v", name, props)
		}
		return s
	}
	// Everything is built high above the terrain.
	const y = 290
	srv.SetBlock(20, y, 20, state("minecraft:glowstone", nil))
	srv.SetBlock(20, y, 24, state("minecraft:campfire", map[string]string{"lit": "true"}))
	srv.SetBlock(20, y+10, 28, state("minecraft:stone", nil))
	srv.SetBlock(20, y, 32, state("minecraft:water", nil))
	srv.SetBlock(21, y, 32, state("minecraft:water", map[string]string{"level": "1"}))
	srv.SetBlock(20, y, 36, state("minecraft:chest", nil))
	srv.SetBlockEntityTagAt(20, y, 36, "minecraft:chest", nbt.Compound{"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"Slot": nbt.Byte(0), "id": nbt.String("minecraft:stick"), "count": nbt.Int(3)},
	}}})
	srv.SetBlock(28, y+10, 28, state("minecraft:glass", nil))
	srv.RelightAt(20, 20)

	says := func(at string, predicate string, want bool) {
		t.Helper()
		command := "execute positioned " + at + " if predicate {condition:\"minecraft:location_check\",predicate:{" + predicate + "}}"
		if want {
			command += " run say hi"
		}
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

	says("20.5 150 20.5", "", true)
	says("20.5 150 20.5", `dimension:"minecraft:overworld"`, true)
	says("20.5 150 20.5", `dimension:"minecraft:the_nether"`, false)
	says("20.5 150 20.5", `position:{y:{min:100}}`, true)
	says("20.5 50 20.5", `position:{y:{min:100}}`, false)
	says("20.5 50 20.5", `position:{x:{min:20,max:21},y:{max:60}}`, true)

	// A glowstone cell holds block light 15; the sealed stone block holds none.
	says("20.5 290.5 20.5", `light:{light:{min:15}}`, true)
	says("20.5 300.5 28.5", `light:{light:{max:7}}`, true)
	says("20.5 290.5 20.5", `light:{light:{max:7}}`, false)

	says("20.5 295.5 28.5", `can_see_sky:false`, true)
	says("20.5 305.5 28.5", `can_see_sky:true`, true)
	says("20.5 295.5 28.5", `can_see_sky:true`, false)
	says("24.5 295.5 28.5", `can_see_sky:true`, true)
	// Glass lets sky light through, so the cell under it sees the sky.
	says("28.5 295.5 28.5", `can_see_sky:true`, true)

	// A dimension without sky light never does, even above its roof.
	c.ChatCommand("execute in minecraft:the_nether run setblock 0 200 0 minecraft:stone")
	c.ExpectPlay(protocol.IDPlaySystemChat)
	c.ChatCommand("execute in minecraft:the_nether positioned 0.5 201 0.5 unless predicate {condition:\"minecraft:location_check\",predicate:{can_see_sky:true}} run say hi")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("nether can_see_sky said % X, want the tail's say", got)
	}

	says("20.5 291.5 24.5", `smokey:true`, true)
	says("22.5 291.5 24.5", `smokey:true`, false)
	says("22.5 291.5 24.5", `smokey:false`, true)

	says("20.5 290.5 32.5", `fluid:{fluids:"minecraft:water"}`, true)
	says("20.5 290.5 32.5", `fluid:{fluids:"minecraft:lava"}`, false)
	says("20.5 290.5 32.5", `fluid:{fluids:"#minecraft:water"}`, true)
	says("20.5 292.5 32.5", `fluid:{fluids:"minecraft:water"}`, false)
	says("21.5 290.5 32.5", `fluid:{fluids:"minecraft:flowing_water",state:{level:"7"}}`, true)
	says("21.5 290.5 32.5", `fluid:{fluids:"minecraft:water"}`, false)

	says("20.5 290.5 36.5", `block:{blocks:"minecraft:chest",nbt:"{Items:[{id:\"minecraft:stick\"}]}"}`, true)
	says("20.5 290.5 36.5", `block:{nbt:"{Items:[{id:\"minecraft:stone\"}]}"}`, false)
	says("20.5 290.5 32.5", `block:{nbt:"{Items:[{id:\"minecraft:stick\"}]}"}`, false)

	// A position whose chunk is not loaded fails the parts that read it.
	says("200000.5 150 20.5", `light:{}`, false)
	says("200000.5 150 20.5", `dimension:"minecraft:overworld"`, true)

	// The usual "player is in the End" predicate, as an entity predicate.
	c.ChatCommand(`execute if predicate {condition:"minecraft:entity_properties",entity:"this",predicate:{"minecraft:location":{dimension:"minecraft:overworld"}}} run say hi`)
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("entity location dimension said % X, want the tail's say", got)
	}
	c.Disconnect()
}
