package world

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"sync"
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

// TestLevelSpawnDimensionRoundTrips checks the dimension a spawn is moved
// into is written to the spawn compound and read back.
func TestLevelSpawnDimensionRoundTrips(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if got := w.SpawnDimension(); got != DimensionName {
		t.Errorf("default spawn dimension = %q, want the overworld", got)
	}
	w.SetSpawnIn(SpawnPos{X: 8, Y: 40, Z: 8}, NetherName)
	w.Save()
	w.Close()
	loaded, err := LoadLevel(dir)
	if err != nil {
		t.Fatalf("LoadLevel: %v", err)
	}
	if loaded.spawnDimension() != NetherName {
		t.Errorf("reloaded spawn dimension = %q, want the nether", loaded.spawnDimension())
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

// TestLoadLevelKeepsSpawnXZOutsideWorld checks that a spawn with Y outside the
// buildable area keeps its X and Z and has only Y clamped.
func TestLoadLevelKeepsSpawnXZOutsideWorld(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ y, want int32 }{{400, 319}, {-200, -64}} {
		dir := t.TempDir()
		writeLevelDat(t, dir, nbt.Compound{
			"SpawnX": nbt.Int(1200), "SpawnY": nbt.Int(tc.y), "SpawnZ": nbt.Int(-300),
			"Version": nbt.Compound{"Id": nbt.Int(dataVersion), "Name": nbt.String("26.2")},
		})
		level, err := LoadLevel(dir)
		if err != nil {
			t.Fatalf("LoadLevel: %v", err)
		}
		if want := (SpawnPos{X: 1200, Y: tc.want, Z: -300}); level.Spawn != want {
			t.Errorf("Y %d: spawn = %+v, want %+v", tc.y, level.Spawn, want)
		}
	}
}

// TestWorldSpawnFromLevelDat checks that a world opened with a level.dat uses
// its spawn point, and one without uses the default.
func TestWorldSpawnFromLevelDat(t *testing.T) {
	t.Parallel()
	// A vanilla level.dat with a spawn at (103, -60, 5).
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"SpawnX": nbt.Int(103), "SpawnY": nbt.Int(-60), "SpawnZ": nbt.Int(5),
		"GameType":  nbt.Int(0),
		"Generator": nbt.Int(generatorDefault),
		"Version":   nbt.Compound{"Id": nbt.Int(dataVersion), "Name": nbt.String("26.2")},
		"LevelName": nbt.String("Vanilla"),
	})

	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	if got := w.Spawn(); got != (SpawnPos{X: 103, Y: -60, Z: 5}) {
		t.Errorf("spawn from level.dat = %+v, want (103, -60, 5)", got)
	}

	// A world with no level.dat gets the default.
	w2 := newTestWorld(t, NewFlatGenerator())
	defer w2.Close()
	if got := w2.Spawn(); got != (SpawnPos{X: 0, Y: GroundLevel, Z: 0}) {
		t.Errorf("default spawn = %+v, want (0, %d, 0)", got, GroundLevel)
	}
}

// TestWorldSpawnFromLevelDatFoldsRotation checks that a level.dat whose spawn
// compound holds an out-of-range rotation is folded on read, the way
// RespawnData.of folds the rotation it stores.
func TestWorldSpawnFromLevelDatFoldsRotation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"SpawnX": nbt.Int(0), "SpawnY": nbt.Int(GroundLevel), "SpawnZ": nbt.Int(0),
		"GameType":  nbt.Int(0),
		"Generator": nbt.Int(generatorDefault),
		"Version":   nbt.Compound{"Id": nbt.Int(dataVersion), "Name": nbt.String("26.2")},
		"LevelName": nbt.String("Vanilla"),
		"spawn": nbt.Compound{
			"dimension": nbt.String(DimensionName),
			"pos":       nbt.IntArray{0, GroundLevel, 0},
			"yaw":       nbt.Float(270),
			"pitch":     nbt.Float(-120),
		},
	})

	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	if got := w.Spawn(); got != (SpawnPos{X: 0, Y: GroundLevel, Z: 0, Yaw: -90, Pitch: -90}) {
		t.Errorf("spawn from level.dat = %+v, want yaw -90, pitch -90", got)
	}
}

// TestWorldWritesLevelDatForNewWorld checks that a new world — one that had no
// level.dat — gets one written on Save, so a vanilla server can open it.
func TestWorldWritesLevelDatForNewWorld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())
	w.Save()
	w.Close()

	if _, err := os.Stat(filepath.Join(dir, levelDatFile)); err != nil {
		t.Fatalf("level.dat was not written: %v", err)
	}

	// A second world opening the same directory should find the spawn this
	// one wrote.
	w2, _ := NewWorld(dir, NewFlatGenerator())
	defer w2.Close()
	if got := w2.Spawn(); got != (SpawnPos{X: 0, Y: GroundLevel, Z: 0}) {
		t.Errorf("spawn after level.dat write = %+v, want default", got)
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

// TestLoadLevelFallbackRestoresBackup checks that a fallback load replaces the
// broken level.dat with level.dat_old, so the next save rotates a readable file
// into the backup rather than the corruption it just recovered from.
func TestLoadLevelFallbackRestoresBackup(t *testing.T) {
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
	if level.Spawn != first.Spawn {
		t.Errorf("spawn from the backup = %+v, want the previous file's %+v", level.Spawn, first.Spawn)
	}
	if _, err := os.Stat(filepath.Join(dir, levelDatBackupFile)); !os.IsNotExist(err) {
		t.Errorf("level.dat_old survives the fallback load, want it moved back to level.dat")
	}
	matches, err := filepath.Glob(filepath.Join(dir, levelDatCorruptedPrefix+"*"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("found %d level.dat_corrupted_* files, want the broken level.dat moved to exactly one: %v", len(matches), matches)
	}

	if err := SaveLevel(dir, level); err != nil {
		t.Fatalf("SaveLevel after the fallback: %v", err)
	}
	if _, err := readLevelFile(dir, levelDatBackupFile); err != nil {
		t.Fatalf("level.dat_old is not readable after the save: %v", err)
	}
}

// TestLoadLevelReadsBackupWhenPrimaryMissing checks the crash window between
// the two rotations: a missing level.dat with a level.dat_old beside it reads
// the backup rather than starting a new world.
func TestLoadLevelReadsBackupWhenPrimaryMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := defaultLevel("first")
	first.Spawn = SpawnPos{X: 4, Y: 66, Z: 9}
	first.raw["SpawnX"] = nbt.Int(4)
	first.raw["SpawnY"] = nbt.Int(66)
	first.raw["SpawnZ"] = nbt.Int(9)
	if err := SaveLevel(dir, first); err != nil {
		t.Fatalf("first SaveLevel: %v", err)
	}
	if err := SaveLevel(dir, defaultLevel("second")); err != nil {
		t.Fatalf("second SaveLevel: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, levelDatFile)); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	level, err := LoadLevel(dir)
	if err != nil {
		t.Fatalf("LoadLevel: %v", err)
	}
	if !level.existed || level.Spawn != first.Spawn {
		t.Errorf("level spawn = %+v, want the backup's %+v", level.Spawn, first.Spawn)
	}
}

// TestLoadLevelRestoresBackupWhenPrimaryMissing checks that reading a missing
// level.dat from the backup also puts the backup back as level.dat, so the next
// save does not remove the only good copy before the temp file lands.
func TestLoadLevelRestoresBackupWhenPrimaryMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := defaultLevel("first")
	first.Spawn = SpawnPos{X: 4, Y: 66, Z: 9}
	first.raw["SpawnX"] = nbt.Int(4)
	first.raw["SpawnY"] = nbt.Int(66)
	first.raw["SpawnZ"] = nbt.Int(9)
	if err := SaveLevel(dir, first); err != nil {
		t.Fatalf("first SaveLevel: %v", err)
	}
	if err := SaveLevel(dir, defaultLevel("second")); err != nil {
		t.Fatalf("second SaveLevel: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, levelDatFile)); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := LoadLevel(dir); err != nil {
		t.Fatalf("LoadLevel: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, levelDatFile)); err != nil {
		t.Errorf("level.dat is not restored from the backup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, levelDatBackupFile)); !os.IsNotExist(err) {
		t.Errorf("level.dat_old survives the fallback load, want it moved back to level.dat")
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

// TestNewWorldRefusesUnreadableLevelData checks that opening a world whose
// level.dat and level.dat_old are both unreadable fails, so the caller does
// not start a fresh world over the damaged one.
func TestNewWorldRefusesUnreadableLevelData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{levelDatFile, "level.dat_old"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not gzipped nbt"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	if w, err := NewWorld(dir, NewFlatGenerator()); err == nil {
		w.Close()
		t.Fatal("NewWorld accepted two unreadable files as a new world")
	}
}

// TestNewWorldKeepsSeedFromBackup checks the issue's own case: a truncated
// level.dat with a readable level.dat_old keeps the world's seed, so new
// chunks are not generated from a seed minted over the damaged file.
func TestNewWorldKeepsSeedFromBackup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := NewWorld(dir, NewGenerator(dir, "minecraft:normal", "5", "{}"))
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Save()
	w.Save()
	want := w.level.Seed
	w.Close()

	path := filepath.Join(dir, levelDatFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.WriteFile(path, raw[:len(raw)/2], 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	again, err := NewWorld(dir, NewGenerator(dir, "minecraft:normal", "999999", "{}"))
	if err != nil {
		t.Fatalf("NewWorld after truncation: %v", err)
	}
	defer again.Close()
	if again.level.Seed != want {
		t.Errorf("seed after truncation = %d, want the saved seed %d", again.level.Seed, want)
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

// TestSpawnKeepClearFollowsTheSpawnDimension checks the natural spawn's
// keep-clear zone guards only the dimension the spawn lies in.
func TestSpawnKeepClearFollowsTheSpawnDimension(t *testing.T) {
	t.Parallel()
	over, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	defer over.Close()
	nether := NewDimensionWorld(t.TempDir(), true, NewNetherGenerator(1))
	nether.FollowSpawnOf(over)

	if _, _, _, g := over.SpawnPoint(); !g {
		t.Error("the overworld is not guarded with the spawn in it")
	}
	if _, _, _, g := nether.SpawnPoint(); g {
		t.Error("the nether is guarded with the spawn in the overworld")
	}
	over.SetSpawnIn(SpawnPos{X: 8, Y: 40, Z: 8}, NetherName)
	if _, _, _, g := over.SpawnPoint(); g {
		t.Error("the overworld is guarded with the spawn in the nether")
	}
	x, _, z, g := nether.SpawnPoint()
	if !g || x != 8 || z != 8 {
		t.Errorf("nether SpawnPoint = (%d, %d, guarded %v), want (8, 8, true)", x, z, g)
	}
}

// TestWorldSpawnReadsDoNotRaceSetSpawn moves the spawn on one goroutine while
// two others read it, for the race detector: /setworldspawn runs on its
// sender's goroutine while joins, respawns and the tick read the spawn.
func TestWorldSpawnReadsDoNotRaceSetSpawn(t *testing.T) {
	t.Parallel()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	defer w.Close()

	const turns = 500
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := range turns {
			name := DimensionName
			if i%2 == 1 {
				name = NetherName
			}
			w.SetSpawnIn(SpawnPos{X: int32(i), Y: 64, Z: int32(i)}, name)
		}
	}()
	go func() {
		defer wg.Done()
		for range turns {
			w.Spawn()
			w.SpawnDimension()
			w.SpawnPoint()
		}
	}()
	go func() {
		defer wg.Done()
		for range turns {
			pos, dim := w.SpawnIn()
			// Before the first write the spawn is the flat world's own, in the
			// overworld; after it the writer pairs odd X with the nether.
			if (pos.X%2 != 0) != (dim == NetherName) {
				t.Errorf("SpawnIn = (%+v, %q), a pair never set together", pos, dim)
				return
			}
		}
	}()
	wg.Wait()
}
