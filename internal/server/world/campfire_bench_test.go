package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkCampfireTick measures a lit campfire with four occupied slots,
// either counting progress up or finishing every slot, on an ordinary input
// and on one whose hinted recipe is a later match of an overlapping pack.
func BenchmarkCampfireTick(b *testing.B) {
	bone := protocol.ItemStack{ItemID: int32(registry.ItemBone), Count: 1}
	beef := protocol.ItemStack{ItemID: int32(registry.ItemBeef), Count: 1}
	for _, c := range []struct {
		name  string
		food  protocol.ItemStack
		hint  string
		pack  bool
		final bool
	}{
		{"advance", beef, "", false, false},
		{"complete", beef, "", false, true},
		{"overlap_advance", bone, "pk:b", true, false},
		{"overlap_complete", bone, "pk:b", true, true},
	} {
		b.Run(c.name, func(b *testing.B) {
			if c.pack {
				b.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
				in := func(items ...int32) registry.BookIngredient { return registry.BookIngredient{Items: items} }
				stick, bowl, feather, str := int32(registry.ItemStick), int32(registry.ItemBowl), int32(registry.ItemFeather), int32(registry.ItemString)
				mk := func(name string, ing registry.BookIngredient, result int32) registry.PackCooking {
					return registry.PackCooking{Name: name, Ingredient: ing, ResultID: result, ResultCount: 1, Duration: 100}
				}
				var p registry.PackRecipes
				p.Cooking[registry.CookingCampfire] = []registry.PackCooking{
					mk("pk:a", in(feather, int32(registry.ItemBone)), stick),
					mk("pk:b", in(int32(registry.ItemBone), str), bowl),
				}
				registry.ApplyPackRecipes(p)
			}
			be := &BlockEntity{Kind: KindCampfire, Slots: make([]protocol.ItemStack, CampfireSlots)}
			b.ReportAllocs()
			for b.Loop() {
				for i := range be.Slots {
					be.Slots[i] = c.food
					be.CampfireTotals[i] = 100
					be.CampfireProgress[i] = 0
					if c.final {
						be.CampfireProgress[i] = 99
					}
				}
				be.CookCheck = c.hint
				be.CampfireTick(true)
			}
		})
	}
}
