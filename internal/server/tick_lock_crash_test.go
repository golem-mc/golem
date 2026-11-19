package server

import (
	"io"
	"log/slog"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestATickPanicUnderAKindLockReleasesIt checks that a panic while the tick
// holds an entity kind's lock leaves every kind's lock free, so a leaving
// player's forget and the shutdown complete instead of blocking.
func TestATickPanicUnderAKindLockReleasesIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fault func(s *Server, mu *sync.Mutex) bool
		// player joins a survival player, whose tick takes the locks.
		player bool
	}{
		{"mob tick", func(s *Server, mu *sync.Mutex) bool { return mu == &s.mobsMu }, false},
		{"item tick", func(s *Server, mu *sync.Mutex) bool { return mu == &s.itemsMu }, false},
		// The second of two locks held at once.
		{"nested locks", func(s *Server, mu *sync.Mutex) bool { return len(s.tickLocks) == 2 }, false},
		// The phases around a tick's locked step.
		{"input reader", func(s *Server, mu *sync.Mutex) bool { return lockTakenIn("readHerdInputs") }, false},
		{"departure sweep", func(s *Server, mu *sync.Mutex) bool {
			return mu == &s.orbsMu && lockTakenIn("sweepDepartingEntities")
		}, false},
		{"cramming crowd mobs", func(s *Server, mu *sync.Mutex) bool {
			return mu == &s.mobsMu && lockTakenIn("buildCrammingCrowd")
		}, true},
		{"cramming crowd boats", func(s *Server, mu *sync.Mutex) bool {
			return mu == &s.boatsMu && lockTakenIn("buildCrammingCrowd")
		}, true},
		{"cramming crowd minecarts", func(s *Server, mu *sync.Mutex) bool {
			return mu == &s.minecartsMu && lockTakenIn("buildCrammingCrowd")
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = t.TempDir()
			cfg.LevelType = TestFlatLevelType
			s, err := newServer(cfg, 1)
			if err != nil {
				t.Fatal(err)
			}
			if tc.player {
				joinTickPanicPlayer(t, s)
			}
			// fired tells the fault's crash from any other panic.
			var fired atomic.Bool
			fault := func(mu *sync.Mutex) {
				if tc.fault(s, mu) {
					fired.Store(true)
					panic("tick fault under " + tc.name)
				}
			}
			s.tickLockFault.Store(&fault)
			s.startTick()

			select {
			case <-s.StopRequested():
			case <-time.After(5 * time.Second):
				t.Fatal("the panicking tick did not request a stop within 5s")
			}
			if !s.Crashed() {
				t.Fatal("Crashed() = false after a tick panicked")
			}
			if !fired.Load() {
				t.Fatal("the tick stopped, but not on the injected fault")
			}

			done := make(chan struct{})
			go func() {
				for _, k := range observerTrackers() {
					k.forget(s, &client{})
				}
				s.Shutdown()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("a leaving player's forget and the shutdown did not return within 10s after the crash")
			}
		})
	}
}

// joinTickPanicPlayer adds a live survival player on the spawn chunk, its
// connection drained, so the player tick runs for it.
func joinTickPanicPlayer(t *testing.T, s *Server) {
	t.Helper()
	pos := world.ChunkPos{X: 0, Z: 0}
	s.world.Acquire(pos)
	t.Cleanup(func() { s.world.Release(pos) })
	serverEnd, peer := net.Pipe()
	t.Cleanup(func() { serverEnd.Close(); peer.Close() })
	go func() { _, _ = io.Copy(io.Discard, peer) }()
	p := &player.Player{EntityID: s.nextEntity(), Name: "crammed", UUID: uuid.New()}
	p.SetPosition(player.Position{X: 0.5, Y: float64(world.GroundLevel), Z: 0.5, OnGround: true})
	p.UpdateVitals(func(v *player.Vitals) { v.Health = 20 })
	// gone is closed already: no connection handler runs to close it on the
	// shutdown's Disconnect, which would wait out shutdownDrainTimeout.
	gone := make(chan struct{})
	close(gone)
	c := &client{srv: s, player: p, conn: protocol.NewConn(serverEnd), log: slog.New(slog.DiscardHandler), gone: gone}
	c.dim.Store(s.allDims()[0])
	c.advancements.Store(advancement.NewProgress(advancement.Current()))
	c.out = c.startMailbox()
	c.out.release()
	t.Cleanup(c.out.stop)
	c.keepAlive = &keepAliveTicker{}
	s.mu.Lock()
	s.assignPlayerSlot(c)
	s.clients[p.UUID] = c
	s.mu.Unlock()
}

// lockTakenIn reports whether the lockTick being faulted was called from the
// Server method fn.
func lockTakenIn(fn string) bool {
	pc := make([]uintptr, 16)
	frames := runtime.CallersFrames(pc[:runtime.Callers(1, pc)])
	for {
		f, more := frames.Next()
		if strings.HasSuffix(f.Function, ".(*Server)."+fn) {
			return true
		}
		if !more {
			return false
		}
	}
}
