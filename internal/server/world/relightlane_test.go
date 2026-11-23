package world

import (
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/registry"
)

func TestRelightLaneCoalescesQueuedBoxes(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	w.SetBlock(8, GroundLevel, 8, torchState)

	// Take the worker's place so all three bookings are in the same batch.
	w.relights.running = true
	first := w.RelightAreaAsync(8, 8, 8, 8)
	var calls int
	record := func(changed []LightChange) {
		calls++
		if changed != nil {
			t.Errorf("later callback got %v, want nil", changed)
		}
	}
	w.RelightAreaThen(8, 8, 8, 8, record)
	w.RelightAreaThen(9, 8, 9, 8, record)
	completion := RelightCompletion{Handle: func(*World, []LightChange) {
		t.Error("tracked booking published the channel's result again")
	}}
	for range 8 {
		w.RelightAreaTracked(9, 8, 9, 8, &completion)
	}
	if n := len(w.relights.queue); n != 2 {
		t.Fatalf("queued %d boxes, want two", n)
	}
	if more := w.relights.queue[1].more; len(more) != 1 || more[0].bookings != 8 {
		t.Fatal("tracked bookings did not share one completion count")
	}

	w.drainRelights()
	select {
	case changes := <-first:
		if len(changes) != 1 || changes[0].Pos != (ChunkPos{}) {
			t.Errorf("first booking got %v, want a light change in chunk (0, 0)", changes)
		}
	default:
		t.Error("first booking got no answer")
	}
	if calls != 2 {
		t.Errorf("got %d later callbacks, want two", calls)
	}
	waitRelightCompletion(t, &completion)
}

func waitRelightCompletion(t *testing.T, completion *RelightCompletion) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		completion.Wait()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relight bookings did not complete")
	}
}

func TestRelightLaneAnswersCallbacksOnPanic(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	w.SetBlock(8, GroundLevel, 8, torchState)

	var calls [3]int
	record := func(i int) func([]LightChange) {
		return func(changed []LightChange) {
			calls[i]++
			if changed != nil {
				t.Errorf("callback %d got %v after panic, want nil", i, changed)
			}
		}
	}
	w.relights.running = true
	w.RelightAreaThen(8, 8, 8, 8, record(0))
	w.RelightAreaThen(8, 8, 8, 8, record(1))
	completion := RelightCompletion{Handle: func(*World, []LightChange) {
		t.Error("failed relight published changes")
	}}
	w.relightComputed = func() {
		w.RelightAreaThen(8, 8, 8, 8, record(2))
		w.RelightAreaTracked(8, 8, 8, 8, &completion)
		panic("light compute failed")
	}
	func() {
		defer func() {
			if recover() != "light compute failed" {
				t.Fatal("expected a panic from the light compute")
			}
		}()
		w.drainRelights()
	}()
	if calls != [3]int{1, 1, 1} {
		t.Errorf("callback counts after panic = %v, want one each", calls)
	}
	if w.relights.running || len(w.relights.queue) != 0 || len(w.relights.queued) != 0 {
		t.Fatal("lane was not reset after panic")
	}
	waitRelightCompletion(t, &completion)
}

func TestRelightLaneSkipsOnlyDarkBandsAfterOverflow(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	box := relightBox{0, 0, 15, 15}
	overflow := func() {
		const y = worldMinY + 8
		state, next := uint16(airState), uint16(stoneState)
		if w.BlockAt(8, y, 8) == state {
			state, next = next, state
		}
		for range maxLightCells + 1 {
			w.SetBlock(8, y, 8, state)
			state, next = next, state
		}
	}
	overflow()
	if !w.settleDarkRelight(box) || w.LightMarkedIn(0, 0, 15, 15) {
		t.Fatal("a dark band with no sources should clear without a shadow")
	}

	overflow()
	const torchY = worldMinY + 40
	w.SetBlock(8, torchY, 8, torchState)
	if w.settleDarkRelight(box) {
		t.Fatal("an emitter added after overflow was skipped")
	}
	<-w.RelightAreaAsync(0, 0, 15, 15)
	if got := w.BlockLightAt(8, torchY, 8); got != 14 {
		t.Errorf("torch light = %d, want 14", got)
	}

	w.SetBlock(8, GroundLevel-1, 8, airState)
	if w.settleDarkRelight(box) {
		t.Fatal("a change next to a lit section was skipped")
	}
	<-w.RelightAreaAsync(0, 0, 15, 15)
	if got := w.SkyLightAt(8, GroundLevel-1, 8); got != 15 {
		t.Errorf("new sky opening = %d, want 15", got)
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
