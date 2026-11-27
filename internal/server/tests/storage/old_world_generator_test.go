package storage

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// writeOldLevelDat writes a pre-1.16 level.dat naming generatorName in dir.
func writeOldLevelDat(t *testing.T, dir, generatorName string, options nbt.Compound) {
	t.Helper()
	data := nbt.Compound{
		"DataVersion": nbt.Int(2230), "RandomSeed": nbt.Long(7), "LevelName": nbt.String("old"),
		"generatorName": nbt.String(generatorName), "generatorOptions": options,
	}
	raw, err := nbt.Encode("", nbt.Compound{"Data": data})
	if err != nil {
		t.Fatal(err)
	}
	var z bytes.Buffer
	w := gzip.NewWriter(&z)
	w.Write(raw)
	w.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "level.dat"), z.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// startOldWorld is the config of a server on dir with the default level-type.
func startOldWorld(t *testing.T, dir string) (cfg *config.Config) {
	cfg = config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = dir
	cfg.LevelType = "normal" // the unprefixed spelling: the harness swaps the default one for its flat ground
	return cfg
}

// TestAFlatWorldSavedBeforeThe116KeepsItsLayers loads a 1.15 superflat
// level.dat with the default level-type: a chunk never generated is still
// the saved layers.
// vanilla: WorldGenSettingsFix.fix
func TestAFlatWorldSavedBeforeThe116KeepsItsLayers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layer := func(block string, height int32) nbt.Tag {
		return nbt.Compound{"block": nbt.String(block), "height": nbt.Int(height)}
	}
	writeOldLevelDat(t, dir, "flat", nbt.Compound{
		"layers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{layer("minecraft:bedrock", 1), layer("minecraft:stone", 3)}},
		"biome":  nbt.String("minecraft:plains"), "structures": nbt.Compound{},
	})
	srv, _ := servertest.Start(t, startOldWorld(t, dir))
	servertest.StopTicking(t, srv)
	stone := registry.BlockStone.DefaultState()
	if got := srv.LoadBlockAt(5000, -61, 5000); got != uint16(stone) {
		t.Errorf("top layer = %d, want stone %d", got, stone)
	}
	if got := srv.LoadBlockAt(5000, -60, 5000); got != uint16(registry.BlockAir.DefaultState()) {
		t.Errorf("block above the layers = %d, want air", got)
	}
}
