package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// vanilla: Ravager.<init> setPathfindingMalus, PathType.LEAVES
func TestOnlyRavagersPathThroughALeafWall(t *testing.T) {
	for _, typ := range []MobType{TypeRavager, TypeZombie} {
		w := newTestWorld()
		floor(w, -4, -6, 4, 6, 63, registry.BlockStone.DefaultState())
		for y := int32(64); y < 67; y++ {
			floor(w, -4, 0, 4, 0, y, registry.BlockOakLeaves.DefaultState())
		}
		m := NewMob(typ, 1, 0.5, 64, -4.5, 1)
		path, reached := findPath(w, m.pathFootprint(w), Waypoint{0, 64, -5}, Waypoint{0, 64, 4}, 16, 0)
		if reached != (typ == TypeRavager) {
			t.Errorf("%v: reached=%v, path=%v", typ, reached, path)
		}
	}
}

func TestARavagerPlansDiagonalsThroughLeaves(t *testing.T) {
	for _, tc := range []struct {
		kind    MobType
		barrier registry.Block
		want    bool
	}{
		{TypeRavager, registry.BlockOakLeaves, true},
		{TypeZombie, registry.BlockOakLeaves, false},
		{TypeRavager, registry.BlockStone, false},
	} {
		w := newTestWorld()
		floor(w, -2, -2, 4, 4, 63, registry.BlockStone.DefaultState())
		for y := int32(64); y < 67; y++ {
			w.set(1, y, 0, tc.barrier.DefaultState())
			w.set(0, y, 1, tc.barrier.DefaultState())
		}
		m := NewMob(tc.kind, 1, 0.5, 64, 0.5, 1)
		if got := stepTaken(w, m.pathFootprint(w), Waypoint{0, 64, 0}, pathStep{dx: 1, dz: 1}); got != tc.want {
			t.Errorf("%v through %v: diagonal=%v, want %v", tc.kind, tc.barrier, got, tc.want)
		}
	}
}
