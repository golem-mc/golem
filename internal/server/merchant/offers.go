// Package merchant owns the saved trade codecs shared by entities and menus.
package merchant

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// ReadOffers writes loaded offers in codec form. A list with bad recipes still
// has a partial result containing the valid ones.
// vanilla: MerchantOffers.CODEC, TagValueInput.read
func ReadOffers(tag nbt.CompoundTag) nbt.Compound {
	if tag == nil {
		return nil
	}
	recipes, _ := nbt.Get[nbt.List](tag.Entries(), "Recipes")
	items := make([]nbt.Tag, 0, len(recipes.Items))
	for _, item := range recipes.Items {
		if recipe, ok := nbt.CompoundView(nbt.ListElement(item)); ok {
			if saved := readOfferRecipe(recipe); saved != nil {
				items = append(items, saved)
			}
		}
	}
	out := nbt.Compound{}
	if len(items) != 0 {
		out["Recipes"] = nbt.NewList(items)
	}
	return out
}

// vanilla: MerchantOffer.CODEC
func readOfferRecipe(recipe nbt.Compound) nbt.CompoundTag {
	buy := readOfferCost(recipe["buy"], true)
	sell := readOfferResult(recipe["sell"])
	if buy == nil || sell == nil {
		return nil
	}
	out := nbt.NewOrderedCompound(10)
	out.Set("buy", buy)
	// A lenient optional field drops even a partial result.
	if buyB := readOfferCost(recipe["buyB"], false); buyB != nil {
		out.Set("buyB", buyB)
	}
	out.Set("sell", sell)
	ReadState(recipe).WriteTo(out)
	return out
}

// vanilla: ItemCost.CODEC, DataComponentExactPredicate.CODEC
func readOfferCost(tag nbt.Tag, partial bool) nbt.CompoundTag {
	cost, ok := nbt.CompoundView(tag)
	if !ok {
		return nil
	}
	name, _ := nbt.Get[nbt.String](cost, "id")
	name = nbt.String(registry.Qualify(string(name)))
	id, ok := registry.ItemID(string(name))
	if !ok || registry.Item(id) == registry.ItemAir {
		return nil
	}
	count := int32(1)
	if value, present := cost["count"]; present {
		if n, valid := nbt.IntValue(value); valid && n > 0 {
			count = n
		} else if !partial {
			return nil
		}
	}
	out := nbt.NewOrderedCompound(3)
	out.Set("id", name)
	out.Set("count", nbt.Int(count))
	if value, present := cost["components"]; present {
		components, valid := readOfferComponents(value, id, false)
		if !valid && !partial {
			return nil
		}
		if components != nil && len(components.Entries()) != 0 {
			out.Set("components", components)
		}
	}
	return out
}

func readOfferComponents(tag nbt.Tag, itemID int32, patch bool) (nbt.CompoundTag, bool) {
	components, ok := tag.(nbt.CompoundTag)
	if !ok {
		return nil, false
	}
	out := nbt.NewOrderedCompound(len(components.Entries()))
	valid := true
	for _, key := range nbt.HashOrderedKeys(components) {
		keyName, removal := strings.CutPrefix(key, "!")
		name := registry.Qualify(keyName)
		if _, known := registry.ComponentID(name); !known || removal && !patch {
			valid = false
			continue
		}
		if removal {
			out.Set("!"+name, nbt.Compound{})
			continue
		}
		value := components.Entries()[key]
		stack := protocol.ItemStack{ItemID: itemID, Count: 1}
		if registry.ReadComponent(&stack, name, value) {
			if encoded, present := registry.StackComponents(stack)[name]; present {
				value = encoded
			}
		} else if registry.ComponentRefused(name, value) {
			valid = false
			continue
		}
		out.Set(name, nbt.Copy(value))
	}
	return out, valid
}

// vanilla: ItemStack.CODEC
func readOfferResult(tag nbt.Tag) nbt.Tag {
	stack, ok := nbt.CompoundView(tag)
	if !ok {
		return nil
	}
	name, _ := nbt.Get[nbt.String](stack, "id")
	id, ok := registry.ItemID(registry.Qualify(string(name)))
	if !ok || registry.Item(id) == registry.ItemAir {
		return nil
	}
	// The required stack accepts the codec's partial result with a default count.
	count := intOf(stack, "count", 1)
	if count < 1 || count > 99 {
		count = 1
	}
	out := registry.StackTag(protocol.ItemStack{ItemID: id, Count: count})
	components, ok := nbt.Get[nbt.CompoundTag](stack, "components")
	if !ok {
		return out
	}
	out.Set("components", components)
	if item, ok := registry.StoredStack(out); ok {
		return registry.StackTag(item)
	}
	// The dispatched component map retains entries that decoded.
	partial, _ := readOfferComponents(components, id, true)
	if len(partial.Entries()) == 0 {
		out.Delete("components")
		return out
	}
	out.Set("components", partial)
	return registry.CodecStackTag(out)
}

// State is the saved numeric state of a merchant offer.
type State struct {
	Uses, MaxUses, SpecialPrice, Demand, XP int32
	PriceMultiplier                         float32
	RewardExp                               bool
}

// ReadState reads the lenient fields of MerchantOffer.CODEC.
func ReadState(recipe nbt.Compound) State {
	state := State{
		Uses:         intOf(recipe, "uses", 0),
		MaxUses:      intOf(recipe, "maxUses", 4),
		SpecialPrice: intOf(recipe, "specialPrice", 0),
		Demand:       intOf(recipe, "demand", 0),
		XP:           intOf(recipe, "xp", 1),
		RewardExp:    true,
	}
	if b, ok := nbt.NumberValue(recipe["rewardExp"]); ok {
		state.RewardExp = b != 0
	}
	if m, ok := nbt.NumberValue(recipe["priceMultiplier"]); ok {
		state.PriceMultiplier = float32(m)
	}
	return state
}

// WriteTo writes the non-default fields in MerchantOffer.CODEC order.
func (s State) WriteTo(recipe nbt.CompoundTag) {
	reward := nbt.Byte(0)
	if s.RewardExp {
		reward = 1
	}
	for _, field := range [...]struct {
		key        string
		value, def nbt.Tag
	}{
		{"uses", nbt.Int(s.Uses), nbt.Int(0)},
		{"maxUses", nbt.Int(s.MaxUses), nbt.Int(4)},
		{"rewardExp", reward, nbt.Byte(1)},
		{"specialPrice", nbt.Int(s.SpecialPrice), nbt.Int(0)},
		{"demand", nbt.Int(s.Demand), nbt.Int(0)},
		{"priceMultiplier", nbt.Float(s.PriceMultiplier), nbt.Float(0)},
		{"xp", nbt.Int(s.XP), nbt.Int(1)},
	} {
		if field.value == field.def {
			recipe.Delete(field.key)
		} else {
			recipe.Set(field.key, field.value)
		}
	}
}

func intOf(tag nbt.Compound, key string, def int32) int32 {
	if v, ok := nbt.IntValue(tag[key]); ok {
		return v
	}
	return def
}
