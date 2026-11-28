package server

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// trackerBenchBodies is how many bodies of a kind each tracker benchmark
// ticks, and trackerBenchWatchers how many players watch every one of them.
const (
	trackerBenchBodies   = 200
	trackerBenchWatchers = 8
	trackingBenchPlayers = 20
)

var playerTrackingBenchMarker = []byte{0x33, 0x21, 0x33, 0x21, 0x33, 0x21, 0x33, 0x21}

type playerTrackingBenchConn struct {
	sentinel  chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	writes    atomic.Int64
}

func (c *playerTrackingBenchConn) Write(p []byte) (int, error) {
	c.writes.Add(1)
	if bytes.Contains(p, playerTrackingBenchMarker) {
		c.sentinel <- struct{}{}
	}
	return len(p), nil
}

func (c *playerTrackingBenchConn) Read([]byte) (int, error) {
	<-c.done
	return 0, io.EOF
}

func (c *playerTrackingBenchConn) Close() error {
	c.closeOnce.Do(func() { close(c.done) })
	return nil
}

func (c *playerTrackingBenchConn) LocalAddr() net.Addr              { return nil }
func (c *playerTrackingBenchConn) RemoteAddr() net.Addr             { return nil }
func (c *playerTrackingBenchConn) SetDeadline(time.Time) error      { return nil }
func (c *playerTrackingBenchConn) SetReadDeadline(time.Time) error  { return nil }
func (c *playerTrackingBenchConn) SetWriteDeadline(time.Time) error { return nil }

// newTrackerBenchServer builds a flat world with its middle chunks held and
// trackerBenchWatchers players standing thirty-odd blocks around the middle:
// in every kind's tracking range, out of an orb's follow. The players have
// no connection, so what the tick sends is built and dropped.
func newTrackerBenchServer(b testing.TB, plugins ...plugin.Registration) (*Server, *world.World) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServerWith(cfg, 1, nil, plugins...)
	if err != nil {
		b.Fatal(err)
	}
	dim := s.allDims()[0]
	w := dim.w
	var holds []world.ChunkPos
	for cx := int32(-3); cx < 3; cx++ {
		for cz := int32(-3); cz < 3; cz++ {
			holds = append(holds, world.ChunkPos{X: cx, Z: cz})
		}
	}
	for _, pos := range holds {
		w.Acquire(pos)
	}
	b.Cleanup(func() {
		for _, pos := range holds {
			w.Release(pos)
		}
		// The players have no connection for the shutdown to close.
		s.mu.Lock()
		clear(s.clients)
		s.mu.Unlock()
		s.Shutdown()
	})
	w.FlushLighting()

	s.mu.Lock()
	for i := range trackerBenchWatchers {
		p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
		x := float64(i%4)*20 - 30
		z := 35.0
		if i >= 4 {
			z = -35
		}
		p.SetPosition(player.Position{X: x, Y: float64(world.GroundLevel), Z: z})
		c := &client{srv: s, player: p}
		c.dim.Store(dim)
		c.worldScope = worldScope{srv: s, in: c.dimension, by: c.Profile}
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
	}
	s.mu.Unlock()
	return s, w
}

// trackerBenchSpot is body i's place on a grid two blocks apart, far enough
// that no two orbs merge.
func trackerBenchSpot(i int) (x, z float64) {
	return float64(i%15)*2 - 15 + 0.5, float64(i/15)*2 - 15 + 0.5
}

// BenchmarkTickOrbsTracked runs the whole orb tick over orbs at rest, every
// one watched by every player.
func BenchmarkTickOrbsTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		it := s.spawnOrb(w, x, float64(world.GroundLevel), z, 0, 0, 0, 1+i%7)
		it.orb.Age = -1 << 30
	}
	for range 40 {
		s.tickOrbs()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickOrbs()
	}
	b.StopTimer()
	if n := len(s.orbs); n != trackerBenchBodies {
		b.Fatalf("%d orbs after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickOrbsInLava runs the orb tick over invulnerable orbs held in
// lava, so every tick reaches the lava damage guard.
func BenchmarkTickOrbsInLava(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		w.SetBlock(int32(math.Floor(x)), world.GroundLevel, int32(math.Floor(z)), registry.BlockLava.DefaultState())
		it := s.spawnOrb(w, x, float64(world.GroundLevel)+0.2, z, 0, 0, 0, 1+i%7)
		it.orb.Age = -1 << 30
		it.orb.Invulnerable = true
	}
	for range 40 {
		s.tickOrbs()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickOrbs()
	}
	b.StopTimer()
	if n := len(s.orbs); n != trackerBenchBodies {
		b.Fatalf("%d orbs after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkPlayerTrackingMoves runs a small step for every player and the
// tracking tick that follows it, with every player watching every body. A
// marked Pong at the end of each iteration waits for the mailbox to write the
// tracking packets that came before it.
func BenchmarkPlayerTrackingMoves(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	s.mu.Lock()
	clear(s.clients)
	s.mu.Unlock()

	dim := s.allDims()[0]
	players := make([]*client, trackingBenchPlayers)
	sentinels := make([]chan struct{}, trackingBenchPlayers)
	for i := range players {
		p := &player.Player{UUID: uuid.New(), EntityID: int32(i + 1)}
		p.SetPosition(player.Position{
			X: float64(i%5)*2 - 4.5,
			Y: float64(world.GroundLevel),
			Z: float64(i/5)*2 - 6.5,
		})
		benchConn := &playerTrackingBenchConn{
			sentinel: make(chan struct{}, 1),
			done:     make(chan struct{}),
		}
		c := &client{
			srv:    s,
			conn:   protocol.NewConn(benchConn),
			log:    slog.New(slog.DiscardHandler),
			player: p,
		}
		c.dim.Store(dim)
		c.out = c.startMailbox()
		c.out.release()
		sentinels[i] = benchConn.sentinel
		cleanupConn := func() {
			benchConn.Close()
			c.out.stop()
		}
		b.Cleanup(cleanupConn)
		players[i] = c
		s.mu.Lock()
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
		s.mu.Unlock()
	}
	for _, body := range players {
		body.trackMu.Lock()
		body.tracked = track(body.player.Position())
		for _, viewer := range players {
			if viewer != body {
				body.seenBy.add(viewer)
			}
		}
		body.trackMu.Unlock()
	}

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 1; iteration <= b.N; iteration++ {
		for i, body := range players {
			pos := body.player.Position()
			pos.X = float64(i%5)*2 - 4.5 + 0.25*float64(iteration%2)
			body.tp.mu.Lock()
			body.moveHeld(pos, false)
			body.tp.mu.Unlock()
		}
		s.tickPlayerTracking()
		for _, viewer := range players {
			var marker protocol.Buffer
			marker.PutI64(0x3321332133213321)
			viewer.send(protocol.IDPlayPong, marker.Bytes())
		}
		for _, sentinel := range sentinels {
			<-sentinel
		}
	}
	b.StopTimer()
}

// BenchmarkTickTNTTracked runs the whole primed TNT tick over bodies at rest
// on a fuse that does not run out, every one watched by every player.
func BenchmarkTickTNTTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.spawnTNT(w, x, float64(world.GroundLevel), z, 1<<30, 0, "", uuid.Nil, false)
	}
	for range 40 {
		s.tickTNT()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickTNT()
	}
	b.StopTimer()
	if n := len(s.tnts); n != trackerBenchBodies {
		b.Fatalf("%d primed TNT after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickFallingBlocksTracked runs the whole falling block tick over
// blocks hanging in the air with no gravity, every one watched by every
// player.
func BenchmarkTickFallingBlocksTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		b.Fatal("no sand")
	}
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		it := s.spawnFallingBlock(w, world.FallingSpawn{X: int32(x), Y: world.GroundLevel + 10, Z: int32(z), State: sand})
		it.fb.NoGravity = true
		it.fb.Time = -1 << 30
	}
	for range 40 {
		s.tickFallingBlocks()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickFallingBlocks()
	}
	b.StopTimer()
	if n := len(s.fallings); n != trackerBenchBodies {
		b.Fatalf("%d falling blocks after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickFallingBlocksLanding runs the falling block tick over sand
// that lands on snow of four layers every round, which the landing test
// refuses and turns into an item.
func BenchmarkTickFallingBlocksLanding(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		b.Fatal("no sand")
	}
	snow, ok := registry.FindState("minecraft:snow", map[string]string{"layers": "4"})
	if !ok {
		b.Fatal("no snow")
	}
	b.ReportAllocs()
	for range b.N {
		b.StopTimer()
		for i := range trackerBenchBodies {
			x, z := trackerBenchSpot(i)
			w.SetBlock(int32(math.Floor(x)), world.GroundLevel, int32(math.Floor(z)), snow)
			s.spawnFallingBlock(w, world.FallingSpawn{X: int32(math.Floor(x)), Y: world.GroundLevel, Z: int32(math.Floor(z)), State: sand})
		}
		b.StartTimer()
		for range 3 {
			s.tickFallingBlocks()
		}
		b.StopTimer()
		if n := len(s.fallings); n != 0 {
			b.Fatalf("%d falling blocks still airborne, want 0", n)
		}
	}
}

// BenchmarkFrozenTracking runs the frozen world's tracking passes of the
// three kinds over every body of each, with every player in range.
func BenchmarkFrozenTracking(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	sand, _ := registry.FindState("minecraft:sand", nil)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.spawnOrb(w, x, float64(world.GroundLevel), z, 0, 0, 0, 1)
		s.spawnTNT(w, x, float64(world.GroundLevel), z, 1<<30, 0, "", uuid.Nil, false)
		s.spawnFallingBlock(w, world.FallingSpawn{X: int32(x), Y: world.GroundLevel + 10, Z: int32(z), State: sand})
	}
	watchers := s.trackingWatchers()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		orbTracker.frozenPass(s, watchers)
		tntTracker.frozenPass(s, watchers)
		fallingTracker.frozenPass(s, watchers)
	}
}

// BenchmarkStaticTracking is one fixture tracking pass over 2000 displays at
// rest, each watched by the players around it.
func BenchmarkStaticTracking(b *testing.B) {
	for _, n := range []int{8, 32, 64} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			logger := slog.Default()
			slog.SetDefault(slog.New(slog.DiscardHandler))
			b.Cleanup(func() { slog.SetDefault(logger) })
			s, _ := newTrackerBenchServer(b)
			dim := s.allDims()[0]
			s.mu.Lock()
			for i := trackerBenchWatchers; i < n; i++ {
				p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
				p.SetPosition(player.Position{X: float64(i%8)*10 - 35, Y: float64(world.GroundLevel), Z: float64(i/8)*10 - 35})
				c := &client{srv: s, player: p}
				c.dim.Store(dim)
				c.worldScope = worldScope{srv: s, in: c.dimension, by: c.Profile}
				s.assignPlayerSlot(c)
				s.clients[p.UUID] = c
			}
			s.mu.Unlock()
			for i := range 2000 {
				x, z := float64(i%45)*2-45, float64(i/45)*2-45
				s.RunRconLine(fmt.Sprintf("summon block_display %g %d %g", x, world.GroundLevel, z))
			}
			for range 20 {
				s.ticksRun.Add(1)
				s.tickFixtureTracking()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				s.ticksRun.Add(1)
				s.tickFixtureTracking()
			}
		})
	}
}

// BenchmarkObstructedCrowded places a block on an empty cell while 58 players
// and 1000 armor stands stand elsewhere in the world.
func BenchmarkObstructedCrowded(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	s.mu.Lock()
	for i := range 50 {
		p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
		p.SetPosition(player.Position{X: float64(i%8)*8 - 28, Y: float64(world.GroundLevel), Z: float64(i/8)*8 - 28})
		c := &client{srv: s, player: p}
		c.dim.Store(dim)
		c.worldScope = worldScope{srv: s, in: c.dimension, by: c.Profile}
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
	}
	s.mu.Unlock()
	for i := range 1000 {
		s.registerArmorStand(s.newArmorStandEntity(w, float64(i%40)*2-39.5, float64(world.GroundLevel), float64(i/40)*2-24.5))
	}
	stone := registry.BlockStone.DefaultState()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if s.obstructed(w, 40, world.GroundLevel+3, 40, stone) {
			b.Fatal("empty cell obstructed")
		}
	}
}
