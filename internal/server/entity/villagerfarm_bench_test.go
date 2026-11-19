package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobsTickFarm measures the farming sensors, work selection and
// navigation. Like Village, it measures simulation without server effects.
func BenchmarkMobsTickFarm(b *testing.B) {
	w := carryWorld{testWorld: newTestWorld()}
	floor(w.testWorld, -5, -5, 35, 20, 63, defaultState(b, "minecraft:stone"))
	var mobs Mobs
	for i := range 24 {
		x, z := int32(i%8*4), int32(i/8*4)
		w.set(x, 63, z, defaultState(b, "minecraft:farmland"))
		w.set(x, 64, z, defaultState(b, "minecraft:wheat"))
		w.set(x+1, 64, z, defaultState(b, "minecraft:composter"))
		m := NewMob(TypeVillager, int32(i+1), float64(x)+0.5, 64, float64(z)+0.5, int64(i+1))
		m.Village = fakeVillage{w: w.testWorld}
		m.Villager.Profession, m.VillagerXp = "minecraft:farmer", 1
		m.SetJobSite("minecraft:overworld", x+1, 64, z)
		m.barter.Inventory[0] = protocol.ItemStack{ItemID: registry.MustItem("minecraft:wheat_seeds"), Count: 32}
		m.barter.Inventory[1] = protocol.ItemStack{ItemID: registry.MustItem("minecraft:bone_meal"), Count: 64}
		mobs = append(mobs, m)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := range b.N {
		for _, m := range mobs {
			m.GameTime, m.DayTime = int64(n)+1, 3000
		}
		mobs.Tick(w, nil, nil)
	}
}
