package world

import (
	"crypto/sha256"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// netherFossilRanges are the chunk ranges testdata/nether_fossils.txt and the
// start and reference files were read from.
var netherFossilRanges = []netherDigestRange{
	{0, 4, -44, 12, -36},
	{1, -3, -43, 11, -37},
}

// readFossilLines returns the data lines of a testdata file as fields.
func readFossilLines(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, strings.Fields(line))
		}
	}
	return out
}

type fossilChunk struct {
	seed   string
	cx, cz int32
}

// fossilKey reads the seed and chunk a testdata line starts with.
func fossilKey(t *testing.T, f []string) fossilChunk {
	t.Helper()
	x, errX := strconv.ParseInt(f[1], 10, 32)
	z, errZ := strconv.ParseInt(f[2], 10, 32)
	if errX != nil || errZ != nil {
		t.Fatalf("bad chunk in %v", f)
	}
	return fossilChunk{f[0], int32(x), int32(z)}
}

// TestNetherFossilsMatchVanilla compares the nether fossils with vanilla's;
// both checks read the same cached chunk ranges.
func TestNetherFossilsMatchVanilla(t *testing.T) {
	t.Parallel()
	t.Run("NetherFossils", testNetherFossilBlocks)
	t.Run("NetherFossilStartsAndReferences", testNetherFossilStartsAndReferences)
}

// testNetherFossilBlocks compares the chunks around nether fossils with
// vanilla's, block for block: the template and its dried ghast, and the terrain
// the beard moulds around it.
// vanilla: NetherFossilPieces.NetherFossilPiece.postProcess, Beardifier
func testNetherFossilBlocks(t *testing.T) {
	t.Parallel()
	checkNetherFeatureDigests(t, "testdata/nether_fossils.txt", netherFossilRanges)
}

// testNetherFossilStartsAndReferences checks the saved start of
// every fossil and every chunk's references to one: a start reaches the chunks
// within 12 blocks of its box, which is what the beard and the placement run
// on. References are compared as sets; vanilla saves them in hash order.
// vanilla: StructureStart.createTag, ChunkGenerator.createReferences
func testNetherFossilStartsAndReferences(t *testing.T) {
	t.Parallel()
	starts, refs := map[fossilChunk]string{}, map[fossilChunk][]string{}
	for _, f := range readFossilLines(t, "testdata/nether_fossil_starts.txt") {
		k := fossilKey(t, f)
		starts[k] = f[3]
	}
	for _, f := range readFossilLines(t, "testdata/nether_fossil_refs.txt") {
		k := fossilKey(t, f)
		refs[k] = slices.Sorted(slices.Values(f[3:]))
	}
	gotStarts, gotRefs := 0, 0
	for _, r := range netherFossilRanges {
		for pos, c := range generateNetherRange(r.seed, r.x0, r.z0, r.x1, r.z1) {
			k := fossilChunk{fmt.Sprint(r.seed), pos.X, pos.Z}
			root, err := encodeChunkNBT(c)
			if err != nil {
				t.Fatal(err)
			}
			structures, _ := nbt.Get[nbt.Compound](root, "structures")
			startTags, _ := nbt.Get[nbt.Compound](structures, "starts")
			start, saved := nbt.Get[nbt.Compound](startTags, netherFossilID)
			want, wantStart := starts[k]
			if saved != wantStart {
				t.Errorf("%v: start saved is %v, vanilla's is %v", k, saved, wantStart)
			}
			if saved {
				data, _ := nbt.Encode("", start)
				if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
					t.Errorf("%v: start digest %s, want vanilla's %s", k, got, want)
				}
				gotStarts++
			}
			refTags, _ := nbt.Get[nbt.Compound](structures, "References")
			packed, has := nbt.Get[nbt.LongArray](refTags, netherFossilID)
			var got []string
			for _, p := range packed {
				got = append(got, fmt.Sprint(p))
			}
			slices.Sort(got)
			if !slices.Equal(got, refs[k]) {
				t.Errorf("%v: references %v, vanilla's %v", k, got, refs[k])
			}
			if has {
				gotRefs++
			}
		}
	}
	if gotStarts != len(starts) || gotRefs != len(refs) {
		t.Errorf("compared %d starts and %d reference lists, the files hold %d and %d", gotStarts, gotRefs, len(starts), len(refs))
	}
}

// TestNetherFossilDataFromArchive checks what the archive says about the
// fossil.
// vanilla: NetherFossilStructure, nether_fossils structure set
func TestNetherFossilDataFromArchive(t *testing.T) {
	t.Parallel()
	d := netherFossils()
	if d.spacing != 2 || d.separation != 1 || d.salt != 14357921 || d.minY != 32 || d.belowTop != 2 {
		t.Errorf("fossil data %+v", d)
	}
	for i, tpl := range d.templates {
		if tpl == nil || len(tpl.Palettes) == 0 {
			t.Errorf("template %d is empty", i+1)
		}
	}
}

// TestNetherFossilsAreOffWithoutStructures checks that generate-structures=false
// leaves out the fossils: no start, no references, no beard.
func TestNetherFossilsAreOffWithoutStructures(t *testing.T) {
	t.Parallel()
	g := NewNetherGenerator(0)
	g.DisableStructures()
	for gz := int32(-22); gz <= -18; gz++ {
		for gx := int32(2); gx <= 6; gx++ {
			if g.fossilInCell(gx, gz) != nil {
				t.Fatalf("cell (%d, %d) has a fossil", gx, gz)
			}
		}
	}
	if g.fossilBeardifier(8, -40) != nil {
		t.Error("a beard with structures off")
	}
}

// firstFossil scans the cells from (0, -44) for a fossil that exists.
func firstFossil(t *testing.T, g *NetherGenerator, ok func(*fossilStart) bool) *fossilStart {
	t.Helper()
	for gz := int32(-44); gz <= -36; gz++ {
		for gx := int32(0); gx <= 12; gx++ {
			if s := g.fossilInCell(gx, gz); s != nil && g.resolved(s) && ok(s) {
				return s
			}
		}
	}
	t.Fatal("no fossil in range")
	return nil
}

// TestBaseColumnsLeaveOutTheFossilBeard checks that a structure's base column
// is the plain terrain even where a fossil's beard reaches.
// vanilla: NoiseBasedChunkGenerator.iterateNoiseColumn (BeardifierMarker)
func TestBaseColumnsLeaveOutTheFossilBeard(t *testing.T) {
	t.Parallel()
	g := NewNetherGenerator(0)
	s := firstFossil(t, g, func(*fossilStart) bool { return true })
	x, z := s.xz.minX, s.xz.minZ
	cx, cz := x>>4, z>>4
	plain, beard := newChunk(cx, cz), newChunk(cx, cz)
	g.terrain(plain, false)
	g.terrain(beard, true)
	differs := func(a, b *Chunk) bool {
		for y := range int32(netherGenHeight) {
			for lx := range int32(16) {
				for lz := range int32(16) {
					if a.BlockAt(lx, y, lz) != b.BlockAt(lx, y, lz) {
						return true
					}
				}
			}
		}
		return false
	}
	if !differs(plain, beard) {
		t.Fatal("the beard does not change the chunk under the fossil")
	}
	col := g.baseColumns([4][2]int32{{x, z}, {x, z}, {x, z}, {x, z}})[0]
	if differs(col, plain) {
		t.Error("the base column carries the beard")
	}
}

// TestPlaceFossilsGivesEachFossilTheWholeChunk checks that a fossil placed
// after another still writes every block in the chunk.
// vanilla: ChunkGenerator.applyBiomeDecoration (getWritableArea per start)
func TestPlaceFossilsGivesEachFossilTheWholeChunk(t *testing.T) {
	t.Parallel()
	for seed := range int64(40) {
		g := NewNetherGenerator(seed)
		for nz := int32(-12); nz <= 12; nz++ {
			for nx := int32(-12); nx <= 12; nx++ {
				var met []*fossilStart
				chunk := jbox{nx * 16, 1, nz * 16, nx*16 + 15, netherGenHeight - 1, nz*16 + 15}
				g.eachFossil(nx, nz, func(s *fossilStart) {
					if s.box.intersects(chunk) {
						met = append(met, s)
					}
				})
				if len(met) < 2 {
					continue
				}
				r := newNetherRegion(g, newChunk(nx, nz))
				r.nx, r.nz = nx, nz
				r.placeFossils(nx, nz)
				checkFossilWritten(t, r, met[len(met)-1], chunk)
				return
			}
		}
	}
	t.Fatal("no chunk meets two fossils")
}

// TestPlaceFossilsSkipsAFossilThatDoesNotMeetTheChunk checks that a fossil
// referenced by a chunk but not meeting it writes nothing, even where a fossil
// that does meet it lies beside it.
// vanilla: ChunkGenerator.applyBiomeDecoration (getWritableArea per start)
func TestPlaceFossilsSkipsAFossilThatDoesNotMeetTheChunk(t *testing.T) {
	t.Parallel()
	// Seed 129 chunk (-3, 13): a fossil that meets the chunk comes first in cell
	// order and a fossil that does not meet it lies within the first one's box
	// grown to the chunk.
	const seed, nx, nz = 129, -3, 13
	g := NewNetherGenerator(seed)
	chunk := jbox{nx * 16, 1, nz * 16, nx*16 + 15, netherGenHeight - 1, nz*16 + 15}
	var met, other []*fossilStart
	g.eachFossil(nx, nz, func(s *fossilStart) {
		if s.box.intersects(chunk) {
			met = append(met, s)
		} else {
			other = append(other, s)
		}
	})
	if len(met) == 0 || len(other) == 0 {
		t.Fatalf("chunk has %d meeting and %d other fossils", len(met), len(other))
	}
	r := newNetherRegion(g, newChunk(nx, nz))
	r.nx, r.nz = nx, nz
	r.placeFossils(nx, nz)
	checked := 0
	for _, s := range other {
		tpl := netherFossils().templates[s.template]
		for _, blk := range tpl.Palettes[0] {
			switch registry.BlockOf(blk.State).Name() {
			case "minecraft:air", "minecraft:structure_block":
				continue
			}
			tx, ty, tz := TransformTemplatePos(blk.Pos[0], blk.Pos[1], blk.Pos[2], MirrorNone, s.rotation)
			x, y, z := tx+s.pos[0], ty+s.pos[1], tz+s.pos[2]
			if absInt32(x>>4-nx) > 1 || absInt32(z>>4-nz) > 1 {
				continue
			}
			covered := false
			for _, m := range met {
				covered = covered || m.box.inside(x, y, z)
			}
			if covered {
				continue
			}
			checked++
			if r.get(x, y, z) == RotateState(blk.State, s.rotation) {
				t.Fatalf("block (%d, %d, %d) of a fossil that does not meet the chunk was written", x, y, z)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no block of the other fossils in the region")
	}
}

func checkFossilWritten(t *testing.T, r *decorRegion, s *fossilStart, chunk jbox) {
	t.Helper()
	tpl := netherFossils().templates[s.template]
	checked := 0
	for _, blk := range tpl.Palettes[0] {
		switch registry.BlockOf(blk.State).Name() {
		case "minecraft:air", "minecraft:structure_block":
			continue
		}
		tx, ty, tz := TransformTemplatePos(blk.Pos[0], blk.Pos[1], blk.Pos[2], MirrorNone, s.rotation)
		x, y, z := tx+s.pos[0], ty+s.pos[1], tz+s.pos[2]
		if x < chunk.minX || x > chunk.maxX || z < chunk.minZ || z > chunk.maxZ {
			continue
		}
		checked++
		if got, want := r.get(x, y, z), RotateState(blk.State, s.rotation); got != want {
			t.Fatalf("block (%d, %d, %d) is %d, want %d", x, y, z, got, want)
		}
	}
	if checked == 0 {
		t.Fatal("no block of the last fossil in the chunk")
	}
}
