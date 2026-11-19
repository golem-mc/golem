package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func BenchmarkBrewingMatch(b *testing.B) {
	bottle := protocol.ItemStack{ItemID: int32(ItemPotion), Count: 1}
	bottle.SetPotion(MustPotion("minecraft:awkward"))
	reagent := protocol.ItemStack{ItemID: int32(ItemSugar), Count: 1}
	b.ReportAllocs()
	for b.Loop() {
		BrewingMatch(bottle, reagent)
	}
}
