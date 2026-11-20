package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A pack that adds obsidian to #minecraft:mineable/axe gives a diamond axe
// the speed 8 on it: 8 / 50 / 30 a tick, so the stop packet breaks it once
// 0.7 of the block is dug (131 ticks' share), not at the 1/100 rate of the
// built-in tags, and the axe, now the correct tool, makes it drop.
// The tables are process-wide, so the test is not parallel.
// vanilla: Tool.getMiningSpeed, ServerPlayerGameMode.handleBlockBreakAction
func TestAPackMineableTagSpeedsUpAndDropsBlocks(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil); registry.ApplyPackRegistries(nil) })
	for _, tc := range []struct {
		ticks  int // ticks between the start and the stop packet
		breaks bool
	}{{129, false}, {133, true}} {
		cfg := servertest.SurvivalConfig()
		cfg.LevelName = t.TempDir()
		pack := filepath.Join(cfg.LevelName, "datapacks", "p")
		for file, body := range map[string]string{
			"pack.mcmeta": `{"pack":{"description":"p"}}`,
			"data/minecraft/tags/block/mineable/axe.json": `{"values":["minecraft:obsidian"]}`,
		} {
			path := filepath.Join(pack, file)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		srv, c := servertest.DiggerIn(t, cfg, servertest.ItemStack(t, "minecraft:diamond_axe", 1), "minecraft:obsidian", nil, true)
		servertest.HoldTicks(t, srv)
		x, y, z := servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]
		before := srv.BlockAt(x, y, z)
		c.StartDigging(x, y, z)
		c.Settle()
		servertest.RunTicks(t, srv, tc.ticks)
		c.StopDigging(x, y, z)
		c.Settle()
		if tc.breaks {
			servertest.ExpectAir(t, srv, servertest.DigTarget)
			servertest.ExpectOneDroppedItem(t, srv, "minecraft:obsidian")
		} else if got := srv.BlockAt(x, y, z); got != before {
			t.Errorf("obsidian after %d ticks = %d, want it standing", tc.ticks, got)
		}
	}
}
