package plugins_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

// vanilla: ClientboundSetTimePacket, ClientboundGameEventPacket
func TestPluginPlayerTimeAndWeather(t *testing.T) {
	t.Parallel()
	srv, h, run, cs := statePlugin(t, "Sky")
	p := handle(t, h, "Sky")
	run(func() { p.SetPlayerTime(6000, false) })
	r := protocol.NewReader(lastPacket(cs[0], srv, protocol.IDPlayUpdateTime))
	r.ReadI64()
	if r.ReadVarInt() != 1 {
		t.Fatal("one clock expected")
	}
	r.ReadVarInt()
	if total := r.ReadVarLong(); total != 6000 {
		t.Fatalf("clock %d", total)
	}
	r.ReadF32()
	if rate := r.ReadF32(); rate != 0 {
		t.Fatalf("fixed clock rate %v", rate)
	}
	if ticks, fixed, over := p.PlayerTime(); ticks != 6000 || !fixed || !over {
		t.Fatal("PlayerTime")
	}
	run(func() { p.ResetPlayerTime() })
	if _, _, over := p.PlayerTime(); over {
		t.Fatal("time override stays")
	}

	run(func() { p.SetPlayerWeather(true) })
	var events []byte
	cs[0].ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id == protocol.IDPlayGameEvent {
			events = append(events, data[0])
		}
		return false
	})
	if len(events) != 3 || events[0] != protocol.GameEventBeginRaining {
		t.Fatalf("weather events %v", events)
	}
	if rain, over := p.PlayerWeather(); !rain || !over {
		t.Fatal("PlayerWeather")
	}
	run(func() { p.ResetPlayerWeather() })
	if _, over := p.PlayerWeather(); over {
		t.Fatal("weather override stays")
	}
}

// vanilla: ServerStatsCounter, UserNameToIdResolver
func TestPluginOfflineStatisticAndNameLookup(t *testing.T) {
	t.Parallel()
	srv, h, run, cs := statePlugin(t, "Leaver")
	p := handle(t, h, "Leaver")
	jump, ok := h.Registries().Statistic(plugin.MinecraftKey("jump"))
	if !ok {
		t.Fatal("jump statistic")
	}
	run(func() { p.SetStatistic(jump, 7) })
	id := p.UUID()
	cs[0].Conn.Close()
	cs[0].AwaitServed()
	servertest.RunTicks(t, srv, 1)
	if p.IsValid() {
		t.Fatal("handle valid after quit")
	}
	off, ok := h.OfflinePlayerNamed("lEAVER")
	if !ok || off.UUID() != id || off.Name() != "Leaver" {
		t.Fatalf("lookup %v %v", off, ok)
	}
	if got := off.Statistic(jump); got != 7 {
		t.Fatalf("offline statistic %d", got)
	}
	if profile, ok := h.ProfileByName("leaver"); !ok || profile.ID != id {
		t.Fatal("profile lookup")
	}
	if _, ok := h.OfflinePlayerNamed("Nobody"); ok {
		t.Fatal("unknown name found")
	}
}

func TestPluginOfflinePlayers(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	levelDir := t.TempDir()
	cfg.LevelName = levelDir
	var h plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "offline", Plugin: &testPlugin{enable: func(s plugin.Server) error { h = s; return nil }}}})

	// Empty server: nobody has played yet.
	if got := h.OfflinePlayers(); len(got) != 0 {
		t.Fatalf("empty server OfflinePlayers() = %d, want 0", len(got))
	}

	join := func(name string) (*servertest.Client, plugin.UUID) {
		c := servertest.DialServer(t, srv)
		c.ViewAsk = config.MinDistance
		c.Join(name)
		p := handle(t, h, name)
		return c, p.UUID()
	}

	alice, aliceID := join("Alice")
	bob, bobID := join("Bob")
	_, carolID := join("Carol")

	alice.Conn.Close()
	alice.AwaitServed()
	bob.Conn.Close()
	bob.AwaitServed()

	// Add a player data file with no usercache entry, plus non-player files
	// that OfflinePlayers must ignore.
	dataDir := filepath.Join(levelDir, "players", "data")
	ghostID := uuid.MustParse("00000000-0000-0000-0000-000000000042")
	for _, name := range []string{
		ghostID.String() + ".dat",
		uuid.MustParse("00000000-0000-0000-0000-000000000099").String() + ".dat_old",
		uuid.MustParse("00000000-0000-0000-0000-000000000098").String() + ".dat_tmp",
		ghostID.String() + "_corrupted_2026-01-01_00-00-00.dat",
		"not-a-uuid.dat",
	} {
		if err := os.WriteFile(filepath.Join(dataDir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dataDir, uuid.MustParse("00000000-0000-0000-0000-000000000097").String()+".dat"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Call from an async goroutine to verify thread safety off the tick loop.
	done := make(chan []plugin.OfflinePlayer, 1)
	h.Scheduler().RunAsync(func() { done <- h.OfflinePlayers() })
	got := <-done

	byID := make(map[plugin.UUID]plugin.OfflinePlayer, len(got))
	for _, p := range got {
		if _, dup := byID[p.UUID()]; dup {
			t.Fatalf("duplicate UUID %v in OfflinePlayers", p.UUID())
		}
		byID[p.UUID()] = p
	}
	if len(byID) != 4 {
		t.Fatalf("OfflinePlayers() returned %d players, want 4", len(byID))
	}

	want := []struct {
		id     plugin.UUID
		name   string
		online bool
	}{
		{aliceID, "Alice", false},
		{bobID, "Bob", false},
		{carolID, "Carol", true},
		{plugin.UUID(ghostID), "", false},
	}
	for _, w := range want {
		p, ok := byID[w.id]
		if !ok {
			t.Fatalf("missing player %q (%v)", w.name, w.id)
		}
		if p.Name() != w.name {
			t.Errorf("player %v Name() = %q, want %q", w.id, p.Name(), w.name)
		}
		if _, online := p.Online(); online != w.online {
			t.Errorf("player %q Online() = %v, want %v", w.name, online, w.online)
		}
		if w.name == "Alice" || w.name == "Bob" {
			if p.FirstPlayed().IsZero() || p.LastSeen().IsZero() {
				t.Errorf("offline player %q missing timestamps: first=%v last=%v", w.name, p.FirstPlayed(), p.LastSeen())
			}
		}
	}

	// Alice rejoins while her .dat file is on disk: still one entry, now online.
	alice2, _ := join("Alice")
	defer alice2.Conn.Close()
	got2 := h.OfflinePlayers()
	if len(got2) != 4 {
		t.Fatalf("after Alice rejoined, OfflinePlayers() returned %d players, want 4", len(got2))
	}
	for _, p := range got2 {
		if p.UUID() == aliceID {
			if _, online := p.Online(); !online {
				t.Error("rejoined Alice is not online")
			}
		}
	}
}
