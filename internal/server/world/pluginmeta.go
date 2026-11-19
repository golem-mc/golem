package world

import (
	"log/slog"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
)

// pluginMetaFile is what a plugin-created dimension keeps of its own: the
// seed it was created with and its spawn. A dimension has no level.dat.
const pluginMetaFile = "data/minecraft/plugin_world.dat"

// PluginMeta is a plugin world's saved seed and spawn.
type PluginMeta struct {
	Seed  int64
	Spawn SpawnPos
}

// ReadPluginMeta reads the dimension's plugin_world.dat under the world in dir,
// before the world is opened; ok is false when the dimension is new.
func ReadPluginMeta(dir, name string) (m PluginMeta, ok bool) {
	data, err := readSavedData(dimensionDir(dir, name), pluginMetaFile)
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
	return PluginMeta{Seed: int64(seed), Spawn: SpawnPos{X: pos[0], Y: pos[1], Z: pos[2], Yaw: float32(yaw), Pitch: float32(pitch)}}, true
}

// SavePluginMeta writes the world's plugin_world.dat.
func (w *World) SavePluginMeta(m PluginMeta) {
	data := nbt.Compound{"seed": nbt.Long(m.Seed), "spawn": nbt.IntArray{m.Spawn.X, m.Spawn.Y, m.Spawn.Z},
		"yaw": nbt.Float(m.Spawn.Yaw), "pitch": nbt.Float(m.Spawn.Pitch)}
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
