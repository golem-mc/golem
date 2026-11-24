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

func TestBoxGridAddPoint(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for _, cell := range []float64{0, 4, 8, 10, 16} {
		var dense, sparse BoxGrid
		dense.Cell = cell
		sparse.Cell = cell
		for round := range 4 {
			dense.Reset()
			sparse.Reset()
			type pt struct{ x, z float64 }
			pts := make([]pt, 250)
			minX, maxX, minZ, maxZ := 1e9, -1e9, 1e9, -1e9
			for i := range pts {
				x := rng.Float64()*120 - 60
				z := rng.Float64()*120 - 60
				if round == 2 && i == 0 {
					// Scattered point forces sparse fallback.
					x, z = 1e6, -1e6
				}
				pts[i] = pt{x, z}
				minX, maxX = min(minX, x), max(maxX, x)
				minZ, maxZ = min(minZ, z), max(maxZ, z)
			}
			if round != 1 {
				dense.SetPointBounds(minX, maxX, minZ, maxZ, len(pts))
			}
			for i, p := range pts {
				dense.AddPoint(int32(i), p.x, p.z)
				sparse.Add(int32(i), NewAABB(p.x, 0, p.z, p.x, 0, p.z))
			}
			if round == 3 {
				// Extra point outside the declared bounds lands in sparse overflow.
				dense.AddPoint(int32(len(pts)), 500, -500)
				sparse.Add(int32(len(pts)), NewAABB(500, 0, -500, 500, 0, -500))
			}
			for range 100 {
				qx, qz := rng.Float64()*160-80, rng.Float64()*160-80
				if round == 3 && rng.Intn(5) == 0 {
					qx, qz = 498, -502
				}
				q := NewAABB(qx, -1, qz, qx+rng.Float64()*24, 1, qz+rng.Float64()*24)
				var got, want []int32
				dense.QueryPoints(q, func(i int32) { got = append(got, i) })
				sparse.QueryPoints(q, func(i int32) { want = append(want, i) })
				slices.Sort(got)
				slices.Sort(want)
				if !slices.Equal(got, want) {
					t.Fatalf("cell=%v round=%d: QueryPoints got %v, want %v", cell, round, got, want)
				}
				got, want = got[:0], want[:0]
				dense.Query(q, func(i int32) { got = append(got, i) })
				sparse.Query(q, func(i int32) { want = append(want, i) })
				if !slices.Equal(got, want) {
					t.Fatalf("cell=%v round=%d: Query got %v, want %v", cell, round, got, want)
				}
			}
		}
	}
}

func TestBoxGridFirstAndSpansMore(t *testing.T) {
	var g BoxGrid
	// Index 0 sits in the far cell, 1 and 2 in the near one.
	g.Add(0, NewAABB(40, 0, 0, 41, 1, 1))
	g.Add(1, NewAABB(10, 0, 0, 20, 1, 1))
	g.Add(2, NewAABB(10, 0, 0, 20, 1, 1))
	query := NewAABB(0, 0, 0, 50, 1, 1)
	var calls []int32
	got := g.First(query, func(i int32) bool { calls = append(calls, i); return i >= 1 })
	if got != 1 {
		t.Fatalf("First = %d, want 1", got)
	}
	if g.First(query, func(int32) bool { return false }) != -1 {
		t.Fatal("First found a match where none holds")
	}
	if g.First(NewAABB(100, 0, 100, 101, 1, 101), func(int32) bool { return true }) != -1 {
		t.Fatal("First found an index in an empty cell")
	}
	if g.SpansMore(NewAABB(0, 0, 0, 47, 1, 1), 3) {
		t.Error("three cells reported as more than three")
	}
	if !g.SpansMore(NewAABB(0, 0, 0, 48, 1, 1), 3) {
		t.Error("four cells not reported as more than three")
	}
	if !g.SpansMore(NewAABB(0, 0, 0, 1, 1, 48), 3) {
		t.Error("four cells in z not reported")
	}
}
