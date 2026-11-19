package world

import (
	"errors"
	"log/slog"
	"maps"
	"sync"

	"github.com/golem-mc/golem/nbt"
)

// entityWrites keeps an older read of a chunk's entities from landing after a
// newer one: the auto-save writes its snapshot after the tick has moved on, and
// an unload in between writes the chunk's current entities.
type entityWrites struct {
	mu      sync.Mutex // guards the fields; never held across a file write
	seq     uint64
	latest  map[ChunkPos]uint64
	writing map[ChunkPos]*chunkWrite
	pending map[ChunkPos]entitySnapshot
}

// entitySnapshot owns its tags; reads copy them before handing them to the herd.
type entitySnapshot struct {
	stamp    uint64
	entities []nbt.Tag
}

// chunkWrite serialises the writes of one chunk's file; users counts the
// writers holding or waiting for it, so the entry can be dropped.
type chunkWrite struct {
	mu    sync.Mutex
	users int
}

// StampChunkEntities marks the read of pos's entities now being made as the
// newest, and returns the stamp to hand to SaveChunkEntitiesStamped.
// Callers must submit every stamped snapshot: the herd queues an unload at once,
// and snapshotMobs hands every snapshot to writeMobs, including in the auto-save
// flush. A stamp alone leaves the last submitted snapshot readable until its
// replacement arrives; dropping it here could lose an unload whose write failed.
func (w *World) StampChunkEntities(pos ChunkPos) uint64 {
	e := &w.entityWrites
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.latest == nil {
		e.latest = make(map[ChunkPos]uint64)
	}
	e.seq++
	e.latest[pos] = e.seq
	return e.seq
}

// ChunkEntitiesStampCurrent reports whether no newer read of pos's entities
// than stamp has been taken, and so a failed write of stamp's is the chunk's
// latest word.
func (w *World) ChunkEntitiesStampCurrent(pos ChunkPos, stamp uint64) bool {
	e := &w.entityWrites
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.latest[pos] == stamp
}

// SaveChunkEntitiesStamped stores entities as pos's entity file unless a newer
// stamp has been taken for pos. Only a write of the same chunk waits for
// another, so a caller holding a lock the tick takes waits for at most that one
// write.
func (w *World) SaveChunkEntitiesStamped(pos ChunkPos, stamp uint64, entities []nbt.Tag) error {
	e := &w.entityWrites
	e.mu.Lock()
	if e.writing == nil {
		e.writing = make(map[ChunkPos]*chunkWrite)
	}
	cw := e.writing[pos]
	if cw == nil {
		cw = &chunkWrite{}
		e.writing[pos] = cw
	}
	cw.users++
	e.mu.Unlock()

	// Held through the check and the write: a newer write of this chunk then
	// either finished before the check or starts after the write.
	cw.mu.Lock()
	defer func() {
		cw.mu.Unlock()
		e.mu.Lock()
		if cw.users--; cw.users == 0 {
			delete(e.writing, pos)
		}
		e.mu.Unlock()
	}()
	e.mu.Lock()
	if e.latest[pos] != stamp {
		e.mu.Unlock()
		return nil
	}
	// Publish synchronous saves too: a newer auto-save must replace an older
	// pending unload, including when its disk write fails.
	if pending, ok := e.pending[pos]; !ok || pending.stamp != stamp {
		w.pendingEntitySnapshot(pos, stamp, entities)
	}
	e.mu.Unlock()
	if gate := w.entityWriteGate.Load(); gate != nil {
		(*gate)()
	}
	var err error
	if fault := w.entityWriteFault.Load(); fault != nil {
		err = *fault
	} else {
		err = w.SaveChunkEntities(pos, entities)
	}
	e.mu.Lock()
	if err == nil && e.latest[pos] == stamp {
		delete(e.latest, pos)
		delete(e.pending, pos)
	}
	e.mu.Unlock()
	return err
}

// SetEntityWriteFault makes SaveChunkEntitiesStamped fail with err, writing
// nothing, until it is called with nil. It is a test hook.
func (w *World) SetEntityWriteFault(err error) {
	if err == nil {
		w.entityWriteFault.Store(nil)
		return
	}
	w.entityWriteFault.Store(&err)
}

// SetEntityWriteGate parks stamped entity writes before disk I/O; nil removes the test hook.
func (w *World) SetEntityWriteGate(gate func()) {
	if gate == nil {
		w.entityWriteGate.Store(nil)
	} else {
		w.entityWriteGate.Store(&gate)
	}
}

// QueueChunkEntitiesStamped publishes an immutable snapshot before scheduling its
// disk write, so a reload can read it without waiting for the I/O executor.
// vanilla: EntityStorage.storeEntities, IOWorker.store/loadAsync
func (w *World) QueueChunkEntitiesStamped(pos ChunkPos, stamp uint64, entities []nbt.Tag) {
	e := &w.entityWrites
	e.mu.Lock()
	if e.latest[pos] != stamp {
		e.mu.Unlock()
		return
	}
	snapshot := w.pendingEntitySnapshot(pos, stamp, entities)
	e.mu.Unlock()
	w.exec.submitIOTracked(nil, func() {
		if err := w.SaveChunkEntitiesStamped(pos, stamp, snapshot.entities); err != nil {
			slog.Error("could not save the entities of a chunk", "x", pos.X, "z", pos.Z, "error", err)
		}
	})
}

// pendingEntitySnapshot is called with entityWrites.mu held.
func (w *World) pendingEntitySnapshot(pos ChunkPos, stamp uint64, entities []nbt.Tag) entitySnapshot {
	snapshot := entitySnapshot{stamp, copyEntities(entities)}
	if w.entityWrites.pending == nil {
		w.entityWrites.pending = make(map[ChunkPos]entitySnapshot)
	}
	w.entityWrites.pending[pos] = snapshot
	return snapshot
}

func copyEntities(entities []nbt.Tag) []nbt.Tag {
	return nbt.Copy(nbt.List{Elem: nbt.TypeCompound, Items: entities}).(nbt.List).Items
}

// pendingChunkEntities returns a caller-owned view, including an empty deletion.
func (w *World) pendingChunkEntities(pos ChunkPos) ([]nbt.Tag, bool) {
	e := &w.entityWrites
	e.mu.Lock()
	defer e.mu.Unlock()
	snapshot, ok := e.pending[pos]
	if !ok {
		return nil, false
	}
	return copyEntities(snapshot.entities), true
}

// retryEntityWrites retries retained failures at save barriers. Writers may have
// moved on meanwhile; the stamp check prevents a retry overwriting their data.
func (w *World) retryEntityWrites() error {
	e := &w.entityWrites
	e.mu.Lock()
	pending := make(map[ChunkPos]entitySnapshot, len(e.pending))
	maps.Copy(pending, e.pending)
	e.mu.Unlock()
	var errs []error
	for pos, snapshot := range pending {
		if err := w.SaveChunkEntitiesStamped(pos, snapshot.stamp, snapshot.entities); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (w *World) logEntityWriteRetries() {
	if err := w.retryEntityWrites(); err != nil {
		slog.Error("could not save pending chunk entities", "error", err)
	}
}
