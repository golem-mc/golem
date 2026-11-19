package registry

import "sync/atomic"

// EnchProviderKind is the type of an enchantment provider.
type EnchProviderKind uint8

// Enchantment provider types.
// vanilla: EnchantmentProvider.DIRECT_CODEC
const (
	EnchProviderSingle EnchProviderKind = iota
	EnchProviderByCost
	EnchProviderByCostWithDifficulty
)

// IntRange is an int provider that draws uniformly from Min to Max inclusive;
// a constant has Min == Max.
// vanilla: ConstantInt, UniformInt
type IntRange struct{ Min, Max int32 }

// EnchantmentProvider is one enchantment_provider entry. Candidates are the
// members of Tag (without the '#'), else Names.
// vanilla: EnchantmentProvider, EnchantmentsByCost, EnchantmentsByCostWithDifficulty, SingleEnchantment
type EnchantmentProvider struct {
	Kind EnchProviderKind
	// Tag and Names pick the by-cost candidates; Names[0] is the single
	// provider's enchantment.
	Tag   string
	Names []string
	// Cost is the by_cost provider's cost, Level the single provider's level.
	Cost, Level IntRange
	// MinCost and MaxCostSpan: by_cost_with_difficulty draws MinCost plus up to
	// MaxCostSpan scaled by the local difficulty's special multiplier.
	MinCost, MaxCostSpan int32
}

// Candidates resolves the provider's enchantments to IDs, in tag order.
func (p EnchantmentProvider) Candidates() []int32 {
	if p.Tag != "" {
		return EnchantmentTagIDs(p.Tag)
	}
	ids := make([]int32, 0, len(p.Names))
	for _, n := range p.Names {
		if id, ok := EnchantmentID(n); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

var enchantmentProviders atomic.Pointer[map[string]EnchantmentProvider]

// EnchantmentProviderByName returns the provider registered under a resource
// id, from the data pack over the built-in table.
func EnchantmentProviderByName(name string) (EnchantmentProvider, bool) {
	if m := enchantmentProviders.Load(); m != nil {
		if p, ok := (*m)[name]; ok {
			return p, true
		}
	}
	p, ok := builtinEnchantmentProviders[name]
	return p, ok
}

// ApplyPackEnchantmentProviders layers the data packs' providers over the
// built-in ones; nil clears them.
// vanilla: RegistryDataLoader.WORLD_REGISTRIES
func ApplyPackEnchantmentProviders(packs map[string]EnchantmentProvider) {
	if len(packs) == 0 {
		enchantmentProviders.Store(nil)
		return
	}
	enchantmentProviders.Store(&packs)
}
