package world

// Regenerate generates the chunk at pos afresh from the world's generator,
// ignoring what is loaded or saved, writes its biomes into the loaded chunk
// and returns the block states that differ, for the caller to apply with
// SetBlocks. biomes is true when a biome cell changed; ok is false, with
// nothing written, when the chunk is not loaded. Block entities and entities
// the generator would add are not regenerated.
func (w *World) Regenerate(pos ChunkPos) (changes []BlockChange, biomes, ok bool) {
	if !w.ChunkLoaded(pos) {
		return nil, false, false
	}
	fresh := w.fromFixture(pos)
	if fresh == nil {
		fresh = newChunk(pos.X, pos.Z)
		w.gen.Generate(fresh)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return nil, false, false
	}
	minY, maxY := w.dim.minY, w.dim.minY+w.dim.height
	for y := minY; y < maxY; y++ {
		for z := range int32(sectionSize) {
			for x := range int32(sectionSize) {
				if s := fresh.BlockAt(x, y, z); s != loaded.chunk.BlockAt(x, y, z) {
					changes = append(changes, BlockChange{X: pos.X<<4 | x, Y: y, Z: pos.Z<<4 | z, State: s})
				}
			}
		}
		if y&3 != 0 {
			continue
		}
		for z := int32(0); z < sectionSize; z += 4 {
			for x := int32(0); x < sectionSize; x += 4 {
				if b := fresh.BiomeAt(x, y, z); b != loaded.chunk.BiomeAt(x, y, z) {
					loaded.chunk.SetCellBiome(x, y, z, b)
					biomes = true
				}
			}
		}
	}
	if biomes {
		w.afterWrite(loaded)
	}
	return changes, biomes, true
}
