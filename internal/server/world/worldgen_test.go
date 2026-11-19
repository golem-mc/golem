package world

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestNewWorldWritesWhatVanillaNeedsToOpenIt checks the files a world this
// server creates starts with: a level.dat carrying DataVersion, version and
// the spawn and difficulty compounds, and a world_gen_settings.dat with
// the seed and each dimension's generator, laid out as a fresh vanilla
// world's are.
func TestNewWorldWritesWhatVanillaNeedsToOpenIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		gen  Generator
		kind string
		seed int64
	}{
		{"flat", NewFlatGenerator(), "minecraft:flat", 0},
		{"normal", NewOverworldGenerator(12345), "minecraft:noise", 12345},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			w, _ := NewWorld(dir, tc.gen)
			w.SetDifficulty(DifficultyHard)
			w.Save()
			w.Close()

			raw, err := LoadLevel(dir)
			if err != nil {
				t.Fatal(err)
			}
			data := raw.raw
			if v, _ := nbt.Get[nbt.Int](data, "DataVersion"); v != dataVersion {
				t.Errorf("DataVersion = %d, want %d", v, dataVersion)
			}
			if v, _ := nbt.Get[nbt.Int](data, "version"); v != levelStorageVersion {
				t.Errorf("version = %d, want %d", v, levelStorageVersion)
			}
			version, _ := nbt.Get[nbt.Compound](data, "Version")
			if id, _ := nbt.Get[nbt.Int](version, "Id"); id != dataVersion {
				t.Errorf("Version.Id = %d, want %d", id, dataVersion)
			}
			if series, _ := nbt.Get[nbt.String](version, "Series"); series != "main" {
				t.Errorf("Version.Series = %q, want main", series)
			}
			spawn, _ := nbt.Get[nbt.Compound](data, "spawn")
			pos, _ := nbt.Get[nbt.IntArray](spawn, "pos")
			if want := (nbt.IntArray{raw.Spawn.X, raw.Spawn.Y, raw.Spawn.Z}); len(pos) != 3 || pos[0] != want[0] || pos[1] != want[1] || pos[2] != want[2] {
				t.Errorf("spawn.pos = %v, want %v", pos, want)
			}
			settings, _ := nbt.Get[nbt.Compound](data, "difficulty_settings")
			if d, _ := nbt.Get[nbt.String](settings, "difficulty"); d != "hard" {
				t.Errorf("difficulty_settings.difficulty = %q, want hard", d)
			}

			gen, err := readSavedData(dir, worldGenFile)
			if err != nil {
				t.Fatal(err)
			}
			if seed, _ := nbt.Get[nbt.Long](gen, "seed"); int64(seed) != tc.seed {
				t.Errorf("seed = %d, want %d", seed, tc.seed)
			}
			dims, _ := nbt.Get[nbt.Compound](gen, "dimensions")
			for name, kind := range map[string]string{DimensionName: tc.kind, NetherName: "minecraft:noise", EndName: "minecraft:noise"} {
				dim, _ := nbt.Get[nbt.Compound](dims, name)
				generator, _ := nbt.Get[nbt.Compound](dim, "generator")
				if got, _ := nbt.Get[nbt.String](generator, "type"); string(got) != kind {
					t.Errorf("%s generator = %q, want %q", name, got, kind)
				}
			}
			if lt, flat, ok := storedGenerator(dir); !ok ||
				(lt == "minecraft:flat") != (tc.kind == "minecraft:flat") ||
				(flat != nil) != (tc.kind == "minecraft:flat") {
				t.Errorf("storedGenerator = %q, %v, %v", lt, flat, ok)
			}
		})
	}
}

// TestAWorldKeepsTheSeedVanillaStores checks that the seed of a world whose
// level.dat vanilla rewrote, with no seed field of its own left, is read from
// world_gen_settings.dat, and that the file is not overwritten by a save.
func TestAWorldKeepsTheSeedVanillaStores(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"spawn": nbt.Compound{
			"dimension": nbt.String(DimensionName),
			"pos":       nbt.IntArray{96, 136, -32},
		},
		"difficulty_settings": nbt.Compound{
			"difficulty": nbt.String("normal"), "hardcore": nbt.Byte(0), "locked": nbt.Byte(1),
		},
	})
	if err := writeSavedData(dir, worldGenFile, worldGenSettings(12345, presetNormal)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, worldGenFile))
	if err != nil {
		t.Fatal(err)
	}

	w, _ := NewWorld(dir, &OverworldGenerator{deferred: true})
	if !w.HasSeed() || w.SeedValue() != 12345 {
		t.Errorf("seed = %d (%v), want 12345 from world_gen_settings.dat", w.SeedValue(), w.HasSeed())
	}
	if got := w.Spawn(); got != (SpawnPos{X: 96, Y: 136, Z: -32}) {
		t.Errorf("spawn = %+v, want the spawn compound's", got)
	}
	if w.Difficulty() != DifficultyNormal || !w.DifficultyLocked() {
		t.Errorf("difficulty = %d locked %v, want normal and locked", w.Difficulty(), w.DifficultyLocked())
	}
	w.Save()
	w.Close()
	after, err := os.ReadFile(filepath.Join(dir, worldGenFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("the save rewrote an existing world_gen_settings.dat")
	}
	if lt, _, ok := storedGenerator(dir); !ok || lt != "minecraft:normal" {
		t.Errorf("storedGenerator = %q, %v; want minecraft:normal", lt, ok)
	}
}

// TestAnOlderWorldIsLeftToVanillasUpgrade checks a level.dat from before
// 26.1, which still holds the world-gen settings and game rules: a save here
// keeps its versions, so vanilla still upgrades it and extracts them, and
// writes no world_gen_settings.dat of its own over them. The generator it
// names is still the one this server picks.
func TestAnOlderWorldIsLeftToVanillasUpgrade(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	amplified := worldGenSettings(12345, presetNormal)
	dims, _ := nbt.Get[nbt.Compound](amplified, "dimensions")
	overworld, _ := nbt.Get[nbt.Compound](dims, DimensionName)
	generator, _ := nbt.Get[nbt.Compound](overworld, "generator")
	generator["settings"] = nbt.String("minecraft:amplified")
	amplified["generate_features"] = nbt.Byte(0)
	delete(amplified, "generate_structures")
	writeLevelDat(t, dir, nbt.Compound{
		"DataVersion":      nbt.Int(4671),
		"version":          nbt.Int(levelStorageVersion),
		"Version":          nbt.Compound{"Id": nbt.Int(4671), "Name": nbt.String("1.21.11")},
		"WorldGenSettings": amplified,
		"GameRules":        nbt.Compound{"minecraft:keep_inventory": nbt.Byte(1)},
		"SpawnX":           nbt.Int(5), "SpawnY": nbt.Int(80), "SpawnZ": nbt.Int(5),
	})
	if lt, _, ok := storedGenerator(dir); !ok || lt != "minecraft:amplified" {
		t.Errorf("storedGenerator = %q, %v; want minecraft:amplified from WorldGenSettings", lt, ok)
	}

	w, _ := NewWorld(dir, &OverworldGenerator{deferred: true})
	if w.SeedValue() != 12345 {
		t.Errorf("seed = %d, want 12345 from WorldGenSettings", w.SeedValue())
	}
	w.Save()
	w.Close()

	if _, err := os.Stat(filepath.Join(dir, worldGenFile)); err == nil {
		t.Error("a world_gen_settings.dat was written for a world vanilla has yet to upgrade")
	}
	level, err := LoadLevel(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := nbt.Get[nbt.Int](level.raw, "DataVersion"); v != 4671 {
		t.Errorf("DataVersion = %d, want the world's own 4671", v)
	}
	version, _ := nbt.Get[nbt.Compound](level.raw, "Version")
	if id, _ := nbt.Get[nbt.Int](version, "Id"); id != 4671 {
		t.Errorf("Version.Id = %d, want 4671", id)
	}
	settings, _ := nbt.Get[nbt.Compound](level.raw, "WorldGenSettings")
	generator, _ = overworldGenerator(settings)
	if kind, _ := nbt.Get[nbt.String](generator, "type"); kind != "minecraft:noise" {
		t.Errorf("WorldGenSettings lost its overworld generator: %q", kind)
	}
	rules, _ := nbt.Get[nbt.Compound](level.raw, "GameRules")
	if v, _ := nbt.Get[nbt.Byte](rules, "minecraft:keep_inventory"); v != 1 {
		t.Error("the level.dat's own game rules were not kept")
	}
	if _, ok := nbt.Get[nbt.Compound](level.raw, "spawn"); ok {
		t.Error("a 26.2 spawn compound was added to a level.dat vanilla has yet to upgrade")
	}
}

// TestFlatWorldSeedIsSettledAndPersisted checks the seed path for a flat
// world: a level-seed is parsed and saved, a text seed is hashed the way Java
// hashes, and a restart keeps the world's saved seed over a new level-seed.
// The flat generator's terrain ignores the seed, but slime chunks, structures
// and mob variants read it.
func TestFlatWorldSeedIsSettledAndPersisted(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spec string
		want int64
	}{
		{"12345", 12345},
		{"hello", 99162322},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			w, _ := NewWorld(dir, NewGenerator(dir, "minecraft:flat", tc.spec, "{}"))
			if !w.HasSeed() || w.SeedValue() != tc.want {
				t.Fatalf("seed = %d (%v), want %d", w.SeedValue(), w.HasSeed(), tc.want)
			}
			level, err := LoadLevel(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !level.HasSeed || level.Seed != tc.want {
				t.Errorf("level.dat seed = %d (%v), want %d", level.Seed, level.HasSeed, tc.want)
			}
			gen, err := readSavedData(dir, worldGenFile)
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := nbt.Get[nbt.Long](gen, "seed"); int64(got) != tc.want {
				t.Errorf("world_gen_settings seed = %d, want %d", got, tc.want)
			}
			w.Close()

			// A restart at a different configured seed keeps the world's own.
			again, _ := NewWorld(dir, NewGenerator(dir, "minecraft:flat", "999999", "{}"))
			defer again.Close()
			if again.SeedValue() != tc.want {
				t.Errorf("seed after restart = %d, want the saved %d", again.SeedValue(), tc.want)
			}
		})
	}
}

// TestDisabledStructuresReachTheNewWorldFile checks that generate-structures
// off writes generate_structures=false into a new overworld's
// world_gen_settings.dat, and that the default writes true.
func TestDisabledStructuresReachTheNewWorldFile(t *testing.T) {
	t.Parallel()
	for _, disable := range []bool{false, true} {
		dir := t.TempDir()
		// Seed 0 has dry ground at the origin; the assertion only reads settings.
		g := NewOverworldGenerator(0)
		if disable {
			g.DisableStructures()
		}
		w, err := NewWorld(dir, g)
		if err != nil {
			t.Fatal(err)
		}
		w.Close()
		settings, err := readSavedData(dir, worldGenFile)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := nbt.Get[nbt.Byte](settings, "generate_structures")
		if want := nbt.Byte(boolByte(!disable)); got != want {
			t.Errorf("disable=%v: generate_structures = %d, want %d", disable, got, want)
		}
	}
}

// TestASingleBiomeWorldReopensAsItsPreset checks a single_biome_surface world
// records vanilla's fixed plains biome source with the overworld's noise
// settings, and that the world reopens as that preset whatever level-type
// server.properties names.
func TestASingleBiomeWorldReopensAsItsPreset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := NewWorld(dir, NewOverworldGeneratorFor("single_biome_surface", 1))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	settings, err := readSavedData(dir, worldGenFile)
	if err != nil {
		t.Fatal(err)
	}
	gen, _ := overworldGenerator(settings)
	source, _ := nbt.Get[nbt.Compound](gen, "biome_source")
	kind, _ := nbt.Get[nbt.String](source, "type")
	biome, _ := nbt.Get[nbt.String](source, "biome")
	noise, _ := nbt.Get[nbt.String](gen, "settings")
	if kind != "minecraft:fixed" || biome != "minecraft:plains" || noise != "minecraft:overworld" {
		t.Errorf("generator = %v, want a fixed plains source with minecraft:overworld settings", gen)
	}
	g, ok := NewGenerator(dir, "minecraft:normal", "", "{}").(*OverworldGenerator)
	if !ok || g.preset != presetSingleBiome {
		t.Fatalf("reopened generator = %#v, want the single_biome_surface preset", g)
	}
	w, err = NewWorld(dir, g)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if got := g.PossibleBiomes(); len(got) != 1 || got[0] != "minecraft:plains" {
		t.Errorf("PossibleBiomes = %v, want only plains", got)
	}
}
