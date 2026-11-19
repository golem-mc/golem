package world

import (
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
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
