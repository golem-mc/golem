package world

import (
	"cmp"
	"slices"
	"sync"
)

// SectionCell is one changed cell of a section, section-local 0-15 on each axis.
type SectionCell struct{ X, Y, Z uint8 }

// SectionChange is the cells of one chunk section that changed, in the order
// each first changed. Section is the section's chunk X, chunk Z and section Y.
type SectionChange struct {
	Section [3]int32
	Cells   []SectionCell
	// seen has one bit per cell of the section (y<<8|z<<4|x), so marking a
	// cell twice costs a bit test instead of a scan of Cells.
	// vanilla: ChunkHolder.changedBlocksPerSection (ShortSet)
	seen [64]uint64
}

// SectionChanges collects the cells changed since the last Take, per chunk
// section, so a tick's changes go out once as the cells' final states.
// vanilla: ChunkHolder.blockChanged
type SectionChanges struct {
	mu       sync.Mutex
	at       map[[3]int32]int // section -> index into sections
	sections []SectionChange
}

// Mark records that the cell at (x, y, z) changed. A cell marked twice is
// recorded once.
func (c *SectionChanges) Mark(x, y, z int32) {
	key := [3]int32{x >> 4, z >> 4, y >> 4}
	cell := SectionCell{uint8(x & 15), uint8(y & 15), uint8(z & 15)}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at == nil {
		c.at = make(map[[3]int32]int)
	}
	i, ok := c.at[key]
	if !ok {
		i = len(c.sections)
		c.at[key] = i
		c.sections = append(c.sections, SectionChange{Section: key})
	}
	s := &c.sections[i]
	bit := uint(cell.Y)<<8 | uint(cell.Z)<<4 | uint(cell.X)
	if s.seen[bit>>6]&(1<<(bit&63)) != 0 {
		return
	}
	s.seen[bit>>6] |= 1 << (bit & 63)
	s.Cells = append(s.Cells, cell)
}

// Take returns the recorded sections, lowest first within each chunk and the
// chunks in X, then Z order, and forgets them.
func (c *SectionChanges) Take() []SectionChange {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.sections
	c.sections = nil
	clear(c.at)
	slices.SortFunc(out, func(a, b SectionChange) int {
		return cmp.Or(cmp.Compare(a.Section[0], b.Section[0]),
			cmp.Compare(a.Section[1], b.Section[1]), cmp.Compare(a.Section[2], b.Section[2]))
	})
	return out
}

// MarkChanged records that the cell at (x, y, z) is to be told to the players
// at the next flush.
func (w *World) MarkChanged(x, y, z int32) { w.changed.Mark(x, y, z) }

// TakeChanged returns the cells marked since the last call, per section.
func (w *World) TakeChanged() []SectionChange { return w.changed.Take() }

// LightChange is the light sections of one chunk whose light changed: bit i of
// Sky or Block is light section i, the index into Chunk.skyLight / blockLight.
type LightChange struct {
	Pos        ChunkPos
	Sky, Block uint32
}

// LightChanges collects the light sections that changed since the last Take,
// one entry per chunk, in the order each chunk first changed.
// vanilla: ChunkHolder.sectionLightChanged
type LightChanges struct {
	mu    sync.Mutex
	order []LightChange
	index map[ChunkPos]int // into order
}

// Mark ORs the change's masks into the chunk's entry, adding it if absent.
func (c *LightChanges) Mark(ch LightChange) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i, ok := c.index[ch.Pos]; ok {
		c.order[i].Sky |= ch.Sky
		c.order[i].Block |= ch.Block
		return
	}
	if c.index == nil {
		c.index = make(map[ChunkPos]int)
	}
	c.index[ch.Pos] = len(c.order)
	c.order = append(c.order, ch)
}

// Take returns the recorded changes and forgets them.
func (c *LightChanges) Take() []LightChange {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.order
	c.order = nil
	clear(c.index)
	return out
}

// MarkLightChanged records that the sections' light is to be told to the
// players at the next flush.
func (w *World) MarkLightChanged(changes []LightChange) {
	for _, ch := range changes {
		w.lightChanged.Mark(ch)
	}
}

// TakeLightChanged returns the light changes marked since the last call.
func (w *World) TakeLightChanged() []LightChange { return w.lightChanged.Take() }
