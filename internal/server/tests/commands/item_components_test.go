package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestCommandTypedItemComponentFeedback(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct {
		name, item, saved string
	}{
		{"int array colours", `stick[custom_model_data={colors:[I;255]}]`,
			`{components: {"minecraft:custom_model_data": {colors: [255]}}, count: 1, id: "minecraft:stick"}`},
		{"numeric arrays and RGB vector", `stick[custom_model_data={floats:[B;-1b,2b],flags:[L;0L,1L,256L],colors:[[I;1,0,0],255]}]`,
			`{components: {"minecraft:custom_model_data": {floats: [-1.0f, 2.0f], flags: [0b, 1b, 1b], colors: [-65536, 255]}}, count: 1, id: "minecraft:stick"}`},
		{"short decoration type", `map[map_decorations={a:{type:"mansion",x:1,z:2,rotation:0}}]`,
			`{components: {"minecraft:map_decorations": {a: {rotation: 0.0f, x: 1.0d, z: 2.0d, type: "minecraft:mansion"}}}, count: 1, id: "minecraft:map"}`},
		{"blocking delay", `stick[blocks_attacks={block_delay_seconds:1}]`,
			`{components: {"minecraft:blocks_attacks": {block_delay_seconds: 1.0f}}, count: 1, id: "minecraft:stick"}`},
		{"explicit blocking defaults", `stick[blocks_attacks={block_delay_seconds:0,disable_cooldown_scale:1,damage_reductions:[{horizontal_blocking_angle:90,base:0,factor:1}],item_damage:{threshold:1,base:0,factor:1}}]`,
			`{components: {"minecraft:blocks_attacks": {}}, count: 1, id: "minecraft:stick"}`},
		{"altered damage reduction", `stick[blocks_attacks={damage_reductions:[{horizontal_blocking_angle:90,base:0,factor:0.5}],item_damage:{threshold:1,base:0,factor:0.5}}]`,
			`{components: {"minecraft:blocks_attacks": {damage_reductions: [{factor: 0.5f, base: 0.0f}], item_damage: {threshold: 1.0f, factor: 0.5f, base: 0.0f}}}, count: 1, id: "minecraft:stick"}`},
		{"empty damage reductions", `stick[blocks_attacks={damage_reductions:[]}]`,
			`{components: {"minecraft:blocks_attacks": {damage_reductions: []}}, count: 1, id: "minecraft:stick"}`},
		{"attack reach", `stick[attack_range={min_reach:1,max_reach:4}]`,
			`{components: {"minecraft:attack_range": {min_reach: 1.0f, max_reach: 4.0f}}, count: 1, id: "minecraft:stick"}`},
		{"explicit attack range defaults", `stick[attack_range={min_reach:0,max_reach:3,max_creative_reach:5,hitbox_margin:0.3,mob_factor:1}]`,
			`{components: {"minecraft:attack_range": {}}, count: 1, id: "minecraft:stick"}`},
		{"default disc song", `music_disc_cat[jukebox_playable="minecraft:cat"]`,
			`{count: 1, id: "minecraft:music_disc_cat"}`},
		{"different disc song", `music_disc_cat[jukebox_playable="blocks"]`,
			`{components: {"minecraft:jukebox_playable": "minecraft:blocks"}, count: 1, id: "minecraft:music_disc_cat"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.ChatCommand("item replace entity @s weapon.mainhand with " + tc.item)
			reply := c.ExpectWithinTicks(srv, 3, protocol.IDPlaySystemChat)
			tag, _, err := nbt.DecodeNetwork(reply)
			if err != nil {
				t.Fatal(err)
			}
			feedback, _ := nbt.CompoundView(tag)
			if feedback["translate"] != nbt.String(itemEntitySetSingleKey) {
				// An argument error also queues a line with the command context.
				c.ExpectWithinTicks(srv, 3, protocol.IDPlaySystemChat)
				t.Fatalf("item replace feedback = %s", nbt.Stringify(tag))
			}
			c.ChatCommand("data get entity @s SelectedItem")
			r := protocol.NewReader(c.ExpectWithinTicks(srv, 3, protocol.IDPlaySystemChat))
			line, ok := protocol.ReadText(r)
			if !ok || line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
				t.Fatalf("data get feedback = %s", line.Plain())
			}
			if got := line.With[1].Plain(); got != tc.saved {
				t.Errorf("SelectedItem = %s, want %s", got, tc.saved)
			}
		})
	}
}

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
