package world

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestOverworldJungleGrassPositionsVanilla pins the grass and ferns after a
// neighbouring chunk's sand disks and jungle bushes have settled their supports.
func TestOverworldJungleGrassPositionsVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	// Frozen vanilla 26.3 seed 0, chunks 13,55..21,63: a recorded
	// FEATURES prefix around chunk 13,56, omitting the distant spawn chunks.
	g.DecorateInOrder([]ChunkPos{
		{12, 54}, {12, 55}, {12, 56}, {13, 54}, {13, 55}, {13, 56},
		{14, 54}, {14, 55}, {14, 56}, {11, 54}, {11, 55}, {11, 56},
		{11, 53}, {12, 53}, {13, 53}, {14, 53}, {12, 57}, {13, 57}, {14, 57},
	})
	c := newChunk(13, 56)
	g.Generate(c)
	h := sha256.New()
	count := 0
	for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
		for z := range int32(16) {
			for x := range int32(16) {
				b := registry.BlockOf(c.BlockAt(x, y, z))
				if b == registry.BlockShortGrass || b == registry.BlockFern {
					fmt.Fprintf(h, "%d %d %d %s\n", x, y, z, b.Name())
					count++
				}
			}
		}
	}
	const wantCount, wantDigest = 85, "a0b6f7c2f0ecd4b93325dffaab55f7d7a09473621f03e26b04f270a3430fa781"
	if got := fmt.Sprintf("%x", h.Sum(nil)); count != wantCount || got != wantDigest {
		t.Fatalf("jungle grass and ferns: %d blocks, digest %s; want %d blocks, %s", count, got, wantCount, wantDigest)
	}
}

// TestOverworldForestLogPositionsVanilla pins the forest's logs in a finished
// chunk; the digest uses local x/y/z lines in y/z/x order.
func TestOverworldForestLogPositionsVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	c := newChunk(-12, -7)
	g.Generate(c)
	h := sha256.New()
	count := 0
	for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
		for z := range int32(16) {
			for x := range int32(16) {
				if registry.BlockOf(c.BlockAt(x, y, z)).InTag(registry.TagIDLogs) {
					fmt.Fprintf(h, "%d %d %d\n", x, y, z)
					count++
				}
			}
		}
	}
	// Frozen vanilla 26.3 seed 0, chunks -24,-24..23,23, recorded from server
	// start: chunk -12,-7 generates alone as it does in that recording.
	const want = "790b65c646df4b7b7ca03fb22cfe3571016c7f0610f62e71dec28f36ad19b691"
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != want || count != 29 {
		t.Fatalf("forest logs: %d blocks, digest %s; want 29 blocks, %s", count, got, want)
	}
}

// TestOverworldForestLogsAfterDiskVanilla pins the logs of chunks -11,-4 and
// -10,-3 of frozen vanilla 26.3 seed 0. A disk leaves the plant above its
// first block until the chunk is finished, so the leaf litter around an oak
// is not drawn for a cell holding that plant; the digest uses world x/y/z
// lines per chunk in y/z/x order.
func TestOverworldForestLogsAfterDiskVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	// The frozen cache's FEATURES order around the two chunks.
	g.DecorateInOrder([]ChunkPos{
		{-13, -2}, {-13, -5}, {-13, -4}, {-13, -3},
		{-12, -5}, {-12, -4}, {-12, -3}, {-12, -2},
		{-11, -5}, {-11, -4}, {-11, -3}, {-11, -2},
		{-10, -5}, {-10, -4}, {-10, -3}, {-10, -2},
		{-9, -5}, {-9, -4}, {-9, -3}, {-9, -2},
	})
	for _, want := range []struct {
		pos    ChunkPos
		count  int
		digest string
	}{
		{ChunkPos{-11, -4}, 21, "043eec317aa6cde7bc8e519da158d58d0c9a91481d161e4f86270bad566ffd10"},
		{ChunkPos{-10, -3}, 15, "8a4614b13a28c9299c955b03e4bdddf3c860d945b67945c29ecd28df5dffdf4a"},
	} {
		c := newChunk(want.pos.X, want.pos.Z)
		g.Generate(c)
		h := sha256.New()
		count := 0
		for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
			for z := range int32(16) {
				for x := range int32(16) {
					if registry.BlockOf(c.BlockAt(x, y, z)).InTag(registry.TagIDLogs) {
						fmt.Fprintf(h, "%d %d %d\n", want.pos.X*16+x, y, want.pos.Z*16+z)
						count++
					}
				}
			}
		}
		if got := fmt.Sprintf("%x", h.Sum(nil)); got != want.digest || count != want.count {
			t.Errorf("chunk %v logs: %d blocks, digest %s; want %d blocks, %s", want.pos, count, got, want.count, want.digest)
		}
	}
}

// TestOverworldGlowLichenHostAcrossBorderVanilla pins the glow lichen of seed
// 0 chunk -21,-22, whose faces on a host in the next chunk are removed once
// that host is gone (frozen from a seed-0 parity run with no glow lichen
// mismatch); the digest uses local x/y/z and state lines in y/z/x order.
func TestOverworldGlowLichenHostAcrossBorderVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	c := newChunk(-21, -22)
	g.Generate(c)
	for _, y := range []int32{-19, -18} {
		if s := c.BlockAt(-321&15, y, -352&15); !IsAirState(s) {
			t.Errorf("(-321,%d,-352) = %v, want air", y, registry.BlockOf(s).Name())
		}
	}
	h := sha256.New()
	count := 0
	for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
		for z := range int32(16) {
			for x := range int32(16) {
				if s := c.BlockAt(x, y, z); registry.BlockOf(s) == registry.BlockGlowLichen {
					fmt.Fprintf(h, "%d %d %d %d\n", x, y, z, s)
					count++
				}
			}
		}
	}
	const wantCount, wantDigest = 2, "199d0155e9a491c60024327c1dd5d61262076d16c43ab566e397bf3a4ab02364"
	if got := fmt.Sprintf("%x", h.Sum(nil)); count != wantCount || got != wantDigest {
		t.Errorf("glow lichen: count %d digest %s", count, got)
	}
}

// TestOverworldPodzolUnderSnowVanilla replays the recorded FEATURES order
// around seed 12345's podzol below snow blocks (vanilla 26.3).
func TestOverworldPodzolUnderSnowVanilla(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		chunk  ChunkPos
		seq    []ChunkPos
		lo, hi treePos
		count  int
	}{
		{ChunkPos{X: 18, Z: -14}, []ChunkPos{
			{17, -15}, {17, -14}, {17, -13}, {18, -15}, {18, -14}, {18, -13}, {19, -13}, {19, -15}, {19, -14},
		}, treePos{296, 123, -217}, treePos{299, 124, -215}, 7},
		{ChunkPos{X: 20, Z: -7}, []ChunkPos{
			{19, -8}, {19, -7}, {20, -8}, {20, -7}, {21, -8}, {21, -7}, {19, -6}, {20, -6}, {21, -6},
		}, treePos{325, 135, -111}, treePos{334, 138, -103}, 18},
	} {
		t.Run(fmt.Sprintf("%d,%d", tc.chunk.X, tc.chunk.Z), func(t *testing.T) {
			g := NewOverworldGenerator(12345)
			seq := tc.seq
			g.DecorateInOrder(seq)
			c := newChunk(tc.chunk.X, tc.chunk.Z)
			g.Generate(c)
			count := 0
			for x := tc.lo[0]; x <= tc.hi[0]; x++ {
				for z := tc.lo[2]; z <= tc.hi[2]; z++ {
					for y := tc.lo[1]; y <= tc.hi[1]; y++ {
						s := c.BlockAt(x&15, y, z&15)
						if registry.BlockOf(s) != registry.BlockPodzol || registry.BlockOf(c.BlockAt(x&15, y+1, z&15)) != registry.BlockSnowBlock {
							continue
						}
						count++
						if !registry.StateBool(s, registry.PropSnowy) {
							t.Errorf("(%d,%d,%d): podzol under snow_block has snowy=false", x, y, z)
						}
					}
				}
			}
			if count != tc.count {
				t.Errorf("checked %d podzol cells under snow blocks, want %d", count, tc.count)
			}
		})
	}
}

func TestTreeSnowyShapeUpdate(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	r := newDecorRegion(g, newChunk(0, 0), worldMinY, worldHeight)
	tree := &treeBuild{r: r}
	p := treePos{8, 100, 8}
	for _, soil := range []registry.Block{registry.BlockGrassBlock, registry.BlockPodzol, registry.BlockMycelium} {
		for _, above := range []registry.Block{registry.BlockSnow, registry.BlockSnowBlock, registry.BlockPowderSnow, registry.BlockAir} {
			want := above.InTag(registry.TagIDSnow)
			state, _ := registry.WithBool(soil.DefaultState(), registry.PropSnowy, !want)
			r.set(p[0], p[1]+1, p[2], above.DefaultState())
			for d := range dirCount {
				got := tree.shapeUpdate(state, p, d)
				if d != dirUp {
					if got != state {
						t.Errorf("%s below %s: direction %d changed snowy", soil.Name(), above.Name(), d)
					}
				} else if registry.BlockOf(got) != soil || registry.StateBool(got, registry.PropSnowy) != want {
					t.Errorf("%s below %s: snowy=%v, want %v", soil.Name(), above.Name(), registry.StateBool(got, registry.PropSnowy), want)
				}
			}
		}
	}
}

// TestOverworldLakePlantsRecordedOrder pins short grass of the frozen vanilla
// 26.3 seed-0 recording whose grass-block support a later neighbour's feature
// turns to sand. It stays until FULL post-processing removes it.
func TestOverworldLakePlantsRecordedOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		order []ChunkPos
		plant registry.Block
		cells []posKey
	}{
		{"grass", []ChunkPos{
			{17, -8}, {17, -7}, {17, -6}, {18, -8}, {18, -7}, {18, -6}, {19, -8}, {19, -7}, {19, -6},
		}, registry.BlockShortGrass, []posKey{{299, 63, -111}, {300, 63, -109}, {300, 63, -110}, {300, 63, -111}, {301, 63, -112}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewOverworldGenerator(0)
			g.DecorateInOrder(tc.order)
			for _, p := range tc.cells {
				own := g.protos.lookup(ChunkPos{p[0] >> 4, p[2] >> 4})
				st, ok := own.get(p[0]&15, p[1]-worldMinY, p[2]&15)
				if !ok || registry.BlockOf(st) != tc.plant {
					t.Fatalf("%v before FULL: got %s (written=%v), want %s", p, registry.BlockOf(st).Name(), ok, tc.plant.Name())
				}
			}
			for _, p := range tc.cells {
				c := newChunk(p[0]>>4, p[2]>>4)
				g.Generate(c)
				if got := registry.BlockOf(c.BlockAt(p[0]&15, p[1], p[2]&15)); got != registry.BlockAir {
					t.Errorf("%v: got %s, want air", p, got.Name())
				}
			}
		})
	}
}

// TestOverworldLeavesNextToOtherTreeVanilla pins an oak leaf of frozen vanilla
// 26.3 seed 0 beside a jungle tree's leaves: the walk of the later tree reaches
// the cell through the jungle log at distance 2 before a stale distance-4
// entry is drained, which needs the HashSet bins of that walk to treeify as
// Java's do.
func TestOverworldLeavesNextToOtherTreeVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	// The frozen cache's FEATURES entries within chunks -20..-11, 46..55.
	g.DecorateInOrder([]ChunkPos{{-20, 46}, {-19, 46}, {-18, 46}, {-20, 47}, {-19, 47}, {-18, 47}, {-17, 46}, {-17, 47}, {-20, 48}, {-19, 48}, {-18, 48}, {-17, 48}, {-20, 49}, {-19, 49}, {-18, 49}, {-20, 50}, {-19, 50}, {-18, 50}, {-17, 49}, {-20, 51}, {-19, 51}, {-17, 50}, {-18, 51}, {-17, 51}, {-20, 52}, {-19, 52}, {-18, 52}, {-20, 53}, {-19, 53}, {-18, 53}, {-20, 54}, {-17, 52}, {-19, 54}, {-18, 54}, {-17, 53}, {-17, 54}, {-16, 46}, {-16, 47}, {-16, 48}, {-16, 49}, {-16, 50}, {-16, 51}, {-16, 52}, {-16, 53}, {-16, 54}, {-15, 46}, {-15, 47}, {-15, 48}, {-15, 49}, {-15, 50}, {-15, 51}})
	own := g.protos.lookup(ChunkPos{-241 >> 4, 814 >> 4})
	st, ok := own.get(-241&15, 77-worldMinY, 814&15)
	if !ok || registry.BlockOf(st) != registry.BlockOakLeaves {
		t.Fatalf("got %s (written=%v), want oak_leaves", registry.BlockOf(st).Name(), ok)
	}
	if d, _ := registry.StateInt(st, registry.PropDistance); d != 2 {
		t.Fatalf("oak leaves distance %d, want 2", d)
	}
}

// TestOverworldTreeCanopyClearsLitterOnPathVanilla pins the five cells of
// seed 0 chunk 10,-12 where the chunk's own canopy leaves the litter that a
// neighbour's tree dropped before the village path went under it: vanilla
// has air there and dirt_path below (frozen from the 26.3 seed-0 parity run).
func TestOverworldTreeCanopyClearsLitterOnPathVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	// The frozen cache's FEATURES order around the chunk.
	g.DecorateInOrder([]ChunkPos{{9, -12}, {9, -11}, {9, -13}, {10, -13}, {10, -11}})
	c := newChunk(10, -12)
	g.Generate(c)
	for x := int32(170); x <= 174; x++ {
		if s := c.BlockAt(x&15, 75, -177&15); !IsAirState(s) {
			t.Errorf("(%d,75,-177) = %s; want air", x, registry.BlockOf(s).Name())
		}
		if b := registry.BlockOf(c.BlockAt(x&15, 74, -177&15)); b != registry.BlockDirtPath {
			t.Errorf("(%d,74,-177) = %s; want dirt_path", x, b.Name())
		}
	}
}

// TestOverworldAncientCitySoulFireOverSculk pins the soul fire a city's
// template leaves in a corridor: the sculk patch replaces the soul sand under
// it, and the fire stays because jigsaw templates carry UPDATE_KNOWN_SHAPE.
// Frozen vanilla 26.3 seed 12345, chunk -19,-19.
// vanilla: SinglePoolElement.place, WorldGenRegion.setBlock
func TestOverworldAncientCitySoulFireOverSculk(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(12345)
	c := newChunk(-19, -19)
	g.Generate(c)
	for x := int32(-296); x <= -289; x++ {
		if got := c.BlockAt(x&15, -42, -289&15); registry.BlockOf(got) != registry.BlockSoulFire {
			t.Errorf("(%d,-42,-289) = %v, want soul_fire", x, registry.BlockOf(got))
		}
	}
}

// TestOverworldAncientCityVeinGrowthSkipsSculk pins a sculk_vein the
// deep-dark growth feature places on a city wall: its spread may not put a
// face against the sculk a neighbour's patch left to the west, so it takes the
// polished deepslate above. Frozen vanilla 26.3 seed 0, in the cache's
// FEATURES order up to chunk 47,75.
// vanilla: SculkVeinBlock.SculkVeinSpreaderConfig.stateCanBeReplaced
func TestOverworldAncientCityVeinGrowthSkipsSculk(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	g.DecorateInOrder([]ChunkPos{{45, 74}, {45, 75}, {45, 76}, {46, 74}, {46, 75}, {46, 76}, {47, 74}, {47, 75}})
	st, ok := g.protos.lookup(ChunkPos{47, 75}).get(752&15, -50-worldMinY, 1209&15)
	if !ok || registry.BlockOf(st) != registry.BlockSculkVein {
		t.Fatalf("(752,-50,1209) = %s (written=%v), want sculk_vein", registry.BlockOf(st).Name(), ok)
	}
	for d, want := range [dirCount]bool{dirUp: true, dirNorth: true} {
		if got := registry.StateBool(st, facePropIDs[d]); got != want {
			t.Errorf("face %d = %v, want %v", d, got, want)
		}
	}
}

// TestOverworldMansionWallClearsLeafLitter pins the air above a woodland
// mansion cobblestone wall: litter placed there earlier cannot survive on a
// wall top. Frozen vanilla 26.3 seed 0, chunk -219,-49.
// vanilla: LeafLitterBlock.canSurvive, VegetationBlock.updateShape
func TestOverworldMansionWallClearsLeafLitter(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	c := newChunk(-219, -49)
	g.Generate(c)
	if b := registry.BlockOf(c.BlockAt(-3498&15, 64, -783&15)); b != registry.BlockCobblestoneWall {
		t.Fatalf("(-3498,64,-783) = %s; want cobblestone_wall", b.Name())
	}
	if s := c.BlockAt(-3498&15, 65, -783&15); !IsAirState(s) {
		t.Errorf("(-3498,65,-783) = %s; want air", registry.BlockOf(s).Name())
	}
}
