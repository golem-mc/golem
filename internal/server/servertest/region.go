package servertest

import (
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/config"

	"github.com/golem-mc/golem/internal/server/world"
)

// RegionFixture describes an inclusive rectangle of generated chunks.
type RegionFixture struct {
	// Nether selects the Nether dimension instead of the overworld.
	Nether             bool
	Seed               int64
	Min, Max           world.ChunkPos
	GenerateStructures bool
}

var regionFixtures sync.Map // RegionFixture -> func() (directory, error)
var regionFixtureDir string

// Copy generates the region once per process and gives each caller its own
// files, world and generator; RunMain owns the source files' lifetime.
func (r RegionFixture) Copy(t testing.TB) string {
	t.Helper()
	if regionFixtureDir == "" {
		t.Fatal("RegionFixture.Copy requires RunMain in TestMain")
	}
	build, _ := regionFixtures.LoadOrStore(r, sync.OnceValues(func() (string, error) {
		dir, err := os.MkdirTemp(regionFixtureDir, "region-")
		if err == nil {
			err = r.generate(dir)
		}
		return dir, err
	}))
	src, err := build.(func() (string, error))()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func (r RegionFixture) generate(dir string) error {
	var w *world.World
	if r.Nether {
		gen := world.NewNetherGenerator(r.Seed)
		if !r.GenerateStructures {
			gen.DisableStructures()
		}
		w = world.NewDimensionWorld(dir, true, gen)
	} else {
		gen := world.NewOverworldGenerator(r.Seed)
		if !r.GenerateStructures {
			gen.DisableStructures()
		}
		var err error
		w, err = world.NewWorld(dir, gen)
		if err != nil {
			return err
		}
	}
	defer w.Close()
	for x := r.Min.X; x <= r.Max.X; x++ {
		for z := r.Min.Z; z <= r.Max.Z; z++ {
			pos := world.ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			w.Release(pos)
		}
	}
	return nil
}

// One arrival test runs per P: an arrival is CPU-bound off the tick loop,
// so on a starved P each test's wait would otherwise cover every other
// test's arrival as well as its own.
var (
	arrivalMu   sync.Mutex
	arrivalFree = sync.NewCond(&arrivalMu)
	arrivals    int
)

// holdArrival blocks until fewer arrival tests than Ps are running, and
// gives the place back when t ends.
func holdArrival(t testing.TB) {
	arrivalMu.Lock()
	for arrivals >= runtime.GOMAXPROCS(0) {
		arrivalFree.Wait()
	}
	arrivals++
	arrivalMu.Unlock()
	t.Cleanup(func() {
		arrivalMu.Lock()
		arrivals--
		arrivalMu.Unlock()
		arrivalFree.Broadcast()
	})
}

// NetherTestConfig gives an arrival test its own copy of the shared Nether
// terrain around the origin, with no portals or player edits in the source.
// It admits one such test per P, so call it at most once per test.
func NetherTestConfig(t *testing.T, cfg *config.Config) *config.Config {
	t.Helper()
	holdArrival(t)
	fixture := RegionFixture{Nether: true, Min: world.ChunkPos{X: -2, Z: -2}, Max: world.ChunkPos{X: 2, Z: 2}}
	cfg.LevelName = fixture.Copy(t)
	cfg.LevelSeed = strconv.FormatInt(fixture.Seed, 10)
	cfg.GenerateStructures = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	return cfg
}
