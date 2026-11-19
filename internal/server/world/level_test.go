package world

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// writeLevelDat writes a Data compound to <dir>/level.dat as gzipped NBT, the
// way a vanilla server does. It is what the tests use to set up a vanilla
// world's level.dat without going through SaveLevel (which is what they are
// testing).
func writeLevelDat(t *testing.T, dir string, data nbt.Compound) {
	t.Helper()
	encoded, err := nbt.Encode("", nbt.Compound{"Data": data})
	if err != nil {
		t.Fatalf("nbt.Encode: %v", err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(encoded); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, levelDatFile), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestLoadLevelMissingFileReturnsDefaults checks that a world without a
// level.dat gets the default spawn and is marked as not existing, so Save will
// write one.
func TestLoadLevelMissingFileReturnsDefaults(t *testing.T) {
	t.Parallel()
	level, err := LoadLevel(t.TempDir())
	if err != nil {
		t.Fatalf("LoadLevel: %v", err)
	}
	if level.existed {
		t.Error("a missing level.dat was reported as existing")
	}
	if level.Spawn != (SpawnPos{X: 0, Y: GroundLevel, Z: 0}) {
		t.Errorf("default spawn = %+v, want (0, %d, 0)", level.Spawn, GroundLevel)
	}
}

// TestLevelRoundTrip checks that a level.dat written by SaveLevel is read back
// by LoadLevel with the same spawn, which is what a world outliving a restart
// amounts to.
func TestLevelRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	level := defaultLevel("test")
	level.Spawn = SpawnPos{X: 12, Y: 70, Z: -34}
	level.raw["SpawnX"] = nbt.Int(12)
	level.raw["SpawnY"] = nbt.Int(70)
	level.raw["SpawnZ"] = nbt.Int(-34)

	if err := SaveLevel(dir, level); err != nil {
		t.Fatalf("SaveLevel: %v", err)
	}

	loaded, err := LoadLevel(dir)
	if err != nil {
		t.Fatalf("LoadLevel: %v", err)
	}
	if !loaded.existed {
		t.Error("a written level.dat was not reported as existing")
	}
	if loaded.Spawn != level.Spawn {
		t.Errorf("spawn = %+v, want %+v", loaded.Spawn, level.Spawn)
	}
}

// TestLevelDatIsGzippedNBT checks that the file written is gzipped NBT the way
// a vanilla server reads it: a gzip header, an unnamed compound root holding a
// Data compound.
func TestLevelDatIsGzippedNBT(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := SaveLevel(dir, defaultLevel("vanilla-friendly")); err != nil {
		t.Fatalf("SaveLevel: %v", err)
	}

	path := filepath.Join(dir, levelDatFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(raw) < 2 || raw[0] != 0x1F || raw[1] != 0x8B {
		t.Fatalf("level.dat starts % X, want a gzip header (1F 8B)", raw[:min(len(raw), 2)])
	}

	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	data, err := readAll(gz)
	if err != nil {
		t.Fatalf("gzip read: %v", err)
	}

	name, tag, err := nbt.Decode(data)
	if err != nil {
		t.Fatalf("nbt.Decode: %v", err)
	}
	if name != "" {
		t.Errorf("root name = %q, want empty", name)
	}
	root, ok := tag.(nbt.Compound)
	if !ok {
		t.Fatalf("root is a %s, want a compound", tag.Type())
	}
	dataTag, ok := nbt.Get[nbt.Compound](root, "Data")
	if !ok {
		t.Fatal("root has no Data compound")
	}

	// The fields a vanilla server needs to open the world.
	if v, _ := nbt.Get[nbt.Int](dataTag, "GameType"); v != 1 {
		t.Errorf("GameType = %d, want 1 (creative)", v)
	}
	if v, _ := nbt.Get[nbt.Int](dataTag, "Generator"); v != generatorFlat {
		t.Errorf("Generator = %d, want %d (flat)", v, generatorFlat)
	}
	if v, _ := nbt.Get[nbt.String](dataTag, "LevelName"); string(v) != "vanilla-friendly" {
		t.Errorf("LevelName = %q, want %q", v, "vanilla-friendly")
	}
	version, ok := nbt.Get[nbt.Compound](dataTag, "Version")
	if !ok {
		t.Fatal("no Version compound")
	}
	if v, _ := nbt.Get[nbt.Int](version, "Id"); v != dataVersion {
		t.Errorf("Version.Id = %d, want %d", v, dataVersion)
	}
}

// TestLoadLevelReadsVanillaSpawn checks that a level.dat written by a vanilla
// server — with its own field set — is read with the spawn point it carries,
// which is what dropping a vanilla world into the server folder does.
func TestLoadLevelReadsVanillaSpawn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vanilla := nbt.Compound{
		"SpawnX":     nbt.Int(103),
		"SpawnY":     nbt.Int(-60),
		"SpawnZ":     nbt.Int(5),
		"GameType":   nbt.Int(0),
		"Generator":  nbt.Int(generatorDefault),
		"RandomSeed": nbt.Long(12345),
		"Time":       nbt.Long(6000),
		"DayTime":    nbt.Long(6000),
		"LevelName":  nbt.String("Vanilla World"),
		"Version": nbt.Compound{
			"Id":         nbt.Int(dataVersion),
			"Name":       nbt.String("26.2"),
			"isSnapshot": nbt.Byte(0),
		},
	}
	writeLevelDat(t, dir, vanilla)

	level, err := LoadLevel(dir)
	if err != nil {
		t.Fatalf("LoadLevel: %v", err)
	}
	if !level.existed {
		t.Error("an existing level.dat was not reported as such")
	}
	if level.Spawn != (SpawnPos{X: 103, Y: -60, Z: 5}) {
		t.Errorf("spawn = %+v, want (103, -60, 5)", level.Spawn)
	}

	// The raw data is carried through, so a re-save does not lose the
	// vanilla server's time and seed.
	if v, _ := nbt.Get[nbt.Long](level.raw, "Time"); v != 6000 {
		t.Errorf("Time = %d, want 6000 (carried through)", v)
	}
	if v, _ := nbt.Get[nbt.Long](level.raw, "RandomSeed"); v != 12345 {
		t.Errorf("RandomSeed = %d, want 12345 (carried through)", v)
	}
}

// TestWorldSavesTimeAndPreservesExistingLevelDat checks that an existing
// level.dat is rewritten on Save — so that the advanced Time and DayTime
// outlive a restart — while everything else this server does not model is
// carried through untouched. The time itself does not advance here, since no
// ticks have passed between NewWorld and Save; what is checked is that the
// save preserves the time the file held rather than resetting it.
func TestWorldSavesTimeAndPreservesExistingLevelDat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"SpawnX":     nbt.Int(0),
		"SpawnY":     nbt.Int(GroundLevel),
		"SpawnZ":     nbt.Int(0),
		"GameType":   nbt.Int(0),
		"Generator":  nbt.Int(generatorDefault),
		"RandomSeed": nbt.Long(12345),
		"Version":    nbt.Compound{"Id": nbt.Int(dataVersion), "Name": nbt.String("26.2")},
		"LevelName":  nbt.String("Vanilla"),
		"Time":       nbt.Long(99999),
	})

	w, _ := NewWorld(dir, NewFlatGenerator())
	w.Save()
	w.Close()

	loaded, err := LoadLevel(dir)
	if err != nil {
		t.Fatalf("LoadLevel: %v", err)
	}
	if loaded.WorldAge != 99999 {
		t.Errorf("Time = %d, want 99999 (carried through from the original file)", loaded.WorldAge)
	}
	if loaded.DayTime != 0 {
		t.Errorf("DayTime = %d, want 0 (the original file had none, so the default)", loaded.DayTime)
	}
	if got, _ := nbt.Get[nbt.Long](loaded.raw, "RandomSeed"); got != 12345 {
		t.Errorf("RandomSeed = %d, want 12345 (preserved)", got)
	}
	if got, _ := nbt.Get[nbt.String](loaded.raw, "LevelName"); string(got) != "Vanilla" {
		t.Errorf("LevelName = %q, want %q (preserved)", got, "Vanilla")
	}
	if got, _ := nbt.Get[nbt.Int](loaded.raw, "GameType"); got != 0 {
		t.Errorf("GameType = %d, want 0 (preserved)", got)
	}
}

// TestLoadLevelFallsBackToBackup checks that an unreadable level.dat — what a
// crash mid-write leaves — reads back from the level.dat_old the save rotated
// beside it, rather than reading as a new world.
func TestLoadLevelFallsBackToBackup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := defaultLevel("first")
	first.Spawn = SpawnPos{X: 7, Y: 70, Z: -3}
	first.raw["SpawnX"] = nbt.Int(7)
	first.raw["SpawnY"] = nbt.Int(70)
	first.raw["SpawnZ"] = nbt.Int(-3)
	if err := SaveLevel(dir, first); err != nil {
		t.Fatalf("first SaveLevel: %v", err)
	}
	if err := SaveLevel(dir, defaultLevel("second")); err != nil {
		t.Fatalf("second SaveLevel: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "level.dat_old")); err != nil {
		t.Fatalf("the second save did not keep level.dat_old: %v", err)
	}

	// Half a file: what an interrupted write leaves.
	path := filepath.Join(dir, levelDatFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.WriteFile(path, raw[:len(raw)/2], 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	level, err := LoadLevel(dir)
	if err != nil {
		t.Fatalf("LoadLevel after truncation: %v", err)
	}
	if !level.existed {
		t.Error("the backup was read as a new world")
	}
	if level.Spawn != first.Spawn {
		t.Errorf("spawn from the backup = %+v, want the previous file's %+v", level.Spawn, first.Spawn)
	}
}

// TestLoadLevelRefusesWhenBothUnreadable checks that LoadLevel reports an
// error, rather than defaults, when level.dat and its backup are both
// unreadable, so a caller does not start a fresh world over a damaged one.
func TestLoadLevelRefusesWhenBothUnreadable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{levelDatFile, "level.dat_old"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not gzipped nbt"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	if _, err := LoadLevel(dir); err == nil {
		t.Fatal("LoadLevel accepted two unreadable files as a new world")
	}
}

// readAll is io.ReadAll without importing io, to keep the test imports tight.
func readAll(r *gzip.Reader) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
