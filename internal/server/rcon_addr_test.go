package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
)

// TestRconListenAddr checks the settings RCON refuses to run on, matching
// RconThread.create: enable-rcon off, an empty password, and a port outside
// 1-65535; a configured listener answers the game host and rcon.port.
func TestRconListenAddr(t *testing.T) {
	t.Parallel()
	valid := func() *config.Config {
		cfg := config.Default()
		cfg.EnableRcon = true
		cfg.RconPassword = "sesame"
		cfg.RconPort = 25575
		return cfg
	}

	if addr, ok := rconListenAddr(valid()); !ok || addr != "0.0.0.0:25575" {
		t.Errorf("configured rcon answered %q ok %v, want 0.0.0.0:25575", addr, ok)
	}

	v6 := valid()
	v6.ServerIP = "::1"
	if addr, ok := rconListenAddr(v6); !ok || addr != "[::1]:25575" {
		t.Errorf("rcon on server-ip ::1 answered %q ok %v, want [::1]:25575", addr, ok)
	}

	v6.ServerIP = "[::1]"
	if addr, ok := rconListenAddr(v6); !ok || addr != "[::1]:25575" {
		t.Errorf("rcon on server-ip [::1] answered %q ok %v, want [::1]:25575", addr, ok)
	}

	for name, mutate := range map[string]func(*config.Config){
		"disabled":    func(c *config.Config) { c.EnableRcon = false },
		"no password": func(c *config.Config) { c.RconPassword = "" },
		"zero port":   func(c *config.Config) { c.RconPort = 0 },
		"large port":  func(c *config.Config) { c.RconPort = 70000 },
	} {
		cfg := valid()
		mutate(cfg)
		if addr, ok := rconListenAddr(cfg); ok {
			t.Errorf("rcon %s was accepted at %q", name, addr)
		}
	}
}
