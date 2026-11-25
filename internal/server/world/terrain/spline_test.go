package terrain_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/terrain"
)

func TestSampleOverworldDepthAllocations(t *testing.T) {
	// Uncached depth sampling evaluates the terrain offset spline at each point.
	depth := terrain.NewRandomState(terrain.Overworld, 12345).Router.Depth
	ctx := terrain.NewUncachedContext()
	if allocs := testing.AllocsPerRun(100, func() {
		depth.Sample(ctx, 7, 64, 9)
	}); allocs != 0 {
		t.Fatalf("scalar depth sampling allocated %g objects per call, want 0", allocs)
	}
}
