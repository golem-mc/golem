package admin

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/console"
)

// captureConsole swaps the default logger for one drawing the console layout
// into the buffer, so a test can assert the framed lines. Not parallel: it
// swaps the default logger.
func captureConsole(t *testing.T) *logBuffer {
	t.Helper()
	logs := &logBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(console.NewLogHandler(logs, slog.LevelDebug, false)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logs
}

// TestConsoleJoinAndLeaveLines checks the join and leave console lines are
// vanilla's: the placeNewPlayer line with its bracketed address, Java doubles
// and entity id, then the chat message's text; a connection that just drops
// is "lost connection: Disconnected", then the left the game message.
// vanilla: PlayerList.placeNewPlayer, ServerGamePacketListenerImpl.onDisconnect
func TestConsoleJoinAndLeaveLines(t *testing.T) {
	logs := captureConsole(t)

	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Tester")
	c.Disconnect()

	got := logs.String()
	joined := regexp.MustCompile(`\[Server thread/INFO\]: Tester\[/127\.0\.0\.1:\d+\] logged in with entity id 1 at \(0\.5, 64\.0, 0\.5\)`)
	if !joined.MatchString(got) {
		t.Errorf("the log lacks the vanilla join line:\n%s", got)
	}
	for _, want := range []string{
		"[Server thread/INFO]: System chat: Tester joined the game",
		"[Server thread/INFO]: Tester lost connection: Disconnected",
		"[Server thread/INFO]: System chat: Tester left the game",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the log lacks %q:\n%s", want, got)
		}
	}
}

// TestLogIPsOffWithholdsTheAddress checks log-ips=false replaces the client
// address in the join line and the connection log attributes.
// vanilla: Connection.getLoggableAddress
func TestLogIPsOffWithholdsTheAddress(t *testing.T) {
	logs := captureConsole(t)

	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LogIPs = false
	_, c := servertest.Join(t, cfg, "Tester")
	c.Disconnect()

	got := logs.String()
	if !strings.Contains(got, "Tester[IP hidden] logged in with entity id 1") {
		t.Errorf("the join line shows an address:\n%s", got)
	}
	if strings.Contains(got, "127.0.0.1") {
		t.Errorf("the log leaks the client address:\n%s", got)
	}
}

// TestConsoleStartupLines checks the server logs the vanilla startup lines:
// the version, the level being prepared, and the done line with its seconds.
// vanilla: DedicatedServer.initServer
func TestConsoleStartupLines(t *testing.T) {
	logs := captureConsole(t)

	cfg := config.Default()
	servertest.Start(t, cfg)

	got := logs.String()
	for _, want := range []string{
		"[Server thread/INFO]: Starting minecraft server version 26.3",
		`[Server thread/INFO]: Preparing level "` + cfg.LevelName + `"`,
		// The test's world is created fresh, so it selects a spawn.
		"[Server thread/INFO]: Selecting global world spawn...",
		"[Server thread/INFO]: Loading 0 persistent chunks...",
		"[Server thread/INFO]: Preparing spawn area: 100%",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the log lacks %q:\n%s", want, got)
		}
	}
	elapsed := regexp.MustCompile(`\[Server thread/INFO\]: Time elapsed: \d+ ms`)
	if !elapsed.MatchString(got) {
		t.Errorf("the log lacks the vanilla time elapsed line:\n%s", got)
	}
	done := regexp.MustCompile(`\[Server thread/INFO\]: Done \(\d+\.\d{3}s\)! For help, type "help"`)
	if !done.MatchString(got) {
		t.Errorf("the log lacks the vanilla done line:\n%s", got)
	}
}

// TestConsoleSaveAllLines runs /save-all through the console and checks the
// two messages vanilla prints, framed the way the console draws a reply.
// vanilla: SaveAllCommand, commands.save.saving and commands.save.success
func TestConsoleSaveAllLines(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	srv, _ := servertest.Start(t, cfg)

	var out bytes.Buffer
	con := console.New(strings.NewReader("save-all\n"), &out, io.Discard, srv)
	con.Serve()

	got := out.String()
	for _, want := range []string{
		"[Server thread/INFO]: Saving the game (this may take a moment!)",
		"[Server thread/INFO]: Saved the game",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the console wrote %q, want it to carry %q", got, want)
		}
	}
}

// TestConsoleSaveAllFlushLogsTheChunkStores runs /save-all flush and checks
// the per-dimension chunk-store report vanilla prints for a flush, and that a
// bare /save-all leaves it out. vanilla: MinecraftServer.saveAllChunks,
// ChunkMap.save
func TestConsoleSaveAllFlushLogsTheChunkStores(t *testing.T) {
	logs := captureConsole(t)

	cfg := config.Default()
	srv, _ := servertest.Start(t, cfg)

	srv.RunLine("save-all")
	bare := logs.String()
	for _, want := range []string{"(world)", "(DIM-1)", "(DIM1)", "All dimensions are saved"} {
		if strings.Contains(bare, want) {
			t.Errorf("a bare /save-all logged %q:\n%s", want, bare)
		}
	}

	srv.RunLine("save-all flush")
	got := logs.String()
	at := -1
	for _, want := range []string{
		"[Server thread/INFO]: ThreadedAnvilChunkStorage (world): All chunks are saved",
		"[Server thread/INFO]: ThreadedAnvilChunkStorage (DIM-1): All chunks are saved",
		"[Server thread/INFO]: ThreadedAnvilChunkStorage (DIM1): All chunks are saved",
		"[Server thread/INFO]: ThreadedAnvilChunkStorage: All dimensions are saved",
	} {
		index := strings.Index(got, want)
		if index < 0 {
			t.Errorf("the log lacks %q:\n%s", want, got)
		} else if index < at {
			t.Errorf("%q is out of order:\n%s", want, got)
		}
		at = index
	}
}

// TestConsoleAutoSaveLogsAtDebug checks the auto-save says nothing at INFO and
// reports its run at debug, the level vanilla's MinecraftServer.autoSave logs
// at. vanilla: MinecraftServer.autoSave
func TestConsoleAutoSaveLogsAtDebug(t *testing.T) {
	logs := captureConsole(t)

	cfg := config.Default()
	srv, _ := servertest.Start(t, cfg)
	servertest.HoldTicks(t, srv)
	servertest.AutoSaveEveryTick(srv)

	servertest.UntilTicks(t, srv, servertest.AutoSaveEvery, "the auto-save finishes", func() bool {
		return strings.Contains(logs.String(), "Autosave finished")
	})
	got := logs.String()
	for _, want := range []string{
		"[Server thread/DEBUG]: Autosave started",
		"[Server thread/DEBUG]: Autosave finished",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the log lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[Server thread/INFO]: Autosave") {
		t.Errorf("the auto-save logged at INFO:\n%s", got)
	}
}
