package world

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestOverworldHugeRedMushroomVanilla pins a mushroom from the frozen seed-12345 cache (vanilla 26.3).
func TestOverworldHugeRedMushroomVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(12345)
	// The 7x7 ring in x/z order gives the frozen cache's result here.
	var order []ChunkPos
	for x := int32(-5); x <= 1; x++ {
		for z := int32(-24); z <= -18; z++ {
			order = append(order, ChunkPos{x, z})
		}
	}
	g.DecorateInOrder(order)
	c := newChunk(-2, -21)
	g.Generate(c)
	for y := int32(71); y <= 76; y++ {
		want := registry.BlockMushroomStem
		if y == 76 {
			want = registry.BlockRedMushroomBlock
		}
		if got := registry.BlockOf(c.BlockAt(-24&15, y, -329&15)); got != want {
			t.Errorf("mushroom at (-24,%d,-329): got %v, want %v", y, got, want)
		}
	}
}

func TestOverworldGroveSprucePositionsVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(12345)
	c := newChunk(9, -20)
	g.Generate(c)
	for _, p := range [][3]int32{{151, 148, -308}, {156, 149, -310}} {
		if got := registry.BlockOf(c.BlockAt(p[0]&15, p[1], p[2]&15)); got != registry.BlockSpruceLog {
			t.Errorf("trunk at %v: got %v, want spruce_log", p, got)
		}
	}
}

// TestOverworldDirtPositionsVanilla pins ore positions and an exposed surface
// from frozen vanilla regions; the digest uses local x/y/z lines in y/z/x order.
func TestOverworldDirtPositionsVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(12345)
	c := newChunk(-18, -20)
	g.Generate(c)
	h := sha256.New()
	count := 0
	for y := range int32(64) {
		for z := range int32(16) {
			for x := range int32(16) {
				if registry.BlockOf(c.BlockAt(x, y, z)) == registry.BlockDirt {
					fmt.Fprintf(h, "%d %d %d\n", x, y, z)
					count++
				}
			}
		}
	}
	// Both vanilla runs contain these 138 underground ore_dirt blocks.
	const want = "05eb01cfe86d791ae6b7581a60c631f75dafaee04b8d2554cdb7310c6ad9698b"
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != want || count != 138 {
		t.Fatalf("dirt y0..63: %d blocks, digest %s; want 138 blocks, %s", count, got, want)
	}
	// A cavity beneath an overhang exposes another surface at world (-398,99,-398).
	surface := newChunk(-25, -25)
	g.shapeTerrain(surface)
	for _, sample := range []struct {
		y     int32
		block registry.Block
	}{
		{90, registry.BlockAir}, {92, registry.BlockAir}, {93, registry.BlockStone},
		{95, registry.BlockStone}, {96, registry.BlockDirt}, {97, registry.BlockDirt},
		{98, registry.BlockDirt}, {99, registry.BlockGrassBlock}, {100, registry.BlockAir},
		{103, registry.BlockAir}, {104, registry.BlockStone},
	} {
		if got := registry.BlockOf(surface.BlockAt(2, sample.y, 2)); got != sample.block {
			t.Errorf("surface (-398,%d,-398): got %v, vanilla %v", sample.y, got, sample.block)
		}
	}
}

// TestOverworldClayPositionsVanilla pins clay from the frozen seed-12345 cache.
func TestOverworldClayPositionsVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(12345)
	// Replay the frozen cache's FEATURES order of the 7x7; the outer rings
	// supply terrain changes read by the neighbours that write into this chunk.
	g.DecorateInOrder([]ChunkPos{
		{11, -23}, {11, -22}, {11, -21}, {11, -20}, {11, -19}, {11, -18}, {11, -17}, {12, -23},
		{12, -22}, {12, -18}, {12, -17}, {12, -21}, {12, -20}, {12, -19}, {13, -23}, {13, -22},
		{13, -21}, {13, -20}, {13, -19}, {13, -18}, {13, -17}, {14, -22}, {14, -21}, {14, -20},
		{14, -23}, {14, -19}, {14, -18}, {14, -17}, {15, -20}, {15, -19}, {15, -18}, {15, -21},
		{15, -22}, {15, -23}, {15, -17}, {16, -23}, {16, -22}, {16, -21}, {16, -20}, {16, -19},
		{16, -18}, {16, -17}, {17, -22}, {17, -21}, {17, -20}, {17, -19}, {17, -18}, {17, -23},
		{17, -17},
	})
	c := newChunk(14, -20)
	g.Generate(c)
	h := sha256.New()
	count := 0
	for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
		for z := range int32(16) {
			for x := range int32(16) {
				if registry.BlockOf(c.BlockAt(x, y, z)) == registry.BlockClay {
					fmt.Fprintf(h, "%d %d %d\n", x, y, z)
					count++
				}
			}
		}
	}
	// Local x/y/z lines in y/z/x order.
	const want = "80788bbac0cf29f949c3ff40d56723140f0f9c2648a26e61aee48375818fce22"
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != want || count != 1018 {
		t.Fatalf("clay: %d blocks, digest %s; want 1018 blocks, %s", count, got, want)
	}
}

// TestOverworldSpawnTreesVanilla pins trunks from the frozen seed-0 cache.
func TestOverworldSpawnTreesVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	// Replay the recorded FEATURES order of the 5x5 around chunk (-2,-1).
	// Forest flowers precede trees; a replaced lilac half must survive until
	// the tree's shape pass so its ground decorator consumes the right draws.
	g.DecorateInOrder([]ChunkPos{
		{-3, -1}, {-3, 0}, {-3, 1}, {-2, -1}, {-2, 0},
		{-2, 1}, {-1, -1}, {-1, 0}, {-1, 1}, {-4, -3},
		{-3, -3}, {-4, 0}, {-4, 1}, {-4, -1}, {-4, -2},
		{-3, -2}, {-2, -3}, {-2, -2}, {-1, -3}, {-1, -2},
		{0, -3}, {0, -2}, {0, -1}, {0, 0}, {0, 1},
	})
	c := newChunk(-2, -1)
	g.Generate(c)
	for _, p := range [][3]int32{{-31, 64, -8}, {-26, 64, -5}} {
		if got := registry.BlockOf(c.BlockAt(p[0]&15, p[1], p[2]&15)); got != registry.BlockOakLog {
			t.Errorf("trunk at %v: got %v, want oak_log", p, got)
		}
	}
}

// TestOverworldReloadedOrePositionsVanilla pins a seed-12345 recording (vanilla 26.3) whose proto chunks were reloaded between FEATURES steps: a reload keeps the worldgen heightmaps the ore blobs read.
func TestOverworldReloadedOrePositionsVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(12345)
	// The recorded FEATURES order of the 7x7 around chunk (5,-4).
	g.DecorateInOrder([]ChunkPos{
		{5, -3},
		{5, -2},
		{5, -1},
		{6, -3},
		{6, -2},
		{6, -1},
		{7, -3},
		{7, -2},
		{7, -1},
		{2, -7},
		{2, -6},
		{3, -7},
		{3, -6},
		{4, -7},
		{5, -7},
		{4, -6},
		{5, -6},
		{6, -7},
		{6, -6},
		{7, -7},
		{7, -6},
		{8, -7},
		{8, -6},
		{2, -5},
		{2, -4},
		{2, -1},
		{2, -3},
		{2, -2},
		{3, -4},
		{3, -3},
		{3, -2},
		{3, -5},
		{3, -1},
		{4, -5},
		{4, -4},
		{4, -3},
		{4, -2},
		{4, -1},
		{5, -5},
		{5, -4},
		{6, -5},
		{6, -4},
		{7, -5},
		{7, -4},
		{8, -5},
		{8, -4},
		{8, -3},
		{8, -2},
		{8, -1},
	})
	c := newChunk(5, -4)
	g.Generate(c)
	for _, tc := range []struct {
		block  registry.Block
		count  int
		digest string
	}{
		{registry.BlockDirt, 988, "60505ce31da38626807447cba074138b9f95843f281767472a03a76315eedc99"},
		{registry.BlockGravel, 663, "77bb59addda86649ed5238caa8497e519b5747f064168189955016c7b1ed62bd"},
	} {
		h := sha256.New()
		count := 0
		for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
			for z := range int32(16) {
				for x := range int32(16) {
					if registry.BlockOf(c.BlockAt(x, y, z)) == tc.block {
						fmt.Fprintf(h, "%d %d %d\n", x, y, z)
						count++
					}
				}
			}
		}
		if got := fmt.Sprintf("%x", h.Sum(nil)); count != tc.count || got != tc.digest {
			t.Errorf("%v: %d blocks, digest %s; want %d blocks, %s", tc.block, count, got, tc.count, tc.digest)
		}
	}
}

// TestOverworldBirchPositionsVanilla pins trunks from the frozen seed-0
// light2 cache, restricted to the 5x5 chunks around -15,-11.
func TestOverworldBirchPositionsVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	g.BeginLightReplay()
	// f: FEATURES, s: source priming, i: INITIALIZE_LIGHT, l: LIGHT.
	for _, step := range []struct {
		x, z  int32
		phase byte
	}{
		{-17, -13, 'f'}, {-17, -12, 'f'}, {-17, -13, 's'}, {-17, -12, 's'},
		{-17, -11, 'f'}, {-17, -10, 'f'}, {-17, -11, 's'}, {-17, -10, 's'},
		{-17, -9, 'f'}, {-17, -9, 's'}, {-17, -13, 'i'}, {-17, -12, 'i'},
		{-17, -11, 'i'}, {-17, -10, 'i'}, {-17, -9, 'i'}, {-16, -13, 'f'},
		{-16, -12, 'f'}, {-16, -13, 's'}, {-16, -12, 's'}, {-16, -11, 'f'},
		{-16, -10, 'f'}, {-16, -11, 's'}, {-16, -10, 's'}, {-16, -9, 'f'},
		{-16, -9, 's'}, {-16, -13, 'i'}, {-16, -12, 'i'}, {-16, -11, 'i'},
		{-16, -10, 'i'}, {-16, -9, 'i'}, {-15, -13, 'f'}, {-15, -13, 's'},
		{-15, -12, 'f'}, {-15, -12, 's'}, {-15, -11, 'f'}, {-15, -11, 's'},
		{-15, -10, 'f'}, {-15, -9, 'f'}, {-15, -10, 's'}, {-15, -9, 's'},
		{-17, -13, 'l'}, {-17, -12, 'l'}, {-17, -11, 'l'}, {-17, -10, 'l'},
		{-17, -9, 'l'}, {-15, -13, 'i'}, {-15, -12, 'i'}, {-15, -11, 'i'},
		{-15, -10, 'i'}, {-15, -9, 'i'}, {-16, -13, 'l'}, {-16, -12, 'l'},
		{-16, -11, 'l'}, {-16, -10, 'l'}, {-16, -9, 'l'}, {-14, -13, 'f'},
		{-14, -13, 's'}, {-14, -13, 'i'}, {-14, -12, 'f'}, {-14, -12, 's'},
		{-14, -11, 'f'}, {-14, -11, 's'}, {-14, -12, 'i'}, {-14, -11, 'i'},
		{-14, -10, 'f'}, {-14, -9, 'f'}, {-14, -10, 's'}, {-14, -9, 's'},
		{-13, -13, 'f'}, {-13, -13, 's'}, {-13, -12, 'f'}, {-13, -12, 's'},
		{-13, -11, 'f'}, {-13, -11, 's'}, {-13, -10, 'f'}, {-13, -9, 'f'},
		{-13, -10, 's'}, {-13, -9, 's'}, {-15, -13, 'l'}, {-15, -12, 'l'},
		{-14, -10, 'i'}, {-14, -9, 'i'}, {-13, -13, 'i'}, {-13, -12, 'i'},
		{-13, -11, 'i'}, {-13, -10, 'i'}, {-13, -9, 'i'}, {-15, -11, 'l'},
		{-15, -10, 'l'}, {-15, -9, 'l'}, {-14, -13, 'l'}, {-14, -12, 'l'},
		{-13, -13, 'l'}, {-14, -11, 'l'}, {-14, -10, 'l'}, {-13, -12, 'l'},
		{-14, -9, 'l'}, {-13, -11, 'l'}, {-13, -10, 'l'}, {-13, -9, 'l'},
	} {
		pos := ChunkPos{X: step.x, Z: step.z}
		switch step.phase {
		case 'f':
			g.DecorateInOrder([]ChunkPos{pos})
		case 's':
			g.PrimeLightSources(pos)
		default:
			g.ReplayLight(pos, step.phase == 'l')
		}
	}
	c := newChunk(-15, -11)
	g.Generate(c)
	for _, p := range [][3]int32{{-235, 70, -165}, {-229, 70, -163}} {
		if got := registry.BlockOf(c.BlockAt(p[0]&15, p[1], p[2]&15)); got != registry.BlockBirchLog {
			t.Errorf("trunk at %v: got %v, want birch_log", p, got)
		}
	}
}
