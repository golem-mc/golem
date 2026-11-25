package server

import (
	mrand "math/rand"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
)

// TestIndexedVehicleQueryMatchesLinearScan checks both lookup paths against
// the ordered scan and its random draw.
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
	excluded := s.spawnMinecart(w, "minecraft:chest_minecart", 0, 64, 0.5)
	s.spawnMinecart(w, "minecraft:chest_minecart", 64, 64, 80)
	s.spawnMinecart(w, "minecraft:minecart", -0.2, 64, 0.5)
	s.spawnBoat(w, "minecraft:oak_chest_boat", -0.2, 65, 0.5, 0, customName{}) // touches the top face

	check := func(name string, ordered ...containerTarget) {
		t.Run(name, func(t *testing.T) {
			for _, indexed := range []bool{false, true} {
				if indexed {
					s.indexCartColumns(w)
					t.Cleanup(s.dropCartColumns)
				}
				for seed := range int64(40) {
					rng := mrand.New(mrand.NewSource(seed))
					var want containerTarget
					if len(ordered) > 0 {
						want = ordered[rng.Intn(len(ordered))]
					}
					s.levelRng = mrand.New(mrand.NewSource(seed))
					got := s.entityContainerAt(w, 0, 64.5, 0.5, excluded)
					if got != want {
						t.Fatalf("indexed %t, seed %d: target %v, want %v", indexed, seed, got, want)
					}
					if got, want := s.levelRng.Int63(), rng.Int63(); got != want {
						t.Fatalf("indexed %t, seed %d: next draw %d, want %d", indexed, seed, got, want)
					}
				}
			}
		})
	}

	check("zero")
	// Spawn one boat across the boundary, then interleave carts and boats so
	// chunk traversal and spawn order both differ from the ordered scan.
	westBoat := s.spawnBoat(w, "minecraft:oak_chest_boat", -0.2, 64, 0.5, 0, customName{})
	check("one", boatTarget{s: s, it: westBoat})
	eastCart := s.spawnMinecart(w, "minecraft:chest_minecart", 0.2, 64, 0.5)
	eastBoat := s.spawnBoat(w, "minecraft:birch_chest_boat", 0.1, 64, 0.5, 0, customName{})
	westCart := s.spawnMinecart(w, "minecraft:chest_minecart", -0.3, 64, 0.5)
	check("several", cartTarget{s: s, it: eastCart}, cartTarget{s: s, it: westCart},
		boatTarget{s: s, it: westBoat}, boatTarget{s: s, it: eastBoat})
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

	done := make(chan containerTarget, 1)
	func() {
		s.boatsMu.Lock()
		defer s.boatsMu.Unlock()
		go func() { done <- s.entityContainerAt(w, 0, 64.5, 0.5, nil) }()
		// Wait until the lookup is parked on boatsMu, having given up minecartsMu.
		for i := 0; !parkedOnBoatsMu(); i++ {
			if i > 5000 {
				t.Error("lookup never blocked on boatsMu")
				return
			}
			time.Sleep(time.Millisecond)
		}
		// A lookup still holding minecartsMu would deadlock dropCartColumns
		// while this goroutine holds boatsMu.
		if !s.minecartsMu.TryLock() {
			t.Error("lookup holds minecartsMu while waiting for boatsMu")
			return
		}
		s.minecartsMu.Unlock()
		s.dropCartColumns()
	}()
	if t.Failed() {
		return
	}
	select {
	case got := <-done:
		if got == nil {
			t.Fatal("no container found")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("lookup did not finish after boatsMu was released")
	}
}

// parkedOnBoatsMu reports whether a goroutine sleeps on a mutex inside
// entityContainerAt; the test holds boatsMu, so that is the lookup.
func parkedOnBoatsMu() bool {
	buf := make([]byte, 1<<20)
	for len(buf) < 1<<26 {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			for g := range strings.SplitSeq(string(buf[:n]), "\n\n") {
				if strings.Contains(g, "[sync.Mutex.Lock") && strings.Contains(g, "entityContainerAt") {
					return true
				}
			}
			return false
		}
		buf = make([]byte, 2*len(buf))
	}
	return false
}
