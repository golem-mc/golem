package terrain

import (
	"testing"
)

// The counts per step are what FeatureSorter built from the vanilla 26.3 data
// pack's 56 overworld biomes; the order within a
// step is the vanilla server's own, from overworld_features.txt.
func TestOverworldFeatureSteps(t *testing.T) {
	t.Parallel()
	want := map[int32]int{1: 4, 2: 5, 3: 4, 4: 4, 6: 34, 7: 7, 8: 3, 9: 109, 10: 1}
	got := map[int32]int{}
	next := map[int32]int32{}
	for _, f := range OverworldFeatures {
		got[f.Slot.Step]++
		if f.Slot.Index != next[f.Slot.Step] {
			t.Fatalf("%s: index %d, want %d", f.Name, f.Slot.Index, next[f.Slot.Step])
		}
		next[f.Slot.Step]++
		if f.Biomes == 0 {
			t.Errorf("%s: no biome carries it", f.Name)
		}
	}
	for step, n := range want {
		if got[step] != n {
			t.Errorf("step %d has %d features, want %d", step, got[step], n)
		}
	}
	if len(got) != len(want) {
		t.Errorf("steps %v, want %v", got, want)
	}
}
