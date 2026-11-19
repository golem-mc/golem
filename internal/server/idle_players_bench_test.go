package server

import (
	"fmt"
	"log/slog"
	"math"
	"net"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Joining 100 clients through the real login over loopback takes about 10 s per
// benchmark; fake clients skip the keep-alive, advancements, mailbox and
// recipe book the tick passes read. Run with -benchtime 2000x.

// newIdlePlayersBenchServer starts a flat-world server whose tick loop never
// runs and joins n players through the real login, configuration and join
// sequence over loopback, each standing still at spawn with its loading
// screen closed. A goroutine per player reads and drops what the server
// sends.
func newIdlePlayersBenchServer(b *testing.B, n int) (*Server, []*client) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	cfg.SpawnProtection = 0
	cfg.PauseWhenEmptySeconds = 0
	cfg.SyncChunkWrites = false
	cfg.ViewDistance = 2
	cfg.SimulationDistance = 2
	cfg.MaxPlayers = n + 1
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	s.SetSpawning(false)
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	var served sync.WaitGroup
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			served.Go(func() { s.HandleConnection(conn) })
		}
	}()
	var conns []net.Conn
	b.Cleanup(func() {
		listener.Close()
		for _, conn := range conns {
			conn.Close()
		}
		served.Wait()
		s.Shutdown()
		slog.SetDefault(logger)
	})
	for i := range n {
		raw, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
		if err != nil {
			b.Fatal(err)
		}
		conns = append(conns, raw)
		idleBenchJoin(b, protocol.NewConn(raw), fmt.Sprintf("idle%03d", i))
	}
	s.mu.RLock()
	clients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()
	if len(clients) != n {
		b.Fatalf("joined %d players, want %d", len(clients), n)
	}
	return s, clients
}

// idleBenchJoin drives one connection through login, configuration and the
// join up to the answer to a ping sent behind Player Loaded, then leaves a
// goroutine draining the connection.
func idleBenchJoin(b *testing.B, conn *protocol.Conn, name string) {
	send := func(id int32, fill func(*protocol.Buffer)) {
		var buf protocol.Buffer
		if fill != nil {
			fill(&buf)
		}
		if err := conn.WritePacket(id, buf.Bytes()); err != nil {
			b.Fatalf("write 0x%02X: %v", id, err)
		}
	}
	read := func() (int32, []byte) {
		id, data, err := conn.ReadPacket()
		if err != nil {
			b.Fatalf("read: %v", err)
		}
		return id, data
	}
	send(protocol.IDHandshake, func(w *protocol.Buffer) {
		w.PutVarInt(protocol.Version)
		w.PutString("localhost")
		w.PutI16(25565)
		w.PutVarInt(protocol.NextStateLogin)
	})
	send(protocol.IDLoginStart, func(w *protocol.Buffer) {
		w.PutString(name)
		w.PutUUID(uuid.Nil)
	})
	id, data := read()
	if id == protocol.IDSetCompression {
		conn.EnableCompression(int(protocol.NewReader(data).ReadVarInt()))
		id, _ = read()
	}
	if id != protocol.IDLoginSuccess {
		b.Fatalf("login: got 0x%02X", id)
	}
	send(protocol.IDLoginAcknowledged, nil)
	send(protocol.IDClientInformation, func(w *protocol.Buffer) {
		w.PutString("en_GB")
		w.PutI8(2)
		w.PutVarInt(0)
		w.PutBool(true)
		w.PutU8(0x7F)
		w.PutVarInt(1)
		w.PutBool(false)
		w.PutBool(true)
		w.PutVarInt(0)
	})
	send(protocol.IDKnownPacksIn, func(w *protocol.Buffer) {
		w.PutVarInt(1)
		w.PutString("minecraft")
		w.PutString("core")
		w.PutString(registry.Version)
	})
	for {
		if id, _ = read(); id == protocol.IDConfigFinish {
			break
		}
	}
	send(protocol.IDConfigFinishAck, nil)
	send(protocol.IDPlayLoaded, nil)
	send(protocol.IDPlayPingRequest, func(w *protocol.Buffer) { w.PutI64(0x10AD) })
	for {
		id, data = read()
		if id == protocol.IDPlaySyncPosition {
			r := protocol.NewReader(data)
			teleport := r.ReadVarInt()
			x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
			r.ReadF64()
			r.ReadF64()
			r.ReadF64()
			yaw, pitch := r.ReadF32(), r.ReadF32()
			send(protocol.IDPlayConfirmTeleport, func(w *protocol.Buffer) {
				w.PutVarInt(teleport)
				w.PutF64(x)
				w.PutF64(y)
				w.PutF64(z)
				w.PutF32(yaw)
				w.PutF32(pitch)
			})
		}
		if id == protocol.IDPlayPingResponse {
			break
		}
	}
	go func() {
		for {
			if _, _, err := conn.ReadPacket(); err != nil {
				return
			}
		}
	}()
}

const idleBenchPlayers = 100

// BenchmarkWholeTickIdle runs the whole tick over a hundred idle players.
func BenchmarkWholeTickIdle(b *testing.B) {
	s, _ := newIdlePlayersBenchServer(b, idleBenchPlayers)
	now := time.Now()
	for range 200 {
		s.tick(now)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tick(now)
	}
}

// BenchmarkTickWaypointsIdle settles the locator bars of a hundred players
// none of whom has moved since the tick before.
func BenchmarkTickWaypointsIdle(b *testing.B) {
	s, _ := newIdlePlayersBenchServer(b, idleBenchPlayers)
	for range 20 {
		s.tickWaypoints()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickWaypoints()
	}
}

// BenchmarkTickWaypointsMoving settles the locator bars of a hundred players
// every one of whom moved 0.2 blocks since the tick before.
func BenchmarkTickWaypointsMoving(b *testing.B) {
	s, clients := newIdlePlayersBenchServer(b, idleBenchPlayers)
	for range 20 {
		s.tickWaypoints()
	}
	step := 0.0
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		step += 0.2
		for i, c := range clients {
			pos := c.player.Position()
			pos.X = float64(i%10)*3 + step
			c.player.SetPosition(pos)
		}
		s.tickWaypoints()
	}
}

// idleBenchGear fills every player's armour, main inventory, hotbar and off
// hand with one of a different item per slot.
func idleBenchGear(clients []*client) {
	for _, c := range clients {
		c.player.Inventory(func(inv *player.Inventory) {
			for slot := player.ArmorSlot; slot < len(inv.Slots); slot++ {
				inv.Slots[slot] = protocol.ItemStack{ItemID: int32(registry.ItemStone) + int32(slot), Count: 1}
			}
		})
	}
}

// BenchmarkTickSurvivalIdle runs the players' own tick over a hundred joined
// players standing still with nothing changing.
func BenchmarkTickSurvivalIdle(b *testing.B) {
	s, _ := newIdlePlayersBenchServer(b, idleBenchPlayers)
	for range 200 {
		s.tickSurvival()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickSurvival()
	}
}

// BenchmarkTickSurvivalHerd is BenchmarkTickSurvivalIdle beside two thousand
// cows standing clear of every player.
func BenchmarkTickSurvivalHerd(b *testing.B) {
	s, clients := newIdlePlayersBenchServer(b, idleBenchPlayers)
	spawn := clients[0].player.Position()
	dim := s.allDims()[0]
	for i := range 2000 {
		x, z := spawn.X+8+float64(i%50), spawn.Z-25+float64(i/50)
		s.spawnMobInDim(dim, entity.TypeCow, x, world.GroundLevel, z, int64(i), nil)
	}
	if n := len(s.herdOf(dim.w).herd); n != 2000 {
		b.Fatalf("herd holds %d mobs, want 2000", n)
	}
	for range 200 {
		s.tickSurvival()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickSurvival()
	}
}

// BenchmarkTickSurvivalOnSculk is BenchmarkTickSurvivalIdle with every player
// standing on a sculk sensor, so every tick schedules each one's step.
func BenchmarkTickSurvivalOnSculk(b *testing.B) {
	s, clients := newIdlePlayersBenchServer(b, idleBenchPlayers)
	sensor := registry.BlockSculkSensor.DefaultState()
	for _, c := range clients {
		pos := c.player.Position()
		s.world.SetBlock(int32(math.Floor(pos.X)), world.GroundLevel, int32(math.Floor(pos.Z)), sensor)
		// A sensor is half a block tall.
		pos.Y, pos.OnGround = world.GroundLevel+0.5, true
		c.player.SetPosition(pos)
	}
	for range 200 {
		s.tickSurvival()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickSurvival()
	}
}

// BenchmarkTickSurvivalIdleGeared is BenchmarkTickSurvivalIdle with every
// inventory slot of every player holding a different item.
func BenchmarkTickSurvivalIdleGeared(b *testing.B) {
	s, clients := newIdlePlayersBenchServer(b, idleBenchPlayers)
	idleBenchGear(clients)
	for range 200 {
		s.tickSurvival()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickSurvival()
	}
}

// BenchmarkTickPlayerTrackingIdle runs the players' tracking pass over a
// hundred players standing still in sight of one another.
func BenchmarkTickPlayerTrackingIdle(b *testing.B) {
	s, _ := newIdlePlayersBenchServer(b, idleBenchPlayers)
	for range 20 {
		s.tickPlayerTracking()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickPlayerTracking()
	}
}

// BenchmarkTickMapsNoMapsFrames runs the map pass of a hundred players who
// hold no map, in a world of two thousand empty item frames.
func BenchmarkTickMapsNoMapsFrames(b *testing.B) {
	s, clients := newIdlePlayersBenchServer(b, idleBenchPlayers)
	w := s.allDims()[0].w
	for i := range 2000 {
		f := newFrame(s.nextEntity(), uuid.New(), w, int32(i%50), 80+int32(i/50), 5, 3, false)
		f.checkInterval = -1 << 30
		s.registerFrame(f)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, c := range clients {
			c.tickMaps()
		}
	}
}

// BenchmarkReadMobTickInputsPlayers reads the mob tick's inputs for a
// hundred players and no mobs.
func BenchmarkReadMobTickInputsPlayers(b *testing.B) {
	s, _ := newIdlePlayersBenchServer(b, idleBenchPlayers)
	b.Logf("entity.Player is %d bytes", unsafe.Sizeof(entity.Player{}))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		in := s.resetMobTickInputs()
		s.readMobTickInputs(in)
	}
}

// BenchmarkReadMobTickInputsPlayersParrot is BenchmarkReadMobTickInputsPlayers
// with one tamed parrot owned by one of the players, who alone get the water
// and snow facts.
func BenchmarkReadMobTickInputsPlayersParrot(b *testing.B) {
	s, clients := newIdlePlayersBenchServer(b, idleBenchPlayers)
	dim := s.allDims()[0]
	id := s.spawnMobInDim(dim, entity.TypeParrot, 0, float64(world.GroundLevel), 0, 1, nil)
	s.mobsMu.Lock()
	if ent := s.mobByEntityIDLocked(dim.w, id); ent != nil {
		ent.mob.Tamable.Tame, ent.mob.Tamable.Owner = true, clients[0].player.UUID
	}
	s.mobsMu.Unlock()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		in := s.resetMobTickInputs()
		s.readMobTickInputs(in)
	}
}
