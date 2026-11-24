package world

import (
	"log/slog"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
)

// pluginMetaFile is what a plugin-created dimension keeps of its own: the
// seed it was created with and its spawn. A dimension has no level.dat.
const pluginMetaFile = "data/minecraft/plugin_world.dat"

// PluginMeta is a plugin world's saved seed and spawn. Level is its age,
// clocks, weather and difficulty, nil in a file written before a plugin world
// kept them.
type PluginMeta struct {
	Seed  int64
	Spawn SpawnPos
	// Gen is how the world's terrain was asked for, so a reload rebuilds it.
	Gen   PluginGenSpec
	Level *pluginLevel
}

// pluginLevel is the level state a plugin dimension keeps of its own.
type pluginLevel struct {
	age        int64
	difficulty uint8
	locked     bool
	weather    nbt.Compound
	clocks     nbt.Compound
}

// ReadPluginMeta reads the dimension's plugin_world.dat under the world in dir,
// before the world is opened; ok is false when the dimension is new.
func ReadPluginMeta(dir, name string) (m PluginMeta, ok bool) {
	data, err := readSavedData(DimensionDir(dir, name), pluginMetaFile)
	if err != nil {
		return m, false
	}
	seed, ok1 := nbt.Get[nbt.Long](data, "seed")
	pos, ok2 := nbt.Get[nbt.IntArray](data, "spawn")
	if !ok1 || !ok2 || len(pos) != 3 {
		return m, false
	}
	yaw, _ := nbt.Get[nbt.Float](data, "yaw")
	pitch, _ := nbt.Get[nbt.Float](data, "pitch")
	typ, _ := nbt.Get[nbt.String](data, "type")
	settings, _ := nbt.Get[nbt.String](data, "generator_settings")
	biome, _ := nbt.Get[nbt.String](data, "biome")
	noStructures, _ := nbt.Get[nbt.Byte](data, "no_structures")
	bonusChest, _ := nbt.Get[nbt.Byte](data, "bonus_chest")
	m = PluginMeta{Seed: int64(seed), Spawn: SpawnPos{X: pos[0], Y: pos[1], Z: pos[2], Yaw: float32(yaw), Pitch: float32(pitch)},
		Gen: PluginGenSpec{Type: string(typ), Settings: string(settings), Biome: string(biome), NoStructures: noStructures != 0, BonusChest: bonusChest != 0}}
	if lv, ok := nbt.Get[nbt.Compound](data, "level"); ok {
		age, _ := nbt.Get[nbt.Long](lv, "age")
		d, _ := nbt.Get[nbt.Byte](lv, "difficulty")
		locked, _ := nbt.Get[nbt.Byte](lv, "difficulty_locked")
		clocks, _ := nbt.Get[nbt.Compound](lv, "clocks")
		m.Level = &pluginLevel{
			age: int64(age), difficulty: min(uint8(d), DifficultyHard), locked: locked != 0,
			weather: lv, clocks: clocks,
		}
	}
	return m, true
}

// SetPluginGen records how the world's terrain was asked for, which
// SavePluginMeta writes.
func (w *World) SetPluginGen(gen PluginGenSpec) {
	w.timeMu.Lock()
	w.pluginGen = gen
	w.timeMu.Unlock()
}

// SavePluginMeta writes the world's plugin_world.dat: its seed, spawn, terrain
// spec and level state as they stand.
func (w *World) SavePluginMeta() {
	w.timeMu.Lock()
	l := w.level
	level := w.weatherCompound()
	level["age"], level["difficulty"] = nbt.Long(l.WorldAge), nbt.Byte(l.Difficulty)
	level["difficulty_locked"], level["clocks"] = nbt.Byte(boolByte(l.DifficultyLocked)), w.clocksCompound()
	data := nbt.Compound{
		"seed": nbt.Long(l.Seed), "spawn": nbt.IntArray{l.Spawn.X, l.Spawn.Y, l.Spawn.Z},
		"yaw": nbt.Float(l.Spawn.Yaw), "pitch": nbt.Float(l.Spawn.Pitch),
		"level": level,
		"type":  nbt.String(w.pluginGen.Type), "generator_settings": nbt.String(w.pluginGen.Settings), "biome": nbt.String(w.pluginGen.Biome),
	}
	if w.pluginGen.NoStructures {
		data["no_structures"] = nbt.Byte(1)
	}
	if w.pluginGen.BonusChest {
		data["bonus_chest"] = nbt.Byte(1)
	}
	w.timeMu.Unlock()
	if err := writeSavedData(w.storage, pluginMetaFile, data); err != nil {
		slog.Error("could not save plugin_world.dat", "dimension", w.dimName, "error", err)
	}
}

// SetOwnSeedAndSpawn gives a dimension without a level.dat a seed and a spawn
// of its own, which SeedValue and SpawnIn then answer.
func (w *World) SetOwnSeedAndSpawn(seed int64, spawn SpawnPos) {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	w.level.Seed, w.level.HasSeed = seed, true
	w.zoomSeed = levelgen.BiomeZoomSeed(seed)
	w.level.Spawn, w.level.SpawnDim = spawn, w.dimName
}

// RestoreLevel puts a saved plugin world's age, clocks, weather and difficulty
// back; a nil level (a new world) keeps the defaults.
func (w *World) RestoreLevel(m PluginMeta) {
	l := m.Level
	if l == nil {
		return
	}
	w.timeMu.Lock()
	w.setWorldAge(l.age)
	w.level.Difficulty, w.level.DifficultyLocked = l.difficulty, l.locked
	applyWeather(l.weather, w.level)
	for i := range w.clocks {
		if entry, ok := nbt.Get[nbt.Compound](l.clocks, w.clocks[i].name); ok {
			readClockEntry(entry, &w.clocks[i].state)
		}
	}
	w.timeMu.Unlock()
	w.prepareWeather()
}
