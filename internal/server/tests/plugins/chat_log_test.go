package plugins_test

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// vanilla: PlayerList.broadcastChatMessage (the console logs the decorated line)
func TestPluginChatLogsTheFormattedLine(t *testing.T) {
	logs := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "chat", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerChatEvent) {
			e.Format = func(_ plugin.Player, message plugin.Text, _ plugin.Player) plugin.Text {
				return plugin.Plain("[Staff] ").Append(message)
			}
		})
		return nil
	}}}})
	alice := servertest.DialServer(t, srv)
	alice.ViewAsk = config.MinDistance
	alice.Join("Alice")
	servertest.HoldTicks(t, srv)
	alice.Settle()
	alice.Chat("hello")
	alice.Settle()
	servertest.RunTicks(t, srv, 1)
	if out := logs.String(); !strings.Contains(out, "[Staff] hello") || strings.Contains(out, "<Alice> hello") {
		t.Fatalf("console log lacks the formatted line:\n%s", out)
	}
}
