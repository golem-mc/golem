package world

import (
	"maps"

	"github.com/golem-mc/golem/nbt"
)

// CarriedBlockEntity identifies one loaded, unmodelled block entity.
type CarriedBlockEntity struct {
	w     *World
	chunk *Chunk
	pos   [3]int32
	kind  string
}

// Pos returns the block position.
func (b *CarriedBlockEntity) Pos() [3]int32 { return b.pos }

// Kind returns the saved block entity type.
func (b *CarriedBlockEntity) Kind() string { return b.kind }

// CarriedBlockEntity returns a live handle without loading a chunk.
func (w *World) CarriedBlockEntity(x, y, z int32) *CarriedBlockEntity {
	w.mu.Lock()
	defer w.mu.Unlock()
	l, ok := w.loadedAt(ChunkPos{X: x >> 4, Z: z >> 4})
	if !ok {
		return nil
	}
	tag, ok := l.chunk.carriedBlockEntityAt(x, y, z)
	if !ok {
		return nil
	}
	return w.carriedHandle(l.chunk, tag)
}

// CarriedBlockEntities lists a live chunk's unmodelled block entities.
func (w *World) CarriedBlockEntities(c *Chunk) []*CarriedBlockEntity {
	w.mu.Lock()
	defer w.mu.Unlock()
	l, ok := w.loadedAt(ChunkPos{X: c.x, Z: c.z})
	if !ok || l.chunk != c {
		return nil
	}
	out := make([]*CarriedBlockEntity, 0, len(c.carriedBlockEntities.Items))
	for _, item := range c.carriedBlockEntities.Items {
		if tag, ok := nbt.CompoundView(item); ok {
			if b := w.carriedHandle(c, tag); b != nil {
				out = append(out, b)
			}
		}
	}
	return out
}

func (w *World) carriedHandle(c *Chunk, tag nbt.Compound) *CarriedBlockEntity {
	x, y, z, valid := carriedPosInTag(tag)
	kind, _ := nbt.Get[nbt.String](tag, "id")
	if !valid || BlockEntityTypeID(string(kind)) < 0 {
		return nil
	}
	key := entityKey(x, y, z)
	if b := c.carriedHandles[key]; b != nil {
		return b
	}
	if c.carriedHandles == nil {
		c.carriedHandles = make(map[int64]*CarriedBlockEntity)
	}
	b := &CarriedBlockEntity{w: w, chunk: c, pos: [3]int32{x, y, z}, kind: string(kind)}
	c.carriedHandles[key] = b
	return b
}

// loaded checks load and replacement identity with the world lock held.
func (b *CarriedBlockEntity) loaded() (*loadedChunk, bool) {
	x, y, z := b.pos[0], b.pos[1], b.pos[2]
	l, ok := b.w.loadedAt(ChunkPos{X: x >> 4, Z: z >> 4})
	return l, ok && l.chunk == b.chunk && l.chunk.carriedHandles[entityKey(x, y, z)] == b
}

// Read visits the current tag under the world lock, false after removal or unload.
// The callback must neither retain nor modify tags; nil only checks validity.
func (b *CarriedBlockEntity) Read(read func(nbt.Compound)) bool {
	b.w.mu.RLock()
	defer b.w.mu.RUnlock()
	if _, ok := b.loaded(); !ok {
		return false
	}
	tag, ok := b.chunk.carriedBlockEntityAt(b.pos[0], b.pos[1], b.pos[2])
	if ok && read != nil {
		read(tag)
	}
	return ok
}

// Edit replaces fields and marks the chunk dirty without a gameplay wake.
// The callback holds the world lock and must replace, never mutate, nested tags.
func (b *CarriedBlockEntity) Edit(edit func(nbt.Compound)) bool {
	b.w.mu.Lock()
	defer b.w.mu.Unlock()
	l, ok := b.loaded()
	if !ok {
		return false
	}
	for i, item := range b.chunk.carriedBlockEntities.Items {
		oldTag, ok := item.(nbt.CompoundTag)
		if !ok {
			continue
		}
		old := oldTag.Entries()
		x, y, z := carriedPos(old)
		if [3]int32{x, y, z} != b.pos {
			continue
		}
		entry := maps.Clone(old)
		edit(entry)
		// Replace in place: private data must not reorder carried tickers.
		b.chunk.carriedBlockEntities.Items[i] = keepCarriedOrder(oldTag, entry)
		l.markDirty()
		return true
	}
	return false
}

// ReadPluginData visits one private value without copying unrelated values.
func (b *CarriedBlockEntity) ReadPluginData(key string, read func(nbt.Tag)) {
	b.Read(func(tag nbt.Compound) {
		data, _ := nbt.Get[nbt.Compound](tag, "plugins")
		read(data[key])
	})
}

// PluginDataKeys lists accepted private keys without a gameplay wake.
func (b *CarriedBlockEntity) PluginDataKeys(keep func(nbt.Tag) bool) (keys []string) {
	b.Read(func(tag nbt.Compound) {
		data, _ := nbt.Get[nbt.Compound](tag, "plugins")
		keys = pluginDataKeys(data, keep)
	})
	return keys
}

// SetPluginData changes one private value; nil deletes it and stale handles do nothing.
func (b *CarriedBlockEntity) SetPluginData(key string, value nbt.Tag) {
	b.Edit(func(tag nbt.Compound) {
		data, _ := nbt.Get[nbt.Compound](tag, "plugins")
		data = maps.Clone(data)
		setPluginData(&data, key, value)
		if len(data) == 0 {
			delete(tag, "plugins")
		} else {
			tag["plugins"] = data
		}
	})
}
