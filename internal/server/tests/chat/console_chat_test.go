package chat

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestChatLogsDecoratedLines checks that each broadcast is logged as its
// decorated line tagged Not Secure, and that the console's own /msg answers
// with the outgoing line. Not parallel: it swaps the default logger.
func TestChatLogsDecoratedLines(t *testing.T) {
	logs := &servertest.LogBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Tester")

	c.Chat("hello from client")
	if got := expectPlayerChat(t, c, "minecraft:chat", "Tester",
		protocol.OfflineUUID("Tester"), "hello from client", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("the speaker's own echo's index = %d, want 0", got)
	}
	c.ChatCommand("me waves")
	servertest.ExpectDisguisedChat(t, c, "minecraft:emote_command", "Tester", "waves", "")
	if reply := srv.RunLine("say console say"); len(reply) != 0 {
		t.Errorf("say answered the console %v, want nothing", reply)
	}
	servertest.ExpectDisguisedChat(t, c, "minecraft:say_command", "Server", "console say", "")

	reply := srv.RunLine("msg Tester whisper")
	servertest.ExpectDisguisedChat(t, c, "minecraft:msg_command_incoming", "Server", "whisper", "")
	if len(reply) != 1 || reply[0].Plain() != "You whisper to Tester: whisper" {
		t.Errorf("msg answered the console %v, want the outgoing whisper", reply)
	}

	got := logs.String()
	for _, want := range []string{
		`level=INFO msg="[Not Secure] <Tester> hello from client"`,
		`level=INFO msg="[Not Secure] * Tester waves"`,
		`level=INFO msg="[Not Secure] [Server] console say"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the log lacks %s", want)
		}
	}
	c.Disconnect()
}

// TestConsoleSayResolvesSelector checks the console's /say expands a selector
// in the [Server] line it logs. Not parallel: it swaps the default logger.
func TestConsoleSayResolvesSelector(t *testing.T) {
	logs := &servertest.LogBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)

	bot := connect().Join("Bot")

	cmd := connect().Join("Cmd")

	srv.RunLine("say @a")
	servertest.UntilTicks(t, srv, 20, "the [Server] line naming both players", func() bool {
		return strings.Contains(logs.String(), `msg="[Not Secure] [Server] Bot, Cmd"`)
	})

	// @s names nobody from the console, so it expands to nothing, and @Cmd
	// is no selector letter and stays as typed.
	srv.RunLine("say @s @Cmd")
	servertest.UntilTicks(t, srv, 20, "the [Server] line with @s gone", func() bool {
		return strings.Contains(logs.String(), `msg="[Not Secure] [Server]  @Cmd"`)
	})

	bot.Disconnect()
	cmd.Disconnect()
}
