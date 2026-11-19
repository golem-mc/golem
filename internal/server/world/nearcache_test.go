package world

import (
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/registry"
)

// TestTheNearCacheFollowsTheChunkMap checks that a read through the near
// cache answers what the chunk map holds: a chunk that left the world reads
// as unloaded, and a chunk loaded into the slot it shared reads as itself,
// not as the one before it.
func TestTheNearCacheFollowsTheChunkMap(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetDropUntouchedChunks(true)
	first, second := ChunkPos{X: 0, Z: 0}, ChunkPos{X: nearSlots, Z: 0}
	if nearIndex(first) != nearIndex(second) {
		t.Fatalf("chunks %v and %v do not share a slot", first, second)
	}

	w.Acquire(first)
	if got := w.BlockAt(0, worldMinY, 0); got == airState {
		t.Fatalf("the loaded chunk's floor reads as air")
	}
	w.Release(first)
	w.FlushSaves()
	if w.ChunkLoaded(first) {
		t.Fatal("the released chunk is still loaded")
	}
	if got := w.BlockAt(0, worldMinY, 0); got != airState {
		t.Errorf("the unloaded chunk reads %d, want air", got)
	}

	w.Acquire(second)
	defer w.Release(second)
	if got := w.BlockAt(0, worldMinY, 0); got != airState {
		t.Errorf("with %v in the shared slot, %v reads %d, want air", second, first, got)
	}
	if !w.SetBlock(nearSlots*16, GroundLevel, 0, stoneState) {
		t.Fatal("the write to the second chunk did not land")
	}
	if got := w.BlockAt(nearSlots*16, GroundLevel, 0); got != stoneState {
		t.Errorf("the second chunk reads %d, want the stone written into it", got)
	}
	blocks := w.BlocksIn(0, GroundLevel, 0, 0, GroundLevel, 0, nil)
	if len(blocks) != 1 || blocks[0] != airState {
		t.Errorf("BlocksIn over the unloaded chunk = %v, want one air", blocks)
	}
}

// TestABlockReadThroughTheNearCacheTakesNoLock reads a loaded chunk's block
// while the world's write lock is held: the read answers without waiting.
func TestABlockReadThroughTheNearCacheTakesNoLock(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	defer w.Release(ChunkPos{})
	w.SetBlock(3, GroundLevel, 4, stoneState)

	w.mu.Lock()
	got := make(chan uint16)
	go func() { got <- w.BlockAt(3, GroundLevel, 4) }()
	select {
	case state := <-got:
		w.mu.Unlock()
		if state != stoneState {
			t.Errorf("the read answered %d, want the stone written", state)
		}
	case <-time.After(5 * time.Second):
		w.mu.Unlock()
		<-got
		t.Fatal("a near-cache read waited for the write lock")
	}
}

// TestBlockReadsBesideWritesSeeOnlyStatesWritten reads cells while another
// goroutine writes them, gives an air section its first block array, and
// loads and unloads a chunk sharing their chunk's near slot: every read
// answers a state the cell held.
func TestBlockReadsBesideWritesSeeOnlyStatesWritten(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetDropUntouchedChunks(true)
	w.Acquire(ChunkPos{})
	defer w.Release(ChunkPos{})
	floor := w.BlockAt(0, worldMinY, 0)
	other := ChunkPos{X: nearSlots, Z: 0}
	glass := registry.BlockStates["minecraft:glass"]
	const high = GroundLevel + 100 // in a section of air until the first write
	cells := [][3]int32{{5, GroundLevel, 5}, {6, high, 6}, {7, high, 6}}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		defer close(done)
		for i := range 2000 {
			state := uint16(stoneState)
			if i%2 == 1 {
				state = glass
			}
			w.SetBlock(5, GroundLevel, 5, state)
			w.SetBlocks([]BlockChange{{X: 6, Y: high, Z: 6, State: state}, {X: 7, Y: high, Z: 6, State: state}})
			if i%100 == 0 {
				w.Acquire(other)
				w.Release(other)
			}
		}
	})
	for range 2 {
		wg.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				for _, cell := range cells {
					if s := w.BlockAt(cell[0], cell[1], cell[2]); s != stoneState && s != glass && s != airState {
						t.Errorf("cell %v read %d, a state never written there", cell, s)
						return
					}
				}
				if s := w.BlockAt(other.X*16, worldMinY, 0); s != floor && s != airState {
					t.Errorf("the floor of the chunk coming and going read %d", s)
					return
				}
			}
		})
	}
	wg.Wait()
}

// TestAMissedReadRefillsItsNearSlot loads two chunks sharing a slot: reading
// one then the other leaves the slot holding the one read last, so a run of
// reads in either chunk takes no lock.
func TestAMissedReadRefillsItsNearSlot(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	first, second := ChunkPos{X: 0, Z: 0}, ChunkPos{X: nearSlots, Z: 0}
	w.Acquire(first)
	defer w.Release(first)
	w.Acquire(second)
	defer w.Release(second)

	for _, pos := range []ChunkPos{first, second, first} {
		if got := w.BlockAt(pos.X*16, worldMinY, 0); got == airState {
			t.Fatalf("the floor of loaded chunk %v reads as air", pos)
		}
		if w.nearAt(pos) == nil {
			t.Errorf("after a read of %v its near slot does not hold it", pos)
		}
	}
}

// TestRefilledNearSlotsLeaveWithTheirChunk reads chunks sharing a slot while
// they load and unload: a read of an unloaded chunk answers air, never a
// chunk that has left the world.
func TestRefilledNearSlotsLeaveWithTheirChunk(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetDropUntouchedChunks(true)
	a, b := ChunkPos{}, ChunkPos{X: nearSlots}
	w.Acquire(a)
	defer w.Release(a)

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		defer close(done)
		for range 300 {
			w.Acquire(b)
			w.Release(b)
			w.FlushSaves()
			if w.ChunkLoaded(b) {
				t.Error("the released chunk is still loaded")
				return
			}
			if got := w.BlockAt(b.X*16, worldMinY, 0); got != airState {
				t.Errorf("the unloaded chunk reads %d, want air", got)
				return
			}
		}
	})
	for range 3 {
		wg.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				w.BlockAt(0, worldMinY, 0)
				w.BlockAt(b.X*16, worldMinY, 0)
			}
		})
	}
	wg.Wait()
}
