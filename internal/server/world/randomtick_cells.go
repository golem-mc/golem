package world

// randomTickMask has bit i set when randomlyTicking(s.at(i)), in the x-fast
// block index order. A section builds it the first time a random tick selects
// it with some but not all cells ticking, so a die landing on a non-ticking
// cell skips decoding the block.
type randomTickMask [blocksPerSection / 64]uint64

// ticks reports whether cell i is random-ticking. The caller holds the world
// lock.
func (m *randomTickMask) ticks(i int) bool {
	return m[i>>6]>>(i&63)&1 != 0
}

// flip toggles cell i. The caller holds the world write lock.
func (m *randomTickMask) flip(i int) {
	m[i>>6] ^= 1 << (i & 63)
}

// tickCells returns the section's mask for a pass reading under the world
// read lock, building and publishing it if absent. It returns nil for a
// section with no or only random-ticking cells, which the counts decide.
func (s *section) tickCells(b *blockWords) *randomTickMask {
	if s.ticking <= 0 || s.ticking >= blocksPerSection || b == nil {
		return nil
	}
	if m := s.tickMask.Load(); m != nil {
		return m
	}
	m := new(randomTickMask)
	// Entries never cross words and the last word is truncated, as in unpack.
	var ticks [256]bool
	for v, state := range b.palette {
		ticks[v] = randomlyTicking(uint16(state))
	}
	i := 0
	for _, word := range b.words {
		for j := 0; j < b.perWord && i < blocksPerSection; j++ {
			value := word & b.mask
			var on bool
			if b.palette != nil {
				on = ticks[value]
			} else {
				on = randomlyTicking(uint16(value))
			}
			if on {
				m[i>>6] |= 1 << (i & 63)
			}
			word >>= b.bits
			i++
		}
	}
	if !s.tickMask.CompareAndSwap(nil, m) {
		m = s.tickMask.Load()
	}
	return m
}
