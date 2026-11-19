package world

// SectionMask marks sections by their offset from the chunk's minimum Y.
type SectionMask uint64

// Has reports whether the section containing y is marked.
func (m SectionMask) Has(y int32) bool {
	local := uint32(y - worldMinY)
	return local < worldHeight && m&(1<<(local>>4)) != 0
}

// PortalSections returns the loaded chunk's sections containing nether portals.
func (w *World) PortalSections(pos ChunkPos) SectionMask {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var mask SectionMask
	if loaded, ok := w.loadedAt(pos); ok {
		for i := range loaded.chunk.sections {
			if loaded.chunk.sections[i].portals != 0 {
				mask |= 1 << i
			}
		}
	}
	return mask
}

// POISections returns the loaded chunk's sections containing POI-type states.
func (w *World) POISections(pos ChunkPos) SectionMask {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var mask SectionMask
	if loaded, ok := w.loadedAt(pos); ok {
		for i := range loaded.chunk.sections {
			if loaded.chunk.sections[i].pois != 0 {
				mask |= 1 << i
			}
		}
	}
	return mask
}
