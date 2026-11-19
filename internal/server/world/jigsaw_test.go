package world

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// bastionStartDigests are vanilla's saved bastion starts, read from the
// reference server's region files: seed, start chunk, piece count and the
// SHA-256 of the start's NBT (structures.starts.minecraft:bastion_remnant, as
// nbt.Encode writes it with no root name).
func readBastionStarts(t *testing.T) []struct {
	seed   int64
	cx, cz int32
	pieces int
	sum    string
} {
	t.Helper()
	data, err := os.ReadFile("testdata/bastion_starts.txt")
	if err != nil {
		t.Fatal(err)
	}
	var out []struct {
		seed   int64
		cx, cz int32
		pieces int
		sum    string
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var e struct {
			seed   int64
			cx, cz int32
			pieces int
			sum    string
		}
		if _, err := fmt.Sscan(line, &e.seed, &e.cx, &e.cz, &e.pieces, &e.sum); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		t.Fatal("no starts listed")
	}
	return out
}

// TestBastionMatchesVanilla compares the bastions with vanilla's; the block and
// chest checks read the same cached chunk ranges.
func TestBastionMatchesVanilla(t *testing.T) {
	t.Parallel()
	t.Run("BastionStarts", testBastionStarts)
	t.Run("BastionBlocks", testBastionBlocks)
	t.Run("BastionChests", testBastionChests)
}

// testBastionStarts grows the bastions vanilla saved, one of each of the four
// start templates (housing, hoglin stables, treasure room, bridge), and
// compares each start's saved NBT: every piece's template, rotation, position,
// bounding box and junctions, in order.
// vanilla: JigsawPlacement.addPieces
func testBastionStarts(t *testing.T) {
	t.Parallel()
	def := jigsawStructureNamed(bastionID)
	for _, e := range readBastionStarts(t) {
		start := buildJigsawStart(def, e.seed, e.cx, e.cz, 0, netherGenHeight-1, nil)
		if start == nil {
			t.Fatalf("seed %d start (%d, %d): no start", e.seed, e.cx, e.cz)
		}
		data, err := nbt.Encode("", start.nbt(bastionID))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); len(start.pieces) != e.pieces || got != e.sum {
			t.Errorf("seed %d start (%d, %d): %d pieces, NBT digest %s; vanilla %d pieces, %s",
				e.seed, e.cx, e.cz, len(start.pieces), got, e.pieces, e.sum)
		}
	}
}

// TestBastionStandsItsMobs checks that the mobs of the bastion's mob templates
// are each stood once, in the chunk they are in, with the position the piece
// turns them to.
// vanilla: StructureTemplate.placeEntities
func TestBastionStandsItsMobs(t *testing.T) {
	t.Parallel()
	const seed, startX, startZ = 0, -27, -10
	g := NewNetherGenerator(seed)
	start := buildJigsawStart(jigsawStructureNamed(bastionID), seed, startX, startZ, 0, netherGenHeight-1, nil)
	want := 0
	for _, p := range start.pieces {
		want += len(p.element.template.Entities)
	}
	if want == 0 {
		t.Fatal("the start has no template entities; pick another")
	}
	got := 0
	seen := map[string]bool{}
	for cx := start.box.minX >> 4; cx <= start.box.maxX>>4; cx++ {
		for cz := start.box.minZ >> 4; cz <= start.box.maxZ>>4; cz++ {
			for _, e := range g.GeneratedEntities(ChunkPos{X: cx, Z: cz}) {
				pos, _ := nbt.Get[nbt.List](e, "Pos")
				x, z := float64(pos.Items[0].(nbt.Double)), float64(pos.Items[2].(nbt.Double))
				if int32(math.Floor(x))>>4 != cx || int32(math.Floor(z))>>4 != cz {
					t.Errorf("an entity at %v, %v is in chunk (%d, %d)", x, z, cx, cz)
				}
				id, _ := nbt.Get[nbt.String](e, "id")
				if id != "minecraft:piglin" && id != "minecraft:piglin_brute" && id != "minecraft:hoglin" {
					t.Errorf("entity %s", id)
				}
				uid := fmt.Sprint(e["UUID"])
				if seen[uid] {
					t.Errorf("UUID %s twice", uid)
				}
				seen[uid] = true
				got++
			}
		}
	}
	if got != want {
		t.Errorf("%d mobs stood, the templates hold %d", got, want)
	}
}

// bastionDigestRanges are the chunk ranges testdata/nether_bastions.txt and
// testdata/bastion_chests.txt were read from, each around a bastion remnant:
// housing (twice), hoglin stables, bridge and treasure room starts.
var bastionDigestRanges = []netherDigestRange{
	{0, -42, -16, -35, -9},
	{0, -30, -14, -25, -9},
	{0, 9, -17, 14, -12},
	{0, 42, -16, 48, -11},
	{1, 10, -4, 15, 3},
}

// testBastionBlocks compares the chunks around each kind of bastion with
// vanilla's, block for block: the pieces through their processors, the jigsaw
// replacement and the rotation, and the terrain they cut into.
// vanilla: SinglePoolElement.place, StructureTemplate.placeInWorld
func testBastionBlocks(t *testing.T) {
	t.Parallel()
	checkNetherFeatureDigests(t, "testdata/nether_bastions.txt", bastionDigestRanges)
}

// testBastionChests compares each chest the bastions place with vanilla's: its
// position, loot table and loot seed. The seeds come from the random the
// surface_structures step seeds per chunk, drawn once per chest in piece and
// block order.
// vanilla: StructureTemplate.placeInWorld, ChunkGenerator.applyBiomeDecoration
func testBastionChests(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/bastion_chests.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			want[line] = true
		}
	}
	got := map[string]bool{}
	for _, r := range bastionDigestRanges {
		for _, c := range generateNetherRange(r.seed, r.x0, r.z0, r.x1, r.z1) {
			for _, be := range c.blockEntities() {
				if be.Kind == KindChest {
					got[fmt.Sprintf("%d chest %d %d %d %d %s", r.seed, be.X, be.Y, be.Z, be.LootTableSeed, be.LootTable)] = true
				}
			}
		}
	}
	for line := range want {
		if !got[line] {
			t.Errorf("vanilla has %q, this generator does not", line)
		}
	}
	for line := range got {
		if !want[line] {
			t.Errorf("this generator has %q, vanilla does not", line)
		}
	}
}

// TestBastionStartIsSavedWithItsChunk checks that the chunk a bastion starts
// in saves the start under structures.starts, as vanilla's does, and that the
// chunk next to it saves a reference to it.
// vanilla: StructureStart.createTag, ChunkStatus.STRUCTURE_REFERENCES
func TestBastionStartIsSavedWithItsChunk(t *testing.T) {
	t.Parallel()
	var want struct {
		seed   int64
		cx, cz int32
		pieces int
		sum    string
	}
	for _, e := range readBastionStarts(t) {
		if e.seed == 0 && e.cx == -27 {
			want = e
		}
	}
	chunks := generateNetherRange(want.seed, want.cx, want.cz, want.cx+1, want.cz)
	for _, pos := range []ChunkPos{{X: want.cx, Z: want.cz}, {X: want.cx + 1, Z: want.cz}} {
		root, err := encodeChunkNBT(chunks[pos])
		if err != nil {
			t.Fatal(err)
		}
		structures, _ := nbt.Get[nbt.Compound](root, "structures")
		refs, _ := nbt.Get[nbt.Compound](structures, "References")
		packed, _ := nbt.Get[nbt.LongArray](refs, bastionID)
		if len(packed) != 1 || packed[0] != int64(uint32(want.cx))|int64(want.cz)<<32 {
			t.Errorf("chunk %v references %v, want the start's chunk", pos, packed)
		}
		starts, _ := nbt.Get[nbt.Compound](structures, "starts")
		start, saved := nbt.Get[nbt.Compound](starts, bastionID)
		if saved != (pos.X == want.cx) {
			t.Errorf("chunk %v: start saved is %v", pos, saved)
		}
		if saved {
			data, _ := nbt.Encode("", start)
			if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want.sum {
				t.Errorf("saved start digest %s, want vanilla's %s", got, want.sum)
			}
		}
	}
}

// BenchmarkJigsawPlan grows one start of each structure at chunk 0, 0 of seed
// 0 on a flat surface at Y 64.
func BenchmarkJigsawPlan(b *testing.B) {
	for _, name := range []string{"minecraft:village_plains", "minecraft:trial_chambers", "minecraft:ancient_city"} {
		def := jigsawStructureNamed(name)
		if def == nil {
			b.Fatalf("no jigsaw structure %s", name)
		}
		b.Run(strings.TrimPrefix(name, "minecraft:"), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, expand := planJigsawStart(def, 0, 0, 0, worldMinY, worldMinY+worldHeight-1, func(int32, int32) int32 { return 64 })
				if expand != nil {
					expand()
				}
			}
		})
	}
}

// A pool graph without structure aliases skips the alias pools its jigsaw
// blocks name, which have no file, rather than fail.
func TestJigsawPoolGraphSkipsAliasPools(t *testing.T) {
	for _, id := range []string{"minecraft:trial_chambers/chamber/end", "minecraft:trial_chambers/spawner/ranged"} {
		start, pools, err := jigsawPoolGraph(id)
		if err != nil || start == nil {
			t.Errorf("jigsawPoolGraph(%s) = %v, %d pools, %v; want a start pool", id, start, len(pools), err)
		}
	}
	if _, _, err := jigsawPoolGraph("minecraft:no/such/pool"); err == nil {
		t.Error("jigsawPoolGraph of a missing root pool: no error")
	}
}
