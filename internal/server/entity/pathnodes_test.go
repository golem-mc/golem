package entity

import (
	"encoding/json"
	"os"
	"slices"
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

func TestPathNodeSearchKeepsBaselineWaypoints(t *testing.T) {
	b, err := os.ReadFile("testdata/pathnodes.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []pathOutcome
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	pathNodeCases(t, func(n int, s *pathScratch, w World, f footprint, from, to Waypoint, r int32) {
		path, ok := s.search(w, f, from, to, r, 0)
		if n >= len(want) || ok != want[n].Reached || !slices.Equal(path, want[n].Path) {
			t.Fatalf("case %d: %v (%v), want %+v", n, path, ok, want[n])
		}
		fresh, reached := newPathScratch(false).search(w, f, from, to, r, 0)
		if reached != ok || !slices.Equal(fresh, path) {
			t.Fatalf("case %d differs on fresh scratch", n)
		}
		uncached, reached := newPathScratch(true).search(w, f, from, to, r, 0)
		if reached != ok || !slices.Equal(uncached, path) {
			t.Fatalf("case %d differs without block cache", n)
		}
	})
}
