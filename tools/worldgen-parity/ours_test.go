package main

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestDecorationOrder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recorded [][2]int32
		want     []world.ChunkPos
	}{
		{name: "missing recording"},
		{name: "empty recording", recorded: [][2]int32{}},
		{
			name:     "interleaved workers",
			recorded: [][2]int32{{-12, -4}, {-12, -5}, {-12, -3}},
			want:     []world.ChunkPos{{X: -12, Z: -4}, {X: -12, Z: -5}, {X: -12, Z: -3}},
		},
		{
			name:     "holes are replayed verbatim",
			recorded: [][2]int32{{-2, 0}, {-3, -2}, {-1, -2}, {-1, 0}},
			want:     []world.ChunkPos{{X: -2, Z: 0}, {X: -3, Z: -2}, {X: -1, Z: -2}, {X: -1, Z: 0}},
		},
		{
			name:     "duplicate task kept for generator to skip",
			recorded: [][2]int32{{5, 6}, {5, 6}},
			want:     []world.ChunkPos{{X: 5, Z: 6}, {X: 5, Z: 6}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := decorationOrder(tc.recorded); !slices.Equal(got, tc.want) {
				t.Fatalf("decoration order = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRequestOrder(t *testing.T) {
	r := chunkRange{X0: -1, Z0: -1, X1: 0, Z1: 0}
	want := []world.ChunkPos{{X: -1, Z: -1}, {X: -1, Z: 0}, {X: 0, Z: -1}, {X: 0, Z: 0}}
	for _, name := range []string{"overworld", "the_end"} {
		if got := requestOrder(dimensions[name], r); !slices.Equal(got, want) {
			t.Errorf("%s request order = %v, want %v", name, got, want)
		}
	}
	// The Nether keeps the forceload scheduler's centre-first requests.
	got := requestOrder(dimensions["the_nether"], r)
	if len(got) != 36 || got[0] != (world.ChunkPos{X: -1, Z: -1}) || got[1] != (world.ChunkPos{X: -2, Z: -2}) {
		t.Fatalf("Nether request order = %v, want 36 centre-first requests including the generation margin", got)
	}
}

// TestGenerateOursRecordedForest catches decoration by the spawn search before
// replay: this forest's trees depend on the order of chunks six away, so the
// vanilla digest needs every recorded FEATURES step of the 13x13 around chunk
// -2,-3 (frozen vanilla 26.3 seed 0, chunks -24,-24..23,23). Replaying those
// 169 chunks takes about 5s.
func TestGenerateOursRecordedForest(t *testing.T) {
	t.Parallel()
	var recorded []recordedStep
	for _, pos := range [][2]int32{
		{-3, -1}, {-3, 0}, {-3, 1}, {-2, -1}, {-2, 0}, {-2, 1}, {-1, -1}, {-1, 0}, {-1, 1},
		{-8, -9}, {-8, -8}, {-8, -7}, {-8, -6}, {-7, -9}, {-7, -8}, {-7, -7}, {-6, -9}, {-6, -8},
		{-6, -7}, {-7, -6}, {-6, -6}, {-5, -9}, {-5, -8}, {-5, -7}, {-4, -9}, {-4, -8}, {-4, -7},
		{-5, -6}, {-4, -6}, {-3, -9}, {-3, -8}, {-3, -7}, {-2, -9}, {-2, -8}, {-2, -7}, {-3, -6},
		{-2, -6}, {-1, -9}, {-1, -8}, {-1, -7}, {0, -9}, {0, -8}, {1, -9}, {-1, -6}, {0, -7},
		{0, -6}, {1, -8}, {2, -9}, {2, -8}, {1, -7}, {2, -7}, {3, -9}, {3, -8}, {1, -6}, {3, -7},
		{2, -6}, {3, -6}, {4, -9}, {4, -8}, {4, -7}, {4, -6}, {-8, -5}, {-8, -4}, {-8, -3},
		{-8, -2}, {-8, -1}, {-7, -5}, {-7, -4}, {-7, -3}, {-7, -2}, {-7, -1}, {-8, 0}, {-8, 1},
		{-8, 2}, {-8, 3}, {-7, 0}, {-7, 1}, {-6, -3}, {-6, -2}, {-6, -1}, {-6, -5}, {-6, -4},
		{-7, 2}, {-7, 3}, {-6, 0}, {-6, 1}, {-6, 2}, {-5, -3}, {-5, -2}, {-5, -1}, {-5, 0},
		{-5, 1}, {-5, -5}, {-5, -4}, {-6, 3}, {-5, 2}, {-5, 3}, {-4, -3}, {-4, -2}, {-4, -1},
		{-4, -4}, {-4, 0}, {-4, -5}, {-4, 1}, {-4, 2}, {-4, 3}, {-3, -2}, {-3, -3}, {-3, -5},
		{-3, -4}, {-3, 2}, {-3, 3}, {-2, -2}, {-2, -3}, {-2, 2}, {-2, -5}, {-2, -4}, {-2, 3},
		{-1, -4}, {-1, -3}, {-1, -2}, {-1, -5}, {-1, 2}, {-1, 3}, {0, -5}, {0, -4}, {0, -3},
		{0, -2}, {0, -1}, {0, 0}, {0, 1}, {0, 2}, {0, 3}, {1, -5}, {1, -4}, {1, -3}, {1, -2},
		{1, -1}, {1, 0}, {1, 1}, {2, -5}, {1, 2}, {1, 3}, {2, -4}, {2, -3}, {2, -2}, {2, -1},
		{2, 0}, {2, 1}, {3, -5}, {3, -4}, {2, 2}, {2, 3}, {3, -3}, {3, -2}, {3, -1}, {4, -5},
		{3, 0}, {3, 1}, {3, 2}, {3, 3}, {4, -4}, {4, -3}, {4, -2}, {4, -1}, {4, 0}, {4, 1},
		{4, 2}, {4, 3},
	} {
		recorded = append(recorded, recordedStep{Pos: pos})
	}
	chunks, err := generateOurs(dimensions["overworld"], 0, cacheConfig{}, chunkRange{-2, -3, -2, -3}, recorded)
	if err != nil {
		t.Fatal(err)
	}
	c := chunks[[2]int32{-2, -3}]
	h := sha256.New()
	count := 0
	for y := c.Dim.MinY; y < c.Dim.MinY+c.Dim.Height; y++ {
		for z := range 16 {
			for x := range 16 {
				if registry.BlockOf(c.Blocks[c.index(x, y, z)]).InTag(registry.TagIDLogs) {
					fmt.Fprintf(h, "%d %d %d\n", x, y, z)
					count++
				}
			}
		}
	}
	// The vanilla cache's logs, as local x/y/z lines in y/z/x order.
	const want = "303a9152453c58ba510a55fa1fa2cf32752cb574a35aa2fa7342aed982f85ab0"
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != want || count != 37 {
		t.Fatalf("replayed forest logs: %d blocks, digest %s; want 37 blocks, %s", count, got, want)
	}
}

// The seed-0 million-block cache's FEATURES prefix through chunk 62503,-62502.
func TestGenerateOursRecordedEndChorus(t *testing.T) {
	t.Parallel()
	var recorded []recordedStep
	for _, pos := range [][2]int32{
		{62499, -62505}, {62499, -62504}, {62499, -62503}, {62500, -62505},
		{62500, -62504}, {62500, -62503}, {62501, -62505}, {62501, -62504},
		{62501, -62503}, {62500, -62506}, {62501, -62506}, {62502, -62506},
		{62502, -62505}, {62502, -62504}, {62499, -62502}, {62500, -62502},
		{62501, -62502}, {62499, -62506}, {62498, -62505}, {62498, -62504},
		{62498, -62503}, {62498, -62502}, {62498, -62506}, {62502, -62503},
		{62502, -62502}, {62499, -62507}, {62500, -62507}, {62501, -62507},
		{62498, -62501}, {62499, -62501}, {62500, -62501}, {62503, -62506},
		{62503, -62505}, {62503, -62504}, {62501, -62501}, {62497, -62505},
		{62497, -62504}, {62497, -62503}, {62497, -62506}, {62502, -62507},
		{62497, -62502}, {62498, -62507}, {62497, -62507}, {62497, -62501},
		{62503, -62507}, {62503, -62503}, {62502, -62501}, {62503, -62502},
	} {
		recorded = append(recorded, recordedStep{Pos: pos})
	}
	chunks, err := generateOurs(dimensions["the_end"], 0, cacheConfig{}, chunkRange{62503, -62502, 62503, -62502}, recorded)
	if err != nil {
		t.Fatal(err)
	}
	c := chunks[[2]int32{62503, -62502}]
	for _, tc := range []struct {
		x, y, z int
		want    registry.Block
	}{
		{11, 64, 0, registry.BlockChorusPlant},
		{10, 73, 2, registry.BlockChorusFlower},
		{11, 75, 3, registry.BlockChorusFlower},
	} {
		if got := registry.BlockOf(c.Blocks[c.index(tc.x, tc.y, tc.z)]); got != tc.want {
			t.Errorf("(%d,%d,%d): %s, want %s", tc.x, tc.y, tc.z, got.Name(), tc.want.Name())
		}
	}
}
