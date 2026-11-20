package world

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// PluginGenSpec is how a plugin asked for a world's vanilla terrain.
type PluginGenSpec struct {
	// Type is "", "normal", "flat", "large_biomes" or "amplified".
	Type string
	// Settings is a flat world's generator-settings JSON, empty for the
	// default preset.
	Settings string
	// Biome is a biome name (minecraft:plains) that fills the whole world.
	Biome        string
	NoStructures bool
	BonusChest   bool
}

// Plain reports whether the spec asks for nothing beyond the dimension's own
// generator.
func (s PluginGenSpec) Plain() bool {
	return (s.Type == "" || s.Type == "normal") && s.Settings == "" && s.Biome == ""
}

// NewPluginOverworldGenerator builds the overworld-type terrain a plugin asked
// for: a noise preset, a single biome, or a flat preset.
// vanilla: WorldPresets.Bootstrap
func NewPluginOverworldGenerator(spec PluginGenSpec, shape DimensionShape, seed int64) (Generator, error) {
	level := levelTypeNormal
	switch spec.Type {
	case "", "normal":
	case "flat", "large_biomes", "amplified":
		level = "minecraft:" + spec.Type
	default:
		return nil, fmt.Errorf("unknown world type %q", spec.Type)
	}
	if level != levelTypeFlat && spec.Settings != "" {
		return nil, fmt.Errorf("generator settings are only for flat worlds")
	}
	if level == levelTypeFlat {
		if spec.Biome != "" {
			return nil, fmt.Errorf("a flat world takes its biome from its generator settings")
		}
		return newPluginFlat(spec, shape, seed)
	}
	g := &OverworldGenerator{preset: presetOf(level), noStructures: spec.NoStructures}
	if spec.Biome != "" {
		if level != levelTypeNormal {
			return nil, fmt.Errorf("a single biome needs the normal world type")
		}
		biome, ok := registry.BiomeByName(spec.Biome)
		if !ok {
			return nil, fmt.Errorf("unknown biome %q", spec.Biome)
		}
		found := false
		for i, id := range overworldBiomeIDs {
			if id == biome {
				g.single, g.hasSingle, found = terrain.OverworldBiome(i), true, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("biome %s is not an overworld biome", spec.Biome)
		}
		g.preset = presetSingleBiome
	}
	g.setSeed(seed)
	return g, nil
}

// newPluginFlat builds a flat world from the spec's layers JSON, which must
// parse and carry a layers list (the server.properties reader is lenient and
// would fall back to the default preset).
func newPluginFlat(spec PluginGenSpec, shape DimensionShape, seed int64) (Generator, error) {
	flat := NewVanillaFlatGenerator()
	if strings.TrimSpace(spec.Settings) != "" {
		var in struct {
			Layers *[]json.RawMessage `json:"layers"`
		}
		if err := json.Unmarshal([]byte(spec.Settings), &in); err != nil {
			return nil, fmt.Errorf("invalid flat generator settings: %w", err)
		}
		if in.Layers == nil {
			return nil, fmt.Errorf("invalid flat generator settings: no layers list")
		}
		flat = flatGeneratorFromSettings(spec.Settings)
	}
	if spec.NoStructures {
		flat.structureSets, flat.allStructureSets = nil, false
	}
	// Layers start at the dimension's min_y; memory starts at worldMinY.
	if gap := shape.minY - worldMinY; gap > 0 {
		flat.layers = append([]flatLayer{{state: registry.BlockAir.DefaultState(), height: gap}}, flat.layers...)
	}
	if flat.decorates() && shape.Type() == DimensionType {
		return newFlatOverworldGenerator(seed, flat), nil
	}
	flat.seed = seed
	return flat, nil
}
