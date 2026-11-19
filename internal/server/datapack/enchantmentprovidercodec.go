package datapack

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/golem-mc/golem/registry"
)

// DecodeEnchantmentProvider parses an enchantment_provider JSON file. An int
// provider other than a number, constant or uniform is an error.
// vanilla: EnchantmentProvider.DIRECT_CODEC
func DecodeEnchantmentProvider(data []byte) (registry.EnchantmentProvider, error) {
	var raw struct {
		Type         string          `json:"type"`
		Enchantments json.RawMessage `json:"enchantments"`
		Enchantment  string          `json:"enchantment"`
		Level        json.RawMessage `json:"level"`
		Cost         json.RawMessage `json:"cost"`
		MinCost      *int32          `json:"min_cost"`
		MaxCostSpan  *int32          `json:"max_cost_span"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return registry.EnchantmentProvider{}, err
	}
	var p registry.EnchantmentProvider
	var err error
	switch registry.Qualify(raw.Type) {
	case "minecraft:single":
		if raw.Enchantment == "" {
			return p, fmt.Errorf("missing enchantment")
		}
		p.Kind = registry.EnchProviderSingle
		p.Names = []string{registry.Qualify(raw.Enchantment)}
		p.Level, err = decodeIntRange(raw.Level)
	case "minecraft:by_cost":
		p.Kind = registry.EnchProviderByCost
		if p.Tag, p.Names, err = decodeEnchantmentSet(raw.Enchantments); err == nil {
			p.Cost, err = decodeIntRange(raw.Cost)
		}
	case "minecraft:by_cost_with_difficulty":
		p.Kind = registry.EnchProviderByCostWithDifficulty
		if p.Tag, p.Names, err = decodeEnchantmentSet(raw.Enchantments); err != nil {
			break
		}
		switch {
		case raw.MinCost == nil || *raw.MinCost < 1 || *raw.MinCost > 10000:
			err = fmt.Errorf("min_cost must be 1 to 10000")
		case raw.MaxCostSpan == nil || *raw.MaxCostSpan < 0 || *raw.MaxCostSpan > 10000:
			err = fmt.Errorf("max_cost_span must be 0 to 10000")
		default:
			p.MinCost, p.MaxCostSpan = *raw.MinCost, *raw.MaxCostSpan
		}
	default:
		err = fmt.Errorf("unknown enchantment provider type %q", raw.Type)
	}
	return p, err
}

// decodeEnchantmentSet reads a holder set: "#tag", one id or a list of ids.
// vanilla: RegistryCodecs.holderSet
func decodeEnchantmentSet(raw json.RawMessage) (tag string, names []string, err error) {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		if t, ok := strings.CutPrefix(one, "#"); ok {
			return registry.Qualify(t), nil, nil
		}
		return "", []string{registry.Qualify(one)}, nil
	}
	if err := json.Unmarshal(raw, &names); err != nil {
		return "", nil, fmt.Errorf("enchantments is not a tag, id or list")
	}
	for i, n := range names {
		names[i] = registry.Qualify(n)
	}
	return "", names, nil
}

// decodeIntRange reads a number, a constant or a uniform int provider.
// vanilla: IntProviders.CODEC
func decodeIntRange(raw json.RawMessage) (registry.IntRange, error) {
	var n int32
	if json.Unmarshal(raw, &n) == nil {
		return registry.IntRange{Min: n, Max: n}, nil
	}
	var p struct {
		Type  string `json:"type"`
		Value *int32 `json:"value"`
		Min   *int32 `json:"min_inclusive"`
		Max   *int32 `json:"max_inclusive"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return registry.IntRange{}, fmt.Errorf("not an int provider")
	}
	switch registry.Qualify(p.Type) {
	case "minecraft:constant":
		if p.Value != nil {
			return registry.IntRange{Min: *p.Value, Max: *p.Value}, nil
		}
	case "minecraft:uniform":
		if p.Min != nil && p.Max != nil && *p.Min <= *p.Max {
			return registry.IntRange{Min: *p.Min, Max: *p.Max}, nil
		}
	}
	return registry.IntRange{}, fmt.Errorf("int provider %q is not supported", p.Type)
}
