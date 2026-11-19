package world

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/nbt"
)

func queuedEntityWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func queueEntity(w *World, pos ChunkPos, id string) uint64 {
	stamp := w.StampChunkEntities(pos)
	w.QueueChunkEntitiesStamped(pos, stamp, entityTag(id))
	return stamp
}

func TestQueuedEntitySnapshotsReloadBeforeIO(t *testing.T) {
	w := queuedEntityWorld(t)
	// Occupy every runner, so no queued snapshot can have reached disk.
	for range cap(w.exec.ioSem) {
		w.exec.ioSem <- struct{}{}
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			for range cap(w.exec.ioSem) {
				<-w.exec.ioSem
			}
		})
	}
	t.Cleanup(release)
	pos := ChunkPos{X: 2, Z: 3}
	source := entityTag("minecraft:pig")
	source[0].(nbt.Compound)["Passengers"] = nbt.List{Elem: nbt.TypeCompound, Items: entityTag("minecraft:cow")}
	older := w.StampChunkEntities(pos)
	w.QueueChunkEntitiesStamped(pos, older, source)
	source[0].(nbt.Compound)["id"] = nbt.String("minecraft:sheep")
	source[0].(nbt.Compound)["Passengers"].(nbt.List).Items[0].(nbt.Compound)["id"] = nbt.String("minecraft:sheep")
	if got := savedEntityID(t, w, pos); got != "minecraft:pig" {
		t.Fatal(got)
	}
	tags, _, _ := w.LoadChunkEntities(pos)
	passengers := tags[0].(nbt.Compound)["Passengers"].(nbt.List)
	if passengers.Items[0].(nbt.Compound)["id"] != nbt.String("minecraft:cow") {
		t.Fatal("queued snapshot shared nested input")
	}
	tags[0].(nbt.Compound)["id"] = nbt.String("minecraft:sheep")
	if got := savedEntityID(t, w, pos); got != "minecraft:pig" {
		t.Fatal("read mutated pending snapshot")
	}
	w.QueueChunkEntitiesStamped(pos, w.StampChunkEntities(pos), nil)
	if got, listed, err := w.LoadChunkEntities(pos); len(got) != 0 || listed || err != nil {
		t.Fatalf("pending deletion = %v, %v, %v", got, listed, err)
	}
	release()
	w.FlushSaves()
	if err := w.SaveChunkEntitiesStamped(pos, older, source); err != nil {
		t.Fatal(err)
	}
	if got, listed, err := w.LoadChunkEntities(pos); len(got) != 0 || listed || err != nil {
		t.Fatalf("stale write resurrected deleted entities: %v, %v, %v", got, listed, err)
	}
}

func TestQueuedEntityWritesRespectNewerSynchronousSnapshot(t *testing.T) {
	w := queuedEntityWorld(t)
	pos := ChunkPos{X: 2, Z: 3}
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	w.SetEntityWriteGate(func() {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	})
	old := queueEntity(w, pos, "minecraft:pig")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("writer never reached gate")
	}
	queueEntity(w, pos, "minecraft:cow")
	stamp := w.StampChunkEntities(pos)
	done := make(chan error, 1)
	go func() { done <- w.SaveChunkEntitiesStamped(pos, stamp, entityTag("minecraft:sheep")) }()
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	w.FlushSaves()
	if err := w.SaveChunkEntitiesStamped(pos, old, entityTag("minecraft:pig")); err != nil {
		t.Fatal(err)
	}
	if got := savedEntityID(t, w, pos); got != "minecraft:sheep" {
		t.Fatal(got)
	}
	if len(w.entityWrites.pending) != 0 || len(w.entityWrites.latest) != 0 {
		t.Fatal("completed writes retained bookkeeping")
	}
}

func TestQueuedEntityFailureRetainsSnapshotAndRetries(t *testing.T) {
	for _, barrier := range []string{"flush", "save", "close"} {
		t.Run(barrier, func(t *testing.T) {
			w := queuedEntityWorld(t)
			pos := ChunkPos{X: 2, Z: 3}
			fault := errors.New("disk full")
			w.SetEntityWriteFault(fault)
			queueEntity(w, pos, "minecraft:pig")
			w.FlushSaves()
			if len(w.entityWrites.pending) != 1 {
				t.Fatal("failed write dropped its snapshot")
			}
			if got := savedEntityID(t, w, pos); got != "minecraft:pig" {
				t.Fatal(got)
			}
			w.SetEntityWriteFault(nil)
			switch barrier {
			case "flush":
				w.FlushSaves()
			case "save":
				w.Save()
			case "close":
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
			}
			// Read through a separate world so a pending snapshot cannot satisfy it.
			other, err := NewWorld(w.dir, NewFlatGenerator())
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			if got := savedEntityID(t, other, pos); got != "minecraft:pig" {
				t.Fatal(got)
			}
		})
	}
}

func TestQueuedEntityReloadWithAnUnsubmittedNewerStamp(t *testing.T) {
	w := queuedEntityWorld(t)
	pos := ChunkPos{X: 2, Z: 3}
	if err := w.SaveChunkEntities(pos, entityTag("minecraft:cow")); err != nil {
		t.Fatal(err)
	}
	w.SetEntityWriteFault(errors.New("disk full"))
	older := queueEntity(w, pos, "minecraft:pig")
	w.FlushSaves()
	w.SetEntityWriteFault(nil)

	// Deliberately violate StampChunkEntities' contract: no production caller
	// abandons a snapshot, but a bare stamp must not discard unsaved entities.
	w.StampChunkEntities(pos)
	if err := w.SaveChunkEntitiesStamped(pos, older, entityTag("minecraft:pig")); err != nil {
		t.Fatal(err)
	}
	w.FlushSaves()
	if got := savedEntityID(t, w, pos); got != "minecraft:pig" {
		t.Fatalf("reload with no replacement submitted = %s, want pending pig", got)
	}
	other, err := NewWorld(w.dir, NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if got := savedEntityID(t, other, pos); got != "minecraft:cow" {
		t.Fatalf("disk changed despite rejecting the older stamp: %s", got)
	}

	queueEntity(w, pos, "minecraft:sheep")
	w.FlushSaves()
	if got := savedEntityID(t, w, pos); got != "minecraft:sheep" {
		t.Fatal(got)
	}
	if len(w.entityWrites.pending) != 0 {
		t.Fatal("replacement left the old pending snapshot readable")
	}
}

func TestQueuedEntitiesDrainBeforeClose(t *testing.T) {
	w := queuedEntityWorld(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	w.SetEntityWriteGate(func() {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	})
	pos := ChunkPos{X: 2, Z: 3}
	queueEntity(w, pos, "minecraft:pig")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("writer never reached gate")
	}
	done := make(chan error, 1)
	go func() { done <- w.Close() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		w.exec.mu.RLock()
		stopped := w.exec.stopped
		w.exec.mu.RUnlock()
		if stopped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("close never stopped executor")
		}
		runtime.Gosched()
	}
	select {
	case err := <-done:
		t.Fatalf("close returned before writing: %v", err)
	default:
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	other, err := NewWorld(w.dir, NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if got := savedEntityID(t, other, pos); got != "minecraft:pig" {
		t.Fatal(got)
	}
}

func TestQueuedEntityCloseReportsWriteFailure(t *testing.T) {
	w := queuedEntityWorld(t)
	fault := errors.New("disk full")
	w.SetEntityWriteFault(fault)
	queueEntity(w, ChunkPos{}, "minecraft:pig")
	if err := w.Close(); !errors.Is(err, fault) {
		t.Fatalf("close = %v, want disk full", err)
	}
	w.SetEntityWriteFault(nil)
	// A terminal write error must reach the caller rather than silently succeed.
}

func TestRegionInternalRecordsSkipExternalCleanup(t *testing.T) {
	path := regionFilePath(t.TempDir(), 0, 0)
	rf, err := openRegion(path, defaultSaveFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.close()
	pos := ChunkPos{}
	external := externalChunkPath(path, pos)
	if err := os.Mkdir(external, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "keep"), []byte("unrelated"), 0644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := rf.writeChunk(pos, []byte("small")); err != nil {
			t.Fatal(err)
		}
	}
	if err := rf.removeChunk(pos); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(external, "keep")); err != nil {
		t.Fatal(err)
	}
}

func TestQueuedEntityFailedOlderWriteKeepsNewerSnapshot(t *testing.T) {
	w := queuedEntityWorld(t)
	pos := ChunkPos{X: 2, Z: 3}
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	w.SetEntityWriteGate(func() {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	})
	w.SetEntityWriteFault(errors.New("disk full"))
	queueEntity(w, pos, "minecraft:pig")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("writer never reached gate")
	}
	queueEntity(w, pos, "minecraft:cow")
	unblock()
	w.FlushSaves()
	if got := savedEntityID(t, w, pos); got != "minecraft:cow" {
		t.Fatalf("failed older write replaced newer snapshot with %s", got)
	}
	w.SetEntityWriteFault(nil)
	w.FlushSaves()
	other, err := NewWorld(w.dir, NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if got := savedEntityID(t, other, pos); got != "minecraft:cow" {
		t.Fatal(got)
	}
}
