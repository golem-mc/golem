package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func BenchmarkSummonDuplicate(b *testing.B) {
	for _, items := range []int{0, 2000} {
		b.Run(fmt.Sprintf("items=%d", items), func(b *testing.B) {
			s, w := newBlockBenchServer(b, 0)
			stack := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
			s.itemsMu.Lock()
			for i := range items {
				x, z := float64(i%32)-15.5, float64(i/32%32)-15.5
				s.appendItemLocked(s.newItemEntity(w, stack, x, 64, z, 0, 0, 0, 0))
			}
			s.itemsMu.Unlock()
			id := uuid.New()
			b.ReportAllocs()
			for b.Loop() {
				if s.summonDuplicate(id) {
					b.Fatal("a fresh UUID clashed")
				}
			}
		})
	}
}
