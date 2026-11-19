package world

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// ruinedPortalDigestRanges are the chunk ranges testdata/nether_ruinedportals.txt
// was read from, each around a start, covering rotations and mirrors.
// Nether portals have no air pocket in 26.3.
var ruinedPortalDigestRanges = []netherDigestRange{
	{0, -20, -27, -14, -21},
	{0, -25, 3, -19, 9},
	{0, -37, -73, -31, -67},
	{0, -59, -67, -53, -61},
	{0, -63, 80, -57, 86},
	{0, -66, 11, -60, 17},
}

// TestRuinedPortalsMatchVanilla compares the Nether's ruined portals with
// vanilla's; the block checks read cached chunk ranges.
func TestRuinedPortalsMatchVanilla(t *testing.T) {
	t.Parallel()
	t.Run("RuinedPortalStarts", testRuinedPortalStarts)
	t.Run("RuinedPortalBlocks", testRuinedPortalBlocks)
	t.Run("GiantRuinedPortalBlocks", testGiantRuinedPortalBlocks)
}

// testRuinedPortalBlocks compares the chunks around each portal with vanilla's,
// block for block: the processors, the rotation and mirror, the shape pass,
// the netherrack spread and the drip columns.
// vanilla: RuinedPortalPiece.postProcess
func testRuinedPortalBlocks(t *testing.T) {
	t.Parallel()
	checkNetherFeatureDigests(t, "testdata/nether_ruinedportals.txt", ruinedPortalDigestRanges)
}

// testGiantRuinedPortalBlocks compares the blocks inside the box of each giant
// portal with vanilla's, across the three giant templates, mirrored and not.
// Nether portals have no air pocket.
// vanilla: RuinedPortalStructure.findGenerationPoint, RuinedPortalPiece.postProcess
func testGiantRuinedPortalBlocks(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/ruinedportal_giants.txt")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var seed int64
		var x0, z0, x1, z1 int32
		var b jbox
		var sum string
		if _, err := fmt.Sscan(line, &seed, &x0, &z0, &x1, &z1, &b.minX, &b.minY, &b.minZ, &b.maxX, &b.maxY, &b.maxZ, &sum); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		chunks := generateNetherRange(seed, x0, z0, x1, z1)
		h := sha256.New()
		for y := b.minY; y <= b.maxY; y++ {
			for z := b.minZ; z <= b.maxZ; z++ {
				for x := b.minX; x <= b.maxX; x++ {
					name, props, _ := registry.BlockStateName(chunks[ChunkPos{X: x >> 4, Z: z >> 4}].BlockAt(x&15, y, z&15))
					fmt.Fprintf(h, "%s[%s]\n", name, props)
				}
			}
		}
		if got := fmt.Sprintf("%x", h.Sum(nil)); got != sum {
			t.Errorf("seed %d box %v: digest %s, vanilla %s", seed, b, got, sum)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no boxes listed")
	}
}

// testRuinedPortalStarts compares each saved start: the template, rotation,
// mirror, position, height, air pocket and box.
// vanilla: RuinedPortalStructure.findGenerationPoint
func testRuinedPortalStarts(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/ruinedportal_starts.txt")
	if err != nil {
		t.Fatal(err)
	}
	gens := map[int64]*NetherGenerator{}
	checked := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var seed int64
		var cx, cz int32
		var sum string
		if _, err := fmt.Sscan(line, &seed, &cx, &cz, &sum); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		g := gens[seed]
		if g == nil {
			g = NewNetherGenerator(seed)
			gens[seed] = g
		}
		p := g.ruinedPortalInCell(floorDiv(cx, ruinedPortalSpacing), floorDiv(cz, ruinedPortalSpacing))
		if p == nil || p.chunkX != cx || p.chunkZ != cz {
			t.Errorf("seed %d: no start at (%d, %d)", seed, cx, cz)
			continue
		}
		enc, err := nbt.Encode("", p.nbt())
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(enc)); got != sum {
			t.Errorf("seed %d start (%d, %d): NBT digest %s, vanilla %s", seed, cx, cz, got, sum)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no starts listed")
	}
}

// TestRuinedPortalSubmergedInLava checks that a block with no full face stands
// as lava where the world is lava, and a full one stands as itself.
// vanilla: LavaSubmergedBlockProcessor
func TestRuinedPortalSubmergedInLava(t *testing.T) {
	t.Parallel()
	g := NewNetherGenerator(0)
	r := newNetherRegion(g, newChunk(0, 0))
	r.set(4, 100, 4, g.lavaState)
	p := &ruinedPortal{airPocket: true}
	if got, ok := r.processPortalBlock(p, airState, 4, 100, 4); !ok || got != g.lavaState {
		t.Errorf("air in lava stands as %d, want lava %d", got, g.lavaState)
	}
	if got, ok := r.processPortalBlock(p, g.netherrackState, 4, 100, 4); !ok || registry.BlockOf(got) == registry.BlockLava {
		t.Errorf("netherrack in lava stands as %d", got)
	}
}

// TestRuinedPortalSettlesWalls checks that two walls the template saved
// apart join once both stand.
// vanilla: StructureTemplate.placeInWorld, Block.updateFromNeighbourShapes
func TestRuinedPortalSettlesWalls(t *testing.T) {
	t.Parallel()
	g := NewNetherGenerator(0)
	r := newNetherRegion(g, newChunk(0, 0))
	wall := registry.BlockStoneBrickWall.DefaultState()
	for x := int32(3); x < 7; x++ {
		r.set(x, 101, 4, airState)
	}
	tpl := &ruinedPortalTemplate{Template: &Template{Size: [3]int32{2, 2, 1}, Palettes: [][]TemplateBlock{{
		{Pos: [3]int32{0, 1, 0}, State: wall}, {Pos: [3]int32{1, 1, 0}, State: wall},
	}}}}
	p := &ruinedPortal{template: tpl, pivot: [2]int32{1, 0}, pos: [3]int32{4, 100, 4}, box: jbox{4, 100, 4, 5, 101, 4}}
	r.placeRuinedPortal(p)
	if got := registry.StateValue(r.get(4, 101, 4), registry.PropEast); got != "low" {
		t.Errorf("the western wall's east side is %q, want low", got)
	}
}

// TestRuinedPortalAirPocket checks that a template's air carves out the
// terrain only when the portal has an air pocket.
// vanilla: BlockIgnoreProcessor, RuinedPortalStructure.findGenerationPoint
func TestRuinedPortalAirPocket(t *testing.T) {
	t.Parallel()
	g := NewNetherGenerator(0)
	r := newNetherRegion(g, newChunk(0, 0))
	if _, ok := r.processPortalBlock(&ruinedPortal{}, airState, 4, 100, 4); ok {
		t.Error("air stands without an air pocket")
	}
	if _, ok := r.processPortalBlock(&ruinedPortal{airPocket: true}, airState, 4, 100, 4); !ok {
		t.Error("air is dropped with an air pocket")
	}
}

func TestNetherPortalZeroAirPocketRandom(t *testing.T) {
	t.Parallel()
	p := NewNetherGenerator(12345).ruinedPortalInCell(0, 0)
	if p.chunkX != 1 || p.chunkZ != 0 || p.template.id != "minecraft:ruined_portal/giant_portal_2" || p.rotation != Rotate180 || p.mirror != MirrorNone || p.airPocket || p.pos != ([3]int32{16, 28, 0}) || p.box != (jbox{16, 28, 1, 26, 43, 16}) {
		t.Fatalf("portal: %+v, template %s", p, p.template.id)
	}
}
