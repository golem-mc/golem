package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestWGHeightUniformSections(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		minY, height         int32
		fill, surface, floor int32
		water                bool
		sparse               bool
	}{
		{"empty", worldMinY, worldHeight, worldMinY, worldMinY, worldMinY, false, false},
		{"solid", worldMinY, worldHeight, 32, 32, 32, false, false},
		{"water", worldMinY, worldHeight, 32, 64, 32, true, false},
		{"sparse", worldMinY, worldHeight, 32, 301, 301, false, true},
		{"bottom", worldMinY, worldHeight, worldMinY, worldMinY + 1, worldMinY + 1, false, true},
		{"nether", 0, 128, 128, 128, 128, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChunk(0, 0)
			c.Fill(tc.minY, tc.fill, registry.BlockStone.DefaultState())
			if tc.water {
				c.Fill(tc.fill, tc.surface, registry.BlockWater.DefaultState())
			}
			if tc.sparse {
				c.SetBlock(3, tc.surface-1, 4, registry.BlockStone.DefaultState())
			}
			terrain := &shapedTerrain{}
			terrain.keep(c, true)
			r := &decorRegion{src: newGoldenTerrain(), center: c, minY: tc.minY, height: tc.height}
			r.near[r.slot(3, 4)] = terrain
			for _, kind := range []heightmapKind{hmWorldSurfaceWG, hmOceanFloorWG} {
				want := tc.surface
				if kind == hmOceanFloorWG {
					want = tc.floor
				}
				for range 2 { // Exercise the first scan and the shared cache hit.
					if got := r.wgHeight(kind, 3, 4); got != want {
						t.Fatalf("kind %d: height %d, want %d", kind, got, want)
					}
				}
			}
		})
	}
}
