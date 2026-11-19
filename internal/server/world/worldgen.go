package world

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// worldGenFile is vanilla's record of the world's seed and each dimension's
// generator; vanilla will not open a world without it.
// vanilla: LevelStorageSource.getLevelDataAndDimensions
const worldGenFile = "data/minecraft/world_gen_settings.dat"

// readWorldGen returns the seed dir's world_gen_settings.dat records.
func readWorldGen(dir string) (int64, error) {
	data, err := readSavedData(dir, worldGenFile)
	if err != nil {
		return 0, err
	}
	s, ok := nbt.Get[nbt.Long](data, "seed")
	if !ok {
		return 0, fmt.Errorf("%s has no seed", worldGenFile)
	}
	return int64(s), nil
}

// overworldGenerator returns the overworld generator compound from
// world_gen_settings.dat's data, or a pre-26.1 level.dat's WorldGenSettings.
func overworldGenerator(settings nbt.Compound) (nbt.Compound, bool) {
	dims, _ := nbt.Get[nbt.Compound](settings, "dimensions")
	overworld, _ := nbt.Get[nbt.Compound](dims, DimensionName)
	return nbt.Get[nbt.Compound](overworld, "generator")
}

// storedGenerator returns the level-type and flat generator the world under dir
// was created with (world_gen_settings.dat, else a pre-26.1 level.dat's
// WorldGenSettings, else the Generator field older builds wrote), so an
// existing world keeps its generator whatever server.properties says. A noise
// overworld's level-type is its preset, read from its noise settings name. It
// reports false for a world being created, and nil flat for a preset missing a
// piece, which NewGenerator replaces with vanilla's default preset.
func storedGenerator(dir string) (levelType string, flat *FlatGenerator, ok bool) {
	data, err := readSavedData(dir, worldGenFile)
	if err == nil {
		gen, _ := overworldGenerator(data)
		levelType, flat = storedLevelType(data, gen)
		return levelType, flat, true
	}
	if !errors.Is(err, errNoSavedData) {
		slog.Warn("could not read the world's generator settings", "error", err)
	}
	level, err := LoadLevel(dir)
	if err != nil || !level.existed {
		return "", nil, false
	}
	if settings, ok := nbt.Get[nbt.Compound](level.raw, "WorldGenSettings"); ok {
		if gen, ok := overworldGenerator(settings); ok {
			levelType, flat = storedLevelType(settings, gen)
			return levelType, flat, true
		}
	}
	if g, ok := nbt.Get[nbt.Int](level.raw, "Generator"); ok {
		if g == generatorFlat {
			return levelTypeFlat, NewFlatGenerator(), true
		}
		return levelTypeNormal, nil, true
	}
	return "", nil, false
}

// storedLevelType is storedGenerator's reading of one world-gen settings
// compound and its overworld generator gen.
func storedLevelType(settings, gen nbt.Compound) (string, *FlatGenerator) {
	if kind, _ := nbt.Get[nbt.String](gen, "type"); kind == levelTypeFlat {
		flat, _ := flatPresetOf(settings)
		return levelTypeFlat, flat
	}
	if kind, _ := nbt.Get[nbt.String](gen, "type"); kind == debugGeneratorType {
		return levelTypeDebug, nil
	}
	name, _ := nbt.Get[nbt.String](gen, "settings")
	switch p := presetOf(string(name)); p {
	case presetLargeBiomes, presetAmplified:
		return p.settings(), nil
	}
	source, _ := nbt.Get[nbt.Compound](gen, "biome_source")
	kind, _ := nbt.Get[nbt.String](source, "type")
	biome, _ := nbt.Get[nbt.String](source, "biome")
	if kind == "minecraft:fixed" && biome == "minecraft:plains" {
		return levelTypeSingleBiome, nil
	}
	return levelTypeNormal, nil
}

// flatPresetOf builds the flat generator a world-gen settings compound
// records, or false for another generator kind or settings flatSettingsOf
// rejects.
func flatPresetOf(settings nbt.Compound) (*FlatGenerator, bool) {
	gen, ok := overworldGenerator(settings)
	if !ok {
		return nil, false
	}
	kind, _ := nbt.Get[nbt.String](gen, "type")
	if kind != "minecraft:flat" {
		return nil, false
	}
	preset, _ := nbt.Get[nbt.Compound](gen, "settings")
	return flatSettingsOf(preset)
}

// flatSettingsOf builds the flat generator one flat settings compound (biome
// and layers) records. False for settings missing their layers or with a
// layer flatLayerOf rejects; a missing or unknown biome becomes plains, a
// height-0 layer adds nothing, and an empty list is a void generator.
func flatSettingsOf(preset nbt.Compound) (*FlatGenerator, bool) {
	biome, _ := nbt.Get[nbt.String](preset, "biome")
	raw, ok := nbt.Get[nbt.List](preset, "layers")
	if !ok {
		return nil, false
	}
	var layers []flatLayer
	for _, item := range raw.Items {
		layer, ok := item.(nbt.Compound)
		if !ok {
			return nil, false
		}
		block, ok := nbt.Get[nbt.String](layer, "block")
		height, okH := nbt.Get[nbt.Int](layer, "height")
		if !ok || !okH {
			return nil, false
		}
		l, err := flatLayerOf(string(block), int(height))
		if err != nil {
			return nil, false
		}
		if l.height > 0 {
			layers = append(layers, l)
		}
	}
	g := newFlatGenerator(flatBiome(withNamespace(string(biome))), layers)
	g.features, g.lakes = nbt.BoolField(preset, "features"), nbt.BoolField(preset, "lakes")
	var ids []string
	present := true
	switch v := preset["structure_overrides"].(type) {
	case nbt.String:
		ids = []string{string(v)}
	case nbt.List:
		for _, item := range v.Items {
			id, ok := item.(nbt.String)
			if !ok {
				present = false
				break
			}
			ids = append(ids, string(id))
		}
	default:
		present = false
	}
	g.setStructureSets(ids, present)
	return g, true
}

// worldGenSettings is a fresh noise world's world_gen_settings.dat: vanilla's
// for the preset, which this overworld generator only approximates.
func worldGenSettings(seed int64, preset overworldPreset) nbt.Compound {
	gen := noiseGenerator("minecraft:overworld", preset.settings())
	if preset == presetSingleBiome {
		gen["biome_source"] = nbt.Compound{"type": nbt.String("minecraft:fixed"), "biome": nbt.String("minecraft:plains")}
	}
	return worldGenSettingsFor(seed, gen, true)
}

// flatWorldGenSettings is a fresh flat world's world_gen_settings.dat: its flat
// generator and whether structures generate.
func flatWorldGenSettings(seed int64, g *FlatGenerator, structures bool) nbt.Compound {
	return worldGenSettingsFor(seed, flatGeneratorSettings(g), structures)
}

// noiseGenerator is the multi-noise generator compound for vanilla's named
// biome source preset and noise settings.
func noiseGenerator(preset, settings string) nbt.Compound {
	return nbt.Compound{
		"type": nbt.String("minecraft:noise"),
		"biome_source": nbt.Compound{
			"type":   nbt.String("minecraft:multi_noise"),
			"preset": nbt.String(preset),
		},
		"settings": nbt.String(settings),
	}
}

// worldGenSettingsFor wraps the overworld generator with the seed, whether
// structures generate, and vanilla's default nether and end generators.
func worldGenSettingsFor(seed int64, overworld nbt.Compound, structures bool) nbt.Compound {
	return nbt.Compound{
		"seed":                nbt.Long(seed),
		"generate_structures": nbt.Byte(boolByte(structures)),
		"bonus_chest":         nbt.Byte(0),
		"dimensions": nbt.Compound{
			DimensionName: nbt.Compound{
				"type":      nbt.String(DimensionType),
				"generator": overworld,
			},
			NetherName: nbt.Compound{
				"type":      nbt.String(NetherType),
				"generator": noiseGenerator("minecraft:nether", "minecraft:nether"),
			},
			EndName: nbt.Compound{
				"type": nbt.String(EndType),
				"generator": nbt.Compound{
					"type":         nbt.String("minecraft:noise"),
					"biome_source": nbt.Compound{"type": nbt.String("minecraft:the_end")},
					"settings":     nbt.String("minecraft:end"),
				},
			},
		},
	}
}

// flatGeneratorSettings is the minecraft:flat compound for the world's layers:
// one entry per slice, its biome, its structure sets (the key left out for
// every set), features and lakes.
func flatGeneratorSettings(g *FlatGenerator) nbt.Compound {
	layers := make([]nbt.Tag, 0, len(g.layers))
	for _, l := range g.layers {
		name, _, ok := registry.BlockStateName(l.state)
		if !ok {
			panic("flat generator layer carries a block state the registry does not know")
		}
		layers = append(layers, nbt.Compound{
			"block":  nbt.String(name),
			"height": nbt.Int(l.height),
		})
	}
	settings := nbt.Compound{
		"biome":    nbt.String(g.biomeName),
		"features": nbt.Byte(boolByte(g.features)),
		"lakes":    nbt.Byte(boolByte(g.lakes)),
		"layers":   nbt.List{Elem: nbt.TypeCompound, Items: layers},
	}
	if !g.allStructureSets {
		sets := make([]nbt.Tag, len(g.structureSets))
		for i, id := range g.structureSets {
			sets[i] = nbt.String(id)
		}
		settings["structure_overrides"] = nbt.List{Elem: nbt.TypeString, Items: sets}
	}
	return nbt.Compound{"type": nbt.String("minecraft:flat"), "settings": settings}
}

func boolByte(b bool) int8 {
	if b {
		return 1
	}
	return 0
}

// ensureWorldGen writes world_gen_settings.dat if missing: at creation, or for
// a world an older build of this server created. An older vanilla's level.dat,
// or one still holding dimension settings, is left for vanilla's upgrade to
// extract.
// vanilla: LevelDatToSavedDataFileFix
func (w *World) ensureWorldGen() error {
	if _, err := os.Stat(filepath.Join(w.dir, worldGenFile)); err == nil {
		return nil
	}
	w.timeMu.Lock()
	seed := w.level.Seed
	settings, _ := nbt.Get[nbt.Compound](w.level.raw, "WorldGenSettings")
	_, hasDimensions := nbt.Get[nbt.Compound](settings, "dimensions")
	older := w.level.olderVersion()
	w.timeMu.Unlock()
	if older || hasDimensions {
		return nil
	}
	var out nbt.Compound
	g, _ := w.gen.(*OverworldGenerator)
	flat, _ := w.gen.(*FlatGenerator)
	_, debug := w.gen.(*DebugGenerator)
	switch {
	case debug:
		out = worldGenSettingsFor(seed, nbt.Compound{"type": nbt.String(debugGeneratorType)}, true)
	case flat != nil:
		out = flatWorldGenSettings(seed, flat, false)
		out["bonus_chest"] = nbt.Byte(boolByte(flat.bonusChest))
	case g != nil && g.flat != nil:
		out = flatWorldGenSettings(seed, g.flat, !g.noStructures)
	default:
		var preset overworldPreset
		if g != nil {
			preset = g.preset
		}
		out = worldGenSettings(seed, preset)
		if g != nil && g.noStructures {
			out["generate_structures"] = nbt.Byte(0)
		}
	}
	if g != nil {
		out["bonus_chest"] = nbt.Byte(boolByte(g.bonusChest))
	}
	return writeSavedData(w.dir, worldGenFile, out)
}
