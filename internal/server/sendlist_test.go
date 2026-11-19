package server

import (
	"slices"
	"sync"
	"testing"
)

// TestSendListReuseKeepsQueuedPackets sends lists in turn, each later one on
// entries an earlier one gave back, some from goroutines of their own, and
// checks every mailbox holds exactly what it was sent, in order.
func TestSendListReuseKeepsQueuedPackets(t *testing.T) {
	t.Parallel()
	newClient := func() *client {
		c := &client{srv: &Server{}}
		c.out = &mailbox{c: c, wake: make(chan struct{}, 1)}
		return c
	}
	a, b := newClient(), newClient()

	var first sendList
	first.add(a, outbound{id: 1, data: []byte{1}})
	first.add(b, outbound{id: 2, data: []byte{2}})
	first.add(a, outbound{id: 3, data: []byte{3}})
	first.send()
	first.send() // sent already: nothing more goes
	var second sendList
	second.add(b, outbound{id: 4, data: []byte{4}})
	second.send()

	var wg sync.WaitGroup
	for i := range 8 {
		c := newClient()
		wg.Go(func() {
			for round := range 50 {
				var l sendList
				l.add(c, outbound{id: int32(i), data: []byte{byte(round)}})
				l.add(c, outbound{id: int32(i), data: []byte{byte(round), 1}})
				l.send()
			}
			if got := len(c.out.queue); got != 100 {
				t.Errorf("client %d: %d packets queued, want 100", i, got)
				return
			}
			for j, p := range c.out.queue {
				want := []byte{byte(j / 2)}
				if j%2 == 1 {
					want = append(want, 1)
				}
				if p.id != int32(i) || !slices.Equal(p.data, want) {
					t.Errorf("client %d: packet %d = %d %v, want %d %v", i, j, p.id, p.data, i, want)
					return
				}
			}
		})
	}
	wg.Wait()

	check := func(name string, c *client, want ...outbound) {
		t.Helper()
		if !slices.EqualFunc(c.out.queue, want, func(x, y outbound) bool {
			return x.id == y.id && slices.Equal(x.data, y.data)
		}) {
			t.Errorf("%s's queue = %v, want %v", name, c.out.queue, want)
		}
	}
	check("a", a, outbound{id: 1, data: []byte{1}}, outbound{id: 3, data: []byte{3}})
	check("b", b, outbound{id: 2, data: []byte{2}}, outbound{id: 4, data: []byte{4}})
}
