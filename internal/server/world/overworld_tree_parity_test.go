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
			t.Parallel()
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
