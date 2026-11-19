package entity

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobsTickAllays steps a hundred allays, each holding a diamond and
// liking a player, with a scan of the diamonds on the ground each tick: the
// errand planning, flight paths and wanted-item scan.
func BenchmarkMobsTickAllays(b *testing.B) {
	w := newTestWorld()
	floor(w, -65, -65, 64, 64, 63, defaultState(b, "minecraft:stone"))
	diamond := protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: 1}
	liked := uuid.New()
	players := []Player{{X: 0, Y: 64, Z: 0}}
	players[0].UUID = liked
	mobs := make(Mobs, 0, 100)
	items := make([]LootItem, 0, 20)
	for i := range 100 {
		m := NewMob(TypeAllay, int32(i+1), float64(i%10)*6-30, 66, float64(i/10)*6-30, int64(i+1))
		m.UseAllay(diamond, protocol.HandMain, liked, 1)
		mobs = append(mobs, m)
	}
	for i := range 20 {
		items = append(items, LootItem{X: float64(i)*3 - 30, Y: 64, Z: 20, Stack: diamond})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.SenseWantedItems(w, items)
		mobs.Tick(w, players, nil)
	}
}

// BenchmarkAllayDeposit exercises remembered note blocks at and beyond the range limit.
func BenchmarkAllayDeposit(b *testing.B) {
	for _, x := range []float64{1024.5, 1025.5} {
		name := "boundary"
		if x > 1025 {
			name = "beyond"
		}
		b.Run(name, func(b *testing.B) {
			w := newTestWorld()
			w.set(0, 64, 0, registry.BlockStates["minecraft:note_block"])
			m := NewMob(TypeAllay, 1, x, 65, 0.5, 1)
			m.Village = fakeVillage{w: w}
			m.allay.liked = uuid.New()
			m.allay.noteTicks = allayNoteMemory
			note := GlobalPos{"minecraft:overworld", 0, 64, 0}
			tick := &tickView{w: w, players: []Player{{UUID: m.allay.liked, X: x - 2, Y: 64, Z: 0.5}}}
			b.ReportAllocs()
			for b.Loop() {
				m.allay.note = &note
				m.allayDeposit(tick)
			}
		})
	}
}
