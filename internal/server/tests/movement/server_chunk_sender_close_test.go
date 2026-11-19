package movement

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// crossingBeside parks a crossing
// between its chunk-sender stop and its restart, runs leave, then
// lets the crossing go on: it must not start a sender for the gone client.
func crossingBeside(t *testing.T, leave func(srv *server.Server, c *servertest.Client)) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Leaver")
	c.Settle()

	// The crossing's stop is the first; the close is the
	// second, and the stop the leave then waits behind, so the second
	// stop says the client or server is done with the sender.
	parked := make(chan struct{})
	left := make(chan struct{})
	release := make(chan struct{})
	var stops atomic.Int32
	var open sync.Once
	t.Cleanup(func() { open.Do(func() { close(release) }) })
	srv.WatchChunkStops(func() {
		switch stops.Add(1) {
		case 1:
			close(parked)
			<-release
		case 2:
			close(left)
		}
	})
	t.Cleanup(func() { srv.WatchChunkStops(nil) })

	started := srv.ChunkSendersStarted()
	crossed := make(chan struct{})
	go func() {
		defer close(crossed)
		srv.MovePlayerToDimension("Leaver", world.NetherName, 8, 40, 8)
	}()
	select {
	case <-parked:
	case <-time.After(20 * time.Second):
		t.Fatal("the crossing never reached the chunk sender's stop")
	}

	leave(srv, c)
	select {
	case <-left:
	case <-time.After(20 * time.Second):
		t.Fatal("nothing stopped the sender for good")
	}
	open.Do(func() { close(release) })
	select {
	case <-crossed:
	case <-time.After(20 * time.Second):
		t.Fatal("the crossing never finished")
	}
	<-c.Served
	if n := srv.ChunkSendersStarted() - started; n != 0 {
		t.Errorf("%d chunk senders started after the sender was closed", n)
	}
}
