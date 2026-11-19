package world

import (
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// testStructure starts at its chunk's middle at height y with one piece: two
// glass blocks, in the start chunk's last column and the next chunk's first.
type testStructure struct{ y int32 }

type testPiece struct{ box jbox }

func (t testStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	x, z := ctx.chunkX*16, ctx.chunkZ*16
	return [3]int32{x + 8, t.y, z + 8}, func() []structurePiece {
		return []structurePiece{&testPiece{jbox{x + 15, 100, z + 4, x + 16, 100, z + 4}}}
	}, true
}

func (p *testPiece) bounds() jbox { return p.box }

func (p *testPiece) nbt() nbt.Compound {
	return nbt.Compound{"id": nbt.String("test")}
}

func (p *testPiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	for x := p.box.minX; x <= p.box.maxX; x++ {
		if chunk.inside(x, p.box.minY, p.box.minZ) {
			r.set(x, p.box.minY, p.box.minZ, registry.BlockStates["minecraft:glass"])
		}
	}
}

func structureRefs(c *Chunk, id string) []int64 {
	structures, _ := nbt.Get[nbt.Compound](c.carriedRoot, "structures")
	refs, _ := nbt.Get[nbt.Compound](structures, "References")
	r, _ := nbt.Get[nbt.LongArray](refs, id)
	return r
}

// structureTestGenerator is an overworld generator growing only the test
// structure, under the id given.
func structureTestGenerator(id string, y int32) *OverworldGenerator {
	g := NewOverworldGenerator(0)
	g.structures.types = map[string]structureType{id: testStructure{y}}
	return g
}

// biomeAllows reports whether the biome at the block is in the structure's
// tag.
func biomeAllows(g *OverworldGenerator, id string, x, y, z int32) bool {
	return registry.Biome(g.biomeIDs[g.biomeSource.At(x>>2, y>>2, z>>2)]).InTag(registry.BiomeTagOf(levelgen.StructureNamed(id).Biomes))
}

// TestStructureStartsWhereItsSetPlacesIt grows a mineshaft in every chunk the
// mineshafts set picks whose point is in a mineshaft biome, and the
// mineshaft_mesa, tried next, where the point is in a badlands.
func TestStructureStartsWhereItsSetPlacesIt(t *testing.T) {
	t.Parallel()
	const normal, mesa = "minecraft:mineshaft", "minecraft:mineshaft_mesa"
	g := NewOverworldGenerator(0)
	g.structures.types = map[string]structureType{normal: testStructure{50}, mesa: testStructure{50}}
	set := levelgen.StructureSetNamed("minecraft:mineshafts")
	count := map[string]int{}
	for x := int32(-150); x < 150; x++ {
		for z := int32(-150); z < 150; z++ {
			want := ""
			if set.IsStructureChunk(0, x, z, nil) {
				for _, id := range []string{normal, mesa} {
					if biomeAllows(g, id, x*16+8, 50, z*16+8) {
						want = id
						break
					}
				}
			}
			got := g.structureStartsIn(x, z)
			if (want != "") != (len(got) == 1) || (want != "" && (got[0].def.Name != want || got[0].chunkX != x || got[0].chunkZ != z)) {
				t.Fatalf("chunk %d %d: starts %v, want %q", x, z, got, want)
			}
			count[want]++
		}
	}
	if count[normal] == 0 || count[mesa] == 0 {
		t.Fatalf("starts %v; the range needs both", count)
	}
}

// firstStart finds the first chunk, x outer, holding a start.
func firstStart(t *testing.T, g *OverworldGenerator, r int32) *structureStart {
	t.Helper()
	for x := -r; x < r; x++ {
		for z := -r; z < r; z++ {
			if s := g.structureStartsIn(x, z); len(s) > 0 {
				return s[0]
			}
		}
	}
	t.Fatal("no start in the range")
	return nil
}

// TestStructureStartAndReferencesAreSaved checks the start chunk's
// structures.starts entry and the references of the chunks its box meets.
func TestStructureStartAndReferencesAreSaved(t *testing.T) {
	t.Parallel()
	const id = "minecraft:mineshaft"
	g := structureTestGenerator(id, 50)
	s := firstStart(t, g, 60)
	for dx := int32(-1); dx <= 2; dx++ {
		c := newChunk(s.chunkX+dx, s.chunkZ)
		g.saveStructures(c)
		start, hasStart := nbt.Get[nbt.Compound](c.structuresTag("starts"), id)
		if hasStart != (dx == 0) {
			t.Fatalf("chunk +%d: start %v", dx, start)
		}
		if dx == 0 {
			children, _ := nbt.Get[nbt.List](start, "Children")
			if x, _ := nbt.IntValue(start["ChunkX"]); x != s.chunkX || len(children.Items) != 1 {
				t.Fatalf("start %v", start)
			}
		}
		refs := structureRefs(c, id)
		// The piece reaches 1 block into the next chunk east.
		if want := dx == 0 || dx == 1; (len(refs) == 1 && refs[0] == packChunk(s.chunkX, s.chunkZ)) != want {
			t.Fatalf("chunk +%d: references %v, want the start: %v", dx, refs, want)
		}
	}
}

// TestStructurePiecesArePlacedInEachChunkTheyReach generates the start chunk
// and the one east of it and finds each one's part of the piece.
func TestStructurePiecesArePlacedInEachChunkTheyReach(t *testing.T) {
	t.Parallel()
	g := structureTestGenerator("minecraft:mineshaft", 50)
	s := firstStart(t, g, 60)
	glass := registry.BlockStates["minecraft:glass"]
	for dx, xs := range [2][2]int32{{15, 15}, {0, 0}} {
		c := newChunk(s.chunkX+int32(dx), s.chunkZ)
		g.Generate(c)
		for x := range int32(16) {
			if got, want := c.BlockAt(x, 100, 4) == glass, x >= xs[0] && x <= xs[1]; got != want {
				t.Errorf("chunk +%d x %d: glass %v, want %v", dx, x, got, want)
			}
		}
	}
	off := structureTestGenerator("minecraft:mineshaft", 50)
	off.DisableStructures()
	c := newChunk(s.chunkX, s.chunkZ)
	off.Generate(c)
	if c.BlockAt(15, 100, 4) == glass || len(c.StructureStarts()) != 0 {
		t.Error("a world without structures placed one")
	}
}

// TestAdaptingStructureGrowsItsBoxAndBendsTheTerrain checks that a buried
// structure's box reaches 12 blocks past its piece, so the chunk north of the
// start is referenced, and that the beardifier of the chunks within 12 blocks
// of the piece raises the density at its bottom.
func TestAdaptingStructureGrowsItsBoxAndBendsTheTerrain(t *testing.T) {
	t.Parallel()
	const id = "minecraft:trail_ruins"
	g := structureTestGenerator(id, 64)
	s := firstStart(t, g, 200)
	piece := s.pieces[0].bounds()
	if s.box != (jbox{piece.minX - 12, piece.minY - 12, piece.minZ - 12, piece.maxX + 12, piece.maxY + 12, piece.maxZ + 12}) {
		t.Fatalf("box %v around piece %v", s.box, piece)
	}
	north := newChunk(s.chunkX, s.chunkZ-1)
	g.saveStructures(north)
	if refs := structureRefs(north, id); len(refs) != 1 {
		t.Fatalf("north chunk references %v", refs)
	}
	for _, dx := range []int32{0, 1} {
		b := g.beardifierAt(s.chunkX+dx, s.chunkZ)
		if v := terrain.BeardifierSampler().Sample(terrain.NewContext(b), piece.minX, piece.minY, piece.minZ); v <= 0 {
			t.Errorf("chunk +%d: density %v at the piece, want it raised", dx, v)
		}
	}
	// The start reaches the chunk after next, but its piece is more than 12
	// blocks from it.
	if b := g.beardifierAt(s.chunkX+2, s.chunkZ); b != nil {
		t.Errorf("a chunk more than 12 blocks from the piece is adapted")
	}
	flat := structureTestGenerator("minecraft:mineshaft", 50)
	if m := firstStart(t, flat, 60); flat.beardifierAt(m.chunkX, m.chunkZ) != nil {
		t.Errorf("a structure that does not adapt the terrain bends it")
	}
}

// TestReferenceOrderMatchesFastutil checks referenceOrder against the
// iteration order of a fastutil 8.5.18 LongOpenHashSet filled x outer, z
// inner; the 8x8 square rehashes twice.
func TestReferenceOrderMatchesFastutil(t *testing.T) {
	for _, c := range []struct {
		x0, z0, x1, z1 int32
		want           [][2]int32
	}{
		{-1, -1, 1, 1, [][2]int32{{0, 0}, {0, 1}, {0, -1}, {1, -1}, {-1, 0}, {-1, 1}, {1, 1}, {1, 0}, {-1, -1}}},
		{-20, -12, -19, -11, [][2]int32{{-20, -12}, {-19, -11}, {-19, -12}, {-20, -11}}},
		{-4, -4, 3, 3, [][2]int32{{0, 0}, {1, -3}, {-1, 2}, {0, -4}, {3, -2}, {-3, 1}, {-1, 1}, {1, -2}, {-1, -3}, {1, 2}, {-3, -1}, {3, 0}, {-2, 3}, {2, -4}, {-4, -4}, {0, -3}, {0, 3}, {0, 1}, {0, -1}, {-4, 3}, {3, 1}, {-3, -2}, {1, 0}, {-1, -1}, {3, -3}, {-3, 2}, {2, -1}, {3, -4}, {-3, 3}, {-2, 0}, {-2, -4}, {-4, -1}, {2, 3}, {0, 2}, {0, -2}, {-4, 0}, {1, -4}, {-1, 3}, {1, 3}, {-1, -4}, {2, -2}, {3, -1}, {-3, 0}, {-2, 1}, {-4, -2}, {2, 2}, {2, 0}, {-2, -3}, {-4, 2}, {-2, -1}, {1, -1}, {-1, 0}, {-1, -2}, {1, 1}, {-3, -4}, {3, 3}, {3, 2}, {-3, -3}, {-4, 1}, {-4, -3}, {-2, -2}, {2, -3}, {-2, 2}, {2, 1}}},
	} {
		var starts []*structureStart
		for x := c.x0; x <= c.x1; x++ {
			for z := c.z0; z <= c.z1; z++ {
				starts = append(starts, &structureStart{chunkX: x, chunkZ: z})
			}
		}
		got := referenceOrder(starts)
		if len(got) != len(c.want) {
			t.Fatalf("%d starts, want %d", len(got), len(c.want))
		}
		for i, s := range got {
			if [2]int32{s.chunkX, s.chunkZ} != c.want[i] {
				t.Fatalf("%d,%d..%d,%d: start %d is %d,%d, want %v", c.x0, c.z0, c.x1, c.z1, i, s.chunkX, s.chunkZ, c.want[i])
			}
		}
	}
}
