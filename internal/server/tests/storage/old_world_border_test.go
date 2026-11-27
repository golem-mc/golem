package storage

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestOldWorldBorderFieldsReachEveryDimension checks that a 1.21.1 level.dat's
// Border* fields, whatever tag type they use, give the overworld and the End
// the same border and the Nether's centre divided by 8, shrink included.
func TestOldWorldBorderFieldsReachEveryDimension(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	data, err := nbt.Encode("", nbt.Compound{"Data": nbt.Compound{
		"DataVersion":   nbt.Int(3955),
		"BorderCenterX": nbt.Double(80), "BorderCenterZ": nbt.Double(-160),
		"BorderSize": nbt.Double(100), "BorderSizeLerpTarget": nbt.Double(50),
		"BorderSizeLerpTime": nbt.Long(2000), "BorderDamagePerBlock": nbt.Double(1.5),
		"BorderSafeZone": nbt.Double(2), "BorderWarningBlocks": nbt.Double(7),
		"BorderWarningTime": nbt.Double(20),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := servertest.WriteGzip(filepath.Join(cfg.LevelName, "level.dat"), data); err != nil {
		t.Fatal(err)
	}

	srv, c := servertest.Join(t, cfg, "Warden", servertest.OpsListing(t, "Warden"))
	srv.StopTick()
	c.ChatCommand("execute in minecraft:the_nether run worldborder get")
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.get", "100")
	srv.Shutdown()

	for dim, cx := range map[string]float64{"overworld": 80, "the_nether": 10, "the_end": 80} {
		got, ok, err := world.ReadSavedData(filepath.Join(cfg.LevelName, "dimensions", "minecraft", dim),
			"data/minecraft/world_border.dat", 1<<20)
		if err != nil || !ok {
			t.Fatalf("%s world_border.dat: ok %v, err %v", dim, ok, err)
		}
		d := func(k string) float64 { return nbt.DoubleFieldOr(got, k, -1) }
		if d("center_x") != cx || d("center_z") != cx*-2 || d("lerp_target") != 50 || d("damage_per_block") != 1.5 ||
			d("safe_zone") != 2 || d("warning_blocks") != 7 || d("warning_time") != 20 {
			t.Errorf("%s border = %v", dim, got)
		}
		if size := d("size"); size > 100 || size < 50 {
			t.Errorf("%s size = %v, want a size in the shrink from 100 to 50", dim, size)
		}
		if lt := d("lerp_time"); lt <= 0 || lt > 2000 {
			t.Errorf("%s lerp_time = %v, want the shrink's remaining ticks", dim, lt)
		}
	}
}
