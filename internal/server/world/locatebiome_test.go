package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

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
