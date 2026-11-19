package terrain

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"strings"
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

func TestOverworldFeatureOrder(t *testing.T) {
	t.Parallel()
	bySlot := map[levelgen.FeatureSlot]OverworldFeature{}
	for _, f := range OverworldFeatures {
		bySlot[f.Slot] = f
	}
	// The three springs, and the frozen one only in the snowy mountains.
	for slot, name := range map[levelgen.FeatureSlot]string{
		{Step: 8, Index: 0}: "minecraft:spring_water",
		{Step: 8, Index: 1}: "minecraft:spring_lava",
		{Step: 8, Index: 2}: "minecraft:spring_lava_frozen",
	} {
		if bySlot[slot].Name != name {
			t.Errorf("slot %v is %q, want %q", slot, bySlot[slot].Name, name)
		}
	}
	frozen := bySlot[levelgen.FeatureSlot{Step: 8, Index: 2}].Biomes
	for b := range OverworldBiome(OverworldBiomeCount) {
		has := frozen&(1<<b) != 0
		snowy := strings.Contains("snowy_slopes grove jagged_peaks frozen_peaks", b.Name())
		if has != snowy {
			t.Errorf("spring_lava_frozen in %s: %v", b.Name(), has)
		}
	}
	if len(OverworldFeatures) != 171 {
		t.Errorf("%d features, want 171", len(OverworldFeatures))
	}
}
