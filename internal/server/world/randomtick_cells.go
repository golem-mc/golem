package world

// cellMask has one bit per cell of a section, in the x-fast block index order.
// The random-tick mask has bit i set when randomlyTicking(s.at(i)); a section
// builds it the first time a random tick selects it with some but not all
// cells ticking, so a die landing on a non-ticking cell skips decoding the
// block.
type cellMask [blocksPerSection / 64]uint64

// has reports whether cell i is set. The caller holds the world lock.
func (m *cellMask) has(i int) bool {
	return m[i>>6]>>(i&63)&1 != 0
}

// flip toggles cell i. The caller holds the world write lock.
func (m *cellMask) flip(i int) {
	m[i>>6] ^= 1 << (i & 63)
}

// set sets cell i. The caller holds the world write lock.
func (m *cellMask) set(i int) {
	m[i>>6] |= 1 << (i & 63)
}

// tickCells returns the section's mask for a pass reading under the world
// read lock, building and publishing it if absent. It returns nil for a
// section with no or only random-ticking cells, which the counts decide.
func (s *section) tickCells(b *blockWords) *cellMask {
	if s.ticking <= 0 || s.ticking >= blocksPerSection || b == nil {
		return nil
	}
	if m := s.tickMask.Load(); m != nil {
		return m
	}
	m := new(cellMask)
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
				m.set(i)
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
