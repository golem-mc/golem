package world

// CollisionCell is a cell whose state may collide with an entity.
type CollisionCell struct {
	X, Y, Z int32
	State   uint16
	// Plain marks cell-contained shapes with no position offset or body context.
	Plain bool
}

type collisionSection struct {
	loZ, hiZ int32
	fill     uint16
	words    *blockWords
}

type collisionBand struct {
	loY, hiY   int32
	start, end int
}

// CollisionCellsIn appends possible collisions to buf[:0], in x/y/z order.
// Empty sections are skipped without reading their states. Body-dependent
// shapes remain candidates; the caller resolves them for each clip.
func (w *World) CollisionCellsIn(x0, y0, z0, x1, y1, z1 int32, buf []CollisionCell) []CollisionCell {
	buf = buf[:0]
	y0, y1 = max(y0, worldMinY), min(y1, worldMinY+worldHeight-1)
	if x1 < x0 || y1 < y0 || z1 < z0 {
		return buf
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	// Flat ground commonly contributes one section; avoid building row descriptors.
	if x0>>4 == x1>>4 && z0>>4 == z1>>4 {
		loaded, _ := w.loadedAt(ChunkPos{X: x0 >> 4, Z: z0 >> 4})
		if loaded == nil {
			return buf
		}
		var only *section
		var onlyY int32
		multiple := false
		for sy := (y0 - worldMinY) >> 4; sy <= (y1-worldMinY)>>4; sy++ {
			s := &loaded.chunk.sections[sy]
			if s.colliding == 0 {
				continue
			}
			if only != nil {
				multiple = true
				break
			}
			only, onlyY = s, worldMinY+sy*16
		}
		if !multiple {
			if only != nil {
				s := collisionSection{loZ: z0, hiZ: z1, fill: only.fill, words: only.blocks.Load()}
				for dx := range int(x1 - x0 + 1) {
					buf = appendCollisionColumn(buf, x0+int32(dx), max(y0, onlyY), min(y1, onlyY+15), s)
				}
			}
			return buf
		}
	}
	return w.collisionCellsAcrossSections(x0, y0, z0, x1, y1, z1, buf)
}

// collisionCellsAcrossSections interleaves section rows with w.mu held for reading.
func (w *World) collisionCellsAcrossSections(x0, y0, z0, x1, y1, z1 int32, buf []CollisionCell) []CollisionCell {
	for cx := x0 >> 4; cx <= x1>>4; cx++ {
		loX, hiX := max(x0, cx<<4), min(x1, cx<<4|15)
		// Most swept boxes touch at most two vertical bands and four sections.
		var smallSections [4]collisionSection
		var smallBands [2]collisionBand
		sections, bands := smallSections[:0], smallBands[:0]
		for sy := (y0 - worldMinY) >> 4; sy <= (y1-worldMinY)>>4; sy++ {
			start := len(sections)
			for cz := z0 >> 4; cz <= z1>>4; cz++ {
				loaded, _ := w.loadedAt(ChunkPos{X: cx, Z: cz})
				if loaded == nil {
					continue
				}
				s := &loaded.chunk.sections[sy]
				if s.colliding != 0 {
					sections = append(sections, collisionSection{
						loZ: max(z0, cz<<4), hiZ: min(z1, cz<<4|15),
						fill: s.fill, words: s.blocks.Load(),
					})
				}
			}
			if len(sections) != start {
				bands = append(bands, collisionBand{
					loY: max(y0, worldMinY+sy*16), hiY: min(y1, worldMinY+sy*16+15),
					start: start, end: len(sections),
				})
			}
		}
		// Resolve each section once, then interleave its rows in BlocksIn's
		// exact x/y/z order: obstacleTop depends on the order of the stoppers.
		// Count offsets so a range ending at MaxInt32 cannot wrap.
		for dx := range int(hiX - loX + 1) {
			x := loX + int32(dx)
			for _, band := range bands {
				for y := band.loY; y <= band.hiY; y++ {
					for _, s := range sections[band.start:band.end] {
						buf = appendCollisionColumn(buf, x, y, y, s)
					}
				}
			}
		}
	}
	return buf
}

// appendCollisionColumn reads a column of a section known to have collisions.
func appendCollisionColumn(buf []CollisionCell, x, y0, y1 int32, s collisionSection) []CollisionCell {
	if s.words == nil {
		// A uniform section with a nonzero count has no states to rule out.
		plain := collisionPlain[s.fill]
		for y := y0; y <= y1; y++ {
			for dz := range int(s.hiZ - s.loZ + 1) {
				buf = append(buf, CollisionCell{x, y, s.loZ + int32(dz), s.fill, plain})
			}
		}
		return buf
	}
	for y := y0; y <= y1; y++ {
		cell := int((y&15)<<8 | (s.loZ&15)<<4 | x&15)
		for dz := range int(s.hiZ - s.loZ + 1) {
			state := s.words.get(cell + dz*16)
			if collisionPossible[state] {
				buf = append(buf, CollisionCell{x, y, s.loZ + int32(dz), state, collisionPlain[state]})
			}
		}
	}
	return buf
}
