package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestCommandItemComponentCodecs(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	const replace = "item replace entity @s weapon.mainhand with "
	for _, tc := range []struct {
		name, item, component, saved string
	}{
		{"lodestone dimension", `compass[lodestone_tracker={target:{pos:[1,2,3],dimension:"overworld"},tracked:false}]`,
			"lodestone_tracker", `{tracked: 0b, target: {pos: [I; 1, 2, 3], dimension: "minecraft:overworld"}}`},
		{"single block", `stone[can_place_on={blocks:"dirt"}]`, "can_place_on", `{blocks: "minecraft:dirt"}`},
		{"singleton list", `stone[can_place_on={blocks:["dirt"]}]`, "can_place_on", `{blocks: "minecraft:dirt"}`},
		{"multiple blocks", `stone[can_break={blocks:["dirt","stone"]}]`, "can_break", `{blocks: ["minecraft:dirt", "minecraft:stone"]}`},
		{"block tag", `stone[can_break={blocks:"#logs"}]`, "can_break", `{blocks: "#minecraft:logs"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.ChatCommand(replace + tc.item)
			c.ExpectWithinTicks(srv, 3, protocol.IDPlaySystemChat)
			c.ChatCommand(`data get entity @s SelectedItem.components."minecraft:` + tc.component + `"`)
			r := protocol.NewReader(c.ExpectWithinTicks(srv, 3, protocol.IDPlaySystemChat))
			line, ok := protocol.ReadText(r)
			if !ok {
				t.Fatalf("decode feedback: %v", r.Err())
			}
			if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
				t.Fatalf("data get feedback = %s", line.Plain())
			}
			if got := line.With[1].Plain(); got != tc.saved {
				t.Errorf("saved %s = %s, want %s", tc.component, got, tc.saved)
			}
		})
	}

	for _, tc := range []struct {
		item, component, value, refusal string
	}{
		{"stone", "max_stack_size", "100", "Value must be within range [1;99]: 100"},
		{"leather_helmet", "dyed_color", `{rgb:16711680}`, "Failed to parse either. First: Not a number; Second: Not a list: {rgb:16711680}"},
		{"spawner", "block_entity_data", `{SpawnData:{entity:{id:"zombie"}}}`, `Expected 'id' field in {SpawnData:{entity:{id:"zombie"}}}`},
		{"armor_stand", "entity_data", `{Small:1b}`, `Expected 'id' field in {Small:1b}`},
	} {
		t.Run(tc.component, func(t *testing.T) {
			for _, prefix := range []string{replace, "give @s "} {
				command := prefix + tc.item + "[" + tc.component + "=" + tc.value + "]"
				c.ChatCommand(command)
				got := c.ExpectWithinTicks(srv, 3, protocol.IDPlaySystemChat)
				want := servertest.CommandErrorMessageForTest("arguments.item.component.malformed", "minecraft:"+tc.component, tc.refusal)
				if !bytes.Equal(got, want) {
					line, _ := protocol.ReadText(protocol.NewReader(got))
					t.Errorf("%s: refusal = %s, want %s", command, line.Plain(), tc.refusal)
				}
				context := c.ExpectWithinTicks(srv, 3, protocol.IDPlaySystemChat)
				if !bytes.Equal(context, servertest.CommandErrorContextForTest(command, strings.Index(command, tc.value))) {
					t.Errorf("%s: wrong refusal context", command)
				}
			}
		})
	}
}
