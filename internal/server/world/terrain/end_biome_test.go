package terrain

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"testing"
)

// TestTheEndBiomeThresholds checks the erosion bands the outer End is sorted
// by, and the the_end disc of 64 chunks around the origin whatever the erosion.
// vanilla: TheEndBiomeSource.getNoiseBiome
func TestTheEndBiomeThresholds(t *testing.T) {
	t.Parallel()
	const far = 65 * 4 // the first quart of chunk 65
	for _, tc := range []struct {
		erosion float32
		want    levelgen.EndBiome
	}{
		{0.2501, levelgen.EndHighlands},
		{0.25, levelgen.EndMidlands},
		{0, levelgen.EndMidlands},
		{-0.0625, levelgen.EndMidlands},
		{-0.0626, levelgen.EndBarrens},
		{-0.21875, levelgen.EndBarrens},
		{-0.21876, levelgen.SmallEndIslands},
		{-1, levelgen.SmallEndIslands},
	} {
		if got := EndBiomeAt(constant(tc.erosion), far, 0, 0); got != tc.want {
			t.Errorf("erosion %v: biome %d, want %d", tc.erosion, got, tc.want)
		}
		if got := EndBiomeAt(constant(tc.erosion), 64*4+3, 0, 0); got != levelgen.TheEnd {
			t.Errorf("erosion %v in chunk 64: biome %d, want the_end", tc.erosion, got)
		}
	}
}

// TestGoldenEndIslandErosion retains far-coordinate and truncated negative
// section regressions against the production sampler's float bits.
func TestGoldenEndIslandErosion(t *testing.T) {
	t.Parallel()
	states := goldenStates{}
	records := goldenOf(t, "endislands")
	if len(records) != 42 {
		t.Fatalf("got %d island points, want 42", len(records))
	}
	for _, r := range records {
		rs := states.get(t, "end", r.int(t, 0))
		checkBits(t, r, rs.Router.Erosion.Sample(NewUncachedContext(), int32(r.int(t, 1)), 64, int32(r.int(t, 2))))
	}
}

// TestGoldenEndBiomes retains the independent outer-island biome coverage on
// the production sampler rather than the unused double engine.
func TestGoldenEndBiomes(t *testing.T) {
	t.Parallel()
	names := [...]string{"minecraft:the_end", "minecraft:end_highlands", "minecraft:end_midlands", "minecraft:small_end_islands", "minecraft:end_barrens"}
	states := goldenStates{}
	records := goldenOf(t, "endbiome")
	if len(records) != 756 {
		t.Fatalf("got %d biome samples, want 756", len(records))
	}
	for _, r := range records {
		rs := states.get(t, "end", r.int(t, 0))
		biome := EndBiomeAt(rs.Router.Erosion, int32(r.int(t, 1)), int32(r.int(t, 2)), int32(r.int(t, 3)))
		if got := names[biome]; got != r.values[0] {
			t.Errorf("%s: biome %s, want %s", r, got, r.values[0])
		}
	}
}
