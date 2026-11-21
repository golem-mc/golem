package servertest

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Every server the harness starts is on the stone flat world unless a test
// names a level-type of its own.
func init() { server.InstallTestWorld() }

// Client drives a server the way a real client would.
type Client struct {
	T    *testing.T
	Conn *protocol.Conn
	// Raw is the connection under the protocol framing, for the tests that
	// split a packet across a keep-alive event and have to write the two
	// halves by hand.
	Raw net.Conn
	// Served is closed once the server has finished with this connection, so
	// that a test can wait for a player to be saved and gone.
	Served <-chan struct{}
	// lastUpdateTime holds the data of an Update Time packet that
	// ExpectTimeFeedback captured while looking for a System Chat message.
	// The broadcast rides the mailbox on the loop's clock, so it can fall
	// between any two packets; ExpectUpdateTime consumes the captured packet
	// before reading from the connection, so the two helpers work in either
	// order.
	lastUpdateTime []byte
	// pendingTeleport holds the position a Synchronize Player Position put
	// the body at while AwaitCommandFeedback read it looking for a command's
	// feedback. The teleport and the feedback both ride the mailbox, but a
	// reader waiting on one keeps the other for the read after it;
	// ExpectTeleport takes this one before reading the connection.
	pendingTeleport *player.Position
	// pos is where the client's body stands, with the pitch clamped the way
	// the game clamps it. A relative Synchronize Player Position resolves
	// against it, as the client applies the offsets to its own body.
	pos player.Position
	// LastTeleport is the position the last Synchronize Player Position
	// resolved to before the pitch clamp, which is the value the packet
	// carries and what ExpectTeleport reports.
	LastTeleport player.Position
	// PlayerEntityID is the entity ID the login packet gave this player's
	// own body, read in passing on the join. The mob tests' readers skip
	// the metadata a player's own appearance turning broadcasts — theirs,
	// not a mob's — and skip it by this ID.
	PlayerEntityID int32
	// players and mobs are the entity IDs of the other players and the mobs
	// whose spawn a reader took. Their metadata (a pose, the hurt of a
	// fall) can fall in front of the next spawn a reader waits for; a new
	// body's metadata never comes before its spawn.
	players, mobs map[int32]bool
	// bodies are every player body this client has been sent, and
	// SeePlayerAttributes lets the players' Update Attributes through:
	// otherwise ReadBy drops them, the background a held sword or a worn
	// piece sends whenever it changes.
	bodies              map[int32]bool
	SeePlayerAttributes bool
	// Playing is set once Configure has seen the play phase begin.
	Playing bool
	// streamTimeout overrides StreamTimeout when set, for the test that
	// checks Stream fails rather than hangs.
	streamTimeout time.Duration
	// WatchBook lets this client read the Recipe Book Add packets every
	// other test steps over, for the tests that watch the book itself.
	WatchBook bool
	// WatchAdvancements lets this client read the Update Advancements
	// packets every other test steps over: a held item or a dimension change
	// can award one at any tick.
	WatchAdvancements bool
	// WatchWaypoints lets a reader see the Waypoint packets; until it is
	// set they wait in WaypointBacklog for trackWaypoint, since the locator
	// bar's tracks can land in the middle of a join.
	WatchWaypoints bool
	// WatchBundles lets a reader see the Bundle Delimiters; until it is set
	// they are dropped, as a client applying a bundle does not surface them.
	WatchBundles    bool
	WaypointBacklog []PushedPacket
	// Pushback holds packets a reader stepped over that a later reader may
	// still be waiting for, served before the connection is read again.
	// AwaitCommandFeedback fills it, so a command's effect read while
	// looking for its feedback is not lost.
	Pushback []PushedPacket
	// ViewAsk is the render distance this fake client answers the server's
	// settings request with; the pacing tests raise it before joining.
	ViewAsk int8
	// Unlisted makes the Client Information say the player disallows
	// server listings.
	Unlisted bool
	// NoBatchAcks turns off the acknowledgements ReadBy answers a finished
	// chunk batch with, for the tests that watch the server stop without
	// them.
	NoBatchAcks bool
	// JoinChunks keeps the Chunk Data payloads a join read, by chunk position,
	// for the tests that tell one dimension's chunk from another's.
	JoinChunks map[[2]int32][]byte
	// ChunkRadius is the view radius in chunks the join's Login named,
	// bounded by ViewAsk.
	ChunkRadius int32
}

// BlockTestConfig is the configuration the block change tests run in:
// creative mode, where placing does not cost the held stack and breaking
// drops nothing. The block tests are about what reaches the wire and the
// world, not about the survival inventory, and in survival every break
// would put an item entity broadcast in the middle of what they are
// watching for.
func BlockTestConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	return cfg
}

// generationSlots bounds the tests that make the server generate nether or
// End terrain at once. A crossing generates its arrival on the tick, and each
// chunk shapes a 5x5 of terrain, so with every parallel test crossing the
// process-wide generation queue outlasts the 15 s a tick may take.
var generationSlots = make(chan struct{}, max(1, runtime.GOMAXPROCS(0)/2))

// LimitGeneration waits for one of the generation slots and holds it until
// the test ends. A test calls it at most once, or it can deadlock on its own slot.
func LimitGeneration(t *testing.T) {
	t.Helper()
	generationSlots <- struct{}{}
	t.Cleanup(func() { <-generationSlots })
}

// CrossingTestConfig is BlockTestConfig's world with the smallest view and no
// structures, for the tests that carry a body or a player into the nether or
// the End: a crossing generates its arrival's terrain, and a test that needs
// neither a view of it nor a fortress should not pay for them. It takes the
// test's generation slot.
func CrossingTestConfig(t *testing.T) *config.Config {
	t.Helper()
	LimitGeneration(t)
	cfg := BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.GenerateStructures = false
	return cfg
}

// SurvivalTestConfig is BlockTestConfig's world with the players in
// survival, which the tests that pay levels or wear tools run in: the
// anvil's take spends and wears only there, and nothing in those tests
// breaks a block for survival's spills to interrupt.
func SurvivalTestConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// Start creates a server and returns it with a function that connects a new
// client to it. Each setup function runs before any connection is taken.
//
// Every client gets a listener of its own, which is the least fiddly way to
// know that the server has picked up a particular connection.
// SoundEventName reads the name a sound event's numeric ID travels under, for
// the tests that name a captured sound ID in their output.
func SoundEventName(id int32) (string, bool) {
	if id < 0 || int(id) >= len(registry.SoundEvents) {
		return "", false
	}
	return registry.SoundEvents[id], true
}

// NewTestServer is the server half of start, for the tests that bring their
// own connection to HandleConnection rather than the harness's TCP dial.
func NewTestServer(t *testing.T, cfg *config.Config, setup ...func(*server.Server)) *server.Server {
	t.Helper()
	return NewTestServerWithPlugins(t, cfg, nil, setup...)
}

// NewTestServerWithPlugins injects registrations without changing the global plugin list.
func NewTestServerWithPlugins(t *testing.T, cfg *config.Config, plugins []plugin.Registration, setup ...func(*server.Server)) *server.Server {
	t.Helper()

	// Players are written to disk under the world directory, so a test left on
	// the default would litter the package directory with save files and hand
	// the next run whatever the last one saved.
	if cfg.LevelName == config.Default().LevelName {
		cfg.LevelName = t.TempDir()
	}

	// A server with an ops list and the default spawn-protection refuses block
	// edits by non-ops within 16 blocks of spawn, where nearly every test
	// edits. A test of the protection puts its radius back with
	// SpawnProtectionRadius.
	cfg.SpawnProtection = 0

	// A paused server stops advancing TicksRun, so a playerless test that
	// waits past the default 60 seconds would hang. A test of the pause sets
	// its own value.
	if cfg.PauseWhenEmptySeconds == config.Default().PauseWhenEmptySeconds {
		cfg.PauseWhenEmptySeconds = 0
	}

	cfg.SyncChunkWrites = false // fsyncing every chunk write only slows the tests down

	// The tests in this package break and place blocks at fixed heights
	// around spawn — the flat world's stone up to y=63 — and put players
	// down at (0.5, 64, 0.5), so they are generated on the flat world
	// whatever the default generator is: the overworld gives every seed
	// different ground. cfg is mutated in place here for the same reason
	// as above, so a server restarted from the same cfg (blockIsSaved,
	// TestPlayerPositionSurvivesARestart) is flat the same way. What stands
	// in is the test seam's own level-type, which the server maps to the
	// stone flat generator; a test that named a level-type of its own keeps
	// it.
	if cfg.LevelType == config.Default().LevelType {
		cfg.LevelType = server.TestFlatLevelType
	}

	// The fake client asks for a render distance of 4, so at the default
	// simulation distance of 10 the server would generate, light and save
	// 360 chunks per join that no test sees. A test that set another
	// distance keeps it; one that needs the default's reach passes
	// FullSimulationDistance, since 10 here reads as unset.
	if cfg.SimulationDistance == config.Default().SimulationDistance {
		cfg.SimulationDistance = 4
	}

	// A test on the harness's flat world takes its nether chunks from a
	// process-wide fixture, so each is generated once per package rather
	// than once per test: generating their own, the crossing tests of a
	// crowded runner all stalled together. Every flat world crosses into the
	// SharedNetherSeed nether, whatever level seed it keeps.
	newServer := server.NewSeeded
	if cfg.LevelType == server.TestFlatLevelType {
		newServer = func(cfg *config.Config, seed int64, plugins ...plugin.Registration) (*server.Server, error) {
			return server.NewSeededSharingNether(cfg, seed, server.SharedNetherSeed, plugins...)
		}
	}
	srv, err := newServer(cfg, RootSeed(t, cfg), plugins...)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	// Every join writes usercache.json, so the cache goes to a directory of
	// the server's own rather than the working directory the package's tests
	// share. A test that seeds or inspects the cache points it elsewhere.
	srv.SetUsercacheFile(filepath.Join(t.TempDir(), "usercache.json"))
	// The natural spawn pass is off: the tests that are not about it were
	// written against a world where nothing turns up unasked, and a monster
	// the spawn put in a dark pocket would walk into the middle of whatever
	// a test is waiting on. A test of the spawn turns it back on, SpawnPass
	// along with it.
	srv.SetSpawning(false)
	// The move checks are off for the same reason: the tests put a body
	// anywhere with a single move, into a wall or across the world, and a
	// test of the checks turns them back on.
	srv.SetMoveChecks(false)
	// The spawn search follows the respawn_radius rule, and vanilla runs it
	// at its default of ten: a fresh join or a bare respawn lands anywhere
	// in the square it draws. The tests' own contract puts every body at the
	// spawn block — the comment at the top of this function says where — so
	// the rule is pinned to a radius of none, and the tests of the search
	// set their own.
	srv.SetGameRuleValue("respawn_radius", 0)
	// A test's world is generated afresh, and saving each of its chunks once
	// would cost a fifth of the tests' CPU for nothing most tests read back.
	srv.SetDropUntouchedChunks(true)
	// The date-bound spawn rolls read the calendar, so a test that spawns a
	// zombie would wear a pumpkin on October 31. The Halloween tests set
	// their own date through setup.
	srv.SetSpawnClock(func() time.Time { return time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC) })
	srv.SetKeepAliveTimeout(DefaultKeepAliveTimeout)
	for _, s := range setup {
		s(srv)
	}

	// Every test server ticks, the way a listened one does: the keep-alive
	// exchange and the auto-save only run on the main loop. The default
	// intervals are long enough that the loop only costs a sleeping goroutine
	// to the tests that do not set one.
	srv.StartTick()

	// testing keeps a finished test's cleanups reachable until the whole
	// package is done (a popped cleanup stays in the slice's backing
	// array), so these reach the server through held, which the last of
	// them clears: otherwise every finished test's server, worlds and all,
	// stays in memory, and its region files and session lock stay open.
	// Registered first, this runs last: after the loop's stop and the
	// saves' flush.
	held := srv
	t.Cleanup(func() {
		if err := held.CloseWorlds(); err != nil {
			t.Errorf("close worlds: %v", err)
		}
		held = nil
	})

	// Players leaving release their chunks, which queues the dirty ones for
	// an async save. Wait for those saves before the temporary world
	// directory is removed, or its removal races a region file being written.
	// Registered in reverse of shutdown order: StopTick runs first, then
	// DisablePlugins (so plugins release tickets and other resources before
	// saves flush), then FlushSaves, then CloseWorlds.
	t.Cleanup(func() { held.FlushSaves() })
	t.Cleanup(func() { held.DisablePlugins() })
	t.Cleanup(func() {
		if err := held.ChunkIndexDrift(); err != nil {
			t.Errorf("chunk index: %v", err)
		}
	})
	t.Cleanup(func() { held.StopTick() })
	return srv
}

func Start(t *testing.T, cfg *config.Config, setup ...func(*server.Server)) (*server.Server, func() *Client) {
	t.Helper()
	srv := NewTestServer(t, cfg, setup...)

	connect := func() *Client {
		t.Helper()
		return DialServer(t, srv)
	}

	return srv, connect
}

// DialServer connects a client to a server the test built itself, over a
// listener of its own.
func DialServer(t *testing.T, srv *server.Server) *Client {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	served := make(chan struct{})
	go func() {
		defer close(served)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		srv.HandleConnection(conn)
	}()

	// No dial deadline: a loopback connect completes in the kernel, so a
	// deadline only measures how long a loaded runner starved this process;
	// the test timeout still bounds it.
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		conn.Close()
		// Wait for the connection handler to finish, so that the player is
		// gone from the server before the test inspects it.
		<-served
	})

	return &Client{T: t, Raw: conn, Conn: protocol.NewConn(conn), Served: served, ViewAsk: 4}
}

// SpawnProtectionRadius is a setup that sets the spawn-protection radius
// NewTestServer zeroes.
func SpawnProtectionRadius(radius int) func(*server.Server) {
	return func(s *server.Server) { s.SetSpawnProtectionRadius(radius) }
}

// FullSimulationDistance is a setup that puts back the default simulation
// distance Start lowers, for the tests of what lies past the view.
func FullSimulationDistance(s *server.Server) {
	s.SetSimulationDistance(config.Default().SimulationDistance)
}

// JoinAtViewEdge joins a watcher and stands it four chunks east of the
// origin, where the chunks of column 0 lie on the border of the view of four
// it asks for: the chunks whose Update Light a player is sent, its client
// lighting the others itself. It returns once the column west of them has
// been unloaded, which is when the server counts them on the border.
// vanilla: ChunkMap.isChunkOnTrackedBorder
func JoinAtViewEdge(t *testing.T, connect func() *Client, name string) *Client {
	t.Helper()
	w := connect().Join(name)
	w.MoveTo(4*16+8.5, 64, 8.5, 0, 0)
	for id, data := range w.Packets(JoinTimeout) {
		if id != protocol.IDPlayUnloadChunk {
			continue
		}
		r := protocol.NewReader(data)
		if z, x := r.ReadI32(), r.ReadI32(); x == -1 && z >= -1 && z <= 1 {
			return w
		}
	}
	t.Fatalf("%s's view never left column -1", name)
	return nil
}

// Disconnect closes the connection and waits until the server has finished with
// it, which is what a test does when it cares about what leaving did: the player
// is off the player list and written to disk by the time this returns.
//
// It half-closes and reads the server out to its own close rather than
// closing outright: a close with the server's packets still unread resets
// the connection, and a reset can throw away the client's last packets
// before the server has read them.
func (c *Client) Disconnect() {
	c.T.Helper()
	if tcp, ok := c.Raw.(*net.TCPConn); ok && tcp.CloseWrite() == nil {
		_ = tcp.SetReadDeadline(time.Now().Add(ExpectTimeout))
		_, _ = io.Copy(io.Discard, tcp)
	}
	c.Conn.Close()
	<-c.Served
}

// Dial starts a server on a random port and connects a single client to it.
func Dial(t *testing.T, cfg *config.Config, setup ...func(*server.Server)) (*server.Server, *Client) {
	t.Helper()
	srv, connect := Start(t, cfg, setup...)
	return srv, connect()
}

// Join is Dial followed by Client.Join: a server with one player in its world.
func Join(t *testing.T, cfg *config.Config, name string, setup ...func(*server.Server)) (*server.Server, *Client) {
	t.Helper()
	srv, c := Dial(t, cfg, setup...)
	c.Join(name)
	return srv, c
}

// JoinAs is Dial followed by Client.JoinAs, and returns the player's own
// entity ID with the server and client.
func JoinAs(t *testing.T, cfg *config.Config, name string, setup ...func(*server.Server)) (*server.Server, *Client, int32) {
	t.Helper()
	srv, c := Dial(t, cfg, setup...)
	return srv, c, c.JoinAs(name)
}

// Run runs f as the subtest name of t with c's failures landing on that
// subtest, and restores c.T afterwards; subtests sharing c run in turn.
func (c *Client) Run(t *testing.T, name string, f func(t *testing.T)) bool {
	t.Helper()
	parent := c.T
	return t.Run(name, func(t *testing.T) {
		c.T = t
		defer func() { c.T = parent }()
		f(t)
	})
}

// Join runs Login, Configure and EnterWorld under one name and returns c.
func (c *Client) Join(name string) *Client {
	c.T.Helper()
	c.Login(name)
	c.Configure()
	c.EnterWorld(name)
	return c
}

// JoinKeeping is Join with every packet of the join kept for the reads
// after, for a test awaiting what the server sends once it treats the client
// as loaded (a file's RootVehicle remounted): on a loaded machine the
// join's read can outlast clientLoadedTimeoutTimer's 60 ticks, so that
// arrives before the join's last chunk batch.
func (c *Client) JoinKeeping(name string) *Client {
	c.T.Helper()
	c.Login(name)
	c.Configure()
	var joined []PushedPacket
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayLogin && c.PlayerEntityID == 0 {
			c.PlayerEntityID = protocol.NewReader(data).ReadI32()
		}
		joined = append(joined, PushedPacket{Id: id, Data: append([]byte(nil), data...)})
	})
	c.MarkLoaded()
	c.Unread(joined...)
	return c
}

// JoinAs runs Login, Configure and EnterWorldAs under one name and returns
// the player's own entity ID.
func (c *Client) JoinAs(name string) int32 {
	c.T.Helper()
	c.Login(name)
	c.Configure()
	return c.EnterWorldAs(name)
}

// AnswerKeepAlive reads a keep-alive packet's ID and sends it back, the way a
// client that is still listening does.
func (c *Client) AnswerKeepAlive(data []byte) {
	c.T.Helper()
	id := protocol.NewReader(data).ReadI64()
	c.Send(protocol.IDPlayKeepAliveIn, func(b *protocol.Buffer) { b.PutI64(id) })
}

// Send writes a packet built by fill.
func (c *Client) Send(packetID int32, fill func(b *protocol.Buffer)) {
	c.T.Helper()
	var b protocol.Buffer
	if fill != nil {
		fill(&b)
	}
	if err := c.Conn.WritePacket(packetID, b.Bytes()); err != nil {
		c.T.Fatalf("write packet 0x%02X: %v", packetID, err)
	}
}

// Expect reads the next packet and fails unless it has the expected ID. An
// Update Time is stepped over unless it is the one wanted: the loop sends it
// on its own clock, so it can fall between any two packets. No login or
// configuration packet has its ID.
func (c *Client) Expect(packetID int32) []byte {
	c.T.Helper()
	gotID, data, err := c.Read()
	for err == nil && gotID == protocol.IDPlayUpdateTime && packetID != protocol.IDPlayUpdateTime {
		gotID, data, err = c.Read()
	}
	if err != nil {
		c.T.Fatalf("read packet 0x%02X: %v", packetID, err)
	}
	if gotID != packetID {
		c.T.Fatalf("got packet 0x%02X, want 0x%02X", gotID, packetID)
	}
	return data
}

// Handshake opens the connection with the given intent.
func (c *Client) Handshake(nextState int32) {
	c.T.Helper()
	c.Send(protocol.IDHandshake, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.Version)
		b.PutString("localhost")
		b.PutI16(25565)
		b.PutVarInt(nextState)
	})
}

// Login runs the login phase in offline mode and returns the player's UUID.
func (c *Client) Login(name string) uuid.UUID {
	c.T.Helper()
	return c.LoginWithIntent(name, protocol.NextStateLogin)
}

// LoginWithIntent is login under the handshake intent the client opens with,
// so that a transfer connection can be driven through the same sequence.
func (c *Client) LoginWithIntent(name string, intent int32) uuid.UUID {
	c.T.Helper()

	c.Handshake(intent)
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
		b.PutString(name)
		b.PutUUID(uuid.Nil)
	})

	// Set Compression arrives only while the server has compression on: a
	// network-compression-threshold in the negative range leaves the login
	// success to follow the login start directly.
	packetID, data, err := c.Read()
	if err != nil {
		c.T.Fatalf("read login: %v", err)
	}
	if packetID == protocol.IDSetCompression {
		c.Conn.EnableCompression(int(protocol.NewReader(data).ReadVarInt()))
		data = c.Expect(protocol.IDLoginSuccess)
	} else if packetID != protocol.IDLoginSuccess {
		c.T.Fatalf("got packet 0x%02X, want set compression or login success", packetID)
	}

	r := protocol.NewReader(data)
	playerUUID := r.ReadUUID()
	if got := r.ReadString(16); got != name {
		c.T.Errorf("login success name = %q, want %q", got, name)
	}
	r.SkipProfileProperties()
	r.ReadUUID() // session ID
	if err := r.Err(); err != nil {
		c.T.Fatalf("login success: %v", err)
	}

	c.Send(protocol.IDLoginAcknowledged, nil)
	return playerUUID
}

// Configure runs the configuration phase, agreeing to the server's data pack.
func (c *Client) Configure() {
	c.T.Helper()

	// The server introduces itself before asking about data packs.
	brand := protocol.NewReader(c.Expect(protocol.IDConfigPluginMessage))
	if got := brand.ReadString(32767); got != "minecraft:brand" {
		c.T.Errorf("first plugin message channel = %q, want minecraft:brand", got)
	}

	r := protocol.NewReader(c.Expect(protocol.IDKnownPacks))
	count := r.ReadVarInt()
	if count != 1 {
		c.T.Fatalf("known packs = %d, want 1", count)
	}
	namespace, id, version := r.ReadString(32767), r.ReadString(32767), r.ReadString(32767)
	if namespace != "minecraft" || id != "core" {
		c.T.Fatalf("known pack = %s:%s, want minecraft:core", namespace, id)
	}
	if version != registry.Version {
		c.T.Fatalf("known pack version = %s, want %s", version, registry.Version)
	}

	c.Send(protocol.IDClientInformation, func(b *protocol.Buffer) {
		b.PutString("en_GB")
		b.PutI8(c.ViewAsk) // render distance
		b.PutVarInt(0)
		b.PutBool(true)
		b.PutU8(0x7F)
		b.PutVarInt(1)
		b.PutBool(false)
		b.PutBool(!c.Unlisted)
		b.PutVarInt(0)
	})
	c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString(namespace)
		b.PutString(id)
		b.PutString(version)
	})

	// Registries and tags arrive next, ending in Finish Configuration.
	registries := map[string]bool{}
	for {
		packetID, data, err := c.Read()
		if err != nil {
			c.T.Fatalf("read configuration packet: %v", err)
		}
		switch packetID {
		case protocol.IDRegistryData:
			registries[protocol.NewReader(data).ReadString(32767)] = true
		case protocol.IDConfigUpdateTags, protocol.IDFeatureFlags:
		case protocol.IDConfigFinish:
			for _, want := range []string{"minecraft:dimension_type", "minecraft:worldgen/biome", "minecraft:damage_type"} {
				if !registries[want] {
					c.T.Errorf("server did not send the %s registry", want)
				}
			}
			c.Send(protocol.IDConfigFinishAck, nil)
			c.Playing = true
			return
		default:
			c.T.Fatalf("unexpected configuration packet 0x%02X", packetID)
		}
	}
}

// EnterWorld reads through the join sequence and closes the loading screen,
// leaving the client in the world with nothing of its own left to receive.
func (c *Client) EnterWorld(name string) {
	c.ReadThroughJoin(name)
	c.MarkLoaded()
}

// ReadThroughJoin reads through the join sequence up to the chunk stream's
// end, with the loading screen still up: the player is in the world but the
// server keeps them out of harm's way until MarkLoaded says otherwise. A test
// that has to make the ground safe before a body arrives into it - one whose
// player file restores them where the flat world has stone - reads this far,
// shapes the world, and only then calls MarkLoaded.
func (c *Client) ReadThroughJoin(name string) {
	c.T.Helper()
	c.ReadJoin(func(packetID int32, data []byte) {
		if packetID == protocol.IDPlayLogin && c.PlayerEntityID == 0 {
			// The player's own entity ID rides at the head of the login
			// packet, which is what tells the mob readers the metadata
			// about this body from the metadata about a mob's.
			c.PlayerEntityID = protocol.NewReader(data).ReadI32()
		}
	})
}

// JoinTimeout bounds the wait for a join's chunk stream from its latest chunk.
const JoinTimeout = ExpectTimeout

// LoadedPing is the ping payload MarkLoaded waits on.
const LoadedPing = 0x10AD

// MarkLoaded closes the loading screen the way the vanilla client does,
// with Player Loaded — until then the server keeps the player out of harm's
// way — and waits for the server to have taken it in: a ping sent behind it
// on the same connection comes back only after it. Whatever else arrives
// meanwhile is kept for the reads after, in order, so a test that acts on
// another connection the moment this returns (a /kill, a hit) finds the
// player already loaded.
func (c *Client) MarkLoaded() {
	c.T.Helper()
	c.Send(protocol.IDPlayLoaded, func(*protocol.Buffer) {})
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(LoadedPing) })
	var kept []PushedPacket
	for packetID, data := range c.Packets(ExpectTimeout) {
		if IsPingAnswer(packetID, data, LoadedPing) {
			c.Unread(kept...)
			return
		}
		kept = append(kept, PushedPacket{Id: packetID, Data: data})
	}
	c.T.Fatalf("the loaded ping did not come back within %s", ExpectTimeout)
}

// ExpectPlay reads packetID, answering keep-alives and skipping background
// world and entity updates that can arrive between a command and its reply.
func (c *Client) ExpectPlay(packetID int32) []byte {
	c.T.Helper()
	// A chest's lid action, which vanilla re-sends every five ticks while
	// the chest is open, can land in front of what is awaited; the ones
	// stepped over are kept for whoever reads the lids next.
	var lids []PushedPacket
	defer func() { c.Unread(lids...) }()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		if gotID == packetID {
			return data
		}
		switch gotID {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayBlockEntityData,
			protocol.IDPlayUpdateLight, protocol.IDPlayUpdateTime:
			// A fill's batched section update is the same noise as a
			// single block update.
		case protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
			protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
			protocol.IDPlayUnloadChunk:
			// The paced chunk stream behind a move still owed its view;
			// background to anything else a test waits for.
		case protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect,
			protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata,
			protocol.IDPlaySetHealth, protocol.IDPlayUpdateAttributes,
			protocol.IDPlayDamageEvent, protocol.IDPlaySetEntityMotion,
			protocol.IDPlayEntityAnimation, protocol.IDPlaySwingAnimation, protocol.IDPlayLevelParticles,
			protocol.IDPlayEntityEvent, protocol.IDPlayHurtAnimation,
			protocol.IDPlayPickupItem, protocol.IDPlayRemoveEntities:
			// A summon's spawn notice, a drifting body's motion, a blow's
			// effects: noise to a test waiting for an answer.
		case protocol.IDPlayUpdateTags, protocol.IDPlayUpdateRecipes,
			protocol.IDPlayRecipeBookSettings:
			// What a data pack reload sends every player again.
		case protocol.IDPlayAckBlockChange:
			// The tick end's answer to an interaction of this client's own:
			// it rides behind the updates at whatever tick the loop has
			// run, so it can land in front of anything a test waits for.
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity:
		case protocol.IDPlayPlayerInfoUpdate:
			// The latency refresh the tab list gets on the loop's clock,
			// which a test slowed past it by a loaded machine sees.
		case protocol.IDPlayBlockAction:
			lids = append(lids, PushedPacket{Id: gotID, Data: data})
		default:
			c.T.Fatalf("got packet 0x%02X, want 0x%02X", gotID, packetID)
		}
	}
	c.T.Fatalf("packet 0x%02X did not arrive within %s", packetID, ExpectTimeout)
	return nil
}

// Chat says something, in the shape a vanilla client sends an unsigned message.
func (c *Client) Chat(message string) {
	c.T.Helper()
	if err := c.WriteChat(message); err != nil {
		c.T.Fatalf("write packet 0x%02X: %v", protocol.IDPlayChatMessage, err)
	}
}

// WriteChat writes one unsigned chat message and returns the write error, for
// a caller whose message may land on a connection the server has reset.
func (c *Client) WriteChat(message string) error {
	var b protocol.Buffer
	b.PutString(message)
	b.PutI64(0)      // timestamp
	b.PutI64(0)      // salt
	b.PutBool(false) // no signature
	b.PutVarInt(0)   // none of the server's messages acknowledged
	b.PutRaw([]byte{0, 0, 0})
	b.PutU8(1) // the checksum of an empty acknowledgement set
	return c.Conn.WritePacket(protocol.IDPlayChatMessage, b.Bytes())
}

// ExpectAnnouncement fails unless the next thing the client hears is the server
// announcing that a player came or went.
func (c *Client) ExpectAnnouncement(key, player string) {
	c.T.Helper()
	data := c.ExpectPlay(protocol.IDPlaySystemChat)

	var want protocol.Buffer
	want.PutText(protocol.Text{
		Translate: key,
		With:      []protocol.Text{PlayerDisplayComponent(player)},
		Color:     protocol.ColorYellow,
	})
	want.PutBool(false) // Chat, not the action bar
	if !bytes.Equal(data, want.Bytes()) {
		c.T.Errorf("system chat = % X, want % X", data, want.Bytes())
	}
}

// ExpectChat fails unless the next thing the client hears is a player
// speaking: an unsigned Player Chat Message under the plain chat type, naming
// the sender by profile. The message goes out under the timestamp the
// client's own packet carried, which the harness writes as zero.
func (c *Client) ExpectChat(sender, message string) {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		if packetID != protocol.IDPlayPlayerChat {
			continue
		}
		ParsePlayerChat(c.T, data, "minecraft:chat", sender,
			protocol.OfflineUUID(sender), message, protocol.Text{Text: message}, "", ServerStampedChatTimestamp)
		return
	}
	c.T.Fatalf("no player chat message arrived within %s", ExpectTimeout)
}

// ExpectPlayerAdded fails unless the next thing the client hears is another
// player arriving: their profile for the tab list, and then their body. It
// returns the entity ID that body was spawned with.
func (c *Client) ExpectPlayerAdded(name string) int32 {
	c.T.Helper()

	info := protocol.NewReader(c.ExpectPlay(protocol.IDPlayPlayerInfoUpdate))
	info.ReadU8() // actions
	if got := info.ReadVarInt(); got != 1 {
		c.T.Fatalf("player info entries = %d, want 1", got)
	}
	playerUUID := info.ReadUUID()
	if want := protocol.OfflineUUID(name); playerUUID != want {
		c.T.Errorf("player info UUID = %s, want %s", playerUUID, want)
	}
	if got := info.ReadString(16); got != name {
		c.T.Errorf("player info name = %q, want %q", got, name)
	}

	spawn := protocol.NewReader(c.ExpectPlay(protocol.IDPlaySpawnEntity))
	entityID := spawn.ReadVarInt()
	if c.players == nil {
		c.players = map[int32]bool{}
	}
	c.players[entityID] = true
	if got := spawn.ReadUUID(); got != playerUUID {
		c.T.Errorf("spawn entity UUID = %s, want %s", got, playerUUID)
	}
	if got := spawn.ReadVarInt(); got != protocol.EntityTypePlayer {
		c.T.Errorf("spawn entity type = %d, want %d", got, protocol.EntityTypePlayer)
	}
	if err := spawn.Err(); err != nil {
		c.T.Fatalf("spawn entity: %v", err)
	}
	if entityID == 0 {
		c.T.Error("spawn entity ID = 0, want a real entity ID")
	}

	// What the player is doing follows their body, so that someone who was
	// already sneaking is not drawn standing up.
	if got := c.ExpectMetadata(); got.EntityID != entityID {
		c.T.Errorf("metadata for entity %d, want %d", got.EntityID, entityID)
	}

	// What the player is wearing and holding comes with the body, so that a
	// player joining in armour is drawn wearing it.
	c.ExpectEquipment(entityID)
	return entityID
}

// EntityMetadata is what a Set Entity Metadata packet says about a player. Only
// the fields this server sends are kept.
type EntityMetadata struct {
	EntityID  int32
	Flags     uint8
	Pose      int32
	SkinParts uint8
	MainHand  int32 // protocol.MainHand*; -1 when the packet omits it
}

// ExpectMetadata fails unless the next thing the client hears is a player's
// state changing, and returns what it was changed to.
func (c *Client) ExpectMetadata() EntityMetadata {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayEntityMetadata))
	m := ParseMetadata(c.T, r)
	if r.Remaining() != 0 {
		c.T.Errorf("entity metadata has %d bytes left over", r.Remaining())
	}
	return m
}

// ParseMetadata reads the pose and the flags a player's own metadata packet
// carries, the way a client keeps the body it watches drawn, from r.
func ParseMetadata(t *testing.T, r *protocol.Reader) EntityMetadata {
	t.Helper()
	m := EntityMetadata{EntityID: r.ReadVarInt()}
	m.Flags, m.Pose, m.SkinParts, m.MainHand = ParseMetadataWithHand(t, r)
	return m
}

// ParseMetadataFields reads the flags, pose and skin parts a player's own
// metadata packet carries, from a reader already past the entity ID.
func ParseMetadataFields(t *testing.T, r *protocol.Reader) (flags uint8, pose int32, skinParts uint8) {
	t.Helper()
	flags, pose, skinParts, _ = ParseMetadataWithHand(t, r)
	return flags, pose, skinParts
}

// BoatPlayerPose reads the pose a metadata packet about the given player
// carries, and -1 for a packet about anything else.
func BoatPlayerPose(t *testing.T, playerEntityID int32, data []byte) int32 {
	t.Helper()
	r := protocol.NewReader(data)
	if r.ReadVarInt() != playerEntityID {
		return -1
	}
	_, pose, _ := ParseMetadataFields(t, r)
	return pose
}

// EquipmentEntry is one slot of the Set Equipment packet: which slot was set
// and what it now holds.
type EquipmentEntry struct {
	Slot  uint8
	Stack protocol.ItemStack
}

func (c *Client) ExpectEquipment(entityID int32) []EquipmentEntry {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayEntityEquipment))
	if got := r.ReadVarInt(); got != entityID {
		c.T.Errorf("equipment entity = %d, want %d", got, entityID)
	}
	var entries []EquipmentEntry
	for {
		raw := r.ReadU8()
		stack, _ := r.ReadSlot()
		entries = append(entries, EquipmentEntry{Slot: raw & 0x7F, Stack: stack})
		if raw&0x80 == 0 {
			break
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("entity equipment: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("entity equipment has %d bytes left over", r.Remaining())
	}
	return entries
}

// EquipEntry finds the stack in the given equipment slot, or an empty stack if
// the slot was not sent.
func EquipEntry(entries []EquipmentEntry, slot uint8) protocol.ItemStack {
	for _, e := range entries {
		if e.Slot == slot {
			return e.Stack
		}
	}
	return protocol.ItemStack{}
}

// ExpectPlayerRemoved fails unless the next thing the client hears is a player
// leaving: their body going first, and then their place on the tab list.
func (c *Client) ExpectPlayerRemoved(entityID int32, name string) {
	c.T.Helper()

	remove := protocol.NewReader(c.ExpectPlay(protocol.IDPlayRemoveEntities))
	if got := remove.ReadVarInt(); got != 1 {
		c.T.Fatalf("removed entities = %d, want 1", got)
	}
	if got := remove.ReadVarInt(); got != entityID {
		c.T.Errorf("removed entity = %d, want %d", got, entityID)
	}

	info := protocol.NewReader(c.ExpectPlay(protocol.IDPlayPlayerInfoRemove))
	if got := info.ReadVarInt(); got != 1 {
		c.T.Fatalf("player info removals = %d, want 1", got)
	}
	if got, want := info.ReadUUID(), protocol.OfflineUUID(name); got != want {
		c.T.Errorf("player info removal UUID = %s, want %s", got, want)
	}
}

// ChatCommand sends a command over the unsigned Chat Command packet, the way
// a client with no chat session does. The command is the text after the
// slash, so "list" for /list.
func (c *Client) ChatCommand(command string) {
	c.T.Helper()
	if err := c.WriteChatCommand(command); err != nil {
		c.T.Fatalf("write packet 0x%02X: %v", protocol.IDPlayChatCommand, err)
	}
}

// WriteChatCommand writes one unsigned chat command and returns the write
// error, for a caller whose command may land on a connection the server has
// reset.
func (c *Client) WriteChatCommand(command string) error {
	var b protocol.Buffer
	b.PutString(command)
	return c.Conn.WritePacket(protocol.IDPlayChatCommand, b.Bytes())
}

// SignedChatCommand sends a command over the Signed Chat Command packet, the
// way a client with a chat session does: its source carries the player's
// signing context, which a chat line it produces travels as a Player Chat
// Message under.
func (c *Client) SignedChatCommand(command string) {
	c.T.Helper()
	c.Send(protocol.IDPlaySignedChatCommand, func(b *protocol.Buffer) {
		b.PutString(command)
		b.PutI64(0)    // timestamp
		b.PutI64(0)    // salt
		b.PutVarInt(0) // no argument signatures
		b.PutVarInt(0) // none of the server's messages acknowledged
		b.PutRaw([]byte{0, 0, 0})
		b.PutU8(1) // the checksum of an empty acknowledgement set
	})
}

// CommandSuggestion requests tab completions for text, which is the full text
// to the left of the cursor including the leading slash. The transaction ID is
// sent back by the server in its response.
func (c *Client) CommandSuggestion(transactionID int32, text string) {
	c.T.Helper()
	c.Send(protocol.IDPlayCommandSuggestion, func(b *protocol.Buffer) {
		b.PutVarInt(transactionID)
		b.PutString(text)
	})
}

// ExpectCommandSuggestions reads a Command Suggestions Response and returns the
// transaction ID, the start, the length, and the matches it carries.
func (c *Client) ExpectCommandSuggestions() (id, start, length int32, matches []string) {
	c.T.Helper()
	data := c.ExpectPlay(protocol.IDPlayCommandSuggestions)
	r := protocol.NewReader(data)
	id = r.ReadVarInt()
	start = r.ReadVarInt()
	length = r.ReadVarInt()
	for range r.ReadVarInt() {
		matches = append(matches, r.ReadString(32767))
		r.ReadBool() // tooltip, which the server does not send
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("command suggestions: %v", err)
	}
	return id, start, length, matches
}

// SetSkinParts reports a change to the settings, the way a client does when the
// player switches a skin layer or their cape off in the options menu. The
// packet carries every setting, not only the one that changed.
func (c *Client) SetSkinParts(parts uint8) {
	c.T.Helper()
	c.SetSettings(parts, protocol.MainHandRight)
}

// PlayerDisplayComponent is the display name the vanilla server's
// Player.getDisplayName hands out for one of its players, which is what the
// feedback of a command that names its target by carries: the name itself,
// with the click that suggests a message to them, the show-entity hover and
// the insertion of the name.
func PlayerDisplayComponent(name string) protocol.Text {
	words := OfflineUUIDWords(name)
	return protocol.Text{
		Text:         name,
		Insertion:    name,
		ClickCommand: "/tell " + name + " ",
		HoverEntity: &protocol.HoverEntity{
			Name: protocol.Text{Text: name},
			ID:   "minecraft:player",
			UUID: words,
		},
	}
}

// TeamPlayerDisplayComponent is PlayerDisplayComponent for a player on a team
// with the given text prefix, no suffix, and colour ("" for none).
// vanilla: PlayerTeam.formatNameForTeam
func TeamPlayerDisplayComponent(name, prefix, color string) protocol.Text {
	text := PlayerDisplayComponent(name)
	text.Text = ""
	text.Color = color
	text.Extra = []protocol.Text{{Text: prefix}, {Text: name}, {}}
	return text
}

// OfflineUUIDWords is an offline player's UUID as the four big-endian words
// the show-entity hover names an entity by.
func OfflineUUIDWords(name string) [4]uint32 {
	id := protocol.OfflineUUID(name)
	var words [4]uint32
	for i := range words {
		words[i] = binary.BigEndian.Uint32(id[i*4 : (i+1)*4])
	}
	return words
}

// EntityDisplayComponent is the display name the vanilla server's
// Entity.getDisplayName hands out for a non-player entity: the type's own
// translation, with the UUID insertion and the show-entity hover that make
// the name pointable and pasteable.
func EntityDisplayComponent(entityType, translation string, id uuid.UUID) protocol.Text {
	var words [4]uint32
	for i := range words {
		words[i] = binary.BigEndian.Uint32(id[i*4 : (i+1)*4])
	}
	return protocol.Text{
		Translate: translation,
		Insertion: id.String(),
		HoverEntity: &protocol.HoverEntity{
			Name: protocol.Text{Translate: translation},
			ID:   entityType,
			UUID: words,
		},
	}
}

// PollUntil polls cond until it holds, failing once timeout of wall time, and
// at least ExpectTimeout, has passed without it. The wait is a stall guard,
// never a way to pass; it is for state that no main-loop tick produces, such
// as a test-owned clock's record or a stopped loop's.
func PollUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	timeout = max(timeout, ExpectTimeout)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

// WaitFor waits for cond, failing once ticks ticks have run without it. A
// stopped loop is polled for the wall time those ticks take at 20 a second.
func WaitFor(t *testing.T, srv *server.Server, ticks int, cond func() bool) {
	t.Helper()
	if srv.TickEnded() == nil {
		PollUntil(t, time.Duration(ticks)*time.Second/20, cond)
		return
	}
	UntilTicks(t, srv, ticks, "condition", cond)
}

// WaitForTicks waits for cond, failing once n ticks have run without it. A
// msg, when given, names what did not happen.
func WaitForTicks(t *testing.T, srv *server.Server, n uint64, cond func() bool, msg ...string) {
	t.Helper()
	what := "condition"
	if len(msg) > 0 {
		what = msg[0]
	}
	UntilTicks(t, srv, int(n), what, cond)
}

// AwaitChunksSettled returns once the chunk senders of the connected players
// have generated and acquired everything around them, the background loading
// of a join's simulation distance done. It waits on the senders' own passes,
// which run on the wall clock beside the tick loop, so it needs no ticks and
// works with the loop held. ChunksSettledTimeout runs from the latest pass
// that left fewer chunks pending, so only a stalled loading fails.
func AwaitChunksSettled(t *testing.T, srv *server.Server) {
	t.Helper()
	pending := srv.ChunksPending()
	deadline := time.NewTimer(ChunksSettledTimeout)
	defer deadline.Stop()
	for pending > 0 {
		beat := srv.ChunkBeat()
		now := srv.ChunksPending()
		if now < pending {
			deadline.Reset(ChunksSettledTimeout)
		}
		if pending = now; pending == 0 {
			return
		}
		select {
		case <-beat:
		case <-deadline.C:
			t.Fatalf("%d chunks still pending, none settled for %s", pending, ChunksSettledTimeout)
		}
	}
}

// SeedPhysics waits out the join's background chunk loading and then seeds
// every dimension's physics engine. The passes roll per loaded chunk, so the
// seed has to land on a settled world, or a chunk arriving between two runs
// of a seeded test moves every die after it.
func SeedPhysics(t *testing.T, srv *server.Server, seed int64) {
	t.Helper()
	AwaitChunksSettled(t, srv)
	srv.SetPhysicsSeed(seed)
}

// AwaitTicks returns once n whole ticks have run since the call.
func AwaitTicks(t *testing.T, srv *server.Server, n uint64) {
	t.Helper()
	RunTicks(t, srv, int(n))
}

// AwaitTicksSince waits for n whole ticks to have run since the tick since
// names rather than since the call, so the packets read in between spend none
// of a window the game's clock opened earlier.
func AwaitTicksSince(t *testing.T, srv *server.Server, since, n uint64) {
	t.Helper()
	for srv.TicksRun() < since+n {
		RunTicks(t, srv, 1)
	}
}

// PlayerFile is where a world keeps what it knows about a player, which is the
// layout a vanilla server uses.
func PlayerFile(level, name string) string {
	return filepath.Join(level, "players", "data", protocol.OfflineUUID(name).String()+".dat")
}

// ReadPlayerFile reads a saved player back, the way the game would.
func ReadPlayerFile(t *testing.T, path string) nbt.Compound {
	t.Helper()
	root, name, err := ReadPlayerFileData(path)
	if err != nil {
		t.Fatalf("read player file: %v", err)
	}
	// The game writes save files with an unnamed root tag, and refuses to read
	// one that carries a name.
	if name != "" {
		t.Errorf("root tag is named %q, want an empty name", name)
	}
	return root
}

// ReadPlayerFileData reads and decodes a saved player file: the root compound
// and the name its root tag carries. It is the core ReadPlayerFile and the
// quietly-reporting form share, differing only in how they report failure.
func ReadPlayerFileData(path string) (nbt.Compound, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, "", err
	}
	defer gz.Close()
	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, "", err
	}

	name, tag, err := nbt.Decode(data)
	if err != nil {
		return nil, name, err
	}
	root, ok := tag.(nbt.Compound)
	if !ok {
		return nil, name, fmt.Errorf("root tag is a %s, want a compound", tag.Type())
	}
	return root, name, nil
}

// Testdata returns the path of a fixture in internal/server/testdata, wherever under
// the module the calling test's package is.
func Testdata(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "internal", "server", "testdata", name)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// InstallPlayerFixture copies a vanilla player file from testdata into the
// world directory a test uses, and returns it decoded. The fixture is the file
// a vanilla server wrote, so the test can check how this server reads it.
func InstallPlayerFixture(t *testing.T, cfg *config.Config, name, fixtureFile string) nbt.Compound {
	t.Helper()
	saved := PlayerFile(cfg.LevelName, name)
	if err := os.MkdirAll(filepath.Dir(saved), 0o755); err != nil {
		t.Fatalf("create world: %v", err)
	}
	fixture, err := os.ReadFile(Testdata(t, fixtureFile))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(saved, fixture, 0o644); err != nil {
		t.Fatalf("write player file: %v", err)
	}
	return ReadPlayerFile(t, saved)
}

// WritePlayerFile writes a player file the way the game stores one, gzipped
// NBT with an unnamed root, for a test to hand a player a state before they
// join.
func WritePlayerFile(t *testing.T, path string, root nbt.Compound) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create world: %v", err)
	}
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("encode player file: %v", err)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(data); err != nil {
		t.Fatalf("compress player file: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("compress player file: %v", err)
	}
	if err := os.WriteFile(path, compressed.Bytes(), 0o644); err != nil {
		t.Fatalf("write player file: %v", err)
	}
}

// SpawnedAt runs the join sequence as far as the teleport the server finishes it
// with, which is where the player is put into the world.
func (c *Client) SpawnedAt() player.Position {
	c.T.Helper()
	if _, ok := ReadUntil(c.T, c, JoinTimeout, protocol.IDPlaySyncPosition); !ok {
		c.T.Fatalf("no synchronize player position within %s", JoinTimeout)
	}
	return c.LastTeleport
}

// MoveTo walks the player somewhere, in the shape a client reports a step.
// A step of more than TeleportSizedMove blocks stands for a teleport, after
// which a real client's next tick reports no move: two tick ends follow
// (one for the tick of the step, one for the quiet tick), so the step is not
// the movement a throw or shot rides. A shorter step sends none, so a test
// can still have its throw ride a walk; use EndTick for any other tick end.
func (c *Client) MoveTo(x, y, z float64, yaw, pitch float32) {
	c.T.Helper()
	c.MoveToGrounded(x, y, z, yaw, pitch, true)
}

// MoveToGrounded is the step MoveTo sends, with the on-ground flag under
// the test's control, the way a client reports a step whose ground state it
// is sure of.
func (c *Client) MoveToGrounded(x, y, z float64, yaw, pitch float32, onGround bool) {
	c.T.Helper()
	var flags uint8
	if onGround {
		flags = 0x01
	}
	c.Send(protocol.IDPlayPositionAndRotation, func(b *protocol.Buffer) {
		b.PutF64(x)
		b.PutF64(y)
		b.PutF64(z)
		b.PutF32(yaw)
		b.PutF32(pitch)
		b.PutU8(flags)
	})
	// The client moves its own body as it reports the step, which is where a
	// following relative teleport resolves from.
	was := c.pos
	c.pos = player.Position{X: x, Y: y, Z: z, Yaw: yaw, Pitch: ClampPitchForTest(pitch)}
	c.LastTeleport = c.pos
	dx, dy, dz := x-was.X, y-was.Y, z-was.Z
	if dx*dx+dy*dy+dz*dz > TeleportSizedMove*TeleportSizedMove {
		c.EndTick()
		c.EndTick()
	}
}

// TeleportSizedMove is the distance in blocks above which MoveToGrounded
// ends the tick like a client that was teleported: no walk, fall or glide a
// real client reports in one tick is so far.
const TeleportSizedMove = 4

// EndTick sends the Client Tick End a client sends as its tick ends. The
// server clears the movement it knows of at a tick end that followed no move
// packet, so a throw or shot stops riding the last step.
// vanilla: ServerGamePacketListenerImpl.handleClientTickEnd
func (c *Client) EndTick() {
	c.T.Helper()
	c.Send(protocol.IDPlayClientTickEnd, func(*protocol.Buffer) {})
}

// ClampPitchForTest is the clamp Entity.setXRot puts on a pitch.
func ClampPitchForTest(pitch float32) float32 {
	return min(90, max(-90, pitch))
}

// SetCreativeSlot puts an item in a slot, the way a creative client does: in
// creative it is the client that decides what exists, and this is the only way
// the server hears about it. The stack travels the untrusted slot shape the
// packet carries, whose component values are each prefixed by their byte
// length - a shape apart from what the server itself puts on the wire, whose
// component values go bare.
func (c *Client) SetCreativeSlot(slot int16, stack protocol.ItemStack) {
	c.T.Helper()
	c.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		b.PutI16(slot)
		PutUntrustedStack(b, stack)
	})
}

// PutUntrustedStack writes a stack the way the Set Creative Mode Slot packet
// carries it: the count and the item the trusted slot's own fields, and then
// each carried component as its type, the byte length of its value, and the
// value. The length prefix is what makes the shape the untrusted one - it is
// how a client's stack can be read past without trusting what is in it - and
// it is the one field the server's own PutSlot leaves out, which is why this
// is written here rather than asked of it. The component data itself comes
// from PutComponentData, whose shapes the vanilla captures pin.
func PutUntrustedStack(b *protocol.Buffer, stack protocol.ItemStack) {
	if stack.Empty() {
		b.PutVarInt(0)
		return
	}
	b.PutVarInt(stack.Count)
	b.PutVarInt(stack.ItemID)
	components := UntrustedComponents(stack)
	b.PutVarInt(int32(len(components)))
	b.PutVarInt(0) // components to remove, of which these stacks carry none
	for _, component := range components {
		var value protocol.Buffer
		value.PutComponentData(stack, component)
		b.PutVarInt(component)
		b.PutVarInt(int32(value.Len()))
		b.PutRaw(value.Bytes())
	}
}

// UntrustedComponents lists the components a stack carries away from its
// item's defaults, in the order the server's own writer names them, so the
// packets the harness sends look like the ones a client would.
func UntrustedComponents(stack protocol.ItemStack) []int32 {
	var ids []int32
	if stack.Damage > 0 {
		ids = append(ids, protocol.DataComponentDamage)
	}
	if stack.Unbreakable {
		ids = append(ids, protocol.DataComponentUnbreakable)
	}
	if stack.CustomName != nil {
		ids = append(ids, protocol.DataComponentCustomName)
	}
	if len(stack.Enchantments) > 0 {
		ids = append(ids, protocol.DataComponentEnchantments)
	}
	if stack.RepairCost > 0 {
		ids = append(ids, protocol.DataComponentRepairCost)
	}
	if stack.GlintOverride != protocol.GlintUnset {
		ids = append(ids, protocol.DataComponentEnchantmentGlintOverride)
	}
	if len(stack.StoredEnchantments) > 0 {
		ids = append(ids, protocol.DataComponentStoredEnchantments)
	}
	if len(stack.Container) > 0 {
		ids = append(ids, protocol.DataComponentContainer)
	}
	for _, raw := range stack.Raw {
		ids = append(ids, raw.Type)
	}
	return ids
}

// PickBlock sends a Pick Item From Block packet, the way a client does on a
// middle-click. The "include data" flag is false, since this server does not
// model block data components.
func (c *Client) PickBlock(x, y, z int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayPickItemFromBlock, func(b *protocol.Buffer) {
		b.PutPosition(x, y, z)
		b.PutBool(false) // include block data
	})
}

// AwaitPlay reads until the given packet turns up, ignoring whatever the join
// sequence sends before it and answering any keep-alive on the way.
func (c *Client) AwaitPlay(packetID int32) []byte {
	c.T.Helper()
	for gotID, data := range c.Packets(ExpectTimeout) {
		if gotID == packetID {
			return data
		}
	}
	c.T.Fatalf("packet 0x%02X did not arrive within %s", packetID, ExpectTimeout)
	return nil
}

// Inventory reads the packet that fills in the player's window, returning what
// every slot holds.
func (c *Client) Inventory() []protocol.ItemStack {
	c.T.Helper()

	r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayContainerContent))
	r.ReadVarInt() // Window ID
	r.ReadVarInt() // State ID
	slots := make([]protocol.ItemStack, 0, r.ReadVarInt())
	for cap(slots) > len(slots) {
		stack, ok := r.ReadSlot()
		if !ok {
			c.T.Fatalf("slot %d did not read back", len(slots))
		}
		slots = append(slots, stack)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("set container content: %v", err)
	}
	return slots
}

// FeedbackArgsForTest builds the arguments of a feedback line. They are
// plain text, except that /setblock sends its coordinates as ints and a
// /data query its tag highlighted.
// vanilla: SetBlockCommand.setBlock
func FeedbackArgsForTest(key string, args []string) []protocol.Text {
	with := make([]protocol.Text, 0, len(args))
	for _, arg := range args {
		if key == "commands.setblock.success" {
			n, err := strconv.ParseInt(arg, 10, 64)
			if err != nil {
				panic(err)
			}
			with = append(with, protocol.Text{Number: &n})
			continue
		}
		with = append(with, protocol.Text{Text: arg})
	}
	switch key {
	case "commands.data.block.query", "commands.data.entity.query", "commands.data.storage.query":
		with[len(with)-1] = NBTArg(args[len(args)-1])
	}
	return with
}

// NBTArg is the highlighted component /data get sends for a tag, given as
// SNBT.
func NBTArg(snbt string) protocol.Text {
	parsed, _, err := nbt.ParseValueTag(snbt)
	if err != nil {
		panic(err)
	}
	return command.PrettyNBTText(parsed, true)
}

// ExpectSayLineAs is expectSayLine for a source that goes by a display
// component, a player or an entity.
func (c *Client) ExpectSayLineAs(t *testing.T, sender protocol.Text, message string) {
	t.Helper()
	want := SayPayload(sender, message)
	for id, data := range c.Packets(10 * time.Second) {
		if id != protocol.IDPlayDisguisedChat {
			continue
		}
		if !bytes.Equal(data, want) {
			t.Fatalf("say line = % X, want % X", data, want)
		}
		return
	}
	t.Fatal("the say line did not arrive within 10s")
}

// SetSettings is setSkinParts with the main hand chosen too.
func (c *Client) SetSettings(parts uint8, mainHand int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayClientInformation, func(b *protocol.Buffer) {
		b.PutString("en_GB")
		b.PutI8(4)     // render distance
		b.PutVarInt(0) // chat mode: enabled
		b.PutBool(true)
		b.PutU8(parts)
		b.PutVarInt(mainHand)
		b.PutBool(false) // no text filtering
		b.PutBool(true)  // allow server listings
		b.PutVarInt(0)   // all particles
	})
}

// ParseMetadataWithHand is parseMetadataFields that also reports the main
// hand, -1 when the packet leaves it out.
func ParseMetadataWithHand(t *testing.T, r *protocol.Reader) (flags uint8, pose int32, skinParts uint8, mainHand int32) {
	t.Helper()
	mainHand = -1
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		switch fieldType := r.ReadVarInt(); index {
		case protocol.MetadataIndexEntityFlags:
			flags = r.ReadU8()
		case protocol.MetadataIndexPose:
			pose = r.ReadVarInt()
		case protocol.MetadataIndexSkinParts:
			skinParts = r.ReadU8()
		case protocol.MetadataIndexMainHand:
			mainHand = r.ReadVarInt()
		default:
			t.Fatalf("unexpected metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("entity metadata: %v", err)
	}
	return flags, pose, skinParts, mainHand
}

// Pos is where the client's body stands.
func (c *Client) Pos() player.Position { return c.pos }

// ConductConfig is an offline-mode configuration with the code of conduct
// enabled on a directory holding files, by name.
func ConductConfig(t *testing.T, files map[string]string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.EnableCodeOfConduct = true
	cfg.CodeOfConductDir = dir
	return cfg
}
