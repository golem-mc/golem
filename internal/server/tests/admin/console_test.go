package admin

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestConsoleRunsCommandAsServer runs a setblock through a console wired to
// a live server: the block is placed in the world the way a player's
// command would place it, and the feedback the command sent comes back as
// the plain text the console printed.
func TestConsoleRunsCommandAsServer(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, config.Default())

	var out bytes.Buffer
	con := console.New(strings.NewReader("setblock 1 65 1 minecraft:stone\n"), &out, io.Discard, srv)
	con.Serve()

	// The command gives its chunk back as it returns and no player holds it,
	// so it is saved and unloaded; read the block back the way a later
	// command would, through the load path rather than BlockAt, which
	// answers air for an unloaded chunk.
	srv.FlushSaves()
	if name, _, ok := registry.BlockStateName(srv.LoadBlockAt(1, 65, 1)); !ok || name != "minecraft:stone" {
		t.Fatalf("block = %q, want minecraft:stone", name)
	}
	if !strings.Contains(out.String(), "Changed the block at 1, 65, 1") {
		t.Errorf("output %q does not carry the setblock feedback", out.String())
	}
}

// TestConsoleStopsTheServer has the console run /stop: the listener ends,
// which is what the console's stop asks the server's Listen for.
func TestConsoleStopsTheServer(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.ServerPort = 0
	srv, _ := servertest.Start(t, cfg)

	served := make(chan error, 1)
	go func() { served <- srv.Listen() }()

	con := console.New(strings.NewReader("stop\n"), io.Discard, io.Discard, srv)
	con.Serve()

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("listen did not end after the console's stop")
	}
}

// TestConsoleReportsAParseError has the console run a line that matches
// nothing, whose error the client would render with the line and the
// marker under it.
func TestConsoleReportsAParseError(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, config.Default())

	var out bytes.Buffer
	con := console.New(strings.NewReader("bogus\n"), &out, io.Discard, srv)
	con.Serve()

	if !strings.Contains(out.String(), "Unknown or incomplete command. See below for error") {
		t.Errorf("output %q does not carry the parse error", out.String())
	}
	if !strings.Contains(out.String(), "bogus<--[HERE]") {
		t.Errorf("output %q does not carry the error's context", out.String())
	}
}

// TestAdminCommandsEchoToTheLog has an operator switch their game mode, have
// a pig switch it through a forked execute, and stop the server from in
// game: each success is echoed to the log as "[Name: message]", under the
// executing entity's name for the execute, then the shutdown logs Stopping
// server; with log_admin_commands off nothing is echoed. vanilla:
// CommandSourceStack.broadcastToAdmins. Not parallel: it swaps the default
// logger.
func TestAdminCommandsEchoToTheLog(t *testing.T) {
	logs := &servertest.SyncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Probe", servertest.OpsListing(t, "Probe"))

	srv.SetGameRule("log_admin_commands", false)
	c.ChatCommand("gamemode creative")
	c.ExpectSystemChat("commands.gamemode.success.self", false, protocol.Text{Translate: "gameMode.creative"})
	// The admin echo reads the rule after the feedback is written, so the
	// first command is only proven quiet once a later command's feedback has
	// arrived: one connection's packets are handled in order.
	c.ChatCommand("gamemode adventure")
	c.ExpectSystemChat("commands.gamemode.success.self", false, protocol.Text{Translate: "gameMode.adventure"})
	srv.SetGameRule("log_admin_commands", true)
	c.ChatCommand("gamemode survival")
	c.ExpectSystemChat("commands.gamemode.success.self", false, protocol.Text{Translate: "gameMode.survival"})
	c.ChatCommand("summon minecraft:pig 0.5 64 0.5")
	c.ChatCommand("execute as @e[type=minecraft:pig,limit=1] run gamemode creative Probe")
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return strings.Contains(logs.String(), `msg="[Pig: Set Probe's game mode to Creative Mode]"`)
	})
	c.ChatCommand("stop")
	// The stop is requested after the command's echo, as main would see it.
	select {
	case <-srv.StopRequested():
	case <-time.After(10 * time.Second):
		t.Fatal("the in-game stop was not requested")
	}
	// Gone before the shutdown: a Shutdown with a player still connected
	// races the connection's leave on the chunk set.
	c.Disconnect()
	srv.Shutdown()

	got := logs.String()
	if strings.Contains(got, "[Probe: Set own game mode to Creative Mode]") {
		t.Errorf("the log echoes a command run with log_admin_commands off: %s", got)
	}
	echo := strings.Index(got, `msg="[Probe: Set own game mode to Survival Mode]"`)
	execute := strings.Index(got, `msg="[Pig: Set Probe's game mode to Creative Mode]"`)
	stop := strings.Index(got, `msg="[Probe: Stopping the server]"`)
	stopped := strings.Index(got, `msg="Stopping server"`)
	if echo < 0 || execute < echo || stop < execute || stopped < stop {
		t.Errorf("the log lacks the echoes and the stop in order: %s", got)
	}
}
