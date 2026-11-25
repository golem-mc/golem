package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkPickupClaimLookup(b *testing.B) {
	s, w := newBlockBenchServer(b, 0)
	const stride = 8
	var drops [2048]*itemEntity
	var candidates [len(drops) / stride]itemPickup
	stack := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
	s.itemsMu.Lock()
	for i := range drops {
		x, z := float64(i%32)-15.5, float64(i/32%32)-15.5
		it := s.newItemEntity(w, stack, x, 64, z, 0, 0, 0, 0)
		s.appendItemLocked(it)
		drops[i] = it
		if i%stride == 0 {
			candidates[i/stride] = itemPickup{entity: it, item: it.item.ID}
		}
	}
	s.itemsMu.Unlock()

	b.ReportAllocs()
	for b.Loop() {
		for i, p := range candidates {
			got := p.entity
			s.itemsMu.Lock()
			live := s.canClaimItemLocked(got)
			s.itemsMu.Unlock()
			if want := drops[i*stride]; !live || got != want {
				b.Fatalf("candidate %d resolved to %p (live: %t), want %p", i, got, live, want)
			}
		}
	}
}
