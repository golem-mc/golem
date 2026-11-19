package inventory

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func BenchmarkUpdateOfferState(b *testing.B) {
	recipes := make([]nbt.Tag, 12)
	for i := range recipes {
		recipes[i] = nbt.Compound{
			"buy":  nbt.Compound{"id": nbt.String("minecraft:emerald"), "count": nbt.Int(2)},
			"buyB": nbt.Compound{"id": nbt.String("minecraft:book"), "count": nbt.Int(1)},
			"sell": nbt.Compound{"id": nbt.String("minecraft:bread"), "count": nbt.Int(6)},
			"uses": nbt.Int(3), "maxUses": nbt.Int(12), "demand": nbt.Int(1),
		}
	}
	tag := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: recipes}}
	restock := func(uses, maxUses, demand int32) (int32, int32) {
		return 0, demand + uses - (maxUses - uses)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = UpdateOfferState(tag, restock)
	}
}
