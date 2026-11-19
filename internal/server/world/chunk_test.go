package world

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// readSection reads back one encoded chunk section, returning its block count,
// fluid count, the bits per entry its blocks were written with, and the block
// state of every block in it.
func readSection(t *testing.T, r *protocol.Reader) (count int16, fluids int16, bits int, blocks []uint16) {
	t.Helper()

	count = r.ReadI16()
	fluids = r.ReadI16()
	bits, blocks = readContainer(t, r, blocksPerSection)

	biomeBits, biomes := readContainer(t, r, 1)
	if biomeBits != 0 {
		t.Errorf("biome bits per entry = %d, want 0", biomeBits)
	}
	if biomes[0] == 0 {
		t.Error("biome was not set")
	}
	return count, fluids, bits, blocks
}

// readContainer reads back a paletted container of the given number of entries.
func readContainer(t *testing.T, r *protocol.Reader, entries int) (int, []uint16) {
	t.Helper()

	bits := int(r.ReadU8())
	if bits == 0 {
		// A single value stands for every entry, and there is no data array.
		value := uint16(r.ReadVarInt())
		all := make([]uint16, entries)
		for i := range all {
			all[i] = value
		}
		return 0, all
	}

	var palette []uint16
	if bits <= maxIndirectBits {
		for range r.ReadVarInt() {
			palette = append(palette, uint16(r.ReadVarInt()))
		}
	}

	perLong, mask := 64/bits, uint64(1)<<bits-1
	values := make([]uint16, 0, entries)
	for len(values) < entries {
		packed := uint64(r.ReadI64())
		for i := 0; i < perLong && len(values) < entries; i++ {
			value := uint16(packed >> (i * bits) & mask)
			if palette != nil {
				if int(value) >= len(palette) {
					t.Fatalf("entry %d is not in a palette of %d", value, len(palette))
				}
				value = palette[value]
			}
			values = append(values, value)
		}
	}
	return bits, values
}

// flatChunk generates a chunk of the flat world.
func flatChunk(x, z int32) *Chunk {
	c := newChunk(x, z)
	NewFlatGenerator().Generate(c)
	return c
}

func TestFlatWorldIsStoneUpToGroundLevel(t *testing.T) {
	t.Parallel()
	c := flatChunk(3, -2)

	for _, tc := range []struct {
		y    int32
		want uint16
	}{
		{worldMinY, stoneState},
		{0, stoneState},
		{GroundLevel - 1, stoneState},
		{GroundLevel, airState},
		{worldMinY + worldHeight - 1, airState},
		// Outside the world, where a generator may not write either.
		{worldMinY - 1, airState},
		{worldMinY + worldHeight, airState},
	} {
		if got := c.BlockAt(7, tc.y, 9); got != tc.want {
			t.Errorf("block at y=%d = %d, want %d", tc.y, got, tc.want)
		}
	}
}

func TestFlatChunkEncodesAsUniformSections(t *testing.T) {
	t.Parallel()
	c := flatChunk(0, 0)

	r := protocol.NewReader(encodeWhole(c))
	for section := range int32(sectionCount) {
		count, fluids, bits, blocks := readSection(t, r)
		if bits != 0 {
			t.Errorf("section %d: bits per entry = %d, want 0 for a section of one block", section, bits)
		}

		want, wantCount := uint16(airState), int16(0)
		if section*sectionSize+worldMinY < GroundLevel {
			want, wantCount = stoneState, blocksPerSection
		}
		if count != wantCount {
			t.Errorf("section %d: block count = %d, want %d", section, count, wantCount)
		}
		// The flat world has no water, lava, or waterlogged blocks.
		if fluids != 0 {
			t.Errorf("section %d: fluid count = %d, want 0", section, fluids)
		}
		for i, got := range blocks {
			if got != want {
				t.Fatalf("section %d: block %d = %d, want %d", section, i, got, want)
			}
		}
	}
	if r.Remaining() != 0 {
		t.Errorf("%d bytes left over after %d sections", r.Remaining(), sectionCount)
	}
	if err := r.Err(); err != nil {
		t.Errorf("read sections: %v", err)
	}
}

func TestSectionOfSeveralBlocksUsesAPalette(t *testing.T) {
	t.Parallel()
	c := flatChunk(0, 0)
	// Dig a hole in the topmost stone section, which is uniform until then.
	c.SetBlock(1, GroundLevel-1, 2, airState)

	const section = (GroundLevel - 1 - worldMinY) / sectionSize
	r := protocol.NewReader(encodeWhole(c))
	for range section {
		readSection(t, r)
	}
	count, fluids, bits, blocks := readSection(t, r)

	if bits != minIndirectBits {
		t.Errorf("bits per entry = %d, want %d", bits, minIndirectBits)
	}
	if want := int16(blocksPerSection - 1); count != want {
		t.Errorf("block count = %d, want %d", count, want)
	}
	if fluids != 0 {
		t.Errorf("fluid count = %d, want 0", fluids)
	}
	hole := blockIndex(1, (GroundLevel-1-worldMinY)%sectionSize, 2)
	for i, got := range blocks {
		want := uint16(stoneState)
		if i == hole {
			want = airState
		}
		if got != want {
			t.Fatalf("block %d = %d, want %d", i, got, want)
		}
	}
}

// TestSectionOfSeveralBiomesSendsAPalette checks the biome container a chunk
// packet carries for a section that mixes biomes: the 64 per-cell entries go
// out through a palette and a packed index array — the [plains, desert] grid
// of the loaded chunk — rather than the one biome a single-valued container
// carries for a uniform section.
func TestSectionOfSeveralBiomesSendsAPalette(t *testing.T) {
	t.Parallel()
	plains := registry.MustID("minecraft:worldgen/biome", "minecraft:plains")
	desert := registry.MustID("minecraft:worldgen/biome", "minecraft:desert")

	loaded, err := decodeChunkNBT(mixedBiomeSections())
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}

	// The bottom section is the packet's first; its blocks are all air, so
	// the block container before the biomes is the single-value one.
	r := protocol.NewReader(encodeWhole(loaded))
	if count, fluids := r.ReadI16(), r.ReadI16(); count != 0 || fluids != 0 {
		t.Fatalf("the empty section counts %d blocks and %d fluids, want none", count, fluids)
	}
	if bits, blocks := readContainer(t, r, blocksPerSection); bits != 0 || blocks[0] != airState {
		t.Fatalf("the bottom section's blocks went out at %d bits, want the single-value air", bits)
	}

	bits, biomes := readContainer(t, r, 64)
	if bits != 1 {
		t.Fatalf("the biome container's bits per entry = %d, want 1 for a palette of two", bits)
	}
	for i, cell := range biomes {
		want := plains
		if i == 63 { // the topmost cell (3, 3, 3), the one the desert fills
			want = desert
		}
		if int32(cell) != want {
			t.Fatalf("biome cell %d = %d, want %d", i, cell, want)
		}
	}
}

// TestSectionOfMoreBiomesThanAPaletteHoldsSendsDirectIDs checks the widest
// biome container a section goes out as: past the eight entries an indirect
// palette carries, the cells travel as raw registry IDs at the biome
// registry's own width — seven bits for the 66 biomes of the registry —
// with no palette list ahead of them.
func TestSectionOfMoreBiomesThanAPaletteHoldsSendsDirectIDs(t *testing.T) {
	t.Parallel()
	names := []string{
		"minecraft:badlands", "minecraft:birch_forest", "minecraft:beach",
		"minecraft:forest", "minecraft:ocean", "minecraft:plains",
		"minecraft:desert", "minecraft:snowy_plains", "minecraft:swamp",
	}

	// Cell i holds the i%9th palette entry, packed by hand at 4 bits each
	// — the width a palette of nine asks for — 16 entries to a long over
	// the grid's 64 cells.
	var data nbt.LongArray
	for i := 0; i < 64; i += 16 {
		var packed uint64
		min := min(16, 64-i)
		for j := range min {
			packed |= uint64((i+j)%len(names)) << (j * 4)
		}
		data = append(data, int64(packed))
	}

	palette := make([]nbt.Tag, len(names))
	for i, name := range names {
		palette[i] = nbt.String(name)
	}
	root := nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"xPos":        nbt.Int(0),
		"yPos":        nbt.Int(worldMinY / sectionSize),
		"zPos":        nbt.Int(0),
		"Status":      nbt.String("minecraft:full"),
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Y": nbt.Byte(worldMinY / sectionSize),
				"biomes": nbt.Compound{
					"palette": nbt.List{Elem: nbt.TypeString, Items: palette},
					"data":    data,
				},
			},
		}},
	}

	loaded, err := decodeChunkNBT(root)
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}

	r := protocol.NewReader(encodeWhole(loaded))
	r.ReadI16() // block count: the section holds none
	r.ReadI16() // fluid count
	readContainer(t, r, blocksPerSection)

	if bits := r.ReadU8(); bits != 7 {
		t.Fatalf("the biome container's bits per entry = %d, want the registry's direct width 7", bits)
	}
	// The direct form carries no palette: the entries are the registry IDs
	// themselves, nine to a long.
	for i := 0; i < 64; i += 9 {
		packed := uint64(r.ReadI64())
		for j := 0; j < 9 && i+j < 64; j++ {
			got := int32(packed>>(j*7)) & 0x7f
			if want := biomeID(names[(i+j)%len(names)]); got != want {
				t.Fatalf("biome cell %d = %d, want %d", i+j, got, want)
			}
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("read the direct container: %v", err)
	}
}

// TestChunkBiomeAtReadsTheMixedCells checks the cell a section's 4x4x4 biome
// grid answers for: every block coordinate inside the desert cell reads
// desert, an adjacent cell reads plains, and the section above reads its own
// single biome.
func TestChunkBiomeAtReadsTheMixedCells(t *testing.T) {
	t.Parallel()
	plains := registry.MustID("minecraft:worldgen/biome", "minecraft:plains")
	desert := registry.MustID("minecraft:worldgen/biome", "minecraft:desert")
	swamp := registry.MustID("minecraft:worldgen/biome", "minecraft:swamp")

	loaded, err := decodeChunkNBT(mixedBiomeSections())
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}

	// The desert fills the grid's topmost cell, local x, y and z 12..15.
	for _, p := range [][3]int32{{12, 12, 12}, {15, 12, 12}, {12, 15, 15}} {
		if got := loaded.BiomeAt(p[0], worldMinY+p[1], p[2]); got != desert {
			t.Errorf("biome at (%d,%d,%d) = %d, want desert (%d)", p[0], worldMinY+p[1], p[2], got, desert)
		}
	}
	// The neighbouring cell and the foot of the section are plains.
	for _, p := range [][3]int32{{0, 0, 0}, {11, 11, 11}, {0, 15, 0}} {
		if got := loaded.BiomeAt(p[0], worldMinY+p[1], p[2]); got != plains {
			t.Errorf("biome at (%d,%d,%d) = %d, want plains (%d)", p[0], worldMinY+p[1], p[2], got, plains)
		}
	}
	// The section above carries its own single biome.
	if got := loaded.BiomeAt(0, worldMinY+sectionSize, 0); got != swamp {
		t.Errorf("biome above = %d, want swamp (%d)", got, swamp)
	}
}

// TestSectionWithWaterReportsFluidCount checks that water blocks in a section
// are counted in the fluid count field, not just the block count. Without a
// non-zero fluid count the client skips fluid physics and a player falls
// through water as if it were air.
func TestSectionWithWaterReportsFluidCount(t *testing.T) {
	t.Parallel()
	c := flatChunk(0, 0)
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("could not find minecraft:water state")
	}
	// Replace three stone blocks with water in the top stone section.
	c.SetBlock(0, GroundLevel-1, 0, water)
	c.SetBlock(1, GroundLevel-1, 0, water)
	c.SetBlock(2, GroundLevel-1, 0, water)

	const section = (GroundLevel - 1 - worldMinY) / sectionSize
	r := protocol.NewReader(encodeWhole(c))
	for range section {
		readSection(t, r)
	}
	count, fluids, bits, blocks := readSection(t, r)

	// Water is non-air, so the block count is still a full section.
	if count != blocksPerSection {
		t.Errorf("block count = %d, want %d", count, blocksPerSection)
	}
	// Three water blocks means three fluids.
	if fluids != 3 {
		t.Errorf("fluid count = %d, want 3", fluids)
	}
	// The section is no longer uniform, so it uses a palette.
	if bits != minIndirectBits {
		t.Errorf("bits per entry = %d, want %d", bits, minIndirectBits)
	}
	// The three water blocks should be in the section at the expected indices.
	var y int32 = (GroundLevel - 1 - worldMinY) % sectionSize
	for _, tc := range []struct {
		x, z int32
	}{
		{0, 0}, {1, 0}, {2, 0},
	} {
		if got := blocks[blockIndex(tc.x, y, tc.z)]; got != water {
			t.Errorf("block at (%d,%d,%d) = %d, want %d (water)", tc.x, y, tc.z, got, water)
		}
	}
}

func TestChunkGeneratedTicks(t *testing.T) {
	c := &Chunk{x: -2, z: -3}
	c.scheduleGenerated(15, -64, 0)
	c.scheduleGenerated(0, 17, 15)
	want := [][3]int32{{-17, -64, -48}, {-32, 17, -33}}
	first := c.GeneratedTicks()
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("ticks = %v, want %v", first, want)
	}
	first[0][0] = 123
	if got := c.GeneratedTicks(); !reflect.DeepEqual(got, want) {
		t.Fatalf("mutation changed pending ticks: %v", got)
	}
	snapshot := c.GeneratedTicks()
	c.scheduleGenerated(1, -1, 2)
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("later scheduling changed snapshot: %v", snapshot)
	}
	want = append(want, [3]int32{-31, -1, -46})
	if got := c.GeneratedTicks(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ticks after scheduling = %v, want %v", got, want)
	}
}
