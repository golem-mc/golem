package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkFireCanSpread measures the radius gate for lava 5000 blocks from
// 11 players, so every player is tested.
func BenchmarkFireCanSpread(b *testing.B) {
	s, w := newBlockBenchServer(b, 0)
	addBlockBenchPlayers(b, s, w, 11, -5, 5)
	dim := s.dimensionOfWorld(w)
	s.refreshFireSpreadPlayers(dim)
	chk := fireSpreadChecker{s: s, dim: dim}
	if w.IntRule(world.RuleFireSpread) < 0 {
		b.Fatal("fire_spread_radius_around_player is unlimited")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if chk.FireCanSpread(5000, 64, 5000) {
			b.Fatal("fire spread from 5000 blocks away")
		}
	}
}
