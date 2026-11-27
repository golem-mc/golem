package storage

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestAFallingAnvilFromAnOldWorldLoadsAsAnAnvil loads an entity record of
// 1.21.1 (DataVersion 3955) holding a falling anvil whose BlockState still
// has Name and Properties.
// vanilla: BlockStateFieldNamesFix
func TestAFallingAnvilFromAnOldWorldLoadsAsAnAnvil(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	level := t.TempDir()
	cfg.LevelName = level
	overworld := filepath.Join(level, "dimensions", "minecraft", "overworld")
	writeRegion0(t, filepath.Join(overworld, "region", "r.0.0.mca"), oldSignChunk(t))
	anvil := nbt.Compound{
		"id":  nbt.String("minecraft:falling_block"),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(8.5), nbt.Double(30), nbt.Double(8.5)}},
		"BlockState": nbt.Compound{
			"Name":       nbt.String("minecraft:anvil"),
			"Properties": nbt.Compound{"facing": nbt.String("east")},
		},
		"Time": nbt.Int(1), "NoGravity": nbt.Byte(1), "UUID": nbt.IntArray{1, 2, 3, 4},
	}
	entities, err := nbt.Encode("", nbt.Compound{
		"DataVersion": nbt.Int(3955),
		"Position":    nbt.IntArray{0, 0},
		"Entities":    nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{anvil}},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeRegion0(t, filepath.Join(overworld, "entities", "r.0.0.mca"), entities)

	srv, _ := servertest.Join(t, cfg, "Watcher")
	release := srv.HoldChunk(0, 0)
	defer release()
	var got string
	servertest.UntilTicks(t, srv, 20, "the falling block loads", func() bool {
		got = fmt.Sprint(srv.RunLine("data get entity @e[type=falling_block,limit=1] BlockState"))
		return !strings.Contains(got, "argument.entity.notfound")
	})
	if !strings.Contains(got, "minecraft:anvil") || !strings.Contains(got, "east") {
		t.Errorf("BlockState = %s, want an east-facing anvil", got)
	}
}
