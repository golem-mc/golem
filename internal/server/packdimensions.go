package server

import (
	"log/slog"
	"maps"
	"slices"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// loadPackStems reads the enabled packs' level stems (data/*/dimension/*.json)
// by dimension name. The three vanilla dimensions are not overridden.
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
		c, ok := tag.(nbt.Compound)
		switch {
		case !ok:
			slog.Warn("Skipping dimension " + name + ": not a compound")
		case name == world.DimensionName || name == world.NetherName || name == world.EndName:
			slog.Warn("Skipping dimension " + name + ": overriding a vanilla dimension is not supported")
		default:
			out[name] = c
		}
	}
	return out, nil
}

// packDimensions builds a dimension for each stem the server can model, in name
// order; the others are skipped with a warning.
func (s *Server) packDimensions(seed int64) []*dimension {
	types, _ := registry.Get("minecraft:dimension_type")
	var out []*dimension
	for _, name := range slices.Sorted(maps.Keys(s.packStems)) {
		stem := s.packStems[name]
		skip := func(why string) { slog.Warn("Skipping dimension " + name + ": " + why) }
		typeName, _ := nbt.Get[nbt.String](stem, "type")
		typeID := types.ID(string(typeName))
		if typeID < 0 {
			skip("unknown dimension type " + string(typeName))
			continue
		}
		shape, err := world.ShapeOfType(string(typeName), types.Data[string(typeName)])
		if err != nil {
			skip(err.Error())
			continue
		}
		genTag, _ := nbt.Get[nbt.Compound](stem, "generator")
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
