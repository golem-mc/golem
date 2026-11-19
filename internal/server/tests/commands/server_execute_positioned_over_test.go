package commands

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestExecutePositionedOverHeightmaps checks that positioned over reads the
// heightmap it names and lands one above its highest matching block.
// vanilla: ExecuteCommand (positioned over), Level.getHeight
func TestExecutePositionedOverHeightmaps(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Surveyor", servertest.OpsListing(t, "Surveyor"))
	servertest.HoldTicks(t, srv)

	// Each command either changes the column or finds it already so.
	run := func(cmd string, keys ...string) {
		t.Helper()
		c.ChatCommand(cmd)
		got := servertest.FeedbackComponent(t, c)
		if slices.Contains(keys, got.Translate) {
			return
		}
		t.Fatalf("%s answered %+v, want one of %v", cmd, got, keys)
	}
	// Two columns: stone at 70 and water at 71 and 72, the first with oak
	// leaves at 74. Leaves block motion, so OCEAN_FLOOR counts them; only the
	// leafless column shows it finding the sea bed.
	for _, x := range []string{"1", "2"} {
		run("setblock "+x+" 200 1 minecraft:stone", "commands.setblock.success", "commands.setblock.failed")
		run("fill "+x+" 71 1 "+x+" 200 1 minecraft:air", "commands.fill.success")
		run("setblock "+x+" 70 1 minecraft:stone", "commands.setblock.success", "commands.setblock.failed")
		run("setblock "+x+" 71 1 minecraft:water", "commands.setblock.success")
		run("setblock "+x+" 72 1 minecraft:water", "commands.setblock.success")
	}
	run("setblock 1 74 1 minecraft:oak_leaves", "commands.setblock.success")

	for _, tc := range []struct {
		x, kind string
		want    string
	}{
		{"1", "world_surface", "75.0d"},
		{"1", "motion_blocking", "75.0d"},
		{"1", "motion_blocking_no_leaves", "73.0d"},
		{"1", "ocean_floor", "75.0d"},
		{"2", "world_surface", "73.0d"},
		{"2", "ocean_floor", "71.0d"},
	} {
		tag := tc.kind + "_" + tc.x
		run("execute positioned "+tc.x+" 0 1 positioned over "+tc.kind+
			" run summon minecraft:marker ~ ~ ~ {Tags:[\""+tag+"\"]}", "commands.summon.success")
		c.ChatCommand("data get entity @e[tag=" + tag + ",limit=1] Pos[1]")
		got := servertest.FeedbackComponent(t, c)
		if got.Translate != "commands.data.entity.query" || len(got.With) != 2 {
			t.Fatalf("%s: data get answered %+v", tag, got)
		}
		if y := got.With[1].Plain(); y != tc.want {
			t.Errorf("positioned over %s at x %s put the marker at y %s, want %s", tc.kind, tc.x, y, tc.want)
		}
	}
	c.Disconnect()
}
