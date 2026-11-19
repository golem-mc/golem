package plugins_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// colourOf answers the colour the component holding text is drawn in.
func colourOf(t protocol.Text, text string) (string, bool) {
	if t.Text == text {
		return t.Color, true
	}
	for _, e := range t.Extra {
		if c, ok := colourOf(e, text); ok {
			if c == "" {
				c = t.Color
			}
			return c, true
		}
	}
	return "", false
}

// vanilla-bukkit: PluginsCommand
func TestPluginsListsThePluginsByName(t *testing.T) {
	t.Parallel()
	ok := &testPlugin{enable: func(plugin.Server) error { return nil }}
	broken := &testPlugin{enable: func(plugin.Server) error { return errors.New("no config") }}
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{
		{Name: "warps", Plugin: ok}, {Name: "Broken", Plugin: broken}, {Name: "Essentials", Plugin: ok},
	})
	for _, line := range []string{"plugins", "pl", "golem:plugins"} {
		out := srv.RunLine(line)
		if got := strings.Join(plainLines(out), "\n"); got != "Plugins (3): Broken, Essentials, warps" {
			t.Fatalf("%s answered %q", line, got)
		}
		for name, want := range map[string]string{"Broken": protocol.ColorRed, "Essentials": protocol.ColorGreen, "warps": protocol.ColorGreen} {
			if got, _ := colourOf(out[0], name); got != want {
				t.Errorf("%s: %s drawn %q, want %q", line, name, got, want)
			}
		}
	}
}

func TestPluginsWithoutPlugins(t *testing.T) {
	t.Parallel()
	srv := servertest.NewTestServer(t, servertest.BlockTestConfig())
	if got := strings.Join(plainLines(srv.RunLine("pl")), "\n"); got != "Plugins (0): " {
		t.Fatalf("pl answered %q", got)
	}
}
