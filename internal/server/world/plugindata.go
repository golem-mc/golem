package world

import "github.com/golem-mc/golem/nbt"

// LoadedChunk returns a loaded chunk without acquiring a hold or generating it.
// The pointer identifies this load; use ChunkInfo to check it is still current.
func (w *World) LoadedChunk(pos ChunkPos) *Chunk {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if l, ok := w.loadedAt(pos); ok {
		return l.chunk
	}
	return nil
}

// ChunkInfo reads a live chunk's age and modelled block entities, false after unload.
func (w *World) ChunkInfo(c *Chunk) (int64, []*BlockEntity, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if l, ok := w.loadedAt(ChunkPos{X: c.x, Z: c.z}); ok && l.chunk == c {
		return c.inhabitedTime, c.blockEntities(), true
	}
	return 0, nil, false
}

// ReadChunkPluginData visits one live chunk value under the world lock, nil if absent.
// The callback must neither retain nor modify the tag.
func (w *World) ReadChunkPluginData(c *Chunk, key string, read func(nbt.Tag)) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if l, ok := w.loadedAt(ChunkPos{X: c.x, Z: c.z}); ok && l.chunk == c {
		read(c.pluginData[key])
	}
}

// ChunkPluginDataKeys lists accepted keys of a live chunk without copying values.
// The predicate runs under the world lock and must neither retain nor modify tags.
func (w *World) ChunkPluginDataKeys(c *Chunk, keep func(nbt.Tag) bool) []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if l, ok := w.loadedAt(ChunkPos{X: c.x, Z: c.z}); ok && l.chunk == c {
		return pluginDataKeys(c.pluginData, keep)
	}
	return nil
}

// SetChunkPluginData changes one private value; nil deletes it. Stale handles do nothing.
func (w *World) SetChunkPluginData(c *Chunk, key string, value nbt.Tag) {
	w.mu.Lock()
	defer w.mu.Unlock()
	l, ok := w.loadedAt(ChunkPos{X: c.x, Z: c.z})
	if !ok || l.chunk != c {
		return
	}
	setPluginData(&c.pluginData, key, value)
	l.markDirty()
}

// IsLive reports whether this block entity has been removed or unloaded.
func (b *BlockEntity) IsLive() bool { return !b.removed.Load() }

// ReadPluginData visits one live entity value under its lock, nil if absent.
// The callback must neither retain nor modify the tag.
func (b *BlockEntity) ReadPluginData(key string, read func(nbt.Tag)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.removed.Load() {
		read(b.pluginData[key])
	}
}

// PluginDataKeys lists accepted keys without waking the ticker or copying values.
// The predicate runs under the entity lock and must neither retain nor modify tags.
func (b *BlockEntity) PluginDataKeys(keep func(nbt.Tag) bool) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.removed.Load() {
		return pluginDataKeys(b.pluginData, keep)
	}
	return nil
}

func pluginDataKeys(data nbt.Compound, keep func(nbt.Tag) bool) []string {
	var keys []string
	for key, tag := range data {
		if keep(tag) {
			keys = append(keys, key)
		}
	}
	return keys
}

// SetBlockEntityPluginData changes a live entity's private value without a comparator wake or packet.
func (w *World) SetBlockEntityPluginData(b *BlockEntity, key string, value nbt.Tag) {
	// Keep the entity-before-world order used by container writes and chunk saves.
	b.mu.Lock()
	defer b.mu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	l, ok := w.loadedAt(ChunkPos{X: b.X >> 4, Z: b.Z >> 4})
	if !ok || b.removed.Load() || l.chunk.blockEntity(b.X, b.Y, b.Z) != b {
		return
	}
	setPluginData(&b.pluginData, key, value)
	l.markDirty()
}

func setPluginData(data *nbt.Compound, key string, value nbt.Tag) {
	if value == nil {
		delete(*data, key)
		return
	}
	if *data == nil {
		*data = nbt.Compound{}
	}
	(*data)[key] = nbt.Copy(value)
}
