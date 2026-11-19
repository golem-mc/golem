package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
)

// TestSpillSplitsEachStackIntoTenToThirty spills a full stack and a small
// one: every piece holds ten to thirty items (or the remainder), the counts
// add back up, and the pieces of one stack share one spot inside the block.
func TestSpillSplitsEachStackIntoTenToThirty(t *testing.T) {
	stacks := []protocol.ItemStack{{ItemID: 1, Count: 64}, {}, {ItemID: 2, Count: 5}}
	for seed := range int64(200) {
		for _, p := range Spill(rules.NewJavaRandom(seed), 3, 70, -2, stacks) {
			if p.Y < 70 {
				t.Fatalf("seed %d: piece at y=%v, below the block", seed, p.Y)
			}
		}
	}
	pieces := Spill(rules.NewJavaRandom(42), 3, 70, -2, stacks)
	totals := map[int32]int32{}
	spots := map[int32][3]float64{}
	for _, p := range pieces {
		if p.Stack.Count < 1 || p.Stack.Count > 30 {
			t.Errorf("piece of %d items, want 1..30", p.Stack.Count)
		}
		totals[p.Stack.ItemID] += p.Stack.Count
		spot := [3]float64{p.X, p.Y, p.Z}
		if was, ok := spots[p.Stack.ItemID]; ok && was != spot {
			t.Errorf("item %d pieces at %v and %v, want one spot", p.Stack.ItemID, was, spot)
		}
		spots[p.Stack.ItemID] = spot
		if p.X < 3 || p.X > 4 || p.Y < 70+ItemHalfSize || p.Y >= 70.75+ItemHalfSize || p.Z < -2 || p.Z > -1 {
			t.Errorf("piece at (%v, %v, %v), outside the block", p.X, p.Y, p.Z)
		}
	}
	if totals[1] != 64 || totals[2] != 5 || len(totals) != 2 {
		t.Errorf("spilled %v, want 64 of item 1 and 5 of item 2", totals)
	}
}
