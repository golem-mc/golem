package world

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BiomeBox is a region of the world in block coordinates, inclusive on both
// ends and already snapped to 4x4x4 cell origins.
type BiomeBox struct {
	MinX, MinY, MinZ, MaxX, MaxY, MaxZ int32
}

// FillBiomes writes biome into every 4x4x4 cell of the box whose origin the
// box holds and whose current biome the filter accepts, and returns how many
// of those cells held another biome before. A nil filter accepts every cell.
// changed lists the chunks with a changed cell, which is what the caller
// re-sends. ok is false, with nothing written, when a spanned chunk is not
// loaded.
// vanilla: FillBiomeCommand.fill
func (w *World) FillBiomes(box BiomeBox, biome int32, filter func(current string) bool) (count int, changed []ChunkPos, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var chunks []ChunkPos
	for cz := ChunkOf(float64(box.MinZ)); cz <= ChunkOf(float64(box.MaxZ)); cz++ {
		for cx := ChunkOf(float64(box.MinX)); cx <= ChunkOf(float64(box.MaxX)); cx++ {
			pos := ChunkPos{X: cx, Z: cz}
			if _, loaded := w.chunks[pos]; !loaded {
				return 0, nil, false
			}
			chunks = append(chunks, pos)
		}
	}
	minY, maxY := max(box.MinY, w.dim.minY), min(box.MaxY, w.dim.minY+w.dim.height-1)
	for _, pos := range chunks {
		loaded, before := w.chunks[pos], count
		for x := max(box.MinX, pos.X*sectionSize); x <= min(box.MaxX, pos.X*sectionSize+sectionSize-1); x += 4 {
			for z := max(box.MinZ, pos.Z*sectionSize); z <= min(box.MaxZ, pos.Z*sectionSize+sectionSize-1); z += 4 {
				for y := minY; y <= maxY; y += 4 {
					current := loaded.chunk.BiomeAt(x&15, y, z&15)
					if filter == nil || filter(registry.Biome(current).Name()) {
						if current != biome {
							count++
						}
						loaded.chunk.SetCellBiome(x&15, y, z&15, biome)
					}
				}
			}
		}
		if count != before {
			w.afterWrite(loaded)
			changed = append(changed, pos)
		}
	}
	return count, changed, true
}

// BiomesPacket is the body of a Chunks Biomes packet carrying the biome
// containers of every listed chunk that is loaded.
// vanilla: ClientboundChunksBiomesPacket
func (w *World) BiomesPacket(chunks []ChunkPos) []byte {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var entries []ChunkPos
	for _, pos := range chunks {
		if _, ok := w.chunks[pos]; ok {
			entries = append(entries, pos)
		}
	}
	var b protocol.Buffer
	b.PutVarInt(int32(len(entries)))
	for _, pos := range entries {
		var data protocol.Buffer
		c := w.chunks[pos].chunk
		for i := w.firstSection(); i < w.firstSection()+w.wireSections(); i++ {
			c.sections[i].putBiomes(&data)
		}
		b.PutI64(int64(uint32(pos.X)) | int64(uint32(pos.Z))<<32)
		b.PutBytes(data.Bytes())
	}
	return b.Bytes()
}
