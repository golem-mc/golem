package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkVindicatorBannerPath searches for a banner across a wall with a
// passage thirteen blocks from the start.
func BenchmarkVindicatorBannerPath(b *testing.B) {
	w := newTestWorld()
	stone := registry.BlockStone.DefaultState()
	floor(w, -20, -3, 16, 4, 63, stone)
	for y := int32(64); y <= 66; y++ {
		floor(w, -20, 1, 12, 1, y, stone)
	}
	m := NewMob(TypeVindicator, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	b.ReportAllocs()
	for b.Loop() {
		m.createReachingPath(w, 12.5, 64, 2.5)
	}
}
