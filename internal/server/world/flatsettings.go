package world

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// flatMaxHeight is the limit on one layer's height and on the layers' total.
// vanilla: DimensionType.Y_SIZE
const flatMaxHeight = 4064

// flatLayerOf resolves one layer of a flat preset: block with its namespace
// defaulted to minecraft, height in 0..flatMaxHeight.
// vanilla: FlatLayerInfo.CODEC
func flatLayerOf(block string, height int) (flatLayer, error) {
	if height < 0 || height > flatMaxHeight {
		return flatLayer{}, fmt.Errorf("layer height %d outside 0..%d", height, flatMaxHeight)
	}
	state, ok := registry.FindState(withNamespace(block), nil)
	if !ok {
		return flatLayer{}, fmt.Errorf("unknown block %q", block)
	}
	return flatLayer{state: state, height: int32(height)}, nil
}

// withNamespace defaults an identifier's namespace to minecraft.
// vanilla: Identifier.parse
func withNamespace(id string) string {
	if strings.Contains(id, ":") {
		return id
	}
	return "minecraft:" + id
}

// flatGeneratorFromSettings decodes server.properties' generator-settings into
// a flat generator. Like vanilla's resultOrPartial, a bad layer or biome is
// logged and dropped and the rest kept; only a value with no layers list keeps
// the default preset. JSON that does not parse also keeps the default preset,
// where vanilla fails to start.
// vanilla: DedicatedServerProperties (generator-settings), FlatLevelGeneratorSettings.CODEC
func flatGeneratorFromSettings(settings string) *FlatGenerator {
	var in map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settings), &in); err != nil {
		slog.Error("Failed to parse generator-settings, using the default flat world", "error", err)
		return NewVanillaFlatGenerator()
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(in["layers"], &raw); err != nil || raw == nil {
		slog.Error("generator-settings has no layers list, using the default flat world")
		return NewVanillaFlatGenerator()
	}
	var layers []flatLayer
	total := 0
	for _, r := range raw {
		var l struct {
			Block  *string `json:"block"`
			Height *int    `json:"height"`
		}
		if err := json.Unmarshal(r, &l); err != nil || l.Block == nil || l.Height == nil {
			slog.Error("Dropping a generator-settings layer without a block and height", "layer", string(r))
			continue
		}
		layer, err := flatLayerOf(*l.Block, *l.Height)
		if err != nil {
			slog.Error("Dropping a generator-settings layer", "error", err)
			continue
		}
		total += *l.Height
		if layer.height > 0 {
			layers = append(layers, layer)
		}
	}
	if total > flatMaxHeight {
		slog.Error(fmt.Sprintf("Sum of layer heights is > %d", flatMaxHeight))
	}
	var biomeID string
	_ = json.Unmarshal(in["biome"], &biomeID)
	biome := flatBiome(withNamespace(biomeID))
	if in["biome"] != nil && biome != withNamespace(biomeID) {
		slog.Warn("Unknown generator-settings biome, defaulting to plains", "biome", string(in["biome"]))
	}
	g := newFlatGenerator(biome, layers)
	g.features, g.lakes = isTrue(in["features"]), isTrue(in["lakes"])
	var ids []string
	overrides, present := in["structure_overrides"]
	if present && string(overrides) != "null" {
		var one string
		if json.Unmarshal(overrides, &one) == nil {
			ids = []string{one}
		} else if json.Unmarshal(overrides, &ids) != nil {
			present = false
		}
	} else {
		present = false
	}
	g.setStructureSets(ids, present)
	return g
}

// setStructureSets records a preset's structure_overrides: present false for
// an absent key or one that does not decode, which means every set; a #tag
// means none here; an unknown id makes the whole value not decode.
// vanilla: FlatLevelGeneratorSettings.CODEC (lenientOptionalFieldOf), FlatLevelSource.createState
func (g *FlatGenerator) setStructureSets(ids []string, present bool) {
	g.structureSets, g.allStructureSets = nil, !present
	if !present {
		return
	}
	sets := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.HasPrefix(id, "#") {
			slog.Warn("This server does not read structure set tags in a flat world's structure_overrides; it places none", "tag", id)
			g.structureSets = nil
			return
		}
		id = withNamespace(id)
		if !slices.ContainsFunc(levelgen.StructureSets, func(s levelgen.StructureSet) bool { return s.Name == id }) {
			slog.Warn("Unknown structure set in a flat world's structure_overrides, placing every set", "set", id)
			g.allStructureSets = true
			return
		}
		sets = append(sets, id)
	}
	g.structureSets = sets
}

// isTrue reports whether a JSON value is true.
func isTrue(raw json.RawMessage) bool {
	var b bool
	return json.Unmarshal(raw, &b) == nil && b
}
