package main

import (
	"fmt"
	"os"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/internal/server/world/chunksched"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// requestOrder acquires finished chunks in the dimension's default order.
func requestOrder(d dimension, r chunkRange) []world.ChunkPos {
	var out []world.ChunkPos
	if d.Name == "the_nether" {
		var rects []chunksched.Rect
		for _, t := range r.tiles(forceloadTile) {
			rects = append(rects, chunksched.Rect{X0: t.X0, Z0: t.Z0, X1: t.X1, Z1: t.Z1})
		}
		for _, p := range chunksched.Forceload(rects) {
			out = append(out, world.ChunkPos{X: p.X, Z: p.Z})
		}
		return out
	}
	for cx := r.X0; cx <= r.X1; cx++ {
		for cz := r.Z0; cz <= r.Z1; cz++ {
			out = append(out, world.ChunkPos{X: cx, Z: cz})
		}
	}
	return out
}

// decorationOrder returns the recording as it is: it covers spawn, and replay
// never decorates a chunk the recording lacks. Worker interleaving is kept.
func decorationOrder(recorded [][2]int32) []world.ChunkPos {
	var out []world.ChunkPos
	for _, p := range recorded {
		out = append(out, world.ChunkPos{X: p[0], Z: p[1]})
	}
	return out
}

// parityOverworld keeps NewWorld from running the overworld spawn search,
// which would decorate chunks before the recorded FEATURES order is replayed.
// Embedding preserves the proto store and the generator's other interfaces.
type parityOverworld struct{ *world.OverworldGenerator }

// replayedLightWorld reads sky light as the SPAWN step sees it: from the
// replayed column sources, not the finished world's relit light.
// vanilla: ChunkStatusTasks.generateSpawn, Animal.isBrightEnoughToSpawn
type replayedLightWorld struct {
	*world.World
	g *world.OverworldGenerator
}

func (w replayedLightWorld) SkyLightAt(x, y, z int32) uint8 {
	return w.g.ReplayedSkyLight(w.BlockAt, x, y, z)
}

// structureMobFinalizer finalizes a structure mob in dim at the difficulty
// OriginalMobs uses.
func structureMobFinalizer(dim string) world.StructureMobFinalizer {
	return func(tag nbt.Compound, biome registry.Biome, blackCat bool, rng levelgen.RandomSource) nbt.Compound {
		return entity.FinalizeStructureTag(tag, dim, biome, blackCat, originalMobDifficulty(0, 0), rng)
	}
}

// generateOurs generates the range with our generator in a throwaway world
// directory, touching no network or existing world.
func generateOurs(d dimension, seed int64, config cacheConfig, r chunkRange, recorded []recordedStep) (map[[2]int32]*chunkData, error) {
	dir, err := os.MkdirTemp("", "worldgen-parity-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	// The nether and the end hang off an overworld, which owns level.dat.
	g := world.NewOverworldGeneratorFor(config.levelType(), seed)
	if config.levelType() == "minecraft:flat" {
		g = world.NewFlatOverworldGeneratorFor(seed, config.GeneratorSettings)
	}
	g.SetStructureMobFinalizer(structureMobFinalizer(world.DimensionName))
	overworld, err := world.NewWorld(dir, parityOverworld{g})
	if err != nil {
		return nil, fmt.Errorf("our world: %w", err)
	}
	defer overworld.Close()
	w := overworld
	var spawnWorld entity.SpawnWorld
	switch d.Name {
	case "the_nether":
		nether := world.NewNetherGenerator(seed)
		nether.SetStructureMobFinalizer(structureMobFinalizer(world.NetherName))
		nether.DecorateInOrder(decorationOrder(featuresOnly(recorded)))
		w = world.NewDimensionWorld(dir, true, nether)
		defer w.Close()
	case "the_end":
		end := world.NewEndGenerator(seed)
		end.DecorateInOrder(decorationOrder(featuresOnly(recorded)))
		w = world.NewDimensionWorld(dir, false, end)
		defer w.Close()
	case "overworld":
		for _, step := range recorded {
			if step.Light != "" {
				g.BeginLightReplay()
				spawnWorld = replayedLightWorld{w, g}
				break
			}
		}
		for _, step := range publishedDuringFeatures(recorded) {
			// A reload keeps the worldgen heightmaps, so it replays as nothing.
			if step.Load {
				continue
			}
			pos := world.ChunkPos{X: step.Pos[0], Z: step.Pos[1]}
			if step.Light == "light_sources" {
				g.PrimeLightSources(pos)
			} else if step.Light != "" {
				g.ReplayLight(pos, step.Light == "light")
			} else {
				g.DecorateInOrder([]world.ChunkPos{pos})
			}
		}
	}

	if spawnWorld == nil {
		spawnWorld = w
	}
	var lastID int32
	nextID := func() int32 { lastID++; return lastID }
	out := map[[2]int32]*chunkData{}
	for _, pos := range requestOrder(d, r) {
		w.Acquire(pos)
		defer w.Release(pos)
	}
	for cx := r.X0; cx <= r.X1; cx++ {
		for cz := r.Z0; cz <= r.Z1; cz++ {
			pos := world.ChunkPos{X: cx, Z: cz}
			c, _ := w.Acquire(pos)
			cd := newChunkData(d, cx, cz)
			for y := d.MinY; y < d.MinY+d.Height; y++ {
				for z := range 16 {
					for x := range 16 {
						cd.Blocks[cd.index(x, y, z)] = c.BlockAt(int32(x), int32(y), int32(z))
					}
				}
			}
			for cy := range d.Height / 4 {
				for z := range 4 {
					for x := range 4 {
						cd.Biomes[cellIndex(x, cy, z)] = c.BiomeAt(int32(x*4), int32(d.MinY+cy*4), int32(z*4))
					}
				}
			}
			cd.Structures = c.StructureStarts()
			cd.addBlockEntities(c.BlockEntitiesNBT())
			cd.Ticks = map[[3]int32]bool{}
			for _, tick := range c.GeneratedTicks() {
				cd.Ticks[tick] = true
			}
			cd.Entities = w.TakeGeneratedEntities(pos)
			cd.EntitiesRecorded = true
			// vanilla: FlatLevelSource.spawnOriginalMobs is empty
			if d.Name == "the_nether" || d.Name == "overworld" && g.Flat() == nil {
				for _, m := range entity.OriginalMobs(spawnWorld, seed, entity.ChunkPos{X: cx, Z: cz}, nextID, originalMobDifficulty) {
					cd.Entities = append(cd.Entities, savedMob(m))
				}
			}
			w.Release(pos)
			out[[2]int32{cx, cz}] = cd
		}
	}
	return out, nil
}

// originalMobDifficulty is a new world's local difficulty: normal, at clock 0,
// inhabited time 0 and a full moon.
func originalMobDifficulty(float64, float64) rules.LocalDifficulty {
	local := rules.NewLocalDifficulty(rules.DifficultyNormal, rules.EffectiveDifficulty(rules.DifficultyNormal, 0, 0, 1))
	local.Moon = 1
	return local
}

// savedMob is a mob's entity file compound: id, position and rotation, then
// the mob's own keys.
func savedMob(m *entity.Mob) nbt.Compound {
	tag := nbt.Compound{
		"id":       nbt.String(m.Type.Name()),
		"Pos":      nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(m.X), nbt.Double(m.Y), nbt.Double(m.Z)}},
		"Rotation": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(m.Yaw), nbt.Float(m.Pitch)}},
	}
	m.SaveTo(tag, func() int64 { return 0 })
	return tag
}
