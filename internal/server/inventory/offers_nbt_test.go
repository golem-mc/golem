package inventory

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// vanilla: MerchantOffer.CODEC, NbtOps.getNumberValue, NbtOps.getBooleanValue
func TestOffersFromTagReadsNumericCodecValues(t *testing.T) {
	for name, number := range map[string]nbt.Tag{
		"byte": nbt.Byte(2), "short": nbt.Short(2), "int": nbt.Int(2),
		"long": nbt.Long(2), "float": nbt.Float(2.75), "double": nbt.Double(2.75),
		"invalid": nbt.String("2"),
	} {
		t.Run(name, func(t *testing.T) {
			recipe := recipeOf("minecraft:emerald", "minecraft:bread", 0)
			for _, key := range []string{"uses", "maxUses", "xp", "specialPrice", "demand", "priceMultiplier", "rewardExp"} {
				recipe[key] = number
			}
			offer, ok := offerFromTag(recipe)
			if !ok {
				t.Fatal("numeric recipe was rejected")
			}
			want := Offer{CostA: offer.CostA, Result: offer.Result, Uses: 2, MaxUses: 2, XP: 2, SpecialPrice: 2, Demand: 2, PriceMultiplier: 2, RewardExp: true}
			if name == "float" || name == "double" {
				want.PriceMultiplier = 2.75
			} else if name == "invalid" {
				want = Offer{CostA: offer.CostA, Result: offer.Result, MaxUses: 4, XP: 1, RewardExp: true}
			}
			if !reflect.DeepEqual(offer, want) {
				t.Errorf("offer = %+v, want %+v", offer, want)
			}
		})
	}
	for _, reward := range []nbt.Tag{nbt.Short(0), nbt.Int(0), nbt.Long(0), nbt.Float(0), nbt.Double(0), nbt.Double(0.5)} {
		recipe := recipeOf("minecraft:emerald", "minecraft:bread", 0)
		recipe["rewardExp"] = reward
		offer, ok := offerFromTag(recipe)
		if !ok || offer.RewardExp != (reward == nbt.Double(0.5)) {
			t.Errorf("rewardExp %v: offer = %+v, ok = %v", reward, offer, ok)
		}
	}
}

func recipeOf(buy, sell string, uses int32) nbt.Compound {
	return nbt.Compound{
		"buy":     nbt.Compound{"id": nbt.String(buy), "count": nbt.Int(2)},
		"sell":    nbt.Compound{"id": nbt.String(sell), "count": nbt.Int(6)},
		"uses":    nbt.Int(uses),
		"maxUses": nbt.Int(16),
		"xp":      nbt.Int(5),
	}
}
