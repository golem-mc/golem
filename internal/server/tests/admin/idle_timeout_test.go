package admin

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// idleTicks returns the ticks a held loop runs to cover d.
func idleTicks(d time.Duration) int { return int(d / (time.Second / 20)) }

// idleServer dials an operator into a server and then holds its tick loop, so
// the idle clock moves only by the ticks the test runs: 20 a second.
func idleServer(t *testing.T, cfg *config.Config) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, cfg, "Sleeper", servertest.OpsListing(t, "Sleeper"), func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
		s.SetTimeSync(0) // minutes of held ticks would queue a sync every second
	})
	servertest.HoldTicks(t, srv)
	return srv, c
}

func expectIdleKick(c *servertest.Client) {
	c.T.Helper()
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "multiplayer.disconnect.idling"})
	if got := c.AwaitPlay(protocol.IDPlayDisconnect); !bytes.Equal(got, want.Bytes()) {
		c.T.Errorf("disconnect = % X, want % X", got, want.Bytes())
	}
}

// TestSetIdleTimeoutKicksAnIdlePlayer checks /setidletimeout 1 turns the
// kick on live: a player silent for over a minute is told they idled.
func TestSetIdleTimeoutKicksAnIdlePlayer(t *testing.T) {
	t.Parallel()
	srv, c := idleServer(t, servertest.BlockTestConfig())
	c.ChatCommand("setidletimeout 1")
	c.ExpectCommandFeedback("commands.setidletimeout.success", "1")

	servertest.RunTicks(t, srv, idleTicks(59*time.Second)+2)
	servertest.RunTicks(t, srv, idleTicks(2*time.Second)+2)
	expectIdleKick(c)
}

// TestIdleTimeoutFromProperty checks player-idle-timeout kicks without any
// command, and that an action restarts the count.
func TestIdleTimeoutFromProperty(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.PlayerIdleTimeout = 1
	srv, c := idleServer(t, cfg)

	servertest.RunTicks(t, srv, idleTicks(50*time.Second))
	c.ChatCommand("seed")
	c.Settle()
	servertest.RunTicks(t, srv, idleTicks(50*time.Second)+2)
	if got := srv.PlayerCount(); got != 1 {
		t.Fatalf("an acting player was kicked: %d players left", got)
	}
	servertest.RunTicks(t, srv, idleTicks(20*time.Second)+2)
	expectIdleKick(c)
}

// TestSetIdleTimeoutZeroTurnsTheKickOff checks 0 disables a running kick.
func TestSetIdleTimeoutZeroTurnsTheKickOff(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.PlayerIdleTimeout = 1
	srv, c := idleServer(t, cfg)
	c.ChatCommand("setidletimeout 0")
	c.ExpectCommandFeedback("commands.setidletimeout.success.disabled")
	c.Settle()

	servertest.RunTicks(t, srv, idleTicks(2*time.Minute))
	c.ChatCommand("seed")
	// The latency broadcasts of the ticks run are queued ahead of the line.
	servertest.CheckCommandFeedback(c.T, c.AwaitPlay(protocol.IDPlaySystemChat), "commands.seed.success", protocol.Text{
		Translate: "chat.square_brackets",
		With: []protocol.Text{{
			Text:      "0",
			Color:     protocol.ColorGreen,
			ClickCopy: "0",
			HoverText: &protocol.Text{Translate: "chat.copy.click"},
			Insertion: "0",
		}},
	})
}

// TestVersionCommand checks /version prints the 26.3 build info.
func TestVersionCommand(t *testing.T) {
	t.Parallel()
	_, c := idleServer(t, servertest.BlockTestConfig())
	c.ChatCommand("version")
	for _, l := range []struct {
		key  string
		with []string
	}{
		{"commands.version.header", nil},
		{"commands.version.id", []string{"26.3"}},
		{"commands.version.name", []string{"26.3"}},
		{"commands.version.data", []string{"5023"}},
		{"commands.version.series", []string{"main"}},
		{"commands.version.protocol", []string{strconv.Itoa(protocol.Version), "0x309"}},
		{"commands.version.build_time", []string{"Tue Sep 15 11:20:48 UTC 2026"}},
		{"commands.version.pack.resource", []string{"97.1"}},
		{"commands.version.pack.data", []string{"121.0"}},
		{"commands.version.stable.yes", nil},
	} {
		var with []protocol.Text
		for _, w := range l.with {
			with = append(with, protocol.Text{Text: w})
		}
		c.ExpectSystemText(protocol.Text{Translate: l.key, With: with})
	}
}

// TestIdleClockCountsOnlyAClientsRealMoves checks that a step under the
// 1e-5 threshold leaves the clock alone and a larger one restarts it.
// vanilla: ServerGamePacketListenerImpl.handlePlayerKnownMovement
func TestIdleClockCountsOnlyAClientsRealMoves(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.PlayerIdleTimeout = 1
	srv, c := idleServer(t, cfg)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	c.Settle()
	p := srv.PlayerPosition("Sleeper")

	servertest.RunTicks(t, srv, idleTicks(50*time.Second))
	c.SendPosition(p.X+0.001, p.Y, p.Z, true) // 1e-6 squared: not an action
	c.Settle()
	servertest.RunTicks(t, srv, idleTicks(20*time.Second)+2)
	expectIdleKick(c)
}

// TestIdleClockRestartsOnAClientMove checks a real step counts as acting.
func TestIdleClockRestartsOnAClientMove(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.PlayerIdleTimeout = 1
	srv, c := idleServer(t, cfg)
	p := srv.PlayerPosition("Sleeper")

	servertest.RunTicks(t, srv, idleTicks(50*time.Second))
	c.SendPosition(p.X+0.5, p.Y, p.Z, true)
	c.Settle()
	servertest.RunTicks(t, srv, idleTicks(50*time.Second)+2)
	if got := srv.PlayerCount(); got != 1 {
		t.Fatalf("a moving player was kicked: %d players left", got)
	}
}

// TestHiddenChatDoesNotRestartTheIdleClock checks a refused chat message is
// not the player acting.
// vanilla: ServerGamePacketListenerImpl.tryHandleChat
func TestHiddenChatDoesNotRestartTheIdleClock(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.PlayerIdleTimeout = 1
	srv, c := idleServer(t, cfg)
	c.Send(protocol.IDPlayClientInformation, func(b *protocol.Buffer) {
		b.PutString("en_GB")
		b.PutI8(4)
		b.PutVarInt(protocol.ChatModeHidden)
		b.PutBool(true)
		b.PutU8(0x7f)
		b.PutVarInt(protocol.MainHandRight)
		b.PutBool(false)
		b.PutBool(true)
		b.PutVarInt(0)
	})
	c.Settle()

	servertest.RunTicks(t, srv, idleTicks(50*time.Second))
	c.Chat("hello")
	c.Settle()
	servertest.RunTicks(t, srv, idleTicks(20*time.Second)+2)
	expectIdleKick(c)
}

// TestACartRiderIsKickedWhileTheServerCarriesThem checks the cart's seat
// writes do not restart the clock.
func TestACartRiderIsKickedWhileTheServerCarriesThem(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.PlayerIdleTimeout = 1
	srv, c := idleServer(t, cfg)
	if !srv.SetBlock(2, 64, 7, servertest.MustState(t, "minecraft:stone", nil)) ||
		!srv.SetBlock(2, 64, 6, servertest.MustState(t, "minecraft:powered_rail",
			map[string]string{"shape": "north_south", "powered": "true"})) {
		t.Fatal("the launcher's chunks are not loaded")
	}
	for z := int32(0); z <= 5; z++ {
		srv.SetBlock(2, 64, z, servertest.MustState(t, "minecraft:rail", map[string]string{"shape": "north_south"}))
	}
	cartID := srv.SpawnMinecart(2.5, 64.0625, 6.5)
	c.MoveTo(2.5, 64, 7.9, 0, 60)
	c.Settle()
	c.UseOn(cartID)
	c.Settle()
	if _, ok := srv.MinecartRider(cartID); !ok {
		t.Fatal("the cart took no rider")
	}
	x0, y0, z0, _, _ := srv.MinecartState(cartID)

	servertest.RunTicks(t, srv, 60)
	if x, y, z, _, _ := srv.MinecartState(cartID); x == x0 && y == y0 && z == z0 {
		t.Fatal("the cart never moved, so the seat was never carried")
	}
	// The held ticks finish before the kick is read, so the socket is closed
	// by then and a batch ack would write into it.
	c.NoBatchAcks = true
	servertest.RunTicks(t, srv, idleTicks(70*time.Second)-60)
	expectIdleKick(c)
}

// TestSetIdleTimeoutWritesServerProperties checks the command persists
// player-idle-timeout.
// vanilla: DedicatedServer.setPlayerIdleTimeout
func TestSetIdleTimeoutWritesServerProperties(t *testing.T) {
	t.Parallel()
	srv, c := idleServer(t, servertest.BlockTestConfig())
	srv.SetConfigFile(filepath.Join(t.TempDir(), "server.properties"))
	c.ChatCommand("setidletimeout 5")
	c.ExpectCommandFeedback("commands.setidletimeout.success", "5")
	loaded, err := config.Load(srv.ConfigFile())
	if err != nil {
		t.Fatalf("reload server.properties: %v", err)
	}
	if loaded.PlayerIdleTimeout != 5 {
		t.Errorf("player-idle-timeout = %d, want 5", loaded.PlayerIdleTimeout)
	}
}
