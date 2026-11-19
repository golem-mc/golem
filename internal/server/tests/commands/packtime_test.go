package commands

import (
	"bytes"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// Not parallel: the pack registry table is process-wide.
// vanilla: TimeCommand.getDefaultClock, suggestTimeMarkers, suggestTimelines
func TestTimeUsesPackDimensionTypeClock(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	dir := filepath.Join(cfg.LevelName, "datapacks", "clocks")
	writeFile(t, filepath.Join(dir, "pack.mcmeta"), `{"pack":{"description":"clocks"}}`)
	for name, clock := range map[string]string{"day_type": `,"default_clock":"minecraft:overworld"`, "none_type": ""} {
		writeFile(t, filepath.Join(dir, "data/pk/dimension_type", name+".json"), `{
			"ambient_light":0,"coordinate_scale":1,"has_ceiling":false,"has_ender_dragon_fight":false,
			"has_skylight":true,"height":384,"infiniburn":"#minecraft:infiniburn_overworld",
			"logical_height":384,"min_y":-64,"monster_spawn_block_light_limit":0,"monster_spawn_light_level":0`+clock+`}`)
	}
	for name, typ := range map[string]string{
		"day": "pk:day_type", "none": "pk:none_type", "inherited": "minecraft:overworld",
		"caves": "minecraft:overworld_caves", "end": "minecraft:the_end",
	} {
		writeFile(t, filepath.Join(dir, "data/pk/dimension", name+".json"), `{"type":"`+typ+`",
			"generator":{"type":"minecraft:flat","settings":{"biome":"minecraft:plains",
			"layers":[{"block":"minecraft:stone","height":1}]}}}`)
	}
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"), func(s *server.Server) {
		s.SetTimeSync(0)
	})
	servertest.HoldTicks(t, srv)
	c.ChatCommand("time of overworld pause")
	c.ExpectTimeFeedback("commands.time.pause", textArg("minecraft:overworld"))
	c.ReadUpdateTime()
	c.ChatCommand("time of the_end pause")
	c.ExpectTimeFeedback("commands.time.pause", textArg("minecraft:the_end"))
	c.ReadUpdateTime()
	c.ChatCommand("time of overworld set 1000")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), intArg(1000))
	c.ExpectUpdateTime(1000)

	for _, name := range []string{"day", "none"} {
		c.ChatCommand("execute in pk:" + name + " run tp @s 0 0 0")
		if !c.ReadTicks(srv, 200, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySystemChat {
				return false
			}
			got, ok := protocol.ReadText(protocol.NewReader(data))
			return ok && got.Translate == "commands.teleport.success.location.single"
		}) {
			t.Fatal("no teleport feedback arrived")
		}
		if got := srv.PlayerDimension("Tester"); got != "pk:"+name {
			t.Fatalf("player dimension = %q, want pk:%s", got, name)
		}
		c.ChatCommand("time set midnight")
		if name == "day" {
			c.ExpectTimeFeedback("commands.time.set.time_marker", textArg("minecraft:overworld"), textArg("minecraft:midnight"))
			c.ExpectUpdateTime(18000)
		} else {
			want := servertest.CommandErrorMessageForTest("commands.time.no_default_clock", "pk:none_type")
			if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want) {
				t.Errorf("no-clock refusal = % X, want % X", got, want)
			}
		}
		c.ChatCommand("time of overworld query time")
		c.ExpectTimeFeedback("commands.time.query.absolute", textArg("minecraft:overworld"), longArg(18000))

		for _, suggest := range []struct{ input, match string }{
			{"/time set mid", "minecraft:midnight"}, {"/time query d", "minecraft:day"},
		} {
			c.CommandSuggestion(1, suggest.input)
			_, _, _, matches := c.ExpectCommandSuggestions()
			var want []string
			if name == "day" {
				want = []string{suggest.match}
			}
			if !slices.Equal(matches, want) {
				t.Errorf("%s in pk:%s suggests %v, want %v", suggest.input, name, matches, want)
			}
		}
	}
	for i, tc := range []struct{ dimension, clock string }{
		{"inherited", "minecraft:overworld"}, {"caves", "minecraft:overworld"}, {"end", "minecraft:the_end"},
	} {
		value := int64(4502 + i)
		c.ChatCommand("execute in pk:" + tc.dimension + " run time set " + strconv.FormatInt(value, 10))
		c.ExpectTimeFeedback("commands.time.set.absolute", textArg(tc.clock), intArg(value))
		c.ChatCommand("time of " + tc.clock + " query time")
		c.ExpectTimeFeedback("commands.time.query.absolute", textArg(tc.clock), longArg(value))
	}
}
