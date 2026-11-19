package storage

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// regionCacheCap is vanilla RegionFileStorage's MAX_CACHE_SIZE: the most
// region files one store keeps open at once.
const regionCacheCap = 256

// TestVisitingManyRegionsKeepsTheFileCountBounded walks a server over more
// distinct regions than its region cache holds and checks the open handle
// count does not grow with the regions visited. Each save opens a region file
// of its own, and one past the cap closes the least recently used instead of
// keeping a handle for every region explored.
func TestVisitingManyRegionsKeepsTheFileCountBounded(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, _ := servertest.Start(t, cfg)

	// One dirty chunk in each of 300 regions: region i holds chunk 32i, so
	// every save opens a region file under a key no earlier save used.
	const regions = 300
	stone := registry.BlockStates["minecraft:stone"]
	for i := range regions {
		srv.SetLoadedBlock(int32(i*512+5), 64, 5, stone)
	}

	chunks, entities := srv.RegionFileCounts()
	if chunks != regionCacheCap {
		t.Errorf("open chunk region files = %d after visiting %d regions, want %d (the cache cap)",
			chunks, regions, regionCacheCap)
	}
	if entities > regionCacheCap {
		t.Errorf("open entity region files = %d, want at most %d", entities, regionCacheCap)
	}
}
