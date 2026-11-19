package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// pack is the inverse of unpackPalette.
func pack(idx []int, bitsPerEntry int) []int64 {
	perLong := 64 / bitsPerEntry
	out := make([]int64, (len(idx)+perLong-1)/perLong)
	for i, v := range idx {
		out[i/perLong] |= int64(uint64(v) << (uint(i%perLong) * uint(bitsPerEntry)))
	}
	return out
}

func TestUnpackPalette(t *testing.T) {
	for _, bits := range []int{1, 3, 4, 5, 6, 7, 15} {
		idx := make([]int, 4096)
		for i := range idx {
			idx[i] = (i * 7) % (1 << bits)
		}
		got, err := unpackPalette(pack(idx, bits), 1<<bits, 4096, bits)
		if err != nil {
			t.Fatalf("%d bits: %v", bits, err)
		}
		for i := range idx {
			if got[i] != idx[i] {
				t.Fatalf("%d bits: entry %d = %d, want %d", bits, i, got[i], idx[i])
			}
		}
	}
	if _, err := unpackPalette(make([]int64, 3), 16, 4096, 4); err == nil {
		t.Error("a short data array unpacked without error")
	}
	if _, err := unpackPalette(pack([]int{5}, 4), 3, 1, 4); err == nil {
		t.Error("an index past the palette unpacked without error")
	}
	got, err := unpackPalette(nil, 1, 64, 0)
	if err != nil || len(got) != 64 || got[63] != 0 {
		t.Errorf("single-entry palette = %v, %v; want 64 zeros", got, err)
	}
}

func TestCeilLog2(t *testing.T) {
	for n, want := range map[int]int{1: 0, 2: 1, 3: 2, 4: 2, 5: 3, 16: 4, 17: 5, 64: 6} {
		if got := ceilLog2(n); got != want {
			t.Errorf("ceilLog2(%d) = %d, want %d", n, got, want)
		}
	}
}

// writeRegion writes a one-chunk region file: the location header, a zero
// timestamp table, then a deflate record.
// location header, a zero timestamp table, then a deflate record.
func writeRegion(t *testing.T, worldDir, folder string, x, z int32, root nbt.Compound) {
	t.Helper()
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatal(err)
	}
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	record := binary.BigEndian.AppendUint32(nil, uint32(zbuf.Len()+1))
	record = append(record, 2)
	record = append(record, zbuf.Bytes()...)
	sectors := (len(record) + 4095) / 4096
	file := make([]byte, 8192+sectors*4096)
	copy(file[8192:], record)
	i := 4 * ((x & 31) + (z&31)*32)
	file[i], file[i+1], file[i+2], file[i+3] = 0, 0, 2, byte(sectors)

	dir := filepath.Join(worldDir, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, fmt.Sprintf("r.%d.%d.mca", x>>5, z>>5))
	if err := os.WriteFile(name, file, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadVanillaChunk(t *testing.T) {
	dir := t.TempDir()
	stone := nbt.Compound{"Name": nbt.String("minecraft:stone")}
	oakLog := nbt.Compound{"Name": nbt.String("minecraft:oak_log"), "Properties": nbt.Compound{"axis": nbt.String("x")}}
	air := nbt.Compound{"Name": nbt.String("minecraft:air")}

	// Section -4 (y -64..-49): stone below y=-60, an x-axis oak log at
	// local 3,5,7, air elsewhere; two biomes split at cell y 2.
	idx := make([]int, 4096)
	for i := range idx {
		if i>>8 < 4 {
			idx[i] = 0
		} else {
			idx[i] = 2
		}
	}
	idx[(5<<4|7)<<4|3] = 1
	bidx := make([]int, 64)
	for i := range bidx {
		if i>>4 >= 2 {
			bidx[i] = 1
		}
	}
	root := nbt.Compound{
		"xPos":   nbt.Int(-33),
		"zPos":   nbt.Int(4),
		"Status": nbt.String("minecraft:full"),
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"Y": nbt.Byte(-5)}, // light-only, below the world
			nbt.Compound{
				"Y": nbt.Byte(-4),
				"block_states": nbt.Compound{
					"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{stone, oakLog, air}},
					"data":    nbt.LongArray(pack(idx, 4)),
				},
				"biomes": nbt.Compound{
					"palette": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:desert"), nbt.String("minecraft:beach")}},
					"data":    nbt.LongArray(pack(bidx, 1)),
				},
			},
			nbt.Compound{
				"Y":            nbt.Byte(19),
				"block_states": nbt.Compound{"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{air}}},
				"biomes":       nbt.Compound{"palette": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:ocean")}}},
			},
		}},
	}
	writeRegion(t, dir, "region", -33, 4, root)

	got, err := readRegionChunk(dir, "region", -33, 4)
	if err != nil || got == nil {
		t.Fatalf("readRegionChunk = %v, %v", got, err)
	}
	if missing, err := readRegionChunk(dir, "region", -34, 4); missing != nil || err != nil {
		t.Errorf("an absent chunk read as %v, %v", missing, err)
	}
	cd, err := decodeVanillaChunk(overworld, got)
	if err != nil {
		t.Fatal(err)
	}
	if cd.X != -33 || cd.Z != 4 {
		t.Errorf("position %d,%d, want -33,4", cd.X, cd.Z)
	}
	checks := []struct {
		x, y, z int
		want    uint16
	}{
		{0, -64, 0, state(t, "minecraft:stone", nil)},
		{15, -61, 15, state(t, "minecraft:stone", nil)},
		{3, -59, 7, state(t, "minecraft:oak_log", map[string]string{"axis": "x"})},
		{3, -59, 8, state(t, "minecraft:air", nil)},
		{0, 250, 0, state(t, "minecraft:air", nil)},
	}
	for _, c := range checks {
		if got := cd.Blocks[cd.index(c.x, c.y, c.z)]; got != c.want {
			t.Errorf("block %d,%d,%d = %s, want %s", c.x, c.y, c.z, blockName(got), blockName(c.want))
		}
	}
	for _, c := range []struct {
		cy   int
		want string
	}{{0, "minecraft:desert"}, {1, "minecraft:desert"}, {2, "minecraft:beach"}, {3, "minecraft:beach"}, {93, "minecraft:ocean"}, {50, "minecraft:plains"}} {
		if got := biomeName(cd.Biomes[cellIndex(1, c.cy, 2)]); got != c.want {
			t.Errorf("biome at cell y %d = %s, want %s", c.cy, got, c.want)
		}
	}

	root["Status"] = nbt.String("minecraft:features")
	if _, err := decodeVanillaChunk(overworld, root); err == nil {
		t.Error("a chunk short of full status decoded without error")
	}
}

func TestResolveStateRejectsUnknownProperties(t *testing.T) {
	if got := resolveState("minecraft:oak_log", map[string]string{"axis": "w"}); got != unknownState {
		t.Errorf("an unknown property value resolved to %s", blockName(got))
	}
	if got := resolveState("minecraft:no_such_block", nil); got != unknownState {
		t.Errorf("an unknown block resolved to %s", blockName(got))
	}
}

func TestReadNetherChunk(t *testing.T) {
	nether := dimensions["the_nether"]
	netherrack := nbt.Compound{"Name": nbt.String("minecraft:netherrack")}
	lava := nbt.Compound{"Name": nbt.String("minecraft:lava"), "Properties": nbt.Compound{"level": nbt.String("0")}}
	uniform := func(y int8, block nbt.Compound, biome string) nbt.Compound {
		return nbt.Compound{
			"Y":            nbt.Byte(y),
			"block_states": nbt.Compound{"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{block}}},
			"biomes":       nbt.Compound{"palette": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String(biome)}}},
		}
	}
	root := nbt.Compound{
		"xPos":   nbt.Int(2),
		"zPos":   nbt.Int(-1),
		"Status": nbt.String("minecraft:full"),
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			uniform(-1, netherrack, "minecraft:basalt_deltas"), // light-only, below the nether
			uniform(0, netherrack, "minecraft:nether_wastes"),
			uniform(15, lava, "minecraft:crimson_forest"),
			uniform(16, netherrack, "minecraft:basalt_deltas"), // light-only, above it
		}},
	}
	cd, err := decodeVanillaChunk(nether, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cd.Blocks) != 256*256 || len(cd.Biomes) != 16*64 {
		t.Fatalf("%d blocks and %d cells, want %d and %d", len(cd.Blocks), len(cd.Biomes), 256*256, 16*64)
	}
	rack := state(t, "minecraft:netherrack", nil)
	for _, c := range []struct {
		y    int
		want uint16
	}{{0, rack}, {15, rack}, {16, state(t, "minecraft:air", nil)}, {255, state(t, "minecraft:lava", nil)}} {
		if got := cd.Blocks[cd.index(4, c.y, 4)]; got != c.want {
			t.Errorf("block at y %d = %s, want %s", c.y, blockName(got), blockName(c.want))
		}
	}
	if got := biomeName(cd.Biomes[cellIndex(0, 0, 0)]); got != "minecraft:nether_wastes" {
		t.Errorf("bottom biome = %s, want nether_wastes", got)
	}
	if got := biomeName(cd.Biomes[cellIndex(0, 63, 0)]); got != "minecraft:crimson_forest" {
		t.Errorf("top biome = %s, want crimson_forest", got)
	}
}

func TestReadVanillaChunkModernPalette(t *testing.T) {
	// vanilla: BlockState.CODEC, StateHolder.codec; NBT wraps mixed palette entries.
	palette := nbt.NewList([]nbt.Tag{
		nbt.String("minecraft:air"), nbt.String("minecraft:oak_log"),
		nbt.Compound{"id": nbt.String("minecraft:oak_log"), "properties": nbt.Compound{"axis": nbt.String("x")}},
		nbt.String("minecraft:missing_block"),
	})
	indices := make([]int, 4096)
	indices[1], indices[2], indices[3] = 1, 2, 3
	root := nbt.Compound{
		"xPos": nbt.Int(0), "zPos": nbt.Int(0), "Status": nbt.String("minecraft:full"),
		"sections": nbt.NewList([]nbt.Tag{nbt.Compound{
			"Y": nbt.Byte(0), "block_states": nbt.Compound{"palette": palette, "data": nbt.LongArray(pack(indices, 4))},
		}}),
	}
	dir := t.TempDir()
	writeRegion(t, dir, "region", 0, 0, root)
	saved, err := readRegionChunk(dir, "region", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeVanillaChunk(dimensions["the_end"], saved)
	if err != nil {
		t.Fatal(err)
	}
	want := []uint16{resolveState("minecraft:air", nil), resolveState("minecraft:oak_log", map[string]string{"axis": "y"}), resolveState("minecraft:oak_log", map[string]string{"axis": "x"}), unknownState}
	for i, state := range want {
		if got.Blocks[i] != state {
			t.Errorf("block %d = %d, want %d", i, got.Blocks[i], state)
		}
	}
}

func TestReadEntitiesChunk(t *testing.T) {
	dir := t.TempDir()
	cow := nbt.Compound{"id": nbt.String("minecraft:cow")}
	writeRegion(t, dir, "entities", 3, -2, nbt.Compound{
		"DataVersion": nbt.Int(1),
		"Position":    nbt.IntArray{3, -2},
		"Entities":    nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{cow}},
	})
	got, err := readEntitiesChunk(dir, 3, -2)
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], cow) {
		t.Fatalf("readEntitiesChunk = %v, %v", got, err)
	}
	if none, err := readEntitiesChunk(dir, 4, -2); none != nil || err != nil {
		t.Errorf("a chunk without a record read as %v, %v", none, err)
	}
	writeRegion(t, dir, "entities", 5, 0, nbt.Compound{"Position": nbt.IntArray{6, 0}})
	if _, err := readEntitiesChunk(dir, 5, 0); err == nil {
		t.Error("a record saved at another chunk read without an error")
	}
}
