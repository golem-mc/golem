package levelgen

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestClimateSearchMatchesExhaustiveSearch(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(1, 313))
	points := make([]ClimatePoint, 400)
	for i := range points {
		points[i].Biome = uint8(i)
		for d := range 7 {
			lo := rng.Int64N(20001) - 10000
			points[i].Bounds[d] = [2]int64{lo, lo + rng.Int64N(8000)}
		}
	}
	tree := NewClimateIndex(points, 6).root
	var leaves []*rtreeNode
	var walk func(*rtreeNode)
	walk = func(n *rtreeNode) {
		if n.children == nil {
			leaves = append(leaves, n)
		}
		for _, child := range n.children {
			walk(child)
		}
	}
	walk(tree)
	var previous *rtreeNode
	for sample := range 512 {
		var target climateTarget
		for i := range 6 {
			target[i] = rng.Int64N(40001) - 20000
		}
		if sample%2 != 0 {
			target[6] = rng.Int64N(101)
		}
		for _, candidate := range []*rtreeNode{nil, previous} {
			want, distance := candidate, int64(math.MaxInt64)
			if want != nil {
				distance = want.distance(&target)
			}
			for _, leaf := range leaves {
				if d := leaf.distance(&target); d < distance {
					want, distance = leaf, d
				}
			}
			got := tree.search(&target, candidate)
			if got != want {
				t.Fatalf("sample %d candidate %p: got leaf %p (%d), want %p (%d)", sample, candidate, got, got.biome, want, want.biome)
			}
			previous = got
		}
	}
}

func TestClimateDistanceCutoffKeepsStrictImprovements(t *testing.T) {
	t.Parallel()
	for dimension := range climateDimensions {
		var target climateTarget
		target[dimension] = -10
		n := &rtreeNode{}
		n.space[dimension] = climateParam{min: -8, max: -6}
		// The only nonzero term may come last, and equality must prune.
		for _, limit := range []int64{0, 1, 4, 5, math.MaxInt64} {
			if got, want := n.distanceBelow(&target, limit), min(n.distance(&target), limit); got != want {
				t.Fatalf("dimension %d limit %d: got %d, want %d", dimension, limit, got, want)
			}
		}
	}
}

func TestClimateSearchKeepsCandidateOnTie(t *testing.T) {
	t.Parallel()
	a := &rtreeNode{biome: 1}
	b := &rtreeNode{biome: 2}
	tree := newSubTree([]*rtreeNode{newSubTree([]*rtreeNode{a}), newSubTree([]*rtreeNode{b})})
	for _, target := range []climateTarget{{}, {5, -5, 10, -10, 20, -20}} {
		if got := tree.search(&target, nil); got != a {
			t.Fatal("a search without a candidate did not keep the first equal leaf")
		}
		if got := tree.search(&target, b); got != b {
			t.Fatal("an equally close candidate was replaced")
		}
	}
}
