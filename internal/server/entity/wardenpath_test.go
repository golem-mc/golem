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

func TestWardenPathPrefersClimbingToALevelDetour(t *testing.T) {
	w := wardenPathWorld()
	floor(w, 3, -2, 5, 2, 64, registry.BlockAir.DefaultState())
	for _, x := range []int32{3, 6} {
		w.set(x, 64, 0, registry.BlockStone.DefaultState())
	}
	foot := newTestWarden().pathFootprint(w)
	if !foot.flatDistance {
		t.Fatal("warden does not use XZ edge distance")
	}
	for _, flat := range []bool{false, true} {
		foot.flatDistance = flat
		path, reached := findPath(w, foot, Waypoint{0, 64, 0}, Waypoint{9, 64, 0}, 16, 0)
		if !reached {
			t.Fatalf("flat=%v: target not reached: %v", flat, path)
		}
		climbed := false
		for _, node := range path {
			climbed = climbed || node.Y > 64
		}
		if climbed != flat {
			t.Fatalf("flat=%v: climbed=%v, path=%v", flat, climbed, path)
		}
	}
}
