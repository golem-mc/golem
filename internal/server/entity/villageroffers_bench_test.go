package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// BenchmarkVillagerOfferState measures saving a restock or missed-day demand
// update for a villager with a full set of offers.
func BenchmarkVillagerOfferState(b *testing.B) {
	for _, catchUp := range []bool{false, true} {
		name := "restock"
		if catchUp {
			name = "catch_up"
		}
		b.Run(name, func(b *testing.B) {
			m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
			m.OfferUpdater = testOfferUpdater{}
			recipes := nbt.List{Elem: nbt.TypeCompound}
			for range 10 {
				recipes.Items = append(recipes.Items, nbt.Compound{
					"buy":  nbt.Compound{"id": nbt.String("minecraft:emerald"), "count": nbt.Int(1)},
					"sell": nbt.Compound{"id": nbt.String("minecraft:bread"), "count": nbt.Int(6)},
					"uses": nbt.Int(6), "maxUses": nbt.Int(12), "demand": nbt.Int(3),
					"xp": nbt.Int(2), "priceMultiplier": nbt.Float(0.05),
				})
			}
			offers := nbt.Compound{"Recipes": recipes}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				m.Offers, m.vb.restocksToday = offers, 0
				if catchUp {
					m.catchUpDemand()
				} else {
					m.restock()
				}
			}
		})
	}
}
