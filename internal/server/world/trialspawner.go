package world

import (
	"maps"

	"github.com/golem-mc/golem/nbt"
)

// TrialSpawnerAt is a carried trial spawner and the state its block is in.
type TrialSpawnerAt struct {
	X, Y, Z int32
	// State is the block's trial_spawner_state.
	State string
	// Ominous is the block's ominous property.
	Ominous bool
	// Tag is the carried compound. It is never modified in place: a change
	// replaces it (see ReplaceCarried).
	Tag nbt.Compound
}

// syncTickingChunk records whether the chunk carries a trial spawner or vault.
// The caller holds the write lock.
func (w *World) syncTickingChunk(pos ChunkPos, c *Chunk) {
	if c.tickingCarried <= 0 {
		delete(w.tickingChunks, pos)
		return
	}
	if w.tickingChunks == nil {
		w.tickingChunks = make(map[ChunkPos]struct{})
	}
	w.tickingChunks[pos] = struct{}{}
}

// TrialSpawners lists the trial spawners the loaded chunks carry whose block
// is still a trial spawner. It walks only the chunks that carry one.
func (w *World) TrialSpawners() []TrialSpawnerAt {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var out []TrialSpawnerAt
	for pos := range w.tickingChunks {
		c := w.chunks[pos].chunk
		for _, item := range c.carriedBlockEntities.Items {
			compound, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			if id, _ := nbt.Get[nbt.String](compound, "id"); id != "minecraft:trial_spawner" {
				continue
			}
			x, y, z, ok := carriedPosInTag(compound)
			if !ok {
				continue
			}
			state, props := trialSpawnerBlock(c.BlockAt(x&0xF, y, z&0xF))
			if props == nil {
				continue
			}
			out = append(out, TrialSpawnerAt{X: x, Y: y, Z: z, State: state, Ominous: props["ominous"] == "true", Tag: compound})
		}
	}
	return out
}

// ReplaceCarried swaps the carried compound at the position for tag, which
// keeps the id and position, and marks the chunk for saving. It reports false
// when the chunk is not loaded or carries none there.
func (w *World) ReplaceCarried(x, y, z int32, tag nbt.Compound) bool {
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return false
	}
	old, found := loaded.chunk.carriedTagAt(x, y, z)
	if !found {
		return false
	}
	entry := make(nbt.Compound, len(tag))
	maps.Copy(entry, tag)
	entry["id"], entry["x"], entry["y"], entry["z"] = old.Entries()["id"], nbt.Int(x), nbt.Int(y), nbt.Int(z)
	loaded.chunk.setCarriedBlockEntity(keepCarriedOrder(old, entry))
	w.syncTickingChunk(pos, loaded.chunk)
	loaded.markDirty()
	return true
}

// SetCarriedField sets one key of the carried compound at the position to
// value, read and written under one world lock so no other key is touched,
// and marks the chunk for saving. It reports false when the chunk is not
// loaded or carries none there.
func (w *World) SetCarriedField(x, y, z int32, key string, value nbt.Tag) bool {
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return false
	}
	old, found := loaded.chunk.carriedTagAt(x, y, z)
	if !found {
		return false
	}
	entry := maps.Clone(old.Entries())
	entry[key] = value
	loaded.chunk.setCarriedBlockEntity(keepCarriedOrder(old, entry))
	w.syncTickingChunk(pos, loaded.chunk)
	loaded.markDirty()
	return true
}
