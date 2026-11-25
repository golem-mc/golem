package entity

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkDistantLoot(b *testing.B) {
	items := make([]LootItem, 2048)
	for i := range items {
		items[i] = LootItem{
			X: float64(500 + i%64), Y: 64, Z: float64(i / 64),
			Stack: protocol.ItemStack{ItemID: int32(registry.ItemIronHelmet), Count: 1},
		}
	}
	for _, n := range []int{1, 64, 256} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			mobs := make(Mobs, n)
			for i := range mobs {
				m := NewMob(TypeZombie, int32(i+1), float64(i%16*4), 64, float64(i/16*4), int64(i+1))
				m.CanPickUpLoot = true
				mobs[i] = m
			}
			b.ReportAllocs()
			for b.Loop() {
				if takes := mobs.TakeLoot(items); len(takes) != 0 {
					b.Fatalf("distant loot yielded %d takes, want zero", len(takes))
				}
			}
		})
	}
}

func BenchmarkPillagerLootPickup(b *testing.B) {
	m := NewMob(TypePillager, 1, 0.5, 64, 0.5, 1)
	m.CanPickUpLoot = true
	mobs := Mobs{m}
	items := []LootItem{
		{X: 0.5, Y: 64, Z: 0.5, Stack: protocol.ItemStack{ItemID: int32(registry.ItemIronHelmet), Count: 1}},
		{X: 0.5, Y: 64, Z: 0.5, Stack: protocol.ItemStack{ItemID: int32(registry.ItemWhiteBanner), Count: 1}},
	}
	b.ReportAllocs()
	for b.Loop() {
		mobs.TakeLoot(items)
	}
}

func BenchmarkWantedItemSensing(b *testing.B) {
	w := griefingWorld{newTestWorld()}
	items := make([]LootItem, 2048)
	for i := range items {
		items[i] = LootItem{
			X: float64(512 + i%64), Y: 64, Z: float64(i / 64),
			Stack: protocol.ItemStack{ItemID: int32(registry.ItemGoldIngot), Count: 1},
		}
	}
	for _, kind := range []struct {
		name string
		typ  MobType
	}{
		{"piglin", TypePiglin},
		{"sulfur_cube", TypeSulfurCube},
	} {
		for _, n := range []int{1, len(items)} {
			b.Run(kind.name+"/drops="+strconv.Itoa(n), func(b *testing.B) {
				mobs := make(Mobs, 128)
				for i := range mobs {
					m := NewMob(kind.typ, int32(i+1), float64(i%16*4), 64, float64(i/16*4), int64(i+1))
					m.CanPickUpLoot = true
					mobs[i] = m
				}
				var scratch BoxGrid
				b.ReportAllocs()
				for b.Loop() {
					if kind.typ == TypePiglin {
						for _, m := range mobs {
							m.barter.scanIn = 1
						}
					}
					grid := mobs.PrepareLootGrid(items[:n], &scratch, true)
					mobs.SenseWantedItemsWithGrid(w, items[:n], grid, true)
					for _, m := range mobs {
						if m.barter.hasWanted || m.sulfur.hasWanted {
							b.Fatalf("mob %d wanted a distant item", m.ID)
						}
					}
				}
			})
		}
	}
}
