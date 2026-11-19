package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestBoundCreakingPathStopsAtTheHomeLeash(t *testing.T) {
	w := newTestWorld()
	floor(w, -5, -3, 60, 3, 63, registry.BlockStone.DefaultState())
	target := Waypoint{45, 64, 0}
	for _, bound := range []bool{false, true} {
		m := NewMob(TypeCreaking, 1, 0.5, 64, 0.5, 1)
		if bound {
			m.CreakingBind(0, 64, 0)
		}
		path, _ := findPath(w, m.pathFootprint(w), Waypoint{0, 64, 0}, target, 64, 0)
		end := path[len(path)-1]
		if bound && end.X != 31 || !bound && end.X != 45 {
			t.Fatalf("bound=%v: path ends at %v", bound, end)
		}
	}
}
