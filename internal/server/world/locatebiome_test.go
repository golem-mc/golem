package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestSpiralAroundOrder(t *testing.T) {
	var got [][2]int32
	spiralAround(1, func(x, z int32) bool { got = append(got, [2]int32{x, z}); return true })
	want := [][2]int32{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}
	if !slices.Equal(got, want) {
		t.Errorf("spiral = %v, want %v", got, want)
	}
	n := 0
	spiralAround(200, func(int32, int32) bool { n++; return true })
	if n != 401*401 {
		t.Errorf("radius 200 spiral visits %d columns, want %d", n, 401*401)
	}
}

func TestOutFromOrigin(t *testing.T) {
	if got, want := outFromOrigin(10, 0, 20, 8), []int32{10, 18, 2}; !slices.Equal(got, want) {
		t.Errorf("outFromOrigin = %v, want %v", got, want)
	}
	// The origin clamps into the range.
	if got, want := outFromOrigin(500, -63, 320, 64), []int32{320, 256, 192, 128, 64, 0}; !slices.Equal(got, want) {
		t.Errorf("outFromOrigin = %v, want %v", got, want)
	}
}

// ridgeGenerator samples desert only on the one quart column given and at
// heights that are multiples of 64 blocks.
type ridgeGenerator struct{ FlatGenerator }

func (g *ridgeGenerator) PossibleBiomes() []string {
	return []string{"minecraft:plains", "minecraft:desert"}
}

func (g *ridgeGenerator) NewBiomeProbe() func(qx, qy, qz int32) string {
	return func(qx, qy, qz int32) string {
		if qx == ridgeQuartX && qz == 0 && qy%16 == 0 {
			return "minecraft:desert"
		}
		return "minecraft:plains"
	}
}

var ridgeQuartX int32

func TestFindClosestBiomeStepAndRadius(t *testing.T) {
	gen := &ridgeGenerator{*NewFlatGenerator()}
	w, err := NewWorld(t.TempDir(), gen)
	if err != nil {
		t.Fatal(err)
	}
	search := BiomeSearch{
		OriginX: 0, OriginY: 70, OriginZ: 0, Radius: 6400, HStep: 32, VStep: 64,
		Allowed: func(b string) bool { return b == "minecraft:desert" },
	}
	// The last column inside the radius is found, at the origin's height
	// when that is a sampled one.
	ridgeQuartX = 6400 >> 2
	search.OriginY = 64
	if hit, ok := w.FindClosestBiome(search); !ok || hit.X != 6400 || hit.Y != 64 || hit.Z != 0 {
		t.Errorf("hit = %+v, %v, want (6400, 64, 0)", hit, ok)
	}
	// Heights are sampled 64 apart out from the origin: 70 never reaches a
	// multiple of 64.
	search.OriginY = 70
	if hit, ok := w.FindClosestBiome(search); ok {
		t.Errorf("found %+v at a height the vertical step skips", hit)
	}
	// One column past the radius is out of range.
	ridgeQuartX = 6432 >> 2
	search.OriginY = 64
	if hit, ok := w.FindClosestBiome(search); ok {
		t.Errorf("found %+v past the radius", hit)
	}
	// A column between two samples is never looked at.
	ridgeQuartX = 48 >> 2
	if hit, ok := w.FindClosestBiome(search); ok {
		t.Errorf("found %+v between two sampled columns", hit)
	}
}

// TestOverworldBiomeProbeMatchesGeneratedChunk: the probe reads the biome a
// generated chunk would carry, at every quart of chunks across land, shore
// and sea. A cell the climate is equally near two entries of (the caves'
// overlaps) goes to the search's last result, which depends on the order of
// the queries, so a few cells may differ.
func TestOverworldBiomeProbeMatchesGeneratedChunk(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(12345)
	g.DisableStructures()
	probe := g.NewBiomeProbe()
	seen := map[string]bool{}
	cells, ties := 0, 0
	// A 4x4 of separated chunks covers negative coordinates and the origin,
	// with all quart heights and the same minimum biome/tie bounds.
	for cz := int32(-12); cz < 12; cz += 6 {
		for cx := int32(-12); cx < 12; cx += 6 {
			c := newChunk(cx, cz)
			g.Generate(c)
			for qz := range int32(4) {
				for qx := range int32(4) {
					for qy := int32(worldMinY >> 2); qy < (worldMinY+worldHeight)>>2; qy++ {
						want := registry.Biome(c.BiomeAt(qx*4, qy*4, qz*4)).Name()
						got := probe(cx*4+qx, qy, cz*4+qz)
						cells++
						if got != want {
							ties++
						}
						seen[got] = true
					}
				}
			}
		}
	}
	if len(seen) < 3 {
		t.Errorf("only %d biomes covered; the comparison proves little", len(seen))
	}
	if ties*1000 > cells {
		t.Errorf("%d of %d cells differ between the probe and the generated chunks, want only the ties", ties, cells)
	}
}

// recordingGenerator logs every height a search probes at one column.
type recordingGenerator struct {
	FlatGenerator
	qys []int32
}

func (g *recordingGenerator) PossibleBiomes() []string { return []string{"minecraft:desert"} }

func (g *recordingGenerator) NewBiomeProbe() func(qx, qy, qz int32) string {
	return func(qx, qy, qz int32) string {
		if qx == 0 && qz == 0 {
			g.qys = append(g.qys, qy)
		}
		return "minecraft:plains"
	}
}

// TestFindClosestBiomeSampleHeights pins the vertical step and the order
// the heights of a column are sampled in.
func TestFindClosestBiomeSampleHeights(t *testing.T) {
	gen := &recordingGenerator{FlatGenerator: *NewFlatGenerator()}
	w, err := NewWorld(t.TempDir(), gen)
	if err != nil {
		t.Fatal(err)
	}
	w.FindClosestBiome(BiomeSearch{
		OriginY: 70, Radius: 32, HStep: 32, VStep: 64,
		Allowed: func(b string) bool { return b == "minecraft:desert" },
	})
	// Y 70, then 64 apart: above first, then below, alternating outwards.
	want := []int32{70 >> 2, 134 >> 2, 6 >> 2, 198 >> 2, -58 >> 2, 262 >> 2}
	if !slices.Equal(gen.qys, want) {
		t.Errorf("sampled quart heights %v, want %v", gen.qys, want)
	}
}

// TestFindClosestBiomeUnplacedBiomeIsRefusedFast: a biome the generator
// never places is refused before any column is probed.
func TestFindClosestBiomeUnplacedBiomeIsRefusedFast(t *testing.T) {
	gen := &recordingGenerator{FlatGenerator: *NewFlatGenerator()}
	w, err := NewWorld(t.TempDir(), gen)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := w.FindClosestBiome(BiomeSearch{
		Radius: 6400, HStep: 32, VStep: 64,
		Allowed: func(b string) bool { return b == "minecraft:badlands" },
	})
	if ok || len(gen.qys) != 0 {
		t.Errorf("ok=%v after %d probes, want a refusal with none", ok, len(gen.qys))
	}
}

// TestOverworldLocateBiomeProbesEveryColumn walks the columns of a search
// without a hit and checks that each probe names a biome the overworld places.
// Each probe reads the heights of its 4x4 patch from the terrain density, so a
// full-radius search is long; the walk here is a tenth of one.
func TestOverworldLocateBiomeProbesEveryColumn(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(12345)
	probe := g.NewBiomeProbe()
	placed := map[string]bool{}
	for _, name := range g.PossibleBiomes() {
		placed[name] = true
	}
	ys := outFromOrigin(64, worldMinY+1, worldMinY+worldHeight, 64)
	probes := 0
	spiralAround(6400/32/10, func(dx, dz int32) bool {
		for _, y := range ys {
			if name := probe(dx*32>>2, y>>2, dz*32>>2); !placed[name] {
				t.Fatalf("probe at %d,%d,%d named %q, which the overworld does not place", dx*32, y, dz*32, name)
			}
			probes++
		}
		return true
	})
	if probes == 0 {
		t.Fatal("the walk probed nothing")
	}
}
