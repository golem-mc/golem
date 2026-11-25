package server

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/entity"
)

// BenchmarkFangsBiteDistantMobs measures an ownerless bite with registered mobs far from its jaws.
func BenchmarkFangsBiteDistantMobs(b *testing.B) {
	for _, n := range []int{0, 128, 2048} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			s, w := newBlockBenchServer(b, 3)
			h := s.herdOf(w)
			s.mobsMu.Lock()
			for i := range n {
				m := entity.NewMob(entity.TypePig, int32(i+1), 48.5+float64(i%8), 64, 48.5+float64(i%8), int64(i))
				h.add(m)
				h.remember(&mobEntity{mob: m, w: w, uuid: uuid.New()})
			}
			s.mobsMu.Unlock()
			it := fangsEffect{w: w, id: 1, x: 0.5, y: 64, z: 0.5}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.fangsBite(it)
			}
		})
	}
}
