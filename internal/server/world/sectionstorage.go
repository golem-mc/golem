package world

import (
	"slices"
	"sync/atomic"
	"unsafe"
)

// blockWords holds palette indices at 4-8 bits, then global IDs at directBits.
// Entries never cross words (vanilla: SimpleBitStorage). Only the locked writer
// changes words, slots or used; readers load words/slots atomically. A resize
// publishes a replacement layout through section.blocks.
type blockWords struct {
	words   []uint64
	palette []uint32
	// slots is a writer-only hash table above four bits (vanilla: Strategy).
	// Entries pack state<<16 | (slot+1); zero is empty, at most half occupied.
	slots      []uint32
	bits       int
	perWord    int
	indexScale uint64
	mask       uint64
	used       int
}

func makeBlockWords(width int) *blockWords {
	perWord := 64 / width
	b := &blockWords{
		words: make([]uint64, (blocksPerSection+perWord-1)/perWord),
		bits:  width, perWord: perWord, mask: 1<<width - 1,
		// For indices below 4096 this reciprocal gives exact division by perWord.
		indexScale: ((1 << 32) + uint64(perWord) - 1) / uint64(perWord),
	}
	if width <= maxIndirectBits {
		b.palette = make([]uint32, 1<<width)
		if width > minIndirectBits {
			b.slots = make([]uint32, 2<<width)
		}
	}
	return b
}

// newBlockWords starts with every index naming state in a four-bit palette.
func newBlockWords(state uint16) *blockWords {
	b := makeBlockWords(minIndirectBits)
	b.palette[0], b.used = uint32(state), 1
	return b
}

func (b *blockWords) offset(i int) (int, int) {
	if b.bits == 4 {
		return i >> 4, (i & 15) << 2
	}
	word := int(uint64(i) * b.indexScale >> 32)
	return word, (i - word*b.perWord) * b.bits
}

// get requires ownership of the chunk or the world lock.
func (b *blockWords) get(i int) uint16 {
	word, shift := b.offset(i)
	value := b.words[word] >> shift & b.mask
	if b.palette != nil {
		return uint16(b.palette[value])
	}
	return uint16(value)
}

// load reads a shared chunk without the world lock.
func (b *blockWords) load(i int) uint16 {
	word, shift := b.offset(i)
	value := atomic.LoadUint64(&b.words[word]) >> shift & b.mask
	if b.palette != nil {
		return uint16(atomic.LoadUint32(&b.palette[value]))
	}
	return uint16(value)
}

// paletteSlot returns the state's slot, or used if it has not been inserted.
func (b *blockWords) paletteSlot(state uint16) int {
	if b.slots != nil {
		for i := b.slotHash(state); ; i = (i + 1) & (len(b.slots) - 1) {
			entry := b.slots[i]
			if entry == 0 {
				return b.used
			}
			if uint16(entry>>16) == state {
				return int(uint16(entry)) - 1
			}
		}
	}
	for slot := range b.used {
		if uint16(b.palette[slot]) == state {
			return slot
		}
	}
	return b.used
}

func (b *blockWords) slotHash(state uint16) int {
	return int(uint32(state) * 0x9e3779b9 >> (31 - b.bits))
}

func (b *blockWords) indexSlot(state uint16, slot int) {
	if b.slots == nil {
		return
	}
	i := b.slotHash(state)
	for b.slots[i] != 0 {
		i = (i + 1) & (len(b.slots) - 1)
	}
	b.slots[i] = uint32(state)<<16 | uint32(slot+1)
}

// put returns the replacement storage on a resize. Palette entries are never
// reused: a reader may still hold an older word naming any previous slot.
// vanilla: PalettedContainer.onResize
func (b *blockWords) put(i int, state uint16, shared bool) *blockWords {
	value := uint64(state)
	if b.palette != nil {
		slot := b.paletteSlot(state)
		if slot == len(b.palette) {
			width := b.bits + 1
			if width > maxIndirectBits {
				width = directBits
			}
			grown := makeBlockWords(width)
			if grown.palette != nil {
				copy(grown.palette, b.palette)
				grown.used = b.used
				for slot := range grown.used {
					grown.indexSlot(uint16(grown.palette[slot]), slot)
				}
			}
			for j := range blocksPerSection {
				old := uint64(b.get(j))
				if grown.palette != nil {
					word, shift := b.offset(j)
					old = b.words[word] >> shift & b.mask
				}
				word, shift := grown.offset(j)
				grown.words[word] |= old << shift
			}
			return grown.put(i, state, false)
		}
		if slot == b.used {
			if shared {
				atomic.StoreUint32(&b.palette[slot], uint32(state))
			} else {
				b.palette[slot] = uint32(state)
			}
			b.indexSlot(state, slot)
			b.used++
		}
		value = uint64(slot)
	}
	word, shift := b.offset(i)
	next := b.words[word]&^(b.mask<<shift) | value<<shift
	if shared {
		atomic.StoreUint64(&b.words[word], next)
	} else {
		b.words[word] = next
	}
	return b
}

func (b *blockWords) clone() *blockWords {
	return &blockWords{words: slices.Clone(b.words), palette: slices.Clone(b.palette),
		slots: slices.Clone(b.slots), bits: b.bits, perWord: b.perWord,
		indexScale: b.indexScale, mask: b.mask, used: b.used}
}

func (b *blockWords) bytes() int {
	return int(unsafe.Sizeof(*b)) + len(b.words)*8 + len(b.palette)*4 + len(b.slots)*4
}

// unpack expands entries in index order for scans holding the world lock.
func (b *blockWords) unpack(states *[blocksPerSection]uint16) {
	i := 0
	for _, word := range b.words {
		for j := 0; j < b.perWord && i < len(states); j++ {
			value := word & b.mask
			if b.palette != nil {
				value = uint64(b.palette[value])
			}
			states[i] = uint16(value)
			word >>= b.bits
			i++
		}
	}
}
