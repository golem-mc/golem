package world

import "sync"

// BlockSnapshot remembers each touched section as it was on its first read.
// It is local to one read-only operation, with no world lock held between reads.
type BlockSnapshot struct {
	w        *World
	sections map[[3]int32]*snapshotSection
	ways     [64]snapshotWay
	free     []*snapshotSection
}

type snapshotWay struct {
	key     [3]int32
	section *snapshotSection
}

type snapshotSection struct {
	fill    uint16
	uniform bool
	blocks  blockWords
	palette []uint32
}

var blockSnapshotPool = sync.Pool{New: func() any {
	return &BlockSnapshot{sections: make(map[[3]int32]*snapshotSection)}
}}

// SnapshotBlocks starts an independent, lazy section snapshot without loading chunks.
// The caller must Release it after the operation's last read.
func (w *World) SnapshotBlocks() *BlockSnapshot {
	s := blockSnapshotPool.Get().(*BlockSnapshot)
	s.w = w
	return s
}

// Release forgets the captured sections and reuses their memory for another operation.
func (s *BlockSnapshot) Release() {
	keep := len(s.sections) <= 64
	s.w = nil
	for _, section := range s.sections {
		s.free = append(s.free, section)
	}
	clear(s.sections)
	s.ways = [64]snapshotWay{}
	if keep {
		blockSnapshotPool.Put(s)
	}
}

// BlockAt reads a cell from the snapshot, including air in unloaded sections.
func (s *BlockSnapshot) BlockAt(x, y, z int32) uint16 {
	key := [3]int32{x >> 4, y >> 4, z >> 4}
	way := &s.ways[(key[0]&3)|((key[1]&3)<<2)|((key[2]&3)<<4)]
	if way.section == nil || way.key != key {
		way.key, way.section = key, s.section(key)
	}
	if way.section.uniform {
		return way.section.fill
	}
	return way.section.blocks.get(int((y&15)<<8 | (z&15)<<4 | x&15))
}

func (s *BlockSnapshot) section(key [3]int32) *snapshotSection {
	if section := s.sections[key]; section != nil {
		return section
	}
	var section *snapshotSection
	if n := len(s.free); n > 0 {
		section, s.free = s.free[n-1], s.free[:n-1]
	} else {
		section = new(snapshotSection)
	}
	section.fill, section.uniform = airState, true
	s.w.mu.RLock()
	loaded, _ := s.w.loadedAt(ChunkPos{X: key[0], Z: key[2]})
	if ly := uint32((key[1] << 4) - worldMinY); loaded != nil && ly < worldHeight {
		source := &loaded.chunk.sections[ly>>4]
		section.fill = source.fill
		if words := source.blocks.Load(); words != nil {
			// Copy only the read layout; palette lookup slots belong to writers.
			b := &section.blocks
			b.words = append(b.words[:0], words.words...)
			b.palette = nil
			if words.palette != nil {
				section.palette = append(section.palette[:0], words.palette[:words.used]...)
				b.palette = section.palette
			}
			b.bits, b.perWord = words.bits, words.perWord
			b.indexScale, b.mask = words.indexScale, words.mask
			section.uniform = false
		}
	}
	s.w.mu.RUnlock()
	s.sections[key] = section
	return section
}
