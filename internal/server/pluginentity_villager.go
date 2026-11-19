package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

type (
	villagerOps struct{ e pluginEntity }

	pluginVillagerEntity struct {
		pluginEntity
		livingOps
		mobOps
		ageOps
		villagerOps
	}
)

// pluginVillagerOf wraps a villager or wandering trader, nil for any other.
func pluginVillagerOf(base pluginEntity, t registry.EntityType) plugin.Entity {
	if t != registry.EntityTypeVillager && t != registry.EntityTypeWanderingTrader {
		return nil
	}
	return pluginVillagerEntity{pluginEntity: base, livingOps: livingOps{base}, mobOps: mobOps{base}, ageOps: ageOps{base}, villagerOps: villagerOps{base}}
}

// villagerData is the saved VillagerData of a villager; a wandering trader
// has none.
// vanilla: Villager.addAdditionalSaveData
func (o villagerOps) villagerData() nbt.Compound {
	data, _ := nbt.Get[nbt.Compound](o.e.saved(), "VillagerData")
	return data
}

func (o villagerOps) dataKey(field string) plugin.Key {
	name, _ := nbt.Get[nbt.String](o.villagerData(), field)
	k, _ := plugin.ParseKey(string(name))
	return k
}

func (o villagerOps) setDataKey(field string, k plugin.Key) {
	if o.e.Type() == registry.EntityTypeVillager {
		o.e.mergeInto("VillagerData", field, nbt.String(k.String()))
	}
}

func (o villagerOps) Profession() plugin.Key       { return o.dataKey("profession") }
func (o villagerOps) SetProfession(k plugin.Key)   { o.setDataKey("profession", k) }
func (o villagerOps) VillagerType() plugin.Key     { return o.dataKey("type") }
func (o villagerOps) SetVillagerType(k plugin.Key) { o.setDataKey("type", k) }

func (o villagerOps) Level() int {
	n, _ := nbt.IntValue(o.villagerData()["level"])
	return int(n)
}

// SetLevel keeps the level in vanilla's 1 to 5.
// vanilla: VillagerData.CODEC
func (o villagerOps) SetLevel(n int) {
	if o.e.Type() == registry.EntityTypeVillager {
		o.e.mergeInto("VillagerData", "level", nbt.Int(int32(min(max(n, 1), 5))))
	}
}

func (o villagerOps) Experience() int { return o.e.intField("Xp") }
func (o villagerOps) SetExperience(n int) {
	if o.e.Type() == registry.EntityTypeVillager {
		o.e.merge(nbt.Compound{"Xp": nbt.Int(int32(max(n, 0)))})
	}
}

// Trades reads the offers as saved.
// vanilla: MerchantOffer.CODEC
func (o villagerOps) Trades() []plugin.Trade {
	offers, _ := nbt.Get[nbt.Compound](o.e.saved(), "Offers")
	recipes, _ := nbt.Get[nbt.List](offers, "Recipes")
	out := make([]plugin.Trade, 0, len(recipes.Items))
	for _, item := range recipes.Items {
		r, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		out = append(out, tradeOfRecipe(r))
	}
	return out
}

// tradeOfRecipe reads one saved offer.
func tradeOfRecipe(r nbt.Compound) plugin.Trade {
	t := plugin.Trade{
		Uses:              tradeInt(r, "uses", 0),
		MaxUses:           tradeInt(r, "maxUses", 4),
		XP:                tradeInt(r, "xp", 1),
		Demand:            tradeInt(r, "demand", 0),
		SpecialPrice:      tradeInt(r, "specialPrice", 0),
		RewardsExperience: true,
	}
	if v, ok := nbt.NumberValue(r["rewardExp"]); ok {
		t.RewardsExperience = v != 0
	}
	if v, ok := nbt.NumberValue(r["priceMultiplier"]); ok {
		t.PriceMultiplier = float32(v)
	}
	t.Cost = tradeCost(r, "buy")
	t.Cost2 = tradeCost(r, "buyB")
	if sell, ok := nbt.Get[nbt.Compound](r, "sell"); ok {
		if s, ok := registry.StoredStack(sell); ok {
			t.Result = pluginStack(s)
		}
	}
	return t
}

func tradeInt(r nbt.Compound, key string, def int) int {
	if n, ok := nbt.IntValue(r[key]); ok {
		return int(n)
	}
	return def
}

// tradeCost reads an ItemCost: an id and a count of 1 unless saved.
// vanilla: ItemCost.CODEC
func tradeCost(r nbt.Compound, key string) plugin.ItemStack {
	c, ok := nbt.Get[nbt.Compound](r, key)
	if !ok {
		return plugin.ItemStack{}
	}
	name, _ := nbt.Get[nbt.String](c, "id")
	id, ok := registry.ItemID(registry.Qualify(string(name)))
	if !ok {
		return plugin.ItemStack{}
	}
	return pluginStack(protocol.ItemStack{ItemID: id, Count: int32(tradeInt(c, "count", 1))})
}

// SetTrades replaces the offers; a trade whose items the registry does not
// know is left out.
func (o villagerOps) SetTrades(trades []plugin.Trade) {
	items := make([]nbt.Tag, 0, len(trades))
	for _, t := range trades {
		r, ok := recipeOfTrade(t)
		if !ok {
			continue
		}
		items = append(items, r)
	}
	o.e.merge(nbt.Compound{"Offers": nbt.Compound{"Recipes": nbt.NewList(items)}})
}

// recipeOfTrade is tradeOfRecipe's inverse; a trade whose items the registry
// does not know has none.
func recipeOfTrade(t plugin.Trade) (nbt.Compound, bool) {
	cost, ok := pluginProtocolStack(t.Cost)
	result, ok2 := pluginProtocolStack(t.Result)
	if !ok || !ok2 || cost.Empty() || result.Empty() {
		return nil, false
	}
	r := nbt.Compound{
		"buy":             costTag(cost),
		"sell":            registry.StackTag(result),
		"uses":            nbt.Int(int32(t.Uses)),
		"maxUses":         nbt.Int(int32(t.MaxUses)),
		"xp":              nbt.Int(int32(t.XP)),
		"demand":          nbt.Int(int32(t.Demand)),
		"specialPrice":    nbt.Int(int32(t.SpecialPrice)),
		"priceMultiplier": nbt.Float(t.PriceMultiplier),
		"rewardExp":       nbt.Byte(boolByte(t.RewardsExperience)),
	}
	if second, ok := pluginProtocolStack(t.Cost2); ok && !second.Empty() {
		r["buyB"] = costTag(second)
	}
	return r, true
}

func costTag(s protocol.ItemStack) nbt.Compound {
	name, _ := registry.ItemName(s.ItemID)
	return nbt.Compound{"id": nbt.String(name), "count": nbt.Int(s.Count)}
}

// Trader is the player whose merchant window is open.
// vanilla: AbstractVillager.getTradingPlayer
func (o villagerOps) Trader() (plugin.Player, bool) {
	b, ok := o.e.live()
	if !ok {
		return nil, false
	}
	snap, ok := b.(*entitySnapshot)
	if !ok {
		return nil, false
	}
	mob, ok := snap.ref.(*mobEntity)
	if !ok {
		return nil, false
	}
	srv := o.e.host.srv
	srv.mobsMu.Lock()
	trading, id := mob.mob.Trading, mob.mob.TradingPlayer
	srv.mobsMu.Unlock()
	if !trading {
		return nil, false
	}
	c := srv.clientByUUID(id)
	if c == nil {
		return nil, false
	}
	var cmd command.Entity = c
	p, ok := o.e.host.pluginEntityOf(cmd).(plugin.Player)
	return p, ok
}
