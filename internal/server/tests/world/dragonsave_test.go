package world

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// readDragonFightFile reads the End's saved fight from disk, or nil if absent.
func readDragonFightFile(t *testing.T, level string) nbt.Compound {
	t.Helper()
	f, err := os.Open(filepath.Join(level, "dimensions", "minecraft", "the_end", "data", "minecraft", "ender_dragon_fight.dat"))
	if err != nil {
		return nil
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	_, tag, err := nbt.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := nbt.Get[nbt.Compound](tag.(nbt.Compound), "data")
	return data
}
