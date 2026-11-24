package server

import (
	mrand "math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/config"
)

// TestIndexedVehicleQueryMatchesLinearScan checks the column index offers the
// same candidates in the same order as the unindexed scan.
func TestIndexedVehicleQueryMatchesLinearScan(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpawning(false)
	t.Cleanup(s.Shutdown)
	w := s.world
	// Overlapping hulls on the x=0 chunk boundary and a far, nonintersecting one.
	s.spawnBoat(w, "minecraft:oak_chest_boat", -0.2, 64, 0.5, 0, customName{})
	s.spawnBoat(w, "minecraft:birch_chest_boat", 0.1, 64, 0.5, 0, customName{})
	s.spawnMinecart(w, "minecraft:chest_minecart", -0.3, 64, 0.5)
	s.spawnMinecart(w, "minecraft:chest_minecart", 0.2, 64, 0.5)
	excluded := s.spawnMinecart(w, "minecraft:chest_minecart", 0, 64, 0.5)
	s.spawnMinecart(w, "minecraft:chest_minecart", 8, 64, 8)

	for seed := range int64(40) {
		s.levelRng = mrand.New(mrand.NewSource(seed))
		linear := s.entityContainerAt(w, 0, 64.5, 0.5, excluded)
		s.indexCartColumns(w)
		s.levelRng = mrand.New(mrand.NewSource(seed))
		indexed := s.entityContainerAt(w, 0, 64.5, 0.5, excluded)
		s.dropCartColumns()
		if linear == nil || indexed == nil || linear != indexed {
			t.Fatalf("seed %d: linear %v, indexed %v", seed, linear, indexed)
		}
	}
}

// TestVehicleQuerySurvivesIndexDropWhileWaitingForBoats drops the index while
// a lookup waits for boatsMu; the lookup must not read a recycled bucket.
func TestVehicleQuerySurvivesIndexDropWhileWaitingForBoats(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpawning(false)
	t.Cleanup(s.Shutdown)
	w := s.world
	for _, x := range []float64{-0.2, 0, 0.2} {
		s.spawnBoat(w, "minecraft:oak_chest_boat", x, 64, 0.5, 0, customName{})
	}
	s.indexCartColumns(w)

	s.boatsMu.Lock()
	done := make(chan containerTarget)
	go func() { done <- s.entityContainerAt(w, 0, 64.5, 0.5, nil) }()
	// The lookup gives up minecartsMu while it waits for boatsMu.
	s.minecartsMu.Lock()
	s.minecartsMu.Unlock()
	s.dropCartColumns()
	s.boatsMu.Unlock()
	if got := <-done; got == nil {
		t.Fatal("no container found")
	}
}
