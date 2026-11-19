package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkHorseLandingSends builds the sounds and damage feedback for a landing.
func BenchmarkHorseLandingSends(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	m := entity.NewMob(entity.TypeHorse, 1, 5.5, world.GroundLevel, 5.5, 1)
	for _, tc := range []struct {
		name           string
		landed, damage float64
		hurt           bool
	}{
		{"accepted", 9, 1, true},
		{"rejected", 9, 1, false},
		{"safe", 3, 0, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				s.horseLandedSends(w, m, tc.landed, tc.damage, tc.hurt, false, rules.DamageTypeFall)
			}
		})
	}
}
