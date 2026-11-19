package world

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/registry"
)

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
