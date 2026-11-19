package admin

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"log/slog"
	"slices"
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

// TestConsoleRefusesARepeatedComponent has the console run a /give whose item
// list names one component twice: the refusal carries no place in the input,
// so the console answers the message alone rather than slicing the input at a
// cursor below zero.
func TestConsoleRefusesARepeatedComponent(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, config.Default())

	reply := srv.RunLine("give Nobody diamond_sword[damage=1,damage=2]")
	if len(reply) != 1 || len(reply[0].Extra) != 1 {
		t.Fatalf("the console answered %v, want the refusal alone", reply)
	}
	inner := reply[0].Extra[0]
	if inner.Translate != "arguments.item.component.repeated" {
		t.Errorf("the console refusal = %q, want the repeated-component message", inner.Translate)
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

// TestConsoleSuggestionsCarryStop completes a bare slash out of the
// console's permission level, which offers /stop where a player's own
// level would not.
func TestConsoleSuggestionsCarryStop(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, config.Default())

	_, matches := srv.Suggest("/")
	if !slices.Contains(matches, "stop") {
		t.Errorf("suggestions %q do not carry stop", matches)
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

// TestConsoleStopSaysSoOnce runs /stop from the console: the reply says
// "Stopping the server" and the shutdown logs "Stopping server", each once.
// vanilla: StopCommand, MinecraftServer.stopServer. Not parallel: it swaps
// the default logger.
func TestConsoleStopSaysSoOnce(t *testing.T) {
	logs := &servertest.SyncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv, _ := servertest.Start(t, config.Default())
	reply := srv.RunLine("stop")
	if len(reply) != 1 || reply[0].Plain() != "Stopping the server" {
		t.Errorf("stop answered %v, want Stopping the server", reply)
	}
	srv.Shutdown()

	got := logs.String()
	if strings.Contains(got, "Stopping the server") {
		t.Errorf("the log repeats the reply: %s", got)
	}
	if strings.Count(got, `msg="Stopping server"`) != 1 {
		t.Errorf("the log has not one Stopping server line: %s", got)
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

// TestConsoleFailuresAreRed runs a console line that fails: the answer is in
// red, as a player's would be.
func TestConsoleFailuresAreRed(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, config.Default())
	reply := srv.RunLine("msg Nobody x")
	if len(reply) != 1 || reply[0].ANSI() != "\x1b[0;91mNo player was found\x1b[0m" {
		t.Errorf("msg to nobody answered %v, want No player was found in red", reply)
	}
}

// TestConsoleEchoesAnOpsGameMode has an operator switch game mode with the
// admin echo on and off: on, the session the log lines go through carries
// "[Probe: Set own game mode to Survival Mode]"; off, it stays quiet.
// vanilla: CommandSourceStack.broadcastToAdmins. Not parallel: it swaps the
// default logger.
func TestConsoleEchoesAnOpsGameMode(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Probe", servertest.OpsListing(t, "Probe"))

	var consoleOut servertest.SyncBuffer
	con := console.New(strings.NewReader(""), io.Discard, &consoleOut, srv)
	prev := slog.Default()
	slog.SetDefault(slog.New(console.NewLogHandler(con.LogWriter(), slog.LevelInfo, false)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv.SetGameRule("log_admin_commands", false)
	c.ChatCommand("gamemode creative")
	c.ExpectSystemChat("commands.gamemode.success.self", false, protocol.Text{Translate: "gameMode.creative"})
	if strings.Contains(consoleOut.String(), "Set own game mode") {
		t.Fatalf("the console echoes a command run with log_admin_commands off:\n%s", consoleOut.String())
	}

	srv.SetGameRule("log_admin_commands", true)
	c.ChatCommand("gamemode survival")
	c.ExpectSystemChat("commands.gamemode.success.self", false, protocol.Text{Translate: "gameMode.survival"})
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return strings.Contains(consoleOut.String(), "[Probe: Set own game mode to Survival Mode]")
	})
}

// TestGameModeChangeLogsNoDuplicateKey has an operator switch game mode and
// holds that no log line repeats a key the logger's With already carries.
// Not parallel: it swaps the default logger.
func TestGameModeChangeLogsNoDuplicateKey(t *testing.T) {
	logs := captureConsole(t)

	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Probe", servertest.OpsListing(t, "Probe"))

	c.ChatCommand("gamemode creative")
	c.ExpectSystemChat("commands.gamemode.success.self", false, protocol.Text{Translate: "gameMode.creative"})

	got := logs.String()
	if !strings.Contains(got, "game mode set") {
		t.Fatalf("the log lacks the game mode set line:\n%s", got)
	}
	for line := range strings.Lines(got) {
		if strings.Count(line, "player=") > 1 {
			t.Errorf("a log line repeats player=: %q", line)
		}
	}
}
