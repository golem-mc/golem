package datapack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/golem-mc/golem/registry"
)

// BiomeSpawner is one entry of a biome's natural_mob_spawns category list.
// vanilla: MobSpawnSettings.SpawnerData, Weighted
type BiomeSpawner struct {
	Type     registry.EntityType
	Weight   int
	MinCount int
	MaxCount int
}

// BiomeSpawnCost is a biome's spawn cost for one entity type.
// vanilla: MobSpawnSettings.MobSpawnCost
type BiomeSpawnCost struct {
	Type   registry.EntityType
	Charge float64
	Budget float64
}

// BiomeOverride is the server-side part of a data pack biome definition:
// its climate, natural mob spawn lists, spawn costs and chunk-generation
// creature probability.
// vanilla: Biome.DIRECT_CODEC, MobSpawnSettings.CODEC
type BiomeOverride struct {
	Climate             registry.BiomeClimate
	Spawners            [registry.MobCategoryCount][]BiomeSpawner
	SpawnCosts          []BiomeSpawnCost
	CreatureProbability float32
}

const (
	attrNaturalMobSpawns     = "minecraft:gameplay/natural_mob_spawns"
	attrCreatureSpawnProb    = "minecraft:gameplay/creature_world_gen_spawn_probability"
	defaultCreatureSpawnProb = float32(0.1)
)

// DecodeBiome parses a worldgen/biome JSON file's climate and mob spawn
// settings, rejecting values vanilla's codec refuses.
// vanilla: Biome.DIRECT_CODEC, Biome.ClimateSettings.CODEC, MobSpawnSettings.CODEC
func DecodeBiome(data []byte) (BiomeOverride, error) {
	var raw struct {
		HasPrecipitation    *bool           `json:"has_precipitation"`
		Temperature         *float32        `json:"temperature"`
		TemperatureModifier string          `json:"temperature_modifier"`
		Downfall            *float32        `json:"downfall"`
		Attributes          json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return BiomeOverride{}, err
	}
	if raw.HasPrecipitation == nil {
		return BiomeOverride{}, fmt.Errorf("missing has_precipitation")
	}
	if raw.Temperature == nil {
		return BiomeOverride{}, fmt.Errorf("missing temperature")
	}
	if raw.Downfall == nil {
		return BiomeOverride{}, fmt.Errorf("missing downfall")
	}
	var frozen bool
	switch raw.TemperatureModifier {
	case "", "none":
	case "frozen":
		frozen = true
	default:
		return BiomeOverride{}, fmt.Errorf("unknown temperature_modifier %q", raw.TemperatureModifier)
	}
	out := BiomeOverride{
		Climate: registry.BiomeClimate{
			Precipitation: *raw.HasPrecipitation,
			Temperature:   *raw.Temperature,
			Frozen:        frozen,
		},
		CreatureProbability: defaultCreatureSpawnProb,
	}
	if len(raw.Attributes) == 0 {
		return out, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw.Attributes), []byte("null")) {
		return BiomeOverride{}, fmt.Errorf("attributes must be an object")
	}
	var attrs map[string]json.RawMessage
	if err := json.Unmarshal(raw.Attributes, &attrs); err != nil {
		return BiomeOverride{}, fmt.Errorf("attributes: %w", err)
	}
	for k, v := range attrs {
		switch registry.Qualify(k) {
		case attrNaturalMobSpawns:
			spawners, costs, err := decodeBiomeMobSpawns(v)
			if err != nil {
				return BiomeOverride{}, fmt.Errorf("%s: %w", attrNaturalMobSpawns, err)
			}
			out.Spawners = spawners
			out.SpawnCosts = costs
		case attrCreatureSpawnProb:
			p, err := decodeCreatureSpawnProbability(v)
			if err != nil {
				return BiomeOverride{}, fmt.Errorf("%s: %w", attrCreatureSpawnProb, err)
			}
			out.CreatureProbability = p
		}
	}
	return out, nil
}

// decodeBiomeMobSpawns reads the natural_mob_spawns attribute entry, either a
// direct MobSpawnSettings object or a modifier wrapper (override or overlay).
// vanilla: EnvironmentAttributeMap.Entry.createCodec, MobSpawnSettingsModifier
func decodeBiomeMobSpawns(raw json.RawMessage) ([registry.MobCategoryCount][]BiomeSpawner, []BiomeSpawnCost, error) {
	var wrap struct {
		Modifier *string         `json:"modifier"`
		Argument json.RawMessage `json:"argument"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return [registry.MobCategoryCount][]BiomeSpawner{}, nil, err
	}
	body := raw
	if wrap.Modifier != nil {
		switch *wrap.Modifier {
		case "override", "overlay":
		default:
			return [registry.MobCategoryCount][]BiomeSpawner{}, nil, fmt.Errorf("unknown modifier %q", *wrap.Modifier)
		}
		if len(wrap.Argument) == 0 || bytes.Equal(bytes.TrimSpace(wrap.Argument), []byte("null")) {
			return [registry.MobCategoryCount][]BiomeSpawner{}, nil, fmt.Errorf("missing argument")
		}
		body = wrap.Argument
	}
	var settings struct {
		SpawnsByCategory map[string]json.RawMessage `json:"spawns_by_category"`
		SpawnCosts       map[string]json.RawMessage `json:"spawn_costs"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		return [registry.MobCategoryCount][]BiomeSpawner{}, nil, err
	}
	if settings.SpawnsByCategory == nil {
		return [registry.MobCategoryCount][]BiomeSpawner{}, nil, fmt.Errorf("missing spawns_by_category")
	}
	if settings.SpawnCosts == nil {
		return [registry.MobCategoryCount][]BiomeSpawner{}, nil, fmt.Errorf("missing spawn_costs")
	}
	var spawners [registry.MobCategoryCount][]BiomeSpawner
	for catName, listRaw := range settings.SpawnsByCategory {
		cat, ok := registry.MobCategoryByName(catName)
		if !ok {
			return [registry.MobCategoryCount][]BiomeSpawner{}, nil, fmt.Errorf("unknown mob category %q", catName)
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(listRaw, &entries); err != nil {
			return [registry.MobCategoryCount][]BiomeSpawner{}, nil, fmt.Errorf("spawns_by_category.%s: %w", catName, err)
		}
		if len(entries) == 0 {
			continue
		}
		list := make([]BiomeSpawner, 0, len(entries))
		for _, entryRaw := range entries {
			entry, err := decodeBiomeSpawner(entryRaw)
			if err != nil {
				return [registry.MobCategoryCount][]BiomeSpawner{}, nil, fmt.Errorf("spawns_by_category.%s: %w", catName, err)
			}
			list = append(list, entry)
		}
		spawners[cat] = list
	}
	var costs []BiomeSpawnCost
	for typeName, costRaw := range settings.SpawnCosts {
		et, ok := registry.EntityTypeByName(registry.Qualify(typeName))
		if !ok {
			return [registry.MobCategoryCount][]BiomeSpawner{}, nil, fmt.Errorf("unknown entity type %q in spawn_costs", typeName)
		}
		var c struct {
			Charge       *float64 `json:"charge"`
			EnergyBudget *float64 `json:"energy_budget"`
		}
		if err := json.Unmarshal(costRaw, &c); err != nil {
			return [registry.MobCategoryCount][]BiomeSpawner{}, nil, fmt.Errorf("spawn_costs.%s: %w", typeName, err)
		}
		if c.Charge == nil {
			return [registry.MobCategoryCount][]BiomeSpawner{}, nil, fmt.Errorf("spawn_costs.%s: missing charge", typeName)
		}
		if c.EnergyBudget == nil {
			return [registry.MobCategoryCount][]BiomeSpawner{}, nil, fmt.Errorf("spawn_costs.%s: missing energy_budget", typeName)
		}
		costs = append(costs, BiomeSpawnCost{Type: et, Charge: *c.Charge, Budget: *c.EnergyBudget})
	}
	slices.SortFunc(costs, func(a, b BiomeSpawnCost) int {
		return strings.Compare(a.Type.Name(), b.Type.Name())
	})
	return spawners, costs, nil
}

// decodeBiomeSpawner parses one Weighted<MobSpawnSettings.SpawnerData>.
// vanilla: Weighted.codec, MobSpawnSettings.SpawnerData.CODEC
func decodeBiomeSpawner(raw json.RawMessage) (BiomeSpawner, error) {
	var s struct {
		Type   *string         `json:"type"`
		Weight *int            `json:"weight"`
		Count  json.RawMessage `json:"count"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return BiomeSpawner{}, err
	}
	if s.Type == nil {
		return BiomeSpawner{}, fmt.Errorf("missing type")
	}
	et, ok := registry.EntityTypeByName(registry.Qualify(*s.Type))
	if !ok {
		return BiomeSpawner{}, fmt.Errorf("unknown entity type %q", *s.Type)
	}
	// vanilla: MobSpawnSettings.SpawnerData constructor replaces MISC with PIG
	if et.Category() == registry.MobCategoryMisc {
		et = registry.EntityTypePig
	}
	if s.Weight == nil {
		return BiomeSpawner{}, fmt.Errorf("missing weight")
	}
	if *s.Weight < 0 || *s.Weight > math.MaxInt32 {
		return BiomeSpawner{}, fmt.Errorf("weight %d out of range", *s.Weight)
	}
	if len(s.Count) == 0 || bytes.Equal(bytes.TrimSpace(s.Count), []byte("null")) {
		return BiomeSpawner{}, fmt.Errorf("missing count")
	}
	count, err := decodeIntRange(s.Count)
	if err != nil {
		return BiomeSpawner{}, fmt.Errorf("count: %w", err)
	}
	return BiomeSpawner{
		Type:     et,
		Weight:   *s.Weight,
		MinCount: int(count.Min),
		MaxCount: int(count.Max),
	}, nil
}

// decodeCreatureSpawnProbability decodes the creature_world_gen_spawn_probability
// float attribute entry and evaluates it over the default 0.1.
// vanilla: EnvironmentAttributes.CREATURE_WORLD_GEN_SPAWN_PROBABILITY, FloatModifier
func decodeCreatureSpawnProbability(raw json.RawMessage) (float32, error) {
	attr, _ := registry.EnvAttributeByName(attrCreatureSpawnProb)
	var direct float32
	if err := json.Unmarshal(raw, &direct); err == nil {
		return attr.Sanitize(direct), nil
	}
	var wrap struct {
		Modifier *string         `json:"modifier"`
		Argument json.RawMessage `json:"argument"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil || wrap.Modifier == nil {
		return 0, fmt.Errorf("expected float or modifier object")
	}
	entry := registry.EnvFloatEntry{Alpha: 1}
	switch *wrap.Modifier {
	case "override":
		entry.Op = registry.EnvOverride
	case "alpha_blend":
		entry.Op = registry.EnvAlphaBlend
	case "add":
		entry.Op = registry.EnvAdd
	case "subtract":
		entry.Op = registry.EnvSubtract
	case "multiply":
		entry.Op = registry.EnvMultiply
	case "minimum":
		entry.Op = registry.EnvMinimum
	case "maximum":
		entry.Op = registry.EnvMaximum
	default:
		return 0, fmt.Errorf("unknown float modifier %q", *wrap.Modifier)
	}
	if entry.Op == registry.EnvAlphaBlend {
		var arg struct {
			Value *float32 `json:"value"`
			Alpha *float32 `json:"alpha"`
		}
		if err := json.Unmarshal(wrap.Argument, &arg); err != nil || arg.Value == nil {
			return 0, fmt.Errorf("alpha_blend argument requires value")
		}
		entry.Value = *arg.Value
		if arg.Alpha != nil {
			if *arg.Alpha < 0 || *arg.Alpha > 1 {
				return 0, fmt.Errorf("alpha %v out of range", *arg.Alpha)
			}
			entry.Alpha = *arg.Alpha
		}
	} else {
		if err := json.Unmarshal(wrap.Argument, &entry.Value); err != nil {
			return 0, fmt.Errorf("argument: %w", err)
		}
	}
	return attr.Sanitize(entry.Apply(attr.Default)), nil
}
