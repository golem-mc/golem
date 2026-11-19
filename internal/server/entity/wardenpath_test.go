package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func wardenPathWorld() testWorld {
	w := newTestWorld()
	floor(w, -12, -12, 20, 12, 63, registry.BlockStone.DefaultState())
	// A low ridge offers a short climbing route and a longer level detour.
	floor(w, 3, -2, 5, 2, 64, registry.BlockStone.DefaultState())
	return w
}

func BenchmarkWardenPath(b *testing.B) {
	w := wardenPathWorld()
	foot := newTestWarden().pathFootprint(w)
	b.ReportAllocs()
	for b.Loop() {
		findPath(w, foot, Waypoint{0, 64, 0}, Waypoint{9, 64, 0}, 16, 0)
	}
}
