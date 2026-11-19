package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// readRegionChunk reads one chunk's NBT out of the region files in the
// world's folder ("region" or "entities"), or returns nil when the region
// holds no record for it.
func readRegionChunk(worldDir, folder string, x, z int32) (nbt.Compound, error) {
	path := filepath.Join(worldDir, folder, fmt.Sprintf("r.%d.%d.mca", x>>5, z>>5))
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// The header is 1024 big-endian location words: a 3-byte sector offset
	// and a 1-byte sector count, sectors being 4 KiB.
	var loc [4]byte
	if _, err := f.ReadAt(loc[:], int64(4*((x&31)+(z&31)*32))); err != nil {
		return nil, fmt.Errorf("%s header: %w", path, err)
	}
	offset := int64(loc[0])<<16 | int64(loc[1])<<8 | int64(loc[2])
	if offset == 0 {
		return nil, nil
	}
	var head [5]byte
	if _, err := f.ReadAt(head[:], offset*4096); err != nil {
		return nil, fmt.Errorf("%s chunk %d,%d: %w", path, x, z, err)
	}
	length := int(binary.BigEndian.Uint32(head[:4]))
	if length < 1 {
		return nil, fmt.Errorf("%s chunk %d,%d: empty record", path, x, z)
	}
	compression := head[4]
	if compression&0x80 != 0 {
		return nil, fmt.Errorf("%s chunk %d,%d: stored in an external .mcc file, which this tool does not read", path, x, z)
	}
	payload := make([]byte, length-1)
	if _, err := f.ReadAt(payload, offset*4096+5); err != nil {
		return nil, fmt.Errorf("%s chunk %d,%d: %w", path, x, z, err)
	}
	var r io.Reader
	switch compression {
	case 1:
		r, err = gzip.NewReader(bytes.NewReader(payload))
	case 2:
		r, err = zlib.NewReader(bytes.NewReader(payload))
	case 3:
		r = bytes.NewReader(payload)
	default:
		return nil, fmt.Errorf("%s chunk %d,%d: compression %d not supported (the reference server writes 2, deflate)", path, x, z, compression)
	}
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%s chunk %d,%d: %w", path, x, z, err)
	}
	_, tag, err := nbt.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s chunk %d,%d: %w", path, x, z, err)
	}
	c, ok := tag.(nbt.Compound)
	if !ok {
		return nil, fmt.Errorf("%s chunk %d,%d: root is not a compound", path, x, z)
	}
	return c, nil
}

// readEntitiesChunk reads one chunk's saved entities out of the world's
// entities region files; a chunk without a record has none.
// vanilla: EntityStorage.readEntities
func readEntitiesChunk(worldDir string, x, z int32) ([]nbt.Compound, error) {
	root, err := readRegionChunk(worldDir, "entities", x, z)
	if err != nil || root == nil {
		return nil, err
	}
	if pos, ok := nbt.Get[nbt.IntArray](root, "Position"); ok && (len(pos) != 2 || pos[0] != x || pos[1] != z) {
		return nil, fmt.Errorf("entities of chunk %d,%d are saved at %v", x, z, pos)
	}
	list, _ := nbt.Get[nbt.List](root, "Entities")
	var out []nbt.Compound
	for _, item := range list.Items {
		if e, ok := item.(nbt.Compound); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// decodeVanillaChunk turns a saved chunk into chunkData, resolving every
// palette entry against this repository's registries. A section the save
// leaves out reads as air in plains.
func decodeVanillaChunk(d dimension, root nbt.Compound) (*chunkData, error) {
	xTag, _ := nbt.IntValue(root["xPos"])
	zTag, _ := nbt.IntValue(root["zPos"])
	cd := newChunkData(d, xTag, zTag)
	plains := biomeID("minecraft:plains")
	for i := range cd.Biomes {
		cd.Biomes[i] = plains
	}

	if status, _ := nbt.Get[nbt.String](root, "Status"); status != "minecraft:full" {
		return nil, fmt.Errorf("chunk %d,%d has status %q, not minecraft:full", cd.X, cd.Z, status)
	}
	sections, ok := nbt.Get[nbt.List](root, "sections")
	if !ok {
		return nil, fmt.Errorf("chunk %d,%d has no sections", cd.X, cd.Z)
	}
	for _, item := range sections.Items {
		sec, ok := item.(nbt.Compound)
		if !ok {
			continue
		}
		sy, _ := nbt.ByteValue(sec["Y"])
		base := int(sy) * 16
		if base < d.MinY || base >= d.MinY+d.Height {
			continue // the light-only sections above and below the world
		}
		if bs, ok := nbt.Get[nbt.Compound](sec, "block_states"); ok {
			states, err := blockPalette(bs)
			if err != nil {
				return nil, fmt.Errorf("chunk %d,%d section %d: %w", cd.X, cd.Z, sy, err)
			}
			data, _ := nbt.Get[nbt.LongArray](bs, "data")
			idx, err := unpackPalette(data, len(states), 4096, max(4, ceilLog2(len(states))))
			if err != nil {
				return nil, fmt.Errorf("chunk %d,%d section %d blocks: %w", cd.X, cd.Z, sy, err)
			}
			for i, p := range idx {
				// The section stores (y<<4|z)<<4|x, which is index's
				// order within one section.
				cd.Blocks[(base-d.MinY)*columnBlocks+i] = states[p]
			}
		}
		if bm, ok := nbt.Get[nbt.Compound](sec, "biomes"); ok {
			pal, _ := nbt.Get[nbt.List](bm, "palette")
			ids := make([]int32, len(pal.Items))
			for i, t := range pal.Items {
				name, _ := t.(nbt.String)
				ids[i] = biomeID(string(name))
			}
			data, _ := nbt.Get[nbt.LongArray](bm, "data")
			idx, err := unpackPalette(data, len(ids), 64, ceilLog2(len(ids)))
			if err != nil {
				return nil, fmt.Errorf("chunk %d,%d section %d biomes: %w", cd.X, cd.Z, sy, err)
			}
			cellBase := (base - d.MinY) / 4 * 16
			for i, p := range idx {
				cd.Biomes[cellBase+i] = ids[p]
			}
		}
	}
	cd.Structures = structureStarts(root, cd.X, cd.Z)
	list, _ := nbt.Get[nbt.List](root, "block_entities")
	var bes []nbt.Compound
	for _, item := range list.Items {
		if be, ok := item.(nbt.Compound); ok {
			bes = append(bes, be)
		}
	}
	cd.addBlockEntities(bes)
	return cd, nil
}

// structureStarts lists the structures whose start is in this chunk; the
// chunks they only reach into hold references to it instead.
func structureStarts(root nbt.Compound, cx, cz int32) []string {
	structs, _ := nbt.Get[nbt.Compound](root, "structures")
	starts, _ := nbt.Get[nbt.Compound](structs, "starts")
	var out []string
	for name, tag := range starts {
		start, ok := tag.(nbt.Compound)
		if !ok {
			continue
		}
		if id, _ := nbt.Get[nbt.String](start, "id"); id == "INVALID" {
			continue
		}
		x, _ := nbt.IntValue(start["ChunkX"])
		z, _ := nbt.IntValue(start["ChunkZ"])
		if x == cx && z == cz {
			out = append(out, name)
		}
	}
	return out
}

// blockPalette resolves a section's block palette to state IDs.
func blockPalette(bs nbt.Compound) ([]uint16, error) {
	pal, ok := nbt.Get[nbt.List](bs, "palette")
	if !ok || len(pal.Items) == 0 {
		return nil, fmt.Errorf("block_states has no palette")
	}
	out := make([]uint16, len(pal.Items))
	for i, t := range pal.Items {
		// vanilla: BlockState.CODEC encodes default states as registry names.
		if name, ok := t.(nbt.String); ok {
			id, found := registry.FindState(string(name), nil)
			if !found {
				id = unknownState
			}
			out[i] = id
			continue
		}
		entry, _ := t.(nbt.Compound)
		name, modern := nbt.Get[nbt.String](entry, "id")
		properties := "properties"
		if !modern {
			name, _ = nbt.Get[nbt.String](entry, "Name")
			properties = "Properties"
		}
		var props map[string]string
		if p, ok := nbt.Get[nbt.Compound](entry, properties); ok {
			props = make(map[string]string, len(p))
			for k, v := range p {
				s, _ := v.(nbt.String)
				props[k] = string(s)
			}
		}
		out[i] = resolveState(string(name), props)
	}
	return out, nil
}

// resolveState finds the state a vanilla palette entry names, insisting on
// every property matching: FindState falls back to the default state, which
// would hide a property our registry spells differently.
func resolveState(name string, props map[string]string) uint16 {
	id, ok := registry.FindState(name, props)
	if !ok {
		return unknownState
	}
	got, have, _ := registry.BlockStateName(id)
	if got != name || have != encodeProps(props) {
		return unknownState
	}
	return id
}

// encodeProps is the sorted key=value form BlockStateName returns.
func encodeProps(props map[string]string) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + props[k]
	}
	return strings.Join(parts, ",")
}

// unpackPalette reads count palette indices of the given width out of a
// packed long array. Entries never straddle two longs. A single-entry
// palette stores no data and reads as all zeros.
// vanilla: SimpleBitStorage, PalettedContainer.unpack
func unpackPalette(data []int64, paletteSize, count, bitsPerEntry int) ([]int, error) {
	out := make([]int, count)
	if paletteSize <= 1 {
		return out, nil
	}
	perLong := 64 / bitsPerEntry
	if want := (count + perLong - 1) / perLong; len(data) != want {
		return nil, fmt.Errorf("%d longs for %d entries of %d bits, want %d", len(data), count, bitsPerEntry, want)
	}
	mask := uint64(1)<<bitsPerEntry - 1
	for i := range out {
		word := uint64(data[i/perLong])
		v := int(word >> (uint(i%perLong) * uint(bitsPerEntry)) & mask)
		if v >= paletteSize {
			return nil, fmt.Errorf("entry %d is palette index %d of %d", i, v, paletteSize)
		}
		out[i] = v
	}
	return out, nil
}

// ceilLog2 is the bits needed to number n palette entries.
// vanilla: Mth.ceillog2
func ceilLog2(n int) int {
	if n <= 1 {
		return 0
	}
	return bits.Len(uint(n - 1))
}
