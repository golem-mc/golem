package world

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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

func TestFourBitSectionMatchesGenericPacking(t *testing.T) {
	t.Parallel()
	choices := [...]uint16{
		stoneState,
		registry.BlockStates["minecraft:granite"],
		registry.BlockStates["minecraft:diorite"],
		registry.BlockStates["minecraft:andesite"],
		registry.BlockStates["minecraft:deepslate"],
		registry.BlockStates["minecraft:calcite"],
		registry.BlockStates["minecraft:tuff"],
		registry.BlockStates["minecraft:cobblestone"],
		registry.BlockStates["minecraft:sandstone"],
		registry.BlockStates["minecraft:red_sandstone"],
		registry.BlockStates["minecraft:blackstone"],
		registry.BlockStates["minecraft:netherrack"],
		registry.BlockStates["minecraft:end_stone"],
		registry.BlockStates["minecraft:obsidian"],
		registry.BlockStates["minecraft:quartz_block"],
		registry.BlockStates["minecraft:bricks"],
	}
	for size := 2; size <= len(choices); size++ {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			s := section{fill: stoneState}
			var states [blocksPerSection]uint16
			set := func(i int, state uint16) {
				s.set(i, state, false)
				states[i] = state
			}
			check := func(stage string) {
				t.Helper()
				var palette []uint16
				slots := make(map[uint16]uint64, size)
				for _, state := range states {
					if _, ok := slots[state]; !ok {
						slots[state] = uint64(len(palette))
						palette = append(palette, state)
					}
				}
				if len(palette) != size {
					t.Fatalf("%s: fixture has %d states, want %d", stage, len(palette), size)
				}

				var got, want protocol.Buffer
				s.encode(&got)
				want.PutI16(blocksPerSection)
				want.PutI16(0) // fluid count
				want.PutU8(4)
				want.PutVarInt(int32(len(palette)))
				for _, state := range palette {
					want.PutVarInt(int32(state))
				}
				putPacked(&want, 4, blocksPerSection, func(i int) uint64 { return slots[states[i]] })
				want.PutU8(0) // uniform biome grid
				want.PutVarInt(s.biome)
				if got.Len() != want.Len() {
					t.Fatalf("%s: section has %d bytes, want %d", stage, got.Len(), want.Len())
				}
				for i, wantByte := range want.Bytes() {
					if gotByte := got.Bytes()[i]; gotByte != wantByte {
						t.Fatalf("%s: byte %d = %02x, want %02x", stage, i, gotByte, wantByte)
					}
				}
			}

			// Keep an unused storage slot, forcing five-bit storage at size 16.
			s.set(0, registry.BlockStates["minecraft:bedrock"], false)
			for i := range states {
				set(i, choices[(i+i/16)%size])
			}
			set(15, choices[size-1])
			set(16, choices[0])
			check("initial")

			set(0, choices[1])
			set(1, choices[0])
			set(15, choices[0])
			set(16, choices[size-1])
			check("after replacements")
		})
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
