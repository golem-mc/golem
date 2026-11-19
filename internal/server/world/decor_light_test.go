package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

func overworldMushroomRegion() *decorRegion {
	r := &decorRegion{src: NewOverworldGenerator(0), minY: worldMinY, height: worldHeight, center: newChunk(0, 0), rng: levelgen.NewWorldgenRandom(0)}
	empty := &shapedTerrain{}
	for i := range empty.fill {
		empty.fill[i] = airState
	}
	g := r.src.(*OverworldGenerator)
	g.cache.byPos = map[ChunkPos]*terrainEntry{}
	for i := range r.near {
		r.near[i] = empty
		pos := ChunkPos{X: int32(i%decorRegionSpan) - decorRegionSpan/2, Z: int32(i/decorRegionSpan) - decorRegionSpan/2}
		e := &terrainEntry{pos: pos, t: empty, ready: make(chan struct{})}
		close(e.ready)
		e.elem = g.cache.lru.PushFront(e)
		g.cache.byPos[pos] = e
	}
	return r
}

func TestDecorInitializationHalo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		contributor, query ChunkPos
		decorated          bool
		top                int32
		dark               bool
	}{
		{"own", ChunkPos{0, 0}, ChunkPos{0, 0}, true, 32, true},
		{"unfinished own", ChunkPos{0, 0}, ChunkPos{0, 0}, false, 32, false},
		{"unfinished query", ChunkPos{0, 0}, ChunkPos{1, 0}, true, 32, true},
		{"diagonal", ChunkPos{-1, -1}, ChunkPos{0, 0}, true, 32, true},
		{"negative section", ChunkPos{-1, -1}, ChunkPos{-1, -1}, true, -32, true},
		{"outside halo", ChunkPos{-1, -1}, ChunkPos{1, 0}, true, 32, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := overworldMushroomRegion()
			r.src.decor().claimAll()
			defer r.src.decor().releaseAll()
			x, z := tc.contributor.X*16, tc.contributor.Z*16
			p := r.src.decor().protos.ensure(tc.contributor)
			p.decorated = tc.decorated
			terrain := *r.near[r.slot(x, z)]
			terrain.fill[(tc.top-worldMinY)/16] = registry.BlockStone.DefaultState()
			r.near[r.slot(x, z)] = &terrain
			for _, y := range []int32{worldMinY, tc.top, tc.top + 15, tc.top + 16, tc.top + 31, tc.top + 32} {
				want := 15
				if tc.dark && y>>4 <= (tc.top>>4)+1 {
					want = 0
				}
				if got := rawLightAt(r, tc.query.X*16, y, tc.query.Z*16); got != want {
					t.Errorf("y=%d: light=%d, want %d", y, got, want)
				}
			}
		})
	}
}

func TestDecorLightPublication(t *testing.T) {
	t.Parallel()
	r := overworldMushroomRegion()
	ds := r.src.decor()
	r.protoAt(0, 0).decorated = true
	r.set(0, 32, 0, registry.BlockStone.DefaultState())
	ds.BeginLightReplay()
	check := func(want int) {
		t.Helper()
		ds.claimAll()
		got := rawLightAt(r, 0, 50, 0)
		ds.releaseAll()
		if got != want {
			t.Fatalf("light=%d, want %d", got, want)
		}
	}
	check(15) // FEATURES alone does not publish the light engine's section map.
	r.src.(*OverworldGenerator).ReplayLight(ChunkPos{0, 0}, false)
	check(0)
	r.src.(*OverworldGenerator).ReplayLight(ChunkPos{0, 0}, true)
	check(15)
}
