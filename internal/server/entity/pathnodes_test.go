package entity

import (
	"testing"
)

type pathOutcome struct {
	Path    []Waypoint
	Reached bool
}

// Exercise both scratch layouts, unreachable cells and mutation between pooled searches.
func pathNodeCases(t *testing.T, visit func(int, *pathScratch, World, footprint, Waypoint, Waypoint, int32)) {
	t.Helper()
	n := 0
	for seed := int64(1); seed <= 40; seed++ {
		w := randomPathWorld(t, seed)
		from := Waypoint{0, 60, 0}
		for _, kind := range []MobType{TypeGoat, TypeFrog} {
			f := NewMob(kind, 1, 0.5, 60, 0.5, 1).pathFootprint(w)
			s := newPathScratch(false)
			for _, r := range []int32{8, 24} {
				for _, to := range []Waypoint{{2, 60, 1}, {9, 60, 7}, {0, 80, 0}} {
					visit(n, s, w, f, from, to, r)
					n++
					s.reset()
				}
			}
			w.set(1, 60, 0, defaultState(t, "minecraft:stone"))
			visit(n, s, w, f, from, Waypoint{9, 60, 7}, 24)
			n++
			s.reset()
		}
	}
}
