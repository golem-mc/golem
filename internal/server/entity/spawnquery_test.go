package entity

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

type spawnDieSource struct {
	values []int32
	calls  int
}

func (s *spawnDieSource) Seed(int64) { s.calls = 0 }
func (s *spawnDieSource) Int63() int64 {
	v := s.values[s.calls%len(s.values)]
	s.calls++
	return int64(v) << 32
}

func TestNaturalSpawnPlayerQueries(t *testing.T) {
	for _, d := range []float64{24, 64, 128} {
		for _, x := range []float64{math.Nextafter(d, 0), d, math.Nextafter(d, math.Inf(1))} {
			for _, point := range [][3]float64{{x, 0, 0}, {0, x, 0}, {0, 0, x}, {-x, 0, 0}, {0, -x, 0}, {0, 0, -x}} {
				players := []SpawnPlayer{{EntityID: 1, X: point[0], Y: point[1], Z: point[2], Dead: true}, {EntityID: 2, Spectator: true}}
				c := naturalSpawnContext{}
				c.preparePlayers(ChunkPos{}, players)
				got, exists := c.playerDistance(players, 0, 0, 0)
				want := Dist2(0, 0, 0, point[0], point[1], point[2])
				if want > 128*128 {
					want = math.MaxFloat64
				}
				if !exists || got != want {
					t.Fatalf("distance at %v: %g/%t, want %g", point, got, exists, want)
				}
				for _, limit := range []float64{24 * 24, 64 * 64, 128 * 128} {
					full := Dist2(0, 0, 0, point[0], point[1], point[2])
					if (got <= limit) != (full <= limit) {
						t.Fatalf("changed boundary %v at %g", point, limit)
					}
				}
			}
		}
	}
	players := []SpawnPlayer{{EntityID: 1, X: 1000, Y: 64}, {EntityID: 2, X: 8, Y: 200}, {EntityID: 3, X: 8, Y: 64, Spectator: true}}
	c := naturalSpawnContext{}
	c.preparePlayers(ChunkPos{}, players)
	if len(c.nearPlayers) != 1 || c.nearPlayers[0] != 1 {
		t.Fatalf("candidates %v", c.nearPlayers)
	}
	if d, ok := c.playerDistance(players, 8, 64, 0); d != math.MaxFloat64 || !ok {
		t.Fatalf("distant observers: %g/%t", d, ok)
	}
	counts := SpawnCounts{PerPlayer: map[int32]int{2: 70}}
	if c.localCapOpen(players, ChunkPos{}, &counts, 70) {
		t.Fatal("cap ignored")
	}
	counts.PerPlayer[2] = 69
	if !c.localCapOpen(players, ChunkPos{}, &counts, 70) {
		t.Fatal("cached counts")
	}
	for _, p := range []SpawnPlayer{{X: math.NaN()}, {Y: math.Inf(1)}, {Z: math.Inf(-1)}, {X: math.Nextafter(1<<40, math.Inf(1))}, {Y: -1 << 41, Spectator: true}} {
		c.preparePlayers(ChunkPos{}, []SpawnPlayer{p})
		if c.query {
			t.Fatalf("optimized invalid coordinates: %v", p)
		}
	}
	for _, pos := range []ChunkPos{{X: math.MaxInt32}, {Z: math.MinInt32}, {X: math.MaxInt32 / 16}, {Z: math.MinInt32 / 16}} {
		c.preparePlayers(pos, nil)
		if c.query {
			t.Fatalf("optimized overflowing rectangle %v", pos)
		}
	}
	c.preparePlayers(ChunkPos{}, nil)
	if _, ok := c.playerDistance(nil, 0, 0, 0); ok {
		t.Fatal("invented player")
	}
	c.preparePlayers(ChunkPos{}, []SpawnPlayer{{X: 1000}})
	if d, ok := c.playerDistance([]SpawnPlayer{{X: 1000}}, 0, 0, 0); !ok || d != math.MaxFloat64 {
		t.Fatal("lost distant player")
	}
}

// initialPositionWorld stops the pass at its first all-stone collision query.
type initialPositionWorld struct {
	SpawnWorld
	minY, surface int32
	reads         []string
}

func (w *initialPositionWorld) MinY() int32 {
	w.reads = append(w.reads, "minY")
	return w.minY
}
func (w *initialPositionWorld) SurfaceHeight(x, z int32) int32 {
	w.reads = append(w.reads, fmt.Sprintf("height %d %d", x, z))
	return w.surface
}
func (w *initialPositionWorld) BlockAt(x, y, z int32) uint16 {
	w.reads = append(w.reads, fmt.Sprintf("block %d %d %d", x, y, z))
	return registry.BlockStone.DefaultState()
}

// vanilla: NaturalSpawner.getRandomPosWithin, ChunkAccess.getHeight
func vanillaInitialPosition(w *initialPositionWorld, rng *rand.Rand, pos ChunkPos) {
	bx := pos.X*16 + int32(rng.Intn(16))
	bz := pos.Z*16 + int32(rng.Intn(16))
	minY := w.MinY()
	surface := w.SurfaceHeight(bx, bz)
	span := (surface + 1) - minY + 1
	if span <= 0 {
		return
	}
	y := minY + int32(rng.Intn(int(span)))
	if y < minY+1 {
		return
	}
	w.BlockAt(bx, y, bz)
}
