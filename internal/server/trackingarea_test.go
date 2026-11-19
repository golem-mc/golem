package server

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestTrackingAreaKeepsEveryVisiblePair compares the index with the unchanged
// range predicate across negative chunks, region edges, ranges and passengers.
func TestTrackingAreaKeepsEveryVisiblePair(t *testing.T) {
	t.Parallel()
	_, clients := trackingAreaFixture(50, 0, true)
	rng := rand.New(rand.NewPCG(6386, 1))
	for round := range 12 {
		watchers := make([]trackingWatcher, len(clients))
		for i, c := range clients {
			// Straddle chunk and region boundaries, including their negative side.
			x := float64(rng.IntN(80)-40)*16 + []float64{-0.001, 0, 0.001, 15.999}[i%4]
			z := float64(rng.IntN(80)-40)*16 + 0.5
			watchers[i] = trackingWatcher{c: c, x: x, z: z, chunkRadius: int32(2 + rng.IntN(31))}
		}
		near := nearbyTracking(watchers)
		for j := range 150 {
			p := &paintingEntity{x: int32(rng.IntN(1500) - 750), z: int32(rng.IntN(1500) - 750)}
			// Previously visible players must be checked even after a far teleport.
			p.tracked.set(clients[j%len(clients)], &toldBody[struct{}]{})
			k := paintingTracker
			k.rangeChunks = 1 + rng.IntN(32)
			k.passengerRange = func(*paintingEntity) int { return 32 }
			body := k.body(p)
			got := body.watchers(near)
			if !slices.IsSorted(got) || len(got) != len(slices.Compact(slices.Clone(got))) {
				t.Fatalf("candidates are not unique and ordered: %v", got)
			}
			pct := []int{1, 50, 100, 1000}[j%4]
			for i, w := range watchers {
				_, tracked := p.tracked.get(w.c)
				if (tracked || k.sees(p, body.p, w.x, w.z, w.chunkRadius, pct)) && !slices.Contains(got, i) {
					t.Fatalf("round %d body (%d,%d): omitted watcher %d at (%g,%g), radius %d", round, p.x, p.z, i, w.x, w.z, w.chunkRadius)
				}
			}
		}
		near.release()
	}
}

// TestTrackingAreaUpdatesCoverage exercises one retained area when a player
// moves, changes view distance, leaves, or changes order in the snapshot.
func TestTrackingAreaUpdatesCoverage(t *testing.T) {
	t.Parallel()
	_, clients := trackingAreaFixture(16, 0, true)
	watchers := make([]trackingWatcher, len(clients))
	for i, c := range clients {
		watchers[i] = trackingWatcher{c: c, x: float64(i) * 1024, chunkRadius: 2}
	}
	near := nearbyTracking(watchers)
	defer near.release()
	body := paintingTracker.body(&paintingEntity{x: 1024})
	check := func(want []int) {
		t.Helper()
		if got := body.watchers(near); !slices.Equal(got, want) {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
	check([]int{1})
	area := near.area
	// Keep ownership of the same area rather than relying on sync.Pool reuse.
	reset := func() {
		near.bySlot = nil
		near.all = nil
		near.n = len(watchers)
		near.view = func(i int) trackingWatcher { return watchers[i] }
		near.update()
	}
	watchers[0].x = 1024
	reset()
	check([]int{0, 1})
	watchers[0].x = 768
	watchers[0].chunkRadius = 32
	reset()
	check([]int{0, 1})
	watchers[0].chunkRadius = 2
	reset()
	check([]int{1})
	watchers = slices.Delete(watchers, 1, 2)
	reset()
	check(nil)
	watchers[2].x = 1024
	slices.Reverse(watchers)
	reset()
	check([]int{len(watchers) - 3})
	if near.area != area {
		t.Fatal("coverage was not updated in place")
	}
}

// TestTrackingAreaDeparturesStillRemove pairs a body, then moves either the
// watcher or the body well outside the indexed region before the next pass.
func TestTrackingAreaDeparturesStillRemove(t *testing.T) {
	t.Parallel()
	for _, movePlayer := range []bool{false, true} {
		s, clients := trackingAreaFixture(16, 1, true)
		c := clients[0]
		c.out = &mailbox{c: c, wake: make(chan struct{}, 1)}
		paintingTracker.frozenPass(s, s.trackingWatchers())
		if len(c.out.queue) == 0 {
			t.Fatal("body was not paired")
		}
		c.out.queue = nil
		if movePlayer {
			c.player.SetPosition(player.Position{X: -10000, Y: 64})
		} else {
			s.paintings[0].x = -10000
		}
		paintingTracker.frozenPass(s, s.trackingWatchers())
		if len(c.out.queue) != 1 || c.out.queue[0].id != protocol.IDPlayRemoveEntities {
			t.Fatalf("move player %v: packets = %v, want a removal", movePlayer, c.out.queue)
		}
	}
}
