package world

import (
	"fmt"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// DimensionShape is a dimension type's wire and disk shape.
type DimensionShape = dimensionShape

// Type is the dimension type's name, MinY and Height its buildable span.
func (s DimensionShape) Type() string  { return s.typ }
func (s DimensionShape) MinY() int32   { return s.minY }
func (s DimensionShape) Height() int32 { return s.height }

// builtinShapes is each built-in dimension type's shape; overworld_caves
// differs from the overworld only in what the client draws.
var builtinShapes = map[string]dimensionShape{
	DimensionType:               dimensionShapes[DimensionName],
	"minecraft:overworld_caves": dimensionShapes[DimensionName],
	NetherType:                  dimensionShapes[NetherName],
	EndType:                     dimensionShapes[EndName],
}

// ShapeOfType reads a dimension type's shape: a built-in type's, or a pack
// type's min_y, height, logical_height, has_skylight, has_ceiling, has_fixed_time
// and default_clock from data. It fails
// for a band outside the chunk storage's -64..320.
func ShapeOfType(name string, data nbt.Tag) (DimensionShape, error) {
	if shape, ok := builtinShapes[name]; ok {
		shape.typ = name
		return shape, nil
	}
	c, ok := data.(nbt.Compound)
	if !ok {
		return DimensionShape{}, fmt.Errorf("dimension type %s has no data", name)
	}
	minY, okMin := nbt.IntField(c, "min_y")
	height, okH := nbt.IntField(c, "height")
	logical, okL := nbt.IntField(c, "logical_height")
	if !okMin || !okH || !okL {
		return DimensionShape{}, fmt.Errorf("dimension type %s lacks min_y, height or logical_height", name)
	}
	if minY < worldMinY || height < sectionSize || minY+height > worldMinY+worldHeight ||
		minY%sectionSize != 0 || height%sectionSize != 0 {
		return DimensionShape{}, fmt.Errorf("dimension type %s spans y=%d..%d, outside the supported -64..320 in whole sections", name, minY, minY+height)
	}
	clock, _ := nbt.Get[nbt.String](c, "default_clock")
	var defaultClock string
	if clock != "" {
		defaultClock = registry.Qualify(string(clock))
	}
	return DimensionShape{minY: minY, height: height, logicalHeight: logical, skyLight: nbt.BoolField(c, "has_skylight"), ceiling: nbt.BoolField(c, "has_ceiling"),
		fixedTime: nbt.BoolField(c, "has_fixed_time"), typ: name, defaultClock: defaultClock}, nil
}

// StemGenerator builds the generator a level stem's generator compound names,
// for a dimension of shape: a flat generator, or a noise generator whose
// settings are the overworld's (also large_biomes or amplified, with the
// multi-noise overworld source or a fixed overworld biome), the nether's or
// the end's. structures false
// leaves the nether's fortresses and bastions out.
// vanilla: LevelStem.CODEC
func StemGenerator(gen nbt.Compound, shape DimensionShape, seed int64, structures bool) (Generator, error) {
	kind, _ := nbt.Get[nbt.String](gen, "type")
	switch kind {
	case "minecraft:flat":
		settings, _ := nbt.Get[nbt.Compound](gen, "settings")
		flat, ok := flatSettingsOf(settings)
		if !ok {
			return nil, fmt.Errorf("flat generator settings are incomplete or name an unknown block")
		}
		// Layers start at the dimension's min_y; memory starts at worldMinY.
		if gap := shape.minY - worldMinY; gap > 0 {
			flat.layers = append([]flatLayer{{state: registry.BlockAir.DefaultState(), height: gap}}, flat.layers...)
		}
		flat.seed = seed
		return flat, nil
	case "minecraft:noise":
		settings, _ := nbt.Get[nbt.String](gen, "settings")
		src, _ := nbt.Get[nbt.Compound](gen, "biome_source")
		srcKind, _ := nbt.Get[nbt.String](src, "type")
		switch settings {
		case "minecraft:overworld", levelTypeLargeBiomes, levelTypeAmplified:
			g := &OverworldGenerator{preset: presetOf(string(settings))}
			switch srcKind {
			case "minecraft:fixed":
				name, _ := nbt.Get[nbt.String](src, "biome")
				biome, ok := overworldBiomeNamed(registry.Qualify(string(name)))
				if !ok {
					return nil, fmt.Errorf("biome %q is not an overworld biome", name)
				}
				g.single, g.hasSingle = biome, true
				g.terrain, g.preset = g.preset, presetSingleBiome
			case "minecraft:multi_noise":
				if preset, _ := nbt.Get[nbt.String](src, "preset"); preset != "minecraft:overworld" {
					return nil, fmt.Errorf("multi_noise preset %q is not supported", preset)
				}
			default:
				return nil, fmt.Errorf("biome source %q is not supported", srcKind)
			}
			g.setSeed(seed)
			return g, nil
		case "minecraft:nether", "minecraft:end":
			if !vanillaBiomeSource(gen, settings) {
				return nil, fmt.Errorf("noise settings %q with this biome_source are not supported", settings)
			}
			if settings == "minecraft:end" {
				return NewEndGenerator(seed), nil
			}
			g := NewNetherGenerator(seed)
			if !structures {
				g.DisableStructures()
			}
			return g, nil
		}
		return nil, fmt.Errorf("noise settings %q are not supported", settings)
	}
	return nil, fmt.Errorf("generator %q is not supported", kind)
}

// vanillaBiomeSource reports whether gen's biome_source is the one vanilla's
// world_preset/normal.json pairs with the nether or end noise settings.
func vanillaBiomeSource(gen nbt.Compound, settings nbt.String) bool {
	src, _ := nbt.Get[nbt.Compound](gen, "biome_source")
	kind, _ := nbt.Get[nbt.String](src, "type")
	preset, _ := nbt.Get[nbt.String](src, "preset")
	if settings == "minecraft:nether" {
		return kind == "minecraft:multi_noise" && preset == "minecraft:nether"
	}
	return kind == "minecraft:the_end"
}

// StemOverworld builds the generator of a stem that replaces minecraft:overworld
// before the world is opened: StemGenerator's, with levelSeed settling the seed
// like NewGenerator's (empty leaves it to NewWorld). It fails for a generator
// that is not an overworld's.
// vanilla: WorldDimensions.bake
func StemOverworld(gen nbt.Compound, shape DimensionShape, levelSeed string, structures bool) (Generator, error) {
	spec := strings.TrimSpace(levelSeed)
	var seed int64
	if spec != "" {
		seed = ParseSeed(spec)
	}
	g, err := StemGenerator(gen, shape, seed, structures)
	if err != nil {
		return nil, err
	}
	switch t := g.(type) {
	case *OverworldGenerator:
		t.deferred = spec == ""
		return t, nil
	case *FlatGenerator:
		return flatGenerator(t, spec), nil
	}
	return nil, fmt.Errorf("generator %T is not an overworld's", g)
}

// NewCustomDimensionWorld builds a pack's dimension attached to the world under
// dir, saved under dimensions/<namespace>/<path>/. Like the nether and the end
// it has no level.dat.
func NewCustomDimensionWorld(dir, name string, shape DimensionShape, gen Generator) *World {
	w, _ := newWorld(dir, gen, name, shape, false)
	return w
}
