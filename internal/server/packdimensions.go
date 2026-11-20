package server

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// loadPackStems reads the enabled packs' level stems (data/*/dimension/*.json)
// by dimension name, those replacing a vanilla dimension included.
// vanilla: RegistryDataLoader.DIMENSION_REGISTRIES
func loadPackStems(dirs []string) (map[string]nbt.Compound, error) {
	stems, err := datapack.LoadRegistryEntries(dirs, "dimension", func(file string, err error) {
		slog.Warn("Skipping data pack file " + file + ": " + err.Error())
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]nbt.Compound, len(stems))
	for name, tag := range stems {
		if c, ok := tag.(nbt.Compound); ok {
			out[name] = c
		} else {
			slog.Warn("Skipping dimension " + name + ": not a compound")
		}
	}
	return out, nil
}

// vanillaDimensionTypes is each vanilla dimension's type, which a pack stem
// replacing it must name.
var vanillaDimensionTypes = map[string]string{
	world.DimensionName: world.DimensionType,
	world.NetherName:    world.NetherType,
	world.EndName:       world.EndType,
}

// stemWorldShape resolves a stem's dimension type: its registry id and shape.
func stemWorldShape(stem nbt.Compound) (typeID int32, shape world.DimensionShape, err error) {
	types, _ := registry.Get("minecraft:dimension_type")
	typeName, _ := nbt.Get[nbt.String](stem, "type")
	typeID = types.ID(string(typeName))
	if typeID < 0 {
		return 0, shape, fmt.Errorf("unknown dimension type %s", typeName)
	}
	shape, err = world.ShapeOfType(string(typeName), types.Data[string(typeName)])
	return typeID, shape, err
}

// vanillaStemGenerator builds the generator of the pack stem replacing the
// vanilla dimension name, if there is one the server can model and whose type
// is the vanilla dimension's; otherwise it warns and answers nil. build makes
// the generator from the stem's generator compound and shape.
// vanilla: WorldDimensions.bake
func vanillaStemGenerator(stems map[string]nbt.Compound, name string, build func(nbt.Compound, world.DimensionShape) (world.Generator, error)) world.Generator {
	stem, ok := stems[name]
	if !ok {
		return nil
	}
	skip := func(why string) world.Generator {
		slog.Warn("Skipping the replacement of dimension " + name + ": " + why)
		return nil
	}
	if typeName, _ := nbt.Get[nbt.String](stem, "type"); string(typeName) != vanillaDimensionTypes[name] {
		return skip("its dimension type is not " + vanillaDimensionTypes[name])
	}
	_, shape, err := stemWorldShape(stem)
	if err != nil {
		return skip(err.Error())
	}
	genTag, _ := nbt.Get[nbt.Compound](stem, "generator")
	gen, err := build(genTag, shape)
	if err != nil {
		return skip(err.Error())
	}
	return gen
}

// earlyOverworldGenerator is the generator of a pack stem replacing
// minecraft:overworld, read from the packs level.dat selects before the world
// is opened; nil when no pack replaces it.
func (s *Server) earlyOverworldGenerator(levelSeed string) world.Generator {
	if s.Config.SafeMode {
		return nil
	}
	enabled, disabled, ok := world.SavedDataPacks(s.levelDir)
	if !ok {
		enabled, disabled = splitPackList(s.Config.InitialEnabledPacks), splitPackList(s.Config.InitialDisabledPacks)
	}
	repo, err := s.openRepository(enabled, disabled, func(string, error) {})
	if err != nil {
		return nil
	}
	stems, err := loadPackStems(repo.Dirs())
	if err != nil {
		return nil
	}
	return vanillaStemGenerator(stems, world.DimensionName, func(gen nbt.Compound, shape world.DimensionShape) (world.Generator, error) {
		return world.StemOverworld(gen, shape, levelSeed, s.Config.GenerateStructures)
	})
}

// packDimensions builds a dimension for each stem the server can model, in name
// order; the others are skipped with a warning. A stem named after a vanilla
// dimension is not one: makeDimensions applies it to that dimension.
func (s *Server) packDimensions(seed int64) []*dimension {
	var out []*dimension
	for _, name := range slices.Sorted(maps.Keys(s.packStems)) {
		if _, vanilla := vanillaDimensionTypes[name]; vanilla {
			continue
		}
		skip := func(why string) { slog.Warn("Skipping dimension " + name + ": " + why) }
		typeID, shape, err := stemWorldShape(s.packStems[name])
		if err != nil {
			skip(err.Error())
			continue
		}
		genTag, _ := nbt.Get[nbt.Compound](s.packStems[name], "generator")
		gen, err := world.StemGenerator(genTag, shape, seed, s.Config.GenerateStructures)
		if err != nil {
			skip(err.Error())
			continue
		}
		out = append(out, &dimension{
			name:   name,
			typeID: typeID,
			gen:    gen,
			w:      world.NewCustomDimensionWorld(s.levelDir, name, shape, gen),
		})
	}
	return out
}
