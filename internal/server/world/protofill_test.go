package world

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// TestFillFromProtoMatchesSetBlock checks the bulk copy against one SetBlock
// per cell: blocks, section counts and light marks, with and without keepAir, over a
// chunk that already holds blocks.
func TestFillFromProtoMatchesSetBlock(t *testing.T) {
	t.Parallel()
	var states [256]uint16
	for i, name := range []string{"air", "stone", "lava", "glowstone", "magma_block", "sculk_catalyst", "lightning_rod", "sculk_sensor", "stone_pressure_plate", "oak_fence"} {
		s, ok := registry.FindState("minecraft:"+name, nil)
		if !ok {
			t.Fatalf("no %s", name)
		}
		states[i] = s
	}
	rng := rand.New(rand.NewPCG(1, 2))
	dense := terrain.NewProtoChunk(0, 256, terrain.Stone)
	for z := range int32(16) {
		for x := range int32(16) {
			for y := range int32(256) {
				// Sparse above 64 and empty above 192, so sections come out
				// mixed, uniform and untouched.
				if m := terrain.Material(rng.IntN(10)); y < 64 || (y < 192 && rng.IntN(16) == 0) {
					dense.Set(x, y, z, m)
				}
			}
		}
	}
	// One section of fence, so the uniform fill counts shape blocks too.
	for z := range int32(16) {
		for x := range int32(16) {
			for y := int32(128); y < 144; y++ {
				dense.Set(x, y, z, terrain.Material(9))
			}
		}
	}
	// Few enough light changes to stay under maxLightCells, so the cells
	// themselves are compared.
	sparse := terrain.NewProtoChunk(0, 256, terrain.Stone)
	for i := range int32(12) {
		sparse.Set(i, 3+i*17, 15-i, terrain.Material(2+i%2)) // lava, glowstone
	}
	stone := states[1]
	for _, tc := range []struct {
		name  string
		proto *terrain.ProtoChunk
	}{{"dense", dense}, {"sparse", sparse}} {
		proto := tc.proto
		for _, keepAir := range []bool{false, true} {
			want, got := newChunk(0, 0), newChunk(0, 0)
			for _, c := range []*Chunk{want, got} {
				for x := range int32(16) {
					c.SetBlock(x, 100, x, stone)
					c.SetBlock(x, 101, x, registry.BlockOakLeaves.DefaultState())
					c.SetBlock(x, -10, 15-x, stone)
				}
				c.lightDirty, c.lightCells, c.lightOverflow = [sectionSize]uint16{}, nil, false
			}
			for z := range int32(16) {
				for x := range int32(16) {
					for y := proto.MinY(); y <= proto.MaxY(); y++ {
						if m := proto.Get(x, y, z); !keepAir || m != terrain.Air {
							want.SetBlock(x, y, z, states[m])
						}
					}
				}
			}
			got.fillFromProto(proto, &states, keepAir)
			var shapeSections, uniformShapes int
			for sy := range want.sections {
				w, g := &want.sections[sy], &got.sections[sy]
				checkRandomTickCount(t, w)
				checkRandomTickCount(t, g)
				for i := range blocksPerSection {
					if w.at(i) != g.at(i) {
						t.Fatalf("%s keepAir %v: section %d index %d: got %d, want %d", tc.name, keepAir, sy, i, g.at(i), w.at(i))
					}
				}
				if [8]int{int(w.ticking), int(w.emitting), int(w.listening), int(w.rods), int(w.catalysts), int(w.eggs), int(w.touches.Load()), int(w.shapes.Load())} !=
					[8]int{int(g.ticking), int(g.emitting), int(g.listening), int(g.rods), int(g.catalysts), int(g.eggs), int(g.touches.Load()), int(g.shapes.Load())} {
					t.Errorf("%s keepAir %v: section %d counts differ", tc.name, keepAir, sy)
				}
				if g.shapes.Load() > 0 {
					shapeSections++
				}
				if g.shapes.Load() == blocksPerSection {
					uniformShapes++
				}
			}
			if tc.proto == dense && (shapeSections == 0 || uniformShapes == 0) {
				t.Errorf("keepAir %v: shape counts %d sections, %d uniform; want both positive", keepAir, shapeSections, uniformShapes)
			}
			if got.lightDirty != want.lightDirty || got.lightOverflow != want.lightOverflow {
				t.Errorf("%s keepAir %v: light marks differ", tc.name, keepAir)
			}
			if tc.proto == sparse {
				if want.lightOverflow {
					t.Fatalf("%s keepAir %v: light cells overflowed", tc.name, keepAir)
				}
				if len(want.lightCells) == 0 {
					t.Fatalf("%s keepAir %v: no light cells marked", tc.name, keepAir)
				}
				if !slices.Equal(sortedCells(got.lightCells), sortedCells(want.lightCells)) {
					t.Errorf("%s keepAir %v: light cells differ", tc.name, keepAir)
				}
			}
		}
	}
}

func sortedCells(cells []uint32) []uint32 {
	return slices.Sorted(slices.Values(cells))
}
