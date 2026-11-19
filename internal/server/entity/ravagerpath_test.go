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
