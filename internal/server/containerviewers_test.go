package server

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

func TestContainerViewerRegistry(t *testing.T) {
	s := &Server{clients: map[uuid.UUID]*client{}}
	w1, w2 := &world.World{}, &world.World{}
	join := func() *client {
		p := &player.Player{UUID: uuid.New()}
		c := &client{srv: s, player: p}
		s.clients[p.UUID] = c
		return c
	}
	viewers := func(w *world.World, x int32, except *client) []*client {
		var got []*client
		s.containerViewers(w, x, 64, 0, except, func(c *client) { got = append(got, c) })
		return got
	}
	a, b := join(), join()

	// Both halves of a double chest.
	double := &openWindow{kind: windowDouble, w: w1, x: 0, y: 64, z: 0, second: [3]int32{1, 64, 0}}
	a.setOpenWindow(double)
	for _, x := range []int32{0, 1} {
		if got := viewers(w1, x, nil); !slices.Equal(got, []*client{a}) {
			t.Fatalf("half %d viewers = %v", x, got)
		}
	}
	// Equal coordinates in another world.
	if got := viewers(w2, 0, nil); len(got) != 0 {
		t.Fatalf("other world viewers = %v", got)
	}
	// The except client.
	b.setOpenWindow(&openWindow{kind: windowBarrel, w: w1, x: 1, y: 64, z: 0})
	if got := viewers(w1, 1, a); !slices.Equal(got, []*client{b}) {
		t.Fatalf("except viewers = %v", got)
	}
	// Replace: the old positions are released.
	a.setOpenWindow(&openWindow{kind: windowBarrel, w: w2, x: 0, y: 64, z: 0})
	if got := viewers(w1, 0, nil); len(got) != 0 {
		t.Fatalf("after replace, old position viewers = %v", got)
	}
	if got := viewers(w2, 0, nil); !slices.Equal(got, []*client{a}) {
		t.Fatalf("after replace, new position viewers = %v", got)
	}
	// Close empties the buckets.
	a.setOpenWindow(nil)
	b.setOpenWindow(nil)
	if n := len(s.viewers.byAt); n != 0 {
		t.Fatalf("%d buckets left after close", n)
	}
	// Disconnect teardown of the last of two viewers releases its references in
	// the bucket while the other stays.
	a.setOpenWindow(&openWindow{kind: windowHopper, w: w1, x: 0, y: 64, z: 0})
	b.setOpenWindow(&openWindow{kind: windowHopper, w: w1, x: 0, y: 64, z: 0})
	k := viewerKey{w1, 0, 64, 0}
	bucket := s.viewers.byAt[k]
	if err := b.closeWindows(false, true, true); err != nil {
		t.Fatal(err)
	}
	if got := viewers(w1, 0, nil); !slices.Equal(got, []*client{a}) {
		t.Fatalf("after one close, viewers = %v", got)
	}
	if left := bucket[:2][1]; left.c != nil || left.window != nil {
		t.Fatalf("bucket still holds the closed viewer: %+v", left)
	}
	a.setOpenWindow(nil)
	// A disconnected client is not a viewer.
	a.setOpenWindow(&openWindow{kind: windowBarrel, w: w1, x: 0, y: 64, z: 0})
	delete(s.clients, a.player.UUID)
	if got := viewers(w1, 0, nil); len(got) != 0 {
		t.Fatalf("disconnected viewers = %v", got)
	}
}
