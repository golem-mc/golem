package world

import (
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// fillFromProto copies a generated ProtoChunk into the chunk's sections a
// section at a time, mapping each material through states; with keepAir, an
// air cell keeps its block. Light marks and section counts come
// out as one SetBlock per cell would leave them. It writes the sections in
// place, so it is only for a chunk not yet shared.
func (c *Chunk) fillFromProto(proto *terrain.ProtoChunk, states *[256]uint16, keepAir bool) {
	pMin := proto.MinY()
	minY, top := max(pMin, worldMinY), min(proto.MaxY()+1, worldMinY+worldHeight)
	// Above filled, every proto cell is air.
	filled := proto.TopFilledY()
	var cols [sectionSize * sectionSize][]terrain.Material // index z<<4 | x
	for z := range int32(sectionSize) {
		for x := range int32(sectionSize) {
			cols[z<<4|x] = proto.Column(x, z)
		}
	}
	var buf [blocksPerSection]uint16
	for sy := (minY - worldMinY) / sectionSize; sy*sectionSize+worldMinY < top; sy++ {
		s := &c.sections[sy]
		old := s.blocks.Load()
		y0 := sy*sectionSize + worldMinY
		if y0 > filled && (keepAir || (old == nil && s.fill == states[terrain.Air])) {
			continue
		}
		ly0, ly1 := max(minY-y0, 0), min(top-y0, sectionSize)
		if old != nil || ly0 > 0 || ly1 < sectionSize || keepAir {
			for i := range buf {
				buf[i] = s.fill
				if old != nil {
					buf[i] = old.get(i)
				}
			}
		}
		// The last light comparison, reused while runs of a material repeat.
		lastWas, lastState, lastSame := s.fill, s.fill, true
		for col := range int32(sectionSize * sectionSize) {
			x, z := col&15, col>>4
			seg := cols[col][y0+ly0-pMin : y0+ly1-pMin]
			for j, m := range seg {
				ly := ly0 + int32(j)
				i := int(ly<<8 | col)
				was := s.fill
				if old != nil || keepAir {
					was = buf[i]
				}
				state := was
				if !keepAir || m != terrain.Air {
					state = states[m]
				}
				buf[i] = state
				if state == was {
					continue
				}
				if was != lastWas || state != lastState {
					lastWas, lastState, lastSame = was, state, registry.SameLight(was, state)
				}
				if !lastSame {
					c.lightDirty[x] |= 1 << z
					c.markLightCell(x, y0+ly-worldMinY, z)
				}
			}
		}
		first, diff := buf[0], uint16(0)
		for _, state := range buf {
			diff |= state ^ first
		}
		if diff == 0 {
			s.setAll(first)
			continue
		}
		b := newBlockWords(buf[0])
		for i, state := range buf {
			b = b.put(i, state, false)
		}
		s.spawn.Store(nil)
		s.blocks.Store(b)
		s.recalcCounts()
	}
}
