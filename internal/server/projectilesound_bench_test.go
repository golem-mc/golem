package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
)

// BenchmarkProjectileSound measures deferred impact encoding, including its pitch draw.
func BenchmarkProjectileSound(b *testing.B) {
	s := &Server{}
	a := entity.NewArrow(1, 1.5, 64, -2.5, 1, 0, 0, false, 2, 0, 0, 1)
	b.ReportAllocs()
	for b.Loop() {
		s.playPending(arrowHitSound(nil, a, a.HitSound))
	}
}
