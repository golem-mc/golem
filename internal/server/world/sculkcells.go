package world

import (
	"slices"

	"github.com/golem-mc/golem/registry"
)

type sculkCellKind uint8

const (
	vibrationListenerCell sculkCellKind = iota
	sculkCatalystCell
	sculkCellKinds
	noSculkCell = sculkCellKinds
)

// sectionSculkCells keeps each list in blockIndex order (y, then z, then x).
type sectionSculkCells [sculkCellKinds][]uint16

func sculkCellKindOf(block registry.Block) sculkCellKind {
	switch block {
	case registry.BlockSculkSensor, registry.BlockCalibratedSculkSensor, registry.BlockSculkShrieker:
		return vibrationListenerCell
	case registry.BlockSculkCatalyst:
		return sculkCatalystCell
	default:
		return noSculkCell
	}
}

func (s *section) sculkCells(kind sculkCellKind) []uint16 {
	if s.sculk == nil {
		return nil
	}
	return s.sculk[kind]
}

func (s *section) sculkList(kind sculkCellKind) *[]uint16 {
	if s.sculk == nil {
		s.sculk = new(sectionSculkCells)
	}
	return &s.sculk[kind]
}

func (s *section) updateSculkCell(index uint16, kind, was sculkCellKind) {
	if was != noSculkCell {
		cells := &s.sculk[was]
		at, _ := slices.BinarySearch(*cells, index)
		*cells = slices.Delete(*cells, at, at+1)
	}
	if kind != noSculkCell {
		cells := s.sculkList(kind)
		at, _ := slices.BinarySearch(*cells, index)
		*cells = slices.Insert(*cells, at, index)
	}
	if len(s.sculk[vibrationListenerCell])+len(s.sculk[sculkCatalystCell]) == 0 {
		s.sculk = nil
	}
}

func (s *section) indexDecodedSculkCells(blocks *blockWords, counts [sculkCellKinds]int) {
	for kind, count := range counts {
		if count > 0 {
			*s.sculkList(sculkCellKind(kind)) = make([]uint16, 0, count)
		}
	}
	for i := range blocksPerSection {
		if kind := sculkCellKindOf(registry.BlockOf(blocks.get(i))); kind != noSculkCell {
			s.sculk[kind] = append(s.sculk[kind], uint16(i))
		}
	}
}

// sculkCellsNear visits loaded sections in chunk x, chunk z, then section y
// order. Catalyst candidates are distance-checked by SculkCatalystDeath.
// vanilla: GameEventDispatcher.post
func (w *World) sculkCellsNear(cx, cy, cz, r int32, kind sculkCellKind, buf []blockKey) []blockKey {
	w.mu.RLock()
	defer w.mu.RUnlock()
	minSec := max((cy-r-worldMinY)>>4, 0)
	maxSec := min((cy+r-worldMinY)>>4, sectionCount-1)
	for chx := (cx - r) >> 4; chx <= (cx+r)>>4; chx++ {
		for chz := (cz - r) >> 4; chz <= (cz+r)>>4; chz++ {
			loaded, ok := w.loadedAt(ChunkPos{X: chx, Z: chz})
			if !ok {
				continue
			}
			for sec := minSec; sec <= maxSec; sec++ {
				cells := loaded.chunk.sections[sec].sculk
				if cells == nil {
					continue
				}
				baseY := worldMinY + sec*sectionSize
				for _, i := range cells[kind] {
					x, y, z := chx<<4+int32(i&15), baseY+int32(i>>8), chz<<4+int32(i>>4&15)
					if kind == vibrationListenerCell {
						dx, dy, dz := x-cx, y-cy, z-cz
						if dx*dx+dy*dy+dz*dz > r*r {
							continue
						}
					}
					buf = append(buf, blockKey{x, y, z})
				}
			}
		}
	}
	return buf
}
