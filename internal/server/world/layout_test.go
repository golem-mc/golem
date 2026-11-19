package world

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func expectFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
		} else if string(got) != want {
			t.Errorf("%s holds %q, want %q", name, got, want)
		}
	}
}

func expectGone(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
			t.Errorf("%s is still there", name)
		}
	}
}

// TestMigrateLayoutMovesTheOldFolders checks the moves of a world in the
// layout before 26.1 into the one the server reads, and that a second run finds
// nothing more to do.
func TestMigrateLayoutMovesTheOldFolders(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"level.dat":                      "level",
		"data/minecraft/game_rules.dat":  "rules",
		"data/raids.dat":                 "overworld raids",
		"data/chunks.dat":                "overworld tickets",
		"region/r.0.0.mca":               "overworld blocks",
		"entities/r.0.0.mca":             "overworld mobs",
		"poi/r.0.0.mca":                  "overworld poi",
		"DIM-1/region/r.0.-1.mca":        "nether blocks",
		"DIM-1/entities/r.0.0.mca":       "nether mobs",
		"DIM-1/data/world_border.dat":    "nether border",
		"DIM1/region/r.-1.0.mca":         "end blocks",
		"DIM1/data/raids_end.dat":        "end raids",
		"DIM1/entities/r.0.0.mca":        "end mobs",
		"DIM1/data/ender_dragon.unknown": "kept",
	})
	moved := map[string]string{
		"level.dat":                     "level",
		"data/minecraft/game_rules.dat": "rules",
		"dimensions/minecraft/overworld/data/minecraft/raids.dat":         "overworld raids",
		"dimensions/minecraft/overworld/data/minecraft/chunk_tickets.dat": "overworld tickets",
		"dimensions/minecraft/overworld/region/r.0.0.mca":                 "overworld blocks",
		"dimensions/minecraft/overworld/entities/r.0.0.mca":               "overworld mobs",
		"dimensions/minecraft/overworld/poi/r.0.0.mca":                    "overworld poi",
		"dimensions/minecraft/the_nether/region/r.0.-1.mca":               "nether blocks",
		"dimensions/minecraft/the_nether/entities/r.0.0.mca":              "nether mobs",
		"dimensions/minecraft/the_nether/data/minecraft/world_border.dat": "nether border",
		"dimensions/minecraft/the_end/region/r.-1.0.mca":                  "end blocks",
		"dimensions/minecraft/the_end/data/minecraft/raids.dat":           "end raids",
		"dimensions/minecraft/the_end/entities/r.0.0.mca":                 "end mobs",
		"DIM1/data/ender_dragon.unknown":                                  "kept",
	}
	for range 2 {
		if err := MigrateLayout(root); err != nil {
			t.Fatal(err)
		}
		expectFiles(t, root, moved)
		expectGone(t, root, "region", "entities", "poi", "data/raids.dat", "DIM-1")
	}
}

// TestMigrateLayoutMovesThePlayerAndDataFiles checks the player folders and
// the loose data files of a world in the layout before 26.1 move into the
// places the server reads, under players/ and data/minecraft.
func TestMigrateLayoutMovesThePlayerAndDataFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"playerdata/3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99.dat":    "player",
		"stats/3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99.json":        "stats",
		"advancements/3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99.json": "advancements",
		"data/scoreboard.dat":                "scoreboard",
		"data/stopwatches.dat":               "stopwatches",
		"data/command_storage_minecraft.dat": "commands",
		"data/idcounts.dat":                  "idcounts",
		"data/map_7.dat":                     "a map",
		"data/random_sequences.dat":          "sequences",
		"data/raids.dat":                     "overworld raids",
	})
	moved := map[string]string{
		"players/data/3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99.dat":          "player",
		"players/stats/3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99.json":        "stats",
		"players/advancements/3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99.json": "advancements",
		"data/minecraft/scoreboard.dat":                                  "scoreboard",
		"data/minecraft/stopwatches.dat":                                 "stopwatches",
		"data/minecraft/command_storage.dat":                             "commands",
		"data/minecraft/maps/last_id.dat":                                "idcounts",
		"data/minecraft/maps/7.dat":                                      "a map",
		"data/minecraft/random_sequences.dat":                            "sequences",
		"dimensions/minecraft/overworld/data/minecraft/raids.dat":        "overworld raids",
	}
	for range 2 {
		if err := MigrateLayout(root); err != nil {
			t.Fatal(err)
		}
		expectFiles(t, root, moved)
	}
	expectGone(t, root, "playerdata", "stats", "advancements", "data/scoreboard.dat")
}

// TestMigrateLayoutLeavesATakenPlaceAlone checks that an old folder whose new
// place already exists is not moved over it, and both are kept.
func TestMigrateLayoutLeavesATakenPlaceAlone(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]string{
		"entities/r.0.0.mca": "old mobs",
		"dimensions/minecraft/overworld/entities/r.0.0.mca": "new mobs",
	}
	writeFiles(t, root, files)
	if err := MigrateLayout(root); err != nil {
		t.Fatal(err)
	}
	expectFiles(t, root, files)
}

// TestMigrateLayoutFailsWithoutLosingAnything checks that a move that cannot
// be made stops the migration with an error and leaves the old files where
// they were, and that the next run finishes it.
func TestMigrateLayoutFailsWithoutLosingAnything(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]string{
		"region/r.0.0.mca":   "overworld blocks",
		"entities/r.0.0.mca": "overworld mobs",
	}
	writeFiles(t, root, files)
	// A file where the dimensions folder belongs makes every move fail.
	writeFiles(t, root, map[string]string{"dimensions": "in the way"})
	if err := MigrateLayout(root); err == nil {
		t.Fatal("MigrateLayout succeeded with its target blocked")
	}
	expectFiles(t, root, files)

	if err := os.Remove(filepath.Join(root, "dimensions")); err != nil {
		t.Fatal(err)
	}
	if err := MigrateLayout(root); err != nil {
		t.Fatal(err)
	}
	expectFiles(t, root, map[string]string{
		"dimensions/minecraft/overworld/region/r.0.0.mca":   "overworld blocks",
		"dimensions/minecraft/overworld/entities/r.0.0.mca": "overworld mobs",
	})
}

// TestWorldKeepsEachDimensionInItsOwnFolder checks where each dimension's
// world saves its chunks: dimensions/minecraft/<name>/region.
func TestWorldKeepsEachDimensionInItsOwnFolder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pos := ChunkPos{X: 0, Z: 0}
	overworld, _ := NewWorld(root, NewFlatGenerator())
	for _, w := range []*World{
		overworld,
		NewDimensionWorld(root, true, NewFlatGenerator()),
		NewDimensionWorld(root, false, NewFlatGenerator()),
	} {
		w.Acquire(pos)
		if !w.SetBlock(1, 70, 1, stoneState) {
			t.Fatalf("%s: the block did not change, so the chunk would not save", w.Name())
		}
		w.Save()
		w.Release(pos)
		w.FlushSaves()
		w.Close()
	}
	for _, name := range []string{"overworld", "the_nether", "the_end"} {
		path := filepath.Join(root, "dimensions", "minecraft", name, "region", "r.0.0.mca")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	expectGone(t, root, "region", "DIM-1", "DIM1")
}
