package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ValueInput.read, TagValueInput.read (resultOrPartial),
// BannerBlockEntity.loadAdditional, SkullBlockEntity.loadAdditional,
// TheEndGatewayBlockEntity.loadAdditional, JigsawBlockEntity.loadAdditional,
// BeehiveBlockEntity.loadAdditional, TrialSpawnerBlockEntity.loadAdditional,
// BrushableBlockEntity.loadAdditional, CrafterBlockEntity.loadAdditional,
// SignBlockEntity.loadAdditional, BaseContainerBlockEntity.loadAdditional
func TestCommandSetblockBlockEntityKeepsPartialCodecResult(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	freeze := func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }
	srv, c := servertest.Join(t, cfg, "Setter", servertest.OpsListing(t, "Setter"), freeze)
	servertest.HoldTicks(t, srv)

	const absent = "<absent>"
	for i, tc := range []struct{ block, path, want string }{
		{`white_banner{patterns:[{pattern:"minecraft:nope",color:"red"},{pattern:"minecraft:cross",color:"red"}]}`, "patterns", `[{color: "red", pattern: "minecraft:cross"}]`},
		{`white_banner{patterns:[{pattern:"minecraft:cross",color:"neon"}]}`, "patterns", absent},
		{`player_head{profile:{name:"abcdefghijklmnopqrstuvwxyz"}}`, "profile", absent},
		{`player_head{note_block_sound:"Not Valid"}`, "note_block_sound", absent},
		{`end_gateway{exit_portal:[I;1,2]}`, "exit_portal", absent},
		{`jigsaw{name:"bad name!",joint:"nope",target:"x y",pool:"A B"}`, "name", `"minecraft:empty"`},
		{`jigsaw{name:"bad name!",joint:"nope",target:"x y",pool:"A B"}`, "target", `"minecraft:empty"`},
		{`jigsaw{name:"bad name!",joint:"nope",target:"x y",pool:"A B"}`, "pool", `"minecraft:empty"`},
		{`jigsaw{name:"bad name!",joint:"nope"}`, "joint", `"aligned"`},
		{`jigsaw{name:"a:b"}`, "placement_priority", "0"},
		{`jigsaw{name:"a:b"}`, "selection_priority", "0"},
		{`beehive{bees:[{entity_data:{id:"minecraft:bee"},min_ticks_in_hive:600,ticks_in_hive:0},{entity_data:{},min_ticks_in_hive:5,ticks_in_hive:0}]}`, "bees", `[{ticks_in_hive: 0, entity_data: {id: "minecraft:bee"}, min_ticks_in_hive: 600}]`},
		{`trial_spawner{normal_config:{spawn_range:-5,total_mobs:-1}}`, "normal_config", absent},
		{`suspicious_sand{item:{id:"minecraft:air",count:1}}`, "item", absent},
		{`crafter{triggered:2}`, "triggered", "2"},
		{`oak_sign{front_text:{messages:["a","b","c","d","e"]}}`, "front_text.messages", `["a", "b", "c", "d"]`},
		{`oak_sign{front_text:{color:"neon",messages:["a","b","c","d"]}}`, "front_text.color", `"black"`},
		{`oak_sign{front_text:{color:"neon",messages:["a","b","c","d"]}}`, "front_text.messages", `["a", "b", "c", "d"]`},
		{`chest{CustomName:{text:"C",hover_event:{action:"show_text"}}}`, "CustomName", `"C"`},
		{`command_block{CustomName:{text:"C",hover_event:{action:"show_text"}}}`, "CustomName", `"C"`},
		{`enchanting_table{CustomName:{text:"C",hover_event:{action:"show_text"}}}`, "CustomName", `"C"`},
	} {
		t.Run(fmt.Sprintf("%d %s", i, tc.block), func(t *testing.T) {
			x := fmt.Sprint(2 * i)
			c.ChatCommand("setblock " + x + " 63 1 minecraft:" + tc.block)
			c.ExpectCommandFeedback("commands.setblock.success", x, "63", "1")
			got := absent
			for _, text := range srv.RunLine("data get block " + x + " 63 1 " + tc.path) {
				if text.Translate == "commands.data.block.query" && len(text.With) > 0 {
					got = text.With[len(text.With)-1].Plain()
				}
			}
			if got != tc.want {
				t.Errorf("%s %s = %s, want %s", tc.block, tc.path, got, tc.want)
			}
		})
	}
}
