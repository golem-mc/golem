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
// the saved layers, from y=0 as before 1.18.
// vanilla: WorldGenSettingsFix.fix, WorldGenSettingsHeightAndBiomeFix
func TestAFlatWorldSavedBeforeThe116KeepsItsLayers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layer := func(block string, height int8) nbt.Tag {
		// 1.14 and 1.15 wrote the height as a byte.
		return nbt.Compound{"block": nbt.String(block), "height": nbt.Byte(height)}
	}
	writeOldLevelDat(t, dir, "flat", nbt.Compound{
		"layers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{layer("minecraft:bedrock", 1), layer("minecraft:stone", 3)}},
		"biome":  nbt.String("minecraft:plains"), "structures": nbt.Compound{},
	})
	srv, _ := servertest.Start(t, startOldWorld(t, dir))
	servertest.StopTicking(t, srv)
	air := uint16(registry.BlockAir.DefaultState())
	for y, want := range map[int32]uint16{
		-64: air, -1: air, 0: uint16(registry.BlockBedrock.DefaultState()),
		1: uint16(registry.BlockStone.DefaultState()), 3: uint16(registry.BlockStone.DefaultState()), 4: air,
	} {
		if got := srv.LoadBlockAt(5000, y, 5000); got != want {
			t.Errorf("y=%d: block %d, want %d", y, got, want)
		}
	}
}
