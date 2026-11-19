package datapack

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestSaveStructureStaysUnderTheGeneratedFolder: an id that climbs out of the
// folder or is no identifier writes nothing, and a valid one lands under
// generated/<namespace>/structure.
// vanilla: StructureTemplateManager.save
func TestSaveStructureStaysUnderTheGeneratedFolder(t *testing.T) {
	dir := t.TempDir()
	world := filepath.Join(dir, "world")
	for _, id := range []string{"test:../../escape", "test:a/../../escape", "../x:y", "nocolon", "Test:upper"} {
		if err := SaveStructure(world, id, []byte("x")); !errors.Is(err, ErrInvalidID) {
			t.Errorf("SaveStructure(%q) = %v, want ErrInvalidID", id, err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a refused id created %d entries beside the world folder", len(entries))
	}
	if err := SaveStructure(world, "test:sub/house", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(world, "generated", "test", "structure", "sub", "house.nbt")); err != nil {
		t.Errorf("valid id not saved: %v", err)
	}
}
