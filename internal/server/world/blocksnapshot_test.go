package world

import (
	"math"
	"testing"
)

func TestBlockSnapshotMatchesLoadedSections(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	for i, pos := range []ChunkPos{{-1, -1}, {-1, 0}, {0, -1}, {0, 0}} {
		c, _ := w.Acquire(pos)
		defer w.Release(pos)
		// Uniform, four-bit, five-bit and direct palettes.
		states := []int{1, 15, 31, 257}[i]
		for y := int32(48); y < 80; y++ {
			for z := range int32(16) {
				for x := range int32(16) {
					c.SetBlock(x, y, z, uint16(1+int(x+16*z+256*(y-48))%states))
				}
			}
		}
	}
	snapshot := w.SnapshotBlocks()
	defer snapshot.Release()
	for x := int32(-18); x <= 18; x++ {
		for y := int32(46); y <= 81; y++ {
			for z := int32(-18); z <= 18; z++ {
				if got, want := snapshot.BlockAt(x, y, z), w.BlockAt(x, y, z); got != want {
					t.Fatalf("snapshot at (%d,%d,%d) = %d, want %d", x, y, z, got, want)
				}
			}
		}
	}
	for _, y := range []int32{math.MinInt32, worldMinY - 1, worldMinY, worldMinY + worldHeight - 1, worldMinY + worldHeight, math.MaxInt32} {
		if got, want := snapshot.BlockAt(0, y, 0), w.BlockAt(0, y, 0); got != want {
			t.Fatalf("snapshot at y=%d = %d, want %d", y, got, want)
		}
	}
}

func TestBlockSnapshotKeepsItsSectionsAndNextSnapshotSeesWrites(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	defer w.Release(ChunkPos{})
	w.SetBlock(1, 100, 1, stoneState)
	snapshot := w.SnapshotBlocks()
	defer snapshot.Release()
	if snapshot.BlockAt(1, 100, 1) != stoneState || snapshot.BlockAt(1, 60, 1) != stoneState {
		t.Fatal("missing stone in packed or uniform section")
	}
	// The packed palette grows; the uniform section becomes packed. Also
	// change a cell not yet read, but in a section already captured.
	w.SetBlock(1, 100, 1, torchState)
	w.SetBlock(2, 100, 1, stoneState)
	w.SetBlock(1, 60, 1, airState)
	if snapshot.BlockAt(1, 100, 1) != stoneState || snapshot.BlockAt(2, 100, 1) != airState || snapshot.BlockAt(1, 60, 1) != stoneState {
		t.Fatal("world writes changed a captured section")
	}
	next := w.SnapshotBlocks()
	defer next.Release()
	if next.BlockAt(1, 100, 1) != torchState || next.BlockAt(2, 100, 1) != stoneState || next.BlockAt(1, 60, 1) != airState {
		t.Fatal("the next snapshot missed the preceding writes")
	}
}

func TestBlockSnapshotReuseSeesFreshSectionsInEachWorld(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	defer w.Release(ChunkPos{})
	unloaded := newTestWorld(t, NewFlatGenerator())
	for i := range 20 {
		state := uint16(stoneState)
		if i%2 == 0 {
			state = torchState
		}
		w.SetBlock(1, 100, 1, state)
		for _, source := range []*World{w, unloaded} {
			snapshot := source.SnapshotBlocks()
			// x=257 collides in the front cache with x=1, but is unloaded.
			for _, x := range []int32{1, 257, 1} {
				if got, want := snapshot.BlockAt(x, 100, 1), source.BlockAt(x, 100, 1); got != want {
					t.Fatalf("snapshot %d at x=%d = %d, want %d", i, x, got, want)
				}
			}
			snapshot.Release()
		}
	}
	if unloaded.LoadedChunks() != 0 {
		t.Fatal("snapshot generated an unloaded chunk")
	}
}

func TestBlockSnapshotCollisionAndPaletteGrowth(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	defer w.Release(ChunkPos{})
	w.SetBlock(1, 100, 1, stoneState)
	a := w.SnapshotBlocks()
	defer a.Release()
	if a.BlockAt(1, 100, 1) != stoneState {
		t.Fatal("missing initial stone")
	}
	// Grow the live palette through every indirect width into direct storage.
	for i := range int32(300) {
		w.SetBlock(i&15, 96+i>>8, i>>4&15, uint16(i+1))
	}
	w.SetBlock(1, 100, 1, torchState)
	w.SetBlock(2, 100, 1, torchState)
	// All these sections collide with the original in the 64-way front cache.
	for _, pos := range [][3]int32{{65, 100, 1}, {1, 164, 1}, {1, 100, 65}} {
		a.BlockAt(pos[0], pos[1], pos[2])
		if a.BlockAt(1, 100, 1) != stoneState || a.BlockAt(2, 100, 1) != airState {
			t.Fatal("a front-cache collision recaptured the changed section")
		}
	}
	b := w.SnapshotBlocks()
	defer b.Release()
	// Independent readers remain alive simultaneously, with different versions.
	done := make(chan bool, 2)
	for _, tc := range []struct {
		snapshot *BlockSnapshot
		state    uint16
	}{{a, stoneState}, {b, torchState}} {
		go func() {
			ok := true
			for range 100 {
				ok = ok && tc.snapshot.BlockAt(1, 100, 1) == tc.state
			}
			done <- ok
		}()
	}
	if !<-done || !<-done {
		t.Fatal("independent snapshots shared live storage")
	}
	for i := range int32(300) {
		if got, want := b.BlockAt(i&15, 96+i>>8, i>>4&15), w.BlockAt(i&15, 96+i>>8, i>>4&15); got != want {
			t.Fatalf("grown palette cell %d = %d, want %d", i, got, want)
		}
	}
}

func TestBlockSnapshotReleaseResetsOwnership(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	defer w.Release(ChunkPos{})
	w.SetBlock(1, 100, 1, stoneState)
	s := w.SnapshotBlocks()
	s.BlockAt(1, 100, 1)
	section := s.sections[[3]int32{0, 6, 0}]
	words, palette := &section.blocks.words[0], &section.palette[0]
	s.Release()
	if s.w != nil || len(s.sections) != 0 || s.ways != [64]snapshotWay{} || len(s.free) == 0 {
		t.Fatal("released snapshot retained world, keys or section ownership")
	}
	if &section.blocks.words[0] != words || &section.palette[0] != palette || section.blocks.slots != nil {
		t.Fatal("release dropped owned buffers or captured writer lookup slots")
	}
	// Exercise reuse across packed, uniform and unloaded sections.
	for range 3 {
		next := w.SnapshotBlocks()
		for _, y := range []int32{60, 100, 120} {
			if got, want := next.BlockAt(1, y, 1), w.BlockAt(1, y, 1); got != want {
				t.Fatalf("reused snapshot y=%d = %d, want %d", y, got, want)
			}
		}
		next.Release()
	}
}
