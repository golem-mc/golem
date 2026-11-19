package world

import (
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/registry"
)

// TestMergeRelightsJoinsOnlyWhatSavesWork holds the merge rule: the same box
// asked twice and the boxes of two neighbouring chunks join, boxes far apart
// stay two, and a merged group sits where its first request came in.
func TestMergeRelightsJoinsOnlyWhatSavesWork(t *testing.T) {
	t.Parallel()
	req := func(minX, minZ, maxX, maxZ int32) relightReq {
		return relightReq{minX: minX, minZ: minZ, maxX: maxX, maxZ: maxZ}
	}
	groups := mergeRelights([]relightReq{
		req(0, 0, 15, 15),       // chunk (0,0)
		req(500, 500, 500, 500), // far away
		req(0, 0, 15, 15),       // (0,0) again
		req(16, 0, 31, 15),      // its east neighbour
	})
	if len(groups) != 2 {
		t.Fatalf("merged into %d groups, want 2", len(groups))
	}
	g := groups[0]
	if g.minX != 0 || g.minZ != 0 || g.maxX != 31 || g.maxZ != 15 || len(g.reqs) != 3 {
		t.Errorf("the first group is (%d,%d)-(%d,%d) of %d requests, want (0,0)-(31,15) of 3",
			g.minX, g.minZ, g.maxX, g.maxZ, len(g.reqs))
	}
	if g := groups[1]; g.minX != 500 || len(g.reqs) != 1 {
		t.Errorf("the second group starts at x=%d with %d requests, want the far box alone", g.minX, len(g.reqs))
	}
}

// TestMergeRelightsCapsTheGroupSize holds the cap on a merged group: a
// scattered burst of single-column changes must not chain into one box whose
// relight holds the write lock past a tick.
func TestMergeRelightsCapsTheGroupSize(t *testing.T) {
	t.Parallel()
	single := func(x, z int32) relightReq {
		return relightReq{minX: x, minZ: z, maxX: x, maxZ: z}
	}
	var diagonal, line []relightReq
	for i := int32(0); i <= 24; i++ {
		diagonal = append(diagonal, single(i*8, i*8))
	}
	for i := range int32(256) {
		line = append(line, single(i, 0))
	}
	// Computed here, not read from mergeRelights, so an uncapped merge still
	// fails the test.
	cap := int64(4 * (2*lightRadius + 1) * (2*lightRadius + 1))
	for name, batch := range map[string][]relightReq{"a diagonal": diagonal, "a line": line} {
		for _, g := range mergeRelights(batch) {
			if g.wipeArea() > cap {
				t.Errorf("%s merged into a box wiping %d columns, over the %d cap",
					name, g.wipeArea(), cap)
			}
		}
	}
}

// TestQueuedRelightsLightLikeOneByOne writes the same blocks to two worlds:
// one relights each write before the next, the other queues all its relights
// from many goroutines at once, for the lane to merge. Every queued request
// is answered, and the light ends the same.
func TestQueuedRelightsLightLikeOneByOne(t *testing.T) {
	t.Parallel()
	queued := newTestWorld(t, testTerrain(31, false))
	serial := newTestWorld(t, testTerrain(31, false))
	var positions []ChunkPos
	for cx := int32(-1); cx <= 1; cx++ {
		for cz := int32(-1); cz <= 1; cz++ {
			positions = append(positions, ChunkPos{cx, cz})
			queued.Acquire(ChunkPos{cx, cz})
			serial.Acquire(ChunkPos{cx, cz})
		}
	}
	states := []uint16{stoneState, torchState, airState, registry.BlockStates["minecraft:glowstone"]}
	rng := rand.New(rand.NewPCG(31, 31))
	type write struct {
		x, y, z int32
		state   uint16
	}
	writes := make([]write, 300)
	for i := range writes {
		writes[i] = write{rng.Int32N(48) - 16, 55 + rng.Int32N(30), rng.Int32N(48) - 16, states[rng.IntN(len(states))]}
	}
	for _, wr := range writes {
		serial.SetBlock(wr.x, wr.y, wr.z, wr.state)
		serial.Relight(wr.x, wr.z)
	}

	// Nobody waits on an answer before queueing the next, so the lane finds
	// long batches to merge.
	pending := make(chan (<-chan []LightChange), len(writes))
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := i; j < len(writes); j += 8 {
				wr := writes[j]
				queued.SetBlock(wr.x, wr.y, wr.z, wr.state)
				pending <- queued.RelightAsync(wr.x, wr.z)
			}
		})
	}
	wg.Wait()
	close(pending)
	for answer := range pending {
		<-answer
	}
	// The interleaved writes may land in another order than the serial
	// world's; the cells written more than once are set to the serial
	// world's last state and relit, so both worlds hold the same blocks.
	for _, wr := range writes {
		if s := serial.BlockAt(wr.x, wr.y, wr.z); queued.BlockAt(wr.x, wr.y, wr.z) != s {
			queued.SetBlock(wr.x, wr.y, wr.z, s)
			<-queued.RelightAsync(wr.x, wr.z)
		}
	}
	compareLight(t, 31, queued, serial, positions)
}

// TestRelightLaneAnswersAndRestartsAfterAPanic panics one relight: its waiter
// is answered, the lane takes the next request, and the world lock is free.
func TestRelightLaneAnswersAndRestartsAfterAPanic(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	receive := func(ch <-chan []LightChange, what string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s was never answered", what)
		}
	}
	w.SetBlock(8, 70, 8, torchState)
	w.relightComputed = func() { panic("relight failed") }
	receive(w.RelightAreaAsync(8, 8, 8, 8), "the panicked relight")
	w.relightComputed = nil
	w.SetBlock(8, 71, 8, torchState)
	receive(w.RelightAreaAsync(8, 8, 8, 8), "the relight after the panic")
	set := make(chan struct{})
	go func() { w.SetBlock(9, 70, 8, stoneState); close(set) }()
	select {
	case <-set:
	case <-time.After(2 * time.Second):
		t.Fatal("SetBlock after the panicked relight blocked on the world lock")
	}
}
