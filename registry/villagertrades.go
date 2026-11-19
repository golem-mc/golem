package registry

import "github.com/golem-mc/golem/internal/protocol"

// TradeCost is an item and count a trade asks for or gives.
type TradeCost struct {
	Item  string
	Count int32
	// Water is a component predicate: the stack's potion_contents is exactly
	// the water potion. It is the only predicate any trade uses.
	Water bool
}

// VillagerTrade is one data-pack villager_trade with constant numbers.
// Variants limits it to villagers of those types (empty for any).
// Unsupported marks a trade whose modifiers or components are not evaluated;
// its draw yields no offer.
// vanilla: VillagerTrade
type VillagerTrade struct {
	Wants, Extra, Gives TradeCost
	GivesStack          *protocol.ItemStack // pack template; nil for built-in id/count results
	MaxUses, XP         int32
	Discount            float32
	Variants            []string
	Modifiers           []LootFunction
	DoublePrice         []string // enchantments that double the additional cost
	Unsupported         bool
}

// TradeSet is a data-pack trade_set: Amount offers drawn from Trades.
// vanilla: TradeSet
type TradeSet struct {
	Amount     int
	Duplicates bool
	Trades     []VillagerTrade
}

// FindTradeSet returns the trade set of a profession's level, or of the
// wandering trader's tiers, by its name under minecraft:trade_set
// ("farmer/level_1"). A data pack's set replaces the built-in one.
func FindTradeSet(name string) (TradeSet, bool) {
	if packed := packTradeSets.Load(); packed != nil {
		if set, ok := (*packed)[name]; ok {
			return set, true
		}
	}
	set, ok := tradeSets[name]
	return set, ok
}
