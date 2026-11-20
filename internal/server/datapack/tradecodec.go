package datapack

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// tradeKeys are the fields of a villager_trade file the server reads; any
// other fails the file, as tools/gen villagertrades does.
var tradeKeys = []string{
	"wants", "additional_wants", "gives", "max_uses", "reputation_discount", "xp",
	"merchant_predicate", "given_item_modifiers", "given_item_modifier", "double_trade_price_enchantments",
}

// DecodeVillagerTrade reads a villager_trade file. A shape the server does not
// evaluate (a number provider, a component predicate beyond the water potion,
// a merchant predicate beyond the villager variant) gives an Unsupported trade
// whose draw yields no offer, as in registry's built-in trades.
// vanilla: VillagerTrade.CODEC
func DecodeVillagerTrade(data []byte) (trade registry.VillagerTrade, err error) {
	defer recoverLoot(&err)
	t := object(parseJSON(data), "villager trade")
	for k := range t {
		if !slices.Contains(tradeKeys, k) {
			fail("key %q is not modelled", k)
		}
	}
	trade = registry.VillagerTrade{MaxUses: 4, XP: 1}
	cost := func(key string, req bool) registry.TradeCost {
		v, ok := t[key]
		if !ok {
			if req {
				fail("No key %s in MapLike[%s]", key, snbt(t))
			}
			return registry.TradeCost{}
		}
		o := object(v, key)
		name := registry.Qualify(requireStr(o, "id"))
		if _, ok := registry.ItemID(name); !ok {
			fail("Unknown item %s", name)
		}
		c := registry.TradeCost{Item: name, Count: 1}
		if raw, ok := o["count"]; ok {
			if n, isNum := num(raw); isNum {
				c.Count = int32(n)
			} else {
				trade.Unsupported = true
			}
		}
		if comps, ok := o["components"]; ok {
			if isWaterComponents(comps) {
				c.Water = true
			} else {
				trade.Unsupported = true
			}
		}
		return c
	}
	trade.Wants = cost("wants", true)
	trade.Extra = cost("additional_wants", false)
	trade.GivesStack = new(tradeResult(required(t, "gives")))
	constant := func(key string, dst *int32) {
		if v, ok := t[key]; ok {
			if n, isNum := num(v); isNum {
				*dst = int32(n)
			} else {
				trade.Unsupported = true
			}
		}
	}
	constant("max_uses", &trade.MaxUses)
	constant("xp", &trade.XP)
	if v, ok := t["reputation_discount"]; ok {
		if n, isNum := num(v); isNum {
			trade.Discount = float32(n)
		} else {
			trade.Unsupported = true
		}
	}
	if v, ok := t["merchant_predicate"]; ok {
		if variants, ok := merchantVariants(v); ok {
			trade.Variants = variants
		} else {
			trade.Unsupported = true
		}
	}
	if v, ok := t["given_item_modifier"]; ok {
		if _, both := t["given_item_modifiers"]; both {
			fail("both given_item_modifier and given_item_modifiers")
		}
		trade.Modifiers = (lootDecoder{legacyFunctions: true}).modifierHolders(v)
	} else {
		trade.Modifiers = (lootDecoder{legacyFunctions: true}).modifierHolders(t["given_item_modifiers"])
	}
	if v, ok := t["double_trade_price_enchantments"]; ok {
		trade.DoublePrice = holderSet(v, "enchantment", registry.EnchantmentTagMembers)
		if tag, ok := v.(string); ok && strings.HasPrefix(tag, "#") {
			trade.DoublePrice, trade.DoublePriceTag = nil, registry.Qualify(tag[1:])
		}
	}
	return trade, nil
}

// tradeResult reads a result template, independently of wanted-item predicates.
// vanilla: ItemStackTemplate.CODEC, ItemStackTemplate.MAP_CODEC
func tradeResult(v any) protocol.ItemStack {
	if name, ok := v.(string); ok {
		return protocol.ItemStack{ItemID: recipeItem(name), Count: 1}
	}
	o := object(v, "gives")
	stack := protocol.ItemStack{ItemID: recipeItem(requireStr(o, "id")), Count: 1}
	if _, ok := o["count"]; ok {
		// Codec.INT truncates and wraps before intRange checks the result.
		stack.Count = int32(int64(requireNum(o, "count")))
		if stack.Count < 1 || stack.Count > 99 {
			fail("result count %v is not in 1..99", o["count"])
		}
	}
	if components, ok := o["components"]; ok {
		for name, value := range object(components, "components") {
			tag, n, err := nbt.ParseValueTag(snbt(value))
			if err != nil || n == 0 || !registry.ReadComponent(&stack, name, tag) {
				fail("Failed to decode %s: %s", name, snbt(value))
			}
		}
	}
	return stack
}

// isWaterComponents is whether a wanted item's components are exactly the
// water potion, the one component predicate the server evaluates.
func isWaterComponents(v any) bool {
	comps, ok := v.(obj)
	if !ok || len(comps) != 1 {
		return false
	}
	contents, ok := comps["minecraft:potion_contents"].(obj)
	if !ok || len(contents) != 1 {
		return false
	}
	potion, _ := contents["potion"].(string)
	return registry.Qualify(potion) == "minecraft:water"
}

// merchantVariants reads a merchant predicate that tests only the villager's
// variant: an entity_properties condition on this.
func merchantVariants(v any) ([]string, bool) {
	c, ok := v.(obj)
	if !ok {
		return nil, false
	}
	kind, _ := c["condition"].(string)
	if kind == "" {
		kind, _ = c["type"].(string)
	}
	if registry.Qualify(kind) != "minecraft:entity_properties" || c["entity"] != "this" {
		return nil, false
	}
	pred, _ := c["predicate"].(obj)
	preds, _ := pred["minecraft:predicates"].(obj)
	if len(pred) != 1 || len(preds) != 1 {
		return nil, false
	}
	switch variant := preds["minecraft:villager/variant"].(type) {
	case string:
		return []string{registry.Qualify(variant)}, true
	case []any:
		out := make([]string, len(variant))
		for i, x := range variant {
			s, ok := x.(string)
			if !ok {
				return nil, false
			}
			out[i] = registry.Qualify(s)
		}
		return out, true
	}
	return nil, false
}

// DecodeTradeSet reads a trade_set file. Amount must be a constant.
// vanilla: TradeSet.CODEC
func DecodeTradeSet(data []byte) (set registry.PackTradeSet, err error) {
	defer recoverLoot(&err)
	o := object(parseJSON(data), "trade set")
	amount, ok := num(required(o, "amount"))
	if !ok {
		fail("amount is not a constant")
	}
	set.Amount = int(amount)
	if b, ok := o["allow_duplicates"].(bool); ok {
		set.Duplicates = b
	}
	switch trades := required(o, "trades").(type) {
	case string:
		set.Trades = []registry.PackTagEntry{tradeHolder(trades)}
	case []any:
		for _, e := range trades {
			set.Trades = append(set.Trades, tradeHolder(str(e, "trade")))
		}
	default:
		fail("trades is not a holder set")
	}
	return set, nil
}

func tradeHolder(s string) registry.PackTagEntry {
	id, tag := strings.CutPrefix(s, "#")
	return registry.PackTagEntry{ID: registry.Qualify(id), Tag: tag, Required: true}
}
