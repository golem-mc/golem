package world

import (
	"maps"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// SignResolver resolves a sign's components against its level's command state.
type SignResolver interface {
	ResolveSignLine(x, y, z int32, line nbt.Tag) nbt.Tag
}

// SetSignResolver sets the component resolver before the world is shared.
func (w *World) SetSignResolver(resolver SignResolver) { w.signResolver = resolver }

// ResolveSignTag resolves both sides of an op-enabled sign before loading its
// data. No world or entity lock may be held: components can read either.
// vanilla: SignBlockEntity.loadAdditional, resolveLines
func (w *World) ResolveSignTag(x, y, z int32, tag nbt.Compound) nbt.Compound {
	if w.signResolver == nil || !nbt.BoolField(tag, "allow_op_features") {
		return tag
	}
	out := maps.Clone(tag)
	for _, side := range []string{"front_text", "back_text"} {
		data, ok := nbt.Get[nbt.Compound](tag, side)
		if !ok {
			continue
		}
		text := decodeSignText(data)
		// The mutable copy starts both lists from the raw messages.
		// vanilla: SignText.Mutable constructor
		text.filtered = nil
		for i, raw := range text.raw {
			if raw != nil {
				text.raw[i] = w.signResolver.ResolveSignLine(x, y, z, raw)
				text.Messages[i], _ = signLiteral(text.raw[i])
			}
		}
		out[side] = text.encode()
	}
	return out
}

// BookResolver resolves a written book's pages as a lectern at x, y, z.
type BookResolver interface {
	ResolveLecternBook(x, y, z int32, book protocol.ItemStack) protocol.ItemStack
}

// SetBookResolver sets the book resolver before the world is shared.
func (w *World) SetBookResolver(resolver BookResolver) { w.bookResolver = resolver }

// resolveLecternBook resolves the book a lectern just loaded. No world or
// entity lock may be held: components can read either.
// vanilla: LecternBlockEntity.loadAdditional, resolveBook
func (w *World) resolveLecternBook(x, y, z int32) {
	be := w.BlockEntity(x, y, z)
	if be == nil || be.Kind != KindLectern {
		return
	}
	var book protocol.ItemStack
	be.Use(func(b *BlockEntity) { book = b.Slots[0] })
	if _, ok := book.WrittenBookOf(); !ok {
		return
	}
	resolved := w.bookResolver.ResolveLecternBook(x, y, z, book)
	be.Use(func(b *BlockEntity) { b.Slots[0] = resolved })
	w.TouchEntity(be)
}

// queueLecternBooks notes each lectern of a chunk entering memory whose book
// is unresolved, for ResolvePendingLecternBooks. The caller holds mu.
func (w *World) queueLecternBooks(c *Chunk) {
	if w.bookResolver == nil {
		return
	}
	for _, be := range c.blockEntities() {
		if be.Kind != KindLectern {
			continue
		}
		if book, ok := be.Slots[0].WrittenBookOf(); ok && !book.Resolved {
			w.pendingBooks = append(w.pendingBooks, posKey{be.X, be.Y, be.Z})
			w.pendingBookCount.Add(1)
		}
	}
}

// ResolvePendingLecternBooks resolves the books of lecterns loaded from saved
// chunks. It runs on the tick, where commands may read the world.
// vanilla: LecternBlockEntity.loadAdditional
func (w *World) ResolvePendingLecternBooks() {
	if w.pendingBookCount.Load() == 0 {
		return
	}
	w.mu.Lock()
	pending := w.pendingBooks
	w.pendingBooks = nil
	w.pendingBookCount.Store(0)
	w.mu.Unlock()
	for _, p := range pending {
		w.resolveLecternBook(p[0], p[1], p[2])
	}
}
