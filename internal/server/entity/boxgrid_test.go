package entity

import (
	"math/rand"
	"slices"
	"testing"
)

func TestBoxGrid(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	rb := func(span float64) AABB {
		x, y, z := rng.Float64()*160-80, rng.Float64()*10, rng.Float64()*160-80
		return NewAABB(x, y, z, x+rng.Float64()*span, y+1, z+rng.Float64()*span)
	}
	var g BoxGrid
	for round := range 3 {
		g.Reset()
		boxes := make([]AABB, 300)
		for i := range boxes {
			span := 1.0
			if i%10 == 0 {
				span = 40
			}
			boxes[i] = rb(span)
			g.Add(int32(i), boxes[i])
		}
		for range 300 {
			q := rb(rng.Float64() * 30)
			var want, got []int32
			for i, b := range boxes {
				if q.Crosses(b) {
					want = append(want, int32(i))
				}
			}
			var all []int32
			g.Query(q, func(i int32) {
				all = append(all, i)
				if q.Crosses(boxes[i]) {
					got = append(got, i)
				}
			})
			if !slices.IsSorted(all) || len(slices.Compact(slices.Clone(all))) != len(all) {
				t.Fatalf("round %d: yielded %v, want ascending without repeats", round, all)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("round %d: got %v, want %v", round, got, want)
			}
		}
	}
}
