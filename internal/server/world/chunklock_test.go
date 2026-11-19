package world

import (
	"bytes"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/registry"
)

// TestASharedHoldIsReleasedUnderTheReadLock releases one of two holds on a
// chunk while a reader holds the world lock: only the release that may
// unload the chunk waits for the write lock.
func TestASharedHoldIsReleasedUnderTheReadLock(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	w.Acquire(pos)

	w.mu.RLock()
	done := make(chan struct{})
	go func() {
		w.Release(pos)
		close(done)
	}()
	select {
	case <-done:
		w.mu.RUnlock()
	case <-time.After(5 * time.Second):
		w.mu.RUnlock()
		<-done
		t.Fatal("releasing one of two holds waited for the write lock")
	}

	if !w.ChunkLoaded(pos) {
		t.Fatal("the chunk unloaded with a hold left")
	}
	w.Release(pos)
	w.FlushSaves() // the last release saves the generated chunk first
	if w.ChunkLoaded(pos) {
		t.Error("the chunk stayed loaded after its last release")
	}
}

// TestALightJoinLeavesTheWorldUntilInstalled computes a new chunk's border
// crossing under the read lock and holds the chunks around it untouched
// until the install, which lights them as the join under the write lock
// does.
func TestALightJoinLeavesTheWorldUntilInstalled(t *testing.T) {
	t.Parallel()
	terrain := testTerrain(7, false)
	gen := func() *World { return newTestWorld(t, terrain) }
	off, locked := gen(), gen()
	pos := ChunkPos{X: 0, Z: 0}
	var around []ChunkPos
	for cx := int32(-1); cx <= 1; cx++ {
		for cz := int32(-1); cz <= 1; cz++ {
			if p := (ChunkPos{cx, cz}); p != pos {
				around = append(around, p)
				off.Acquire(p)
				locked.Acquire(p)
			}
		}
	}

	// A lamp in the west neighbour, so the new chunk's lamp raises light in
	// a section that already holds some, which the join must copy first.
	for _, w := range []*World{off, locked} {
		w.SetBlock(-8, 120, 8, glowstoneState)
		w.Relight(-8, 8)
	}
	c, motion, fresh := readyWithLamp(off, pos)
	off.mu.RLock()
	before := make(map[ChunkPos][]byte)
	for _, p := range around {
		before[p] = off.chunks[p].chunk.encodeLight()
	}
	j := off.newLightJoin(pos, c)
	for _, p := range around {
		if got := off.chunks[p].chunk.encodeLight(); !bytes.Equal(got, before[p]) {
			t.Errorf("the join wrote the light of %v before its install", p)
		}
	}
	off.mu.RUnlock()
	if len(j.touched) < 2 {
		t.Fatalf("the join raised the light of %d chunks, want the new one and a neighbour", len(j.touched))
	}

	off.mu.Lock()
	if !j.current(off) {
		t.Fatal("the join is stale with nothing changed")
	}
	j.install(off)
	off.putChunk(pos, c, motion, fresh)
	off.mu.Unlock()

	lc, lmotion, lfresh := readyWithLamp(locked, pos)
	locked.mu.Lock()
	locked.putChunk(pos, lc, lmotion, lfresh)
	want := locked.joinChunkLight(pos)
	locked.mu.Unlock()

	got := slices.Clone(j.changed)
	slices.SortFunc(got, cmpChunkPos)
	slices.SortFunc(want, cmpChunkPos)
	if !slices.Equal(got, want) {
		t.Errorf("the join changed the light of %v, the locked join %v", got, want)
	}
	compareLight(t, 7, off, locked, append(around, pos))
}

// glowstoneState is a block light source, lighting 15.
var glowstoneState = registry.BlockStates["minecraft:glowstone"]

// readyWithLamp readies the chunk at pos with a lamp in the air on its west
// border, its block light reaching the west, north and south neighbours.
func readyWithLamp(w *World, pos ChunkPos) (*Chunk, *[16][16]int32, bool) {
	c, motion, fresh := w.readyChunk(pos)
	c.SetBlock(0, 120, 8, glowstoneState)
	w.lightAlone(c)
	return c, motion, fresh
}

// TestAStaleJoinIsComputedAgain loads a neighbour between a join's compute
// and its install: the insert computes the join again, and the light crosses
// into the neighbour as if it had loaded first.
func TestAStaleJoinIsComputedAgain(t *testing.T) {
	t.Parallel()
	testStaleJoins(t, []ChunkPos{{X: -1, Z: 0}}, 2)
}

// TestAJoinThatKeepsGoingStaleIsJoinedUnderTheLock loads a neighbour after
// every compute: after relightTries computes the insert joins under the
// write lock, and the light still crosses into every neighbour.
func TestAJoinThatKeepsGoingStaleIsJoinedUnderTheLock(t *testing.T) {
	t.Parallel()
	testStaleJoins(t, []ChunkPos{{X: -1, Z: 0}, {X: 0, Z: 1}, {X: 0, Z: -1}}, relightTries)
}

// testStaleJoins inserts a lit chunk at the origin, loading one of loads
// after each of its joins' computes, and checks the insert computed
// wantComputes joins and lit as a world that loaded them first.
func testStaleJoins(t *testing.T, loads []ChunkPos, wantComputes int) {
	t.Helper()
	const seed = 7
	pos := ChunkPos{X: 0, Z: 0}
	terrain := testTerrain(seed, false)
	raced, first := newTestWorld(t, terrain), newTestWorld(t, terrain)
	raced.Acquire(ChunkPos{X: 1, Z: 0})
	first.Acquire(ChunkPos{X: 1, Z: 0})
	for _, p := range loads {
		first.Acquire(p)
	}
	computes := 0
	raced.joinComputed = func(p ChunkPos) {
		if p != pos {
			return
		}
		if computes < len(loads) {
			raced.Acquire(loads[computes])
		}
		computes++
	}
	c, motion, fresh := readyWithLamp(raced, pos)
	raced.insertChunk(pos, c, motion, fresh)
	fc, fmotion, ffresh := readyWithLamp(first, pos)
	first.insertChunk(pos, fc, fmotion, ffresh)

	if computes != wantComputes {
		t.Fatalf("the insert computed %d join(s), want %d", computes, wantComputes)
	}
	compareLight(t, seed, raced, first, append([]ChunkPos{pos, {X: 1, Z: 0}}, loads...))
}

// TestALightJoinGoesStale holds a join computed before a block write beside
// it, or before a neighbour's load, not current: either would leave its
// light wrong.
func TestALightJoinGoesStale(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, testTerrain(7, false))
	pos := ChunkPos{X: 0, Z: 0}
	w.Acquire(ChunkPos{X: 1, Z: 0})
	c, _, _ := w.readyChunk(pos)

	join := func() *lightJoin {
		w.mu.RLock()
		defer w.mu.RUnlock()
		return w.newLightJoin(pos, c)
	}
	current := func(j *lightJoin) bool {
		w.mu.RLock()
		defer w.mu.RUnlock()
		return j.current(w)
	}

	j := join()
	w.SetBlock(20, 100, 3, torchState)
	if current(j) {
		t.Error("a join is current after a block write beside it")
	}
	j = join()
	w.Acquire(ChunkPos{X: -1, Z: 1})
	if current(j) {
		t.Error("a join is current after a neighbour loaded")
	}
	j = join()
	if !current(j) {
		t.Error("a join is stale with nothing changed")
	}
}

// TestChunksLoadedTogetherLightAsOneByOne acquires a square of chunks from
// many goroutines at once, their joins racing each other's inserts, and
// holds the light equal to the same square acquired one by one.
func TestChunksLoadedTogetherLightAsOneByOne(t *testing.T) {
	t.Parallel()
	together := newTestWorld(t, testTerrain(11, false))
	alone := newTestWorld(t, testTerrain(11, false))
	var positions []ChunkPos
	for cx := int32(-3); cx <= 3; cx++ {
		for cz := int32(-3); cz <= 3; cz++ {
			positions = append(positions, ChunkPos{cx, cz})
		}
	}
	var wg sync.WaitGroup
	for _, pos := range positions {
		wg.Go(func() { together.Acquire(pos) })
		alone.Acquire(pos)
	}
	wg.Wait()
	compareLight(t, 11, together, alone, positions)
}

// joinChunkLight finishes a lightAlone chunk now in the map: light crosses its
// four borders both ways and the chunks it changed are re-encoded. The result
// equals relightBox's, since both only raise cells from the same sources to the
// fixpoint the neighbours held. It returns the chunks whose encoded light
// changed; the caller holds the world write lock. It writes the neighbours'
// arrays in place, so it is only the reference newLightJoin is held to.
func (w *World) joinChunkLight(pos ChunkPos) []ChunkPos {
	self := w.chunks[pos].chunk
	touched := []*Chunk{self}
	ctx := &lightCtx{w: w, touched: &touched}
	ctx.joinBorders(pos)

	var changed []ChunkPos
	for _, c := range touched {
		old := c.lightEncoded
		c.lightEncoded = c.encodeLight()
		c.chunkLightEncoded = c.encodeChunkLight()
		c.lightFormsStale = false
		c.lightGen++
		if old == nil || !bytes.Equal(old, c.lightEncoded) {
			changed = append(changed, ChunkPos{c.x, c.z})
		}
	}
	self.lightCorrect = true
	return changed
}

// TestAPanicInALightJoinReleasesTheWorldLock panics an insert's join under
// the read lock and under the write lock: either way a later SetBlock gets
// the world lock.
func TestAPanicInALightJoinReleasesTheWorldLock(t *testing.T) {
	t.Parallel()
	for _, under := range []string{"read", "write"} {
		t.Run(under, func(t *testing.T) {
			t.Parallel()
			w := newTestWorld(t, NewFlatGenerator())
			pos, east := ChunkPos{X: 0, Z: 0}, ChunkPos{X: 1, Z: 0}
			w.Acquire(east)
			loaded := w.chunks[east]
			real := loaded.chunk
			// The join reads the neighbour's chunk: stamping it under the read
			// lock, checking it is current under the write lock.
			breakJoin := func(ChunkPos) { loaded.chunk = nil }
			if under == "read" {
				breakJoin(pos)
			} else {
				w.joinComputed = breakJoin
			}
			c, motion, fresh := w.readyChunk(pos)
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("the broken join did not panic")
					}
				}()
				w.insertChunk(pos, c, motion, fresh)
			}()
			w.joinComputed = nil
			loaded.chunk = real

			set := make(chan struct{})
			go func() { w.SetBlock(20, 70, 3, stoneState); close(set) }()
			select {
			case <-set:
			case <-time.After(2 * time.Second):
				t.Fatalf("SetBlock after a join panicked under the %s lock blocked on the world lock", under)
			}
		})
	}
}
