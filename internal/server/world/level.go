package world

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// levelDatFile is the world's metadata (spawn, game type, seed, generator,
// time), the first file the game reads. A save writes levelDatTempFile and
// rotates the previous file to levelDatBackupFile first, so an interrupted
// write can be recovered.
const (
	levelDatFile            = "level.dat"
	levelDatBackupFile      = "level.dat_old"
	levelDatTempFile        = "level.dat_tmp"
	levelDatCorruptedPrefix = "level.dat_corrupted_"
)

// The difficulties, as level.dat's Difficulty byte and Change Difficulty number
// them; exported for the survival code and the config.
const (
	DifficultyPeaceful uint8 = 0
	DifficultyEasy     uint8 = 1
	DifficultyNormal   uint8 = 2
	DifficultyHard     uint8 = 3
)

// The generator IDs level.dat stores. This server writes GeneratorFlat; a
// vanilla default world carries GeneratorDefault, which is read but not acted
// on.
const (
	generatorDefault     = 0
	generatorFlat        = 1
	generatorLargeBiomes = 2
)

// SpawnPos is the block new players spawn on: at its centre (X+0.5, Z+0.5) and
// this Y. Yaw and Pitch, in degrees, are the stored rotation /setworldspawn
// sets.
type SpawnPos struct {
	X, Y, Z    int32
	Yaw, Pitch float32
}

// normalized folds the rotation as RespawnData.of does: yaw into [-180, 180),
// pitch clamped to [-90, 90].
// vanilla: Mth.wrapDegrees, Mth.clamp
func (s SpawnPos) normalized() SpawnPos {
	s.Yaw = WrapDegrees(s.Yaw)
	s.Pitch = clampPitch(s.Pitch)
	return s
}

// WrapDegrees folds an angle into [-180, 180). vanilla: Mth.wrapDegrees
func WrapDegrees(v float32) float32 {
	w := math.Mod(float64(v), 360)
	switch {
	case w >= 180:
		w -= 360
	case w < -180:
		w += 360
	}
	return float32(w)
}

func clampPitch(v float32) float32 {
	return min(max(v, -90), 90)
}

// LevelData is what this server uses of level.dat (spawn, time, difficulty);
// the rest is carried as raw NBT so a vanilla world's game type, seed and
// anything else unmodelled survive a save.
type LevelData struct {
	Spawn SpawnPos
	// SpawnDim is the dimension the spawn is in; empty is the overworld.
	SpawnDim string
	// WorldAge is level.dat's Time: the ticks the world has run, which always
	// advances and no command changes.
	WorldAge int64
	// DayTime, Rate and Paused are the overworld clock as older files held it
	// (DayTime from vanilla before world_clocks.dat, Rate and Paused from an
	// earlier build of this server). They only seed the clock of a world
	// without world_clocks.dat (clocks.go) and are never written back.
	DayTime int64
	Rate    float32
	Paused  bool
	// Difficulty is level.dat's Difficulty byte: 0 peaceful, 1 easy, 2 normal,
	// 3 hard. A new world starts at easy or server.properties' value at
	// creation; /difficulty changes it and it persists.
	Difficulty uint8
	// DifficultyLocked is level.dat's DifficultyLocked byte; only the options
	// menu's lock button changes it.
	DifficultyLocked bool
	// Hardcore is difficulty_settings' hardcore flag; a hardcore world is
	// always hard.
	Hardcore bool
	// The weather is saved to weather.dat (weather.go); a world without one
	// seeds it from the older level.dat fields of the same meaning.
	//
	// Raining is whether it is raining.
	Raining bool
	// RainTime counts down the ticks until the rain cycle turns over; at zero
	// the rain toggles and the dice roll again.
	RainTime int32
	// Thundering is whether a thunderstorm rides on the rain; it means nothing
	// without rain.
	Thundering bool
	// ThunderTime is how many ticks until the thunder cycle next turns over.
	ThunderTime int32
	// ClearWeatherTime is the ticks the weather is held clear (/weather clear
	// <duration>); while above zero the rain cycle is suspended.
	ClearWeatherTime int32
	// Seed is the generation seed from world_gen_settings.dat, or the level.dat
	// fields older worlds used. HasSeed reports whether there was one; a world
	// without gets a seed from the first seeded generator, written back so it
	// sticks.
	Seed    int64
	HasSeed bool
	// raw is the Data compound as read, spawn included, so a save loses nothing
	// vanilla wrote; nil for a new world, which gets this server's defaults on
	// shutdown. Time and difficulty are written into it before every save; the
	// weather lives in weather.dat and the game rules in game_rules.dat; the
	// rest is untouched.
	raw nbt.Compound
	// existed is whether level.dat was found at load. Either way Save writes
	// one, so the advanced Time outlives a restart.
	existed bool
}

// defaultLevel is a world without level.dat: the flat spawn, creative, easy,
// clear weather and this server's data version, written so vanilla can open the
// world. Weather times start at 0, so the first weather tick rolls the dice.
func defaultLevel(name string) *LevelData {
	data := nbt.Compound{
		"SpawnX":           nbt.Int(0),
		"SpawnY":           nbt.Int(GroundLevel),
		"SpawnZ":           nbt.Int(0),
		"GameType":         nbt.Int(1), // creative until the server sets server.properties' gamemode
		"Generator":        nbt.Int(generatorFlat),
		"RandomSeed":       nbt.Long(0),
		"MapFeatures":      nbt.Byte(0),
		"LevelName":        nbt.String(name),
		"Difficulty":       nbt.Byte(DifficultyEasy),
		"DifficultyLocked": nbt.Byte(0),
		"allowCommands":    nbt.Byte(1),
		"initialized":      nbt.Byte(1),
		"DataPacks": nbt.Compound{
			"Enabled": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("vanilla")}},
			"Disabled": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
				nbt.String("minecart_improvements"), nbt.String("redstone_experiments"), nbt.String("trade_rebalance"),
			}},
		},
		// The border, clocks and weather live in their own .dat files, so a
		// A level.dat holds none of them; Time stays here.
		"Time":                       nbt.Long(0),
		"GameRules":                  nbt.Compound{},
		"WanderingTraderSpawnChance": nbt.Int(25),
		"WanderingTraderSpawnDelay":  nbt.Int(24000),
		"WanderingTraderId":          nbt.IntArray{0, 0, 0, 0},
	}
	return &LevelData{
		Spawn:      SpawnPos{X: 0, Y: GroundLevel, Z: 0},
		raw:        data,
		existed:    false,
		WorldAge:   0,
		DayTime:    0,
		Rate:       1,
		Paused:     false,
		Difficulty: DifficultyEasy,
	}
}

// readLevelFile reads the Data compound of a world level data file; a missing
// file returns fs.ErrNotExist.
func readLevelFile(dir, name string) (nbt.Compound, error) {
	path := filepath.Join(dir, name)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("decompress %s: %w", path, err)
	}
	defer gz.Close()
	data, err := io.ReadAll(io.LimitReader(gz, maxLevelDat+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxLevelDat {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxLevelDat)
	}

	_, tag, err := nbt.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	root, ok := tag.(nbt.Compound)
	if !ok {
		return nil, fmt.Errorf("%s root is a %s, want a compound", path, tag.Type())
	}
	dataTag, ok := nbt.Get[nbt.Compound](root, "Data")
	if !ok {
		return nil, fmt.Errorf("%s has no Data compound", path)
	}
	return dataTag, nil
}

// corruptedLevelDatName is the timestamped name a broken level.dat is moved
// aside to, vanilla's own.
// vanilla: LevelStorageAccess.corruptedDataFile, FileNameDateFormatter.FORMATTER
func corruptedLevelDatName(now time.Time) string {
	return levelDatCorruptedPrefix + now.Format("2006-01-02_15-04-05")
}

// restoreLevelDatFromBackup moves an unreadable level.dat aside (kept, not
// deleted) and restores level.dat_old, so the next save does not rotate the
// only good copy into the backup.
// vanilla: LevelStorageSource.restoreLevelDataFromOld, Util.safeReplaceOrMoveFile
func restoreLevelDatFromBackup(dir string) error {
	data := filepath.Join(dir, levelDatFile)
	backup := filepath.Join(dir, levelDatBackupFile)
	corrupted := filepath.Join(dir, corruptedLevelDatName(time.Now()))
	// A missing level.dat (between the old file's rotation and the temp's
	// rename) leaves nothing to set aside.
	moved := true
	if err := os.Rename(data, corrupted); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("move broken %s aside: %w", levelDatFile, err)
		}
		moved = false
	}
	if err := os.Rename(backup, data); err != nil {
		// Leave the world as it was found if the backup cannot take its place.
		if moved {
			if restoreErr := os.Rename(corrupted, data); restoreErr != nil {
				return fmt.Errorf("restore %s: %w (moving the broken file back: %v)", levelDatFile, err, restoreErr)
			}
		}
		return fmt.Errorf("restore %s: %w", levelDatFile, err)
	}
	return nil
}

// LoadLevel reads level.dat from dir. Only when neither level.dat nor
// level.dat_old exists is the world new: it reads as the defaults with existed
// false, so the caller writes one on save. An unreadable file falls back to the
// other, erring only when neither reads.
// vanilla: LevelStorageAccess.getUnfixedDataTagWithFallback
func LoadLevel(dir string) (*LevelData, error) {
	dataTag, err := readLevelFile(dir, levelDatFile)
	if err != nil {
		backup, backupErr := readLevelFile(dir, levelDatBackupFile)
		switch {
		case backupErr == nil:
			// An unreadable level.dat is an interrupted write, a missing one
			// the window after rotation; either way the backup stands in, and
			// restoring it keeps the next save from rotating the only good copy
			// away.
			if !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("level.dat is unreadable, reading the backup", "error", err)
			}
			if restoreErr := restoreLevelDatFromBackup(dir); restoreErr != nil {
				slog.Warn("could not restore level.dat from the backup", "error", restoreErr)
			}
			dataTag = backup
		case errors.Is(err, fs.ErrNotExist) && errors.Is(backupErr, fs.ErrNotExist):
			return defaultLevel(""), nil
		case errors.Is(backupErr, fs.ErrNotExist):
			return nil, err
		default:
			return nil, backupErr
		}
	}

	// A level.dat with no spawn (one holding only a seed, say) spawns at the
	// flat surface like a new world, not at the zero value's floor, which would
	// bury the player in stone.
	level := &LevelData{raw: dataTag, existed: true,
		Spawn: SpawnPos{X: 0, Y: GroundLevel, Z: 0}}
	// The level.dat keeps the spawn in a spawn compound. Vanilla drops SpawnX/Y/Z on
	// save while this server writes both, so where both exist the older fields
	// are the newer write.
	if spawn, ok := nbt.Get[nbt.Compound](dataTag, "spawn"); ok {
		if pos, ok := nbt.Get[nbt.IntArray](spawn, "pos"); ok && len(pos) == 3 {
			level.Spawn = SpawnPos{X: pos[0], Y: pos[1], Z: pos[2]}
		}
		if yaw, ok := nbt.Get[nbt.Float](spawn, "yaw"); ok {
			level.Spawn.Yaw = float32(yaw)
		}
		if pitch, ok := nbt.Get[nbt.Float](spawn, "pitch"); ok {
			level.Spawn.Pitch = float32(pitch)
		}
		// Fold a rotation the writer left out of range, as the spawn is stored
		// and sent.
		level.Spawn = level.Spawn.normalized()
		if dim, ok := nbt.Get[nbt.String](spawn, "dimension"); ok {
			level.SpawnDim = string(dim)
		}
	}
	if x, ok := nbt.Get[nbt.Int](dataTag, "SpawnX"); ok {
		level.Spawn.X = int32(x)
	}
	if y, ok := nbt.Get[nbt.Int](dataTag, "SpawnY"); ok {
		level.Spawn.Y = int32(y)
	}
	if z, ok := nbt.Get[nbt.Int](dataTag, "SpawnZ"); ok {
		level.Spawn.Z = int32(z)
	}
	// Time, the world's age in ticks, rides the raw compound and is written
	// back, so the age survives a restart.
	if t, ok := nbt.Get[nbt.Long](dataTag, "Time"); ok {
		level.WorldAge = int64(t)
	}
	// DayTime, ClockRate and ClockPaused are older clock fields, read only to
	// seed a world without world_clocks.dat (clocks.go).
	if t, ok := nbt.Get[nbt.Long](dataTag, "DayTime"); ok {
		level.DayTime = int64(t)
	}
	level.Rate = 1
	if r, ok := nbt.Get[nbt.Float](dataTag, "ClockRate"); ok {
		level.Rate = float32(r)
	}
	if p, ok := nbt.Get[nbt.Byte](dataTag, "ClockPaused"); ok {
		level.Paused = p != 0
	}
	// The seed: world_gen_settings.dat's since 26.1, else whichever level.dat
	// field the writer used.
	if seed, err := readWorldGen(dir); err == nil {
		level.Seed, level.HasSeed = seed, true
	} else if seed, ok := worldSeed(dataTag); ok {
		level.Seed, level.HasSeed = seed, true
	}
	// A level.dat without a Difficulty field came from a game that tracked
	// none, so easy stands in. The level.dat keeps difficulty and lock in
	// difficulty_settings; the older fields override it, as with the spawn.
	level.Difficulty = DifficultyEasy
	if settings, ok := nbt.Get[nbt.Compound](dataTag, "difficulty_settings"); ok {
		if name, ok := nbt.Get[nbt.String](settings, "difficulty"); ok {
			if d, ok := difficultyByName[string(name)]; ok {
				level.Difficulty = d
			}
		}
		if l, ok := nbt.Get[nbt.Byte](settings, "locked"); ok {
			level.DifficultyLocked = l != 0
		}
		if h, ok := nbt.Get[nbt.Byte](settings, "hardcore"); ok {
			level.Hardcore = h != 0
		}
	}
	if d, ok := nbt.Get[nbt.Byte](dataTag, "Difficulty"); ok && d <= nbt.Byte(DifficultyHard) {
		level.Difficulty = uint8(d)
	}
	if l, ok := nbt.Get[nbt.Byte](dataTag, "DifficultyLocked"); ok {
		level.DifficultyLocked = l != 0
	}
	// vanilla: LevelDatDifficultyFix moves an older level.dat's top-level
	// hardcore byte into difficulty_settings.
	if h, ok := nbt.Get[nbt.Byte](dataTag, "hardcore"); ok {
		level.Hardcore = h != 0
	}
	if level.Hardcore {
		level.Difficulty = DifficultyHard
	}
	// Older weather fields, read only as the fallback for a world without
	// weather.dat (weather.go).
	if r, ok := nbt.Get[nbt.Byte](dataTag, "raining"); ok {
		level.Raining = r != 0
	}
	if t, ok := nbt.Get[nbt.Int](dataTag, "rainTime"); ok {
		level.RainTime = int32(t)
	}
	if t, ok := nbt.Get[nbt.Byte](dataTag, "thundering"); ok {
		level.Thundering = t != 0
	}
	if t, ok := nbt.Get[nbt.Int](dataTag, "thunderTime"); ok {
		level.ThunderTime = int32(t)
	}
	if t, ok := nbt.Get[nbt.Int](dataTag, "clearWeatherTime"); ok {
		level.ClearWeatherTime = int32(t)
	}
	// Only Y is clamped: a spawn outside the buildable area keeps X and Z, so a
	// save does not rewrite them.
	level.Spawn.Y = min(max(level.Spawn.Y, worldMinY), worldMinY+worldHeight-1)
	return level, nil
}

// worldSeed reads the seed from level.dat's Data for a world without
// world_gen_settings.dat: WorldGenSettings.seed (before 26.1), then the older
// RandomSeed. Zero does not count: a fresh flat default carries it, and
// honouring it would give every legacy world the same terrain.
func worldSeed(data nbt.Compound) (int64, bool) {
	if settings, ok := nbt.Get[nbt.Compound](data, "WorldGenSettings"); ok {
		if seed, ok := nbt.Get[nbt.Long](settings, "seed"); ok && seed != 0 {
			return int64(seed), true
		}
	}
	if seed, ok := nbt.Get[nbt.Long](data, "RandomSeed"); ok && seed != 0 {
		return int64(seed), true
	}
	return 0, false
}

// putSeed stores the seed in the raw compound for the next SaveLevel to write
// into the fields worldSeed reads (vanilla reads world_gen_settings.dat, which
// ensureWorldGen writes), so a seed chosen at creation survives a restart.
func (level *LevelData) putSeed(seed int64) {
	if level.raw == nil {
		return
	}
	level.Seed, level.HasSeed = seed, true
	level.raw["RandomSeed"] = nbt.Long(seed)
	settings, ok := nbt.Get[nbt.Compound](level.raw, "WorldGenSettings")
	if !ok {
		settings = nbt.Compound{}
		level.raw["WorldGenSettings"] = settings
	}
	settings["seed"] = nbt.Long(seed)
}

// SaveLevel writes level.dat to dir: the Data compound in an unnamed root,
// gzipped as the game reads it.
func SaveLevel(dir string, level *LevelData) error {
	if level.raw == nil {
		return fmt.Errorf("level data has no compound to write")
	}
	return writeLevelCompound(dir, level.stamped())
}

// writeLevelCompound writes a Data compound SaveLevel's way.
func writeLevelCompound(dir string, compound nbt.Compound) error {
	data, err := nbt.Encode("", nbt.Compound{"Data": compound})
	if err != nil {
		return fmt.Errorf("encode level.dat: %w", err)
	}

	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(data); err != nil {
		return fmt.Errorf("compress level.dat: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("compress level.dat: %w", err)
	}

	path := filepath.Join(dir, levelDatFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	// Write a temp file, rotate level.dat to level.dat_old, then move the temp
	// in, so a crash mid-write leaves the previous file whole in the backup.
	// vanilla: Util.safeReplaceFile
	tmp := filepath.Join(dir, levelDatTempFile)
	// Sync the temp before the rename, or a power loss right after can leave an
	// empty level.dat.
	// vanilla: NbtIo.writeCompressed uses StandardOpenOption.SYNC
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if _, err := file.Write(compressed.Bytes()); err != nil {
		file.Close()
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	backup := filepath.Join(dir, levelDatBackupFile)
	if err := os.Remove(backup); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", backup, err)
	}
	if err := os.Rename(path, backup); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("rename %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Put the rotated file back rather than leave no level.dat at all.
		if restoreErr := os.Rename(backup, path); restoreErr != nil {
			return fmt.Errorf("rename %s: %w (restoring %s: %v)", tmp, err, levelDatBackupFile, restoreErr)
		}
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}

// levelStorageVersion is level.dat's version field, without which vanilla
// refuses a world.
// vanilla: PrimaryLevelData.setTagData
const levelStorageVersion = 19133

// difficultyByName maps difficulty_settings' names to the Difficulty byte.
var difficultyByName = map[string]uint8{
	"peaceful": DifficultyPeaceful, "easy": DifficultyEasy,
	"normal": DifficultyNormal, "hard": DifficultyHard,
}

// olderVersion reports a level.dat older than this server's data version, which vanilla upgrades on
// open, extracting its world-gen settings and game rules. No DataVersion means
// an older build of this server wrote it, not vanilla.
func (level *LevelData) olderVersion() bool {
	v, ok := nbt.Get[nbt.Int](level.raw, "DataVersion")
	return ok && v != 0 && v < dataVersion
}

// keepsRulesInLevel reports whether this data version keeps game rules in
// level.dat; 26.1 moved them to game_rules.dat, so an upgraded world holds none
// there.
// vanilla: LevelDatToSavedDataFileFix
func (level *LevelData) keepsRulesInLevel() bool {
	v, ok := nbt.Get[nbt.Int](level.raw, "DataVersion")
	return ok && v != 0 && v < gameRulesFileVersion
}

// stamped returns the raw compound with the current fields set from the level
// (versions, spawn compound, difficulty_settings) beside the older fields this
// server reads. An older version's level.dat keeps its own format so vanilla
// still upgrades it.
func (level *LevelData) stamped() nbt.Compound {
	out := maps.Clone(level.raw)
	out["LastPlayed"] = nbt.Long(time.Now().UnixMilli())
	// From 1.21.9 level.dat keeps the spawn in this compound, which vanilla
	// reads over SpawnX/Y/Z.
	// vanilla: WorldSpawnDataFix
	if old, ok := nbt.Get[nbt.Compound](level.raw, "spawn"); ok {
		spawn := maps.Clone(old)
		spawn["pos"] = nbt.IntArray{level.Spawn.X, level.Spawn.Y, level.Spawn.Z}
		spawn["yaw"] = nbt.Float(level.Spawn.Yaw)
		spawn["pitch"] = nbt.Float(level.Spawn.Pitch)
		if level.SpawnDim != "" {
			spawn["dimension"] = nbt.String(level.SpawnDim)
		}
		out["spawn"] = spawn
	}
	if level.olderVersion() {
		return out
	}
	out["DataVersion"] = nbt.Int(dataVersion)
	out["version"] = nbt.Int(levelStorageVersion)
	out["Version"] = nbt.Compound{
		"Id":       nbt.Int(dataVersion),
		"Name":     nbt.String("26.3"),
		"Series":   nbt.String("main"),
		"Snapshot": nbt.Byte(0),
	}

	if _, ok := out["spawn"]; !ok {
		out["spawn"] = nbt.Compound{
			"dimension": nbt.String(level.spawnDimension()),
			"pos":       nbt.IntArray{level.Spawn.X, level.Spawn.Y, level.Spawn.Z},
			"yaw":       nbt.Float(level.Spawn.Yaw),
			"pitch":     nbt.Float(level.Spawn.Pitch),
		}
	}

	settings := nbt.Compound{}
	if old, ok := nbt.Get[nbt.Compound](level.raw, "difficulty_settings"); ok {
		settings = maps.Clone(old)
	}
	settings["hardcore"] = nbt.Byte(boolByte(level.Hardcore))
	for name, d := range difficultyByName {
		if d == level.Difficulty {
			settings["difficulty"] = nbt.String(name)
		}
	}
	settings["locked"] = nbt.Byte(boolByte(level.DifficultyLocked))
	out["difficulty_settings"] = settings
	return out
}

// maxLevelDat bounds a level.dat's decompressed size: real ones are a few
// kilobytes, and a small crafted file could otherwise exhaust memory.
const maxLevelDat = 1 << 20

// Difficulty is level.dat's Difficulty: 0 peaceful, 1 easy, 2 normal, 3 hard.
// A plugin world has its own; the other dimensions answer the overworld's.
func (w *World) Difficulty() uint8 {
	t := w.timeWorld()
	t.timeMu.Lock()
	defer t.timeMu.Unlock()
	return t.level.Difficulty
}

// DifficultyLocked reports whether the difficulty is pinned (the options menu's
// lock).
func (w *World) DifficultyLocked() bool {
	t := w.timeWorld()
	t.timeMu.Lock()
	defer t.timeMu.Unlock()
	return t.level.DifficultyLocked
}

// ChangeDifficulty is SetDifficulty for the options menu, refused while locked
// (/difficulty ignores the lock); it reports whether it went through.
// vanilla: MinecraftServer.setDifficulty
func (w *World) ChangeDifficulty(d uint8) bool {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	if w.level.DifficultyLocked {
		return false
	}
	w.applyDifficulty(d)
	return true
}

// SetDifficultyLocked pins or frees the difficulty, saved with level.dat.
func (w *World) SetDifficultyLocked(locked bool) {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	w.level.DifficultyLocked = locked
}

// SetDifficulty sets the difficulty (/difficulty), at once for every player and
// persisted by the next save.
func (w *World) SetDifficulty(d uint8) {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	w.applyDifficulty(d)
}

// applyDifficulty is SetDifficulty with timeMu held.
func (w *World) applyDifficulty(d uint8) {
	if d > DifficultyHard || w.level.Hardcore {
		d = DifficultyHard
	}
	w.level.Difficulty = d
	w.refreshSpawnEnemies()
}

// SetDefaultDifficulty sets a new world's difficulty (server.properties at
// creation); a world already on disk keeps its stored one.
func (w *World) SetDefaultDifficulty(d uint8) {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	if w.level.existed || w.level.DifficultyLocked {
		return
	}
	if d > DifficultyHard || w.level.Hardcore {
		d = DifficultyHard
	}
	w.level.Difficulty = d
}

// SetupDebugLevel gives a new debug world its fixed settings: peaceful and
// locked, no weather advance, noon. The caller sets the spectator default
// game mode.
// vanilla: MinecraftServer.setupDebugLevel
func (w *World) SetupDebugLevel() {
	w.timeMu.Lock()
	w.level.Difficulty = DifficultyPeaceful
	w.level.DifficultyLocked = true
	w.refreshSpawnEnemies()
	w.timeMu.Unlock()
	w.SetBoolRule(RuleAdvanceWeather, false)
	w.MoveClockToMarker(OverworldClock, 6000, 24000) // noon of the 24000-tick day
}

// SetGameType records the default game mode (0 survival .. 3 spectator) as
// level.dat's GameType, written by the next save.
// vanilla: DedicatedServer.initServer, MinecraftServer.setDefaultGameType
func (w *World) SetGameType(mode int32) { w.setRaw("GameType", nbt.Int(mode)) }

// SetLevelName records level.dat's LevelName, the name the client's world list
// shows, written by the next save.
// vanilla: Main.createNewWorldData
func (w *World) SetLevelName(name string) { w.setRaw("LevelName", nbt.String(name)) }

func (w *World) setRaw(key string, v nbt.Tag) {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	if w.level.raw == nil {
		w.level.raw = nbt.Compound{}
	}
	w.level.raw[key] = v
}

// Hardcore reports a hardcore world: difficulty pinned to hard, death ending in
// spectator mode.
func (w *World) Hardcore() bool {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	return w.level.Hardcore
}

// SetDefaultHardcore makes a new world hardcore (server.properties at
// creation); an existing level.dat keeps its flag.
func (w *World) SetDefaultHardcore(h bool) {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	if w.level.existed {
		return
	}
	w.level.Hardcore = h
	if h {
		w.level.Difficulty = DifficultyHard
	}
}

// HasSeed reports whether the world has a generation seed, which /seed asks
// first.
func (w *World) HasSeed() bool {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	return w.level.HasSeed
}

// SeedValue is the world's seed, meaningful only when HasSeed.
func (w *World) SeedValue() int64 {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	return w.level.Seed
}

// SetSpawn moves the world spawn (/setworldspawn): where new players arrive and
// compasses point, saved with level.dat, with the rotation given or 0/0 (the
// command's ZERO_ROTATION).
func (w *World) SetSpawn(pos SpawnPos) { w.SetSpawnIn(pos, DimensionName) }

// SetSpawnIn moves the spawn into a dimension.
func (w *World) SetSpawnIn(pos SpawnPos, dimension string) {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	w.level.Spawn = pos
	w.level.SpawnDim = dimension
	if w.level.raw != nil {
		w.level.raw["SpawnX"] = nbt.Int(pos.X)
		w.level.raw["SpawnY"] = nbt.Int(pos.Y)
		w.level.raw["SpawnZ"] = nbt.Int(pos.Z)
		// The older SpawnX/Y/Z form's yaw, for a world still in that form; the
		// compound's yaw and pitch go out with stamped().
		w.level.raw["SpawnAngle"] = nbt.Float(pos.Yaw)
	}
}

// SpawnIn returns the spawn and its dimension as one consistent pair.
func (w *World) SpawnIn() (SpawnPos, string) {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	return w.level.Spawn, w.level.spawnDimension()
}

// RainingAt reports rain on the spot: it is raining, the sky reaches the cell
// (sky light 15) above the MOTION_BLOCKING heightmap, and the biome rains there
// rather than snows or stays dry. Worlds without their own weather never rain.
// vanilla: Level.isRainingAt
func (w *World) RainingAt(x, y, z int32) bool {
	return w.precipitationOn(x, y, z) == precipRain
}

// PrecipitatesAt reports rain or snow on the spot, whichever the biome has
// there.
// vanilla: Level.precipitationAt
func (w *World) PrecipitatesAt(x, y, z int32) bool {
	return w.precipitationOn(x, y, z) != precipNone
}

// precipitationOn is the biome's precipitation at the cell when it is raining
// and the sky reaches the cell above the MOTION_BLOCKING heightmap, else
// precipNone.
func (w *World) precipitationOn(x, y, z int32) precipitation {
	if !w.IsRaining() || w.SkyLightAt(x, y, z) != 15 || w.motionBlockingHeight(x, z) >= y {
		return precipNone
	}
	return w.precipitationAt(x, y, z)
}

// MotionBlockingHeight is motionBlockingHeight for the entity steps.
func (w *World) MotionBlockingHeight(x, z int32) int32 { return w.motionBlockingHeight(x, z) }

// motionBlockingHeight is the y of the column's highest MOTION_BLOCKING block
// (leaves and fluids included), or one below the floor for none or an unloaded
// chunk; the first free y above it is the rain's height check.
// vanilla: Heightmap.Types.MOTION_BLOCKING
func (w *World) motionBlockingHeight(x, z int32) int32 {
	empty := w.dim.minY - 1
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	lx, lz := x&15, z&15

	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.chunks[pos]
	if !ok || loaded.motion == nil {
		return empty
	}
	return loaded.motion[lx][lz]
}

// motionHeight is the y of one column's highest motion-blocking block, or one
// below the floor for none.
func (w *World) motionHeight(c *Chunk, lx, lz int32) int32 {
	for y := int32(worldMinY + worldHeight - 1); y >= worldMinY; {
		s := &c.sections[(y-worldMinY)/sectionSize]
		if s.isUniform(airState) {
			// An all-air section holds nothing: skip below its floor in one
			// step.
			y = worldMinY + (y-worldMinY)/sectionSize*sectionSize - 1
			continue
		}
		if motionBlockingByState[c.BlockAt(lx, y, lz)] {
			return y
		}
		y--
	}
	return w.dim.minY - 1
}

// primeMotion builds a chunk's whole MOTION_BLOCKING heightmap, run by a chunk
// worker during preparation so a ticking chunk's rain never pays for the scan
// on the tick goroutine.
func (w *World) primeMotion(c *Chunk) *[16][16]int32 {
	motion := new([16][16]int32)
	for lx := range int32(16) {
		for lz := range int32(16) {
			motion[lx][lz] = w.motionHeight(c, lx, lz)
		}
	}
	return motion
}

// motionBlockingByState is blocksMotionInHeightmap for every state, so a scan
// is one array read per cell instead of a fluid-map miss and two name lookups
// for air.
var motionBlockingByState = func() [65536]bool {
	var m [65536]bool
	for state := range m {
		m[state] = blocksMotionInHeightmap(uint16(state))
	}
	return m
}()

// blocksMotionInHeightmap reports MOTION_BLOCKING: any fluid, or a block in
// blocks_motion_in_heightmap.
// vanilla: Heightmap.Types.MOTION_BLOCKING
func blocksMotionInHeightmap(state uint16) bool {
	return fluidHolding(state).kind != fluidNone || heightmapSolid(state)
}

// MonstersBurn reads gameplay/monsters_burn from the dimension's layers.
func (w *World) MonstersBurn() bool {
	return w.environmentGlobal(envMonstersBurn) != 0
}

// PiglinsZombify is gameplay/piglins_zombify: true by default, false in the
// Nether's dimension type.
func (w *World) PiglinsZombify() bool { return w.dimName != NetherName }

// SkyDarken is Level.getSkyDarken: 15 less the dimension's sky_light_level,
// truncated. In the overworld's type a clear day darkens 0 and a clear night
// 11, settled rain a day by 3 and a thunderstorm by 5; the nether's type sets the level to 4 (darken 11), and
// the end's keeps the default 15.
// vanilla: Level.updateSkyBrightness
func (w *World) SkyDarken() int {
	return int(float32(15) - w.envValue(&w.skyLight, ""))
}

// HasSkyLight is the dimension type's has_skylight: the overworld only.
func (w *World) HasSkyLight() bool {
	return w.dimName == DimensionName
}

// BrightOutside is Level.isBrightOutside: the clock runs and the sky is
// darkened by less than 4. A clear day darkens 0, a clear night 11, and
// arrived rain 3, which stays inside 4; the fixed-time nether and end are never
// bright. The mob simulation's sun goals read this before the light at the mob;
// the burning daylight is a different attribute of the same timeline, and the
// two need not agree.
func (w *World) BrightOutside() bool {
	return !w.dim.fixedTime && w.SkyDarken() < 4
}

// DarkOutside is Level.isDarkOutside: the clock runs and the sky is not
// bright, so never in the fixed-time nether and end.
func (w *World) DarkOutside() bool {
	return !w.dim.fixedTime && !w.BrightOutside()
}

// MaxLocalRawLightAt is LevelReader.getMaxLocalRawBrightness: the brighter of
// block light and the sky light less the hour's darkening, the raw light
// light-dependent mobs (the spider) read through their brightness curve and the
// monster spawn's darkness check takes. 0 in an unloaded chunk.
func (w *World) MaxLocalRawLightAt(x, y, z int32) uint8 {
	block := w.BlockLightAt(x, y, z)
	sky := max(int(w.SkyLightAt(x, y, z))-w.SkyDarken(), 0)
	if int(block) > sky {
		return block
	}
	return uint8(sky)
}

// MoonPhase is the moon face 0-7 (0 full, waning a step a day), the index the
// moon timeline's eight-day tracks step through, including the surface slime's
// chance the spawn rule reads.
func (w *World) MoonPhase() int {
	w.timeMu.Lock()
	dayTime := w.clock(OverworldClock).total
	w.timeMu.Unlock()
	return int(dayTime / ticksPerDay % 8)
}

// AmbientLight is the dimension type's minecraft:ambient_light: 0 for the
// overworld and end, 0.1 for the nether. It is the base the light-dependent
// magic value lerps from, so the same light reads a tenth brighter there.
func (w *World) AmbientLight() float32 {
	switch w.dimName {
	case NetherName:
		return 0.1
	default:
		return 0
	}
}

// WakeUpFromSleep jumps the named clock to the next wake_up_from_sleep
// marker (tick 0 of the day), ServerLevel.moveToTimeMarker(WAKE_UP_FROM_SLEEP).
// The jump is durationToNext: always positive, a full day when already on the
// marker. A rate below one's fractional ticks are dropped, since a jump sets
// the clock absolutely.
func (w *World) WakeUpFromSleep(clock string) {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	overworld := w.clock(clock)
	overworld.total += ticksPerDay - overworld.total%ticksPerDay
	overworld.fractional = 0
}

// syncLevelData writes what this server changes (age, difficulty, and game
// rules for a world still keeping them in level.dat) into the raw compound,
// leaving everything else vanilla keeps, and returns it, under the setters'
// lock. Clocks and weather go to world_clocks.dat and weather.dat.
func (w *World) syncLevelData() nbt.Compound {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	if w.level.raw == nil {
		return nil
	}
	w.level.raw["Time"] = nbt.Long(w.level.WorldAge)
	w.level.raw["Difficulty"] = nbt.Byte(w.level.Difficulty)
	w.level.raw["DifficultyLocked"] = nbt.Byte(boolByte(w.level.DifficultyLocked))
	// Vanilla keeps the weather in weather.dat; drop the older level.dat fields so
	// a stale copy cannot outlive it.
	// vanilla: LevelDatToSavedDataPreparationFix
	for _, key := range [...]string{"raining", "rainTime", "thundering", "thunderTime", "clearWeatherTime"} {
		delete(w.level.raw, key)
	}
	if w.rules != nil && w.level.keepsRulesInLevel() {
		rulesToLevel(w.rules, w.level.raw)
	}
	return w.level.stamped()
}

// snowGolemMeltBiomes set gameplay/snow_golem_melts: desert, savannas and
// badlands.
// vanilla: OverworldBiomes.desert, savanna, badlands
var snowGolemMeltBiomes = [registry.BiomeCount]bool{
	registry.BiomeDesert:           true,
	registry.BiomeSavanna:          true,
	registry.BiomeSavannaPlateau:   true,
	registry.BiomeWindsweptSavanna: true,
	registry.BiomeBadlands:         true,
	registry.BiomeErodedBadlands:   true,
	registry.BiomeWoodedBadlands:   true,
}

// SnowGolemMelts is gameplay/snow_golem_melts at the cell: everywhere in the
// nether (its dimension type), elsewhere by biome.
// vanilla: DimensionTypes, SnowGolem.aiStep
func (w *World) SnowGolemMelts(x, y, z int32) bool {
	if w.dimName == NetherName {
		return true
	}
	b := w.NoiseBiome(x, y, z)
	return b >= 0 && b < registry.BiomeCount && snowGolemMeltBiomes[b]
}

// SnowLayerSurvives is SnowLayerBlock.canSurvive for one layer, checked before
// a snow golem's trail lays it.
func (w *World) SnowLayerSurvives(x, y, z int32) bool {
	return w.Survives(x, y, z, snowByLayers[1])
}

// IsAirAt is Block.isAir at the cell.
func (w *World) IsAirAt(x, y, z int32) bool {
	return IsKnownAirState(w.BlockAt(x, y, z))
}

// AirIn reports whether every cell from floor(min) to floor(max) of the box is
// air; unloaded chunks read as air.
// vanilla: ServerGamePacketListenerImpl.noBlocksAround
func (w *World) AirIn(box [6]float64) bool {
	cell := func(lo, hi float64) (int32, int32) {
		return int32(math.Floor(lo)), int32(math.Floor(hi))
	}
	x0, x1 := cell(box[0], box[3])
	y0, y1 := cell(box[1], box[4])
	z0, z1 := cell(box[2], box[5])
	for _, s := range w.BlocksIn(x0, y0, z0, x1, y1, z1, nil) {
		if !IsKnownAirState(s) {
			return false
		}
	}
	return true
}

// MobGriefing is the mob_griefing game rule.
func (w *World) MobGriefing() bool { return w.BoolRule(RuleMobGriefing) }

// MaxEntityCramming is the max_entity_cramming rule.
func (w *World) MaxEntityCramming() int32 { return w.IntRule(RuleMaxCramming) }

// spawnDimension is the spawn's dimension, the overworld if none is stored.
func (level *LevelData) spawnDimension() string {
	if level.SpawnDim == "" {
		return DimensionName
	}
	return level.SpawnDim
}
