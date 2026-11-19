package world

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"testing"
)

func TestNetherFeatureHeightAnchors(t *testing.T) {
	// The dimension is 256 blocks high, but WorldGenerationContext limits
	// the placement anchors to the 128-block noise volume.
	r := &decorRegion{g: &NetherGenerator{}, minY: 0, height: netherGenHeight, rng: levelgen.NewWorldgenRandom(0)}
	mod := compileMod(parseJSON(`{"type":"height_range","height":{"type":"uniform","min_inclusive":{"above_bottom":4},"max_inclusive":{"below_top":4}}}`), 0, true)
	for range 256 {
		r.walk([]placementMod{mod}, [3]int32{7, 0, 9}, func(_ *decorRegion, p [3]int32) {
			if p[0] != 7 || p[2] != 9 || p[1] < 4 || p[1] > 123 {
				t.Fatalf("placement outside generation bounds: %v", p)
			}
		})
	}
}
