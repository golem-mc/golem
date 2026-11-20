// Package plugintest starts the real server in-process for a plugin's tests:
// join players, run commands as them and read what they were told.
//
//	func TestWarp(t *testing.T) {
//		srv := plugintest.Start(t, plugintest.Ops("Alice"))
//		alice := srv.Join("Alice")
//		alice.Run("/warp home")
//		if msgs := alice.Messages(2); len(msgs) == 0 {
//			t.Fatal("no reply")
//		}
//	}
//
// Importing it installs the test-only flat world generator, so build it into
// test binaries only.
package plugintest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// WorldType is the kind of world a test server generates.
type WorldType string

const (
	// WorldFlat is a stone floor up to y=63, spawn at (0.5, 64, 0.5).
	WorldFlat WorldType = "flat"
	// WorldNormal is the overworld generator, seeded by WithSeed.
	WorldNormal WorldType = "normal"
)

type options struct {
	plugins         []plugin.Registration
	gameMode        string
	world           WorldType
	seed            string
	ops             []string
	cache           []plugin.Profile
	spawnProtection int
}

// Option configures Start.
type Option func(*options)

// WithPlugins replaces the plugins to load; the default is plugin.Registered().
func WithPlugins(regs ...plugin.Registration) Option {
	return func(o *options) { o.plugins = regs }
}

// WithGameMode sets the mode new players join in: "survival" (the default),
// "creative", "adventure" or "spectator".
func WithGameMode(mode string) Option { return func(o *options) { o.gameMode = mode } }

// WithWorldType picks the world generator; the default is WorldFlat.
func WithWorldType(w WorldType) Option { return func(o *options) { o.world = w } }

// WithSeed sets the seed of a WorldNormal world. A flat world has no use for it.
func WithSeed(seed string) Option { return func(o *options) { o.seed = seed } }

// Ops lists the players who are operators from their first join.
func Ops(names ...string) Option { return func(o *options) { o.ops = append(o.ops, names...) } }

// WithUsercache makes the server know profiles that never joined, as if
// usercache.json held them: /seen, /mute and the like resolve their names.
func WithUsercache(entries ...plugin.Profile) Option {
	return func(o *options) { o.cache = append(o.cache, entries...) }
}

// WithSpawnProtection sets server.properties' spawn-protection radius. The
// default is 0 so test players can edit blocks near spawn without being ops.
func WithSpawnProtection(radius int) Option {
	return func(o *options) { o.spawnProtection = max(radius, 0) }
}

// Server is a running test server, stopped when the test ends.
type Server struct {
	t    *testing.T
	srv  *server.Server
	host plugin.Server
}

// captureName is the registration through which the harness receives the
// plugin.Server handle.
const captureName = "plugintest"

// Start runs a server in-process, with a listener per joined client, and the plugins
// enabled. Ticks run at the normal rate until HoldTicks.
func Start(t *testing.T, opts ...Option) *Server {
	t.Helper()
	o := options{gameMode: "survival", world: WorldFlat}
	for _, opt := range opts {
		opt(&o)
	}
	if o.plugins == nil {
		o.plugins = plugin.Registered()
	}

	s := &Server{t: t}
	capture := plugin.Registration{Name: captureName, Plugin: capturePlugin{s}}
	regs := append(append([]plugin.Registration(nil), o.plugins...), capture)

	cfg := servertest.SurvivalTestConfig()
	cfg.GameMode = o.gameMode
	cfg.LevelSeed = o.seed
	if o.world == WorldNormal {
		cfg.LevelType = "minecraft:normal"
	}
	// An ops list always exists, so that a player it does not name is no
	// operator.
	s.srv = servertest.NewTestServerWithPlugins(t, cfg, regs,
		servertest.OpsListing(t, o.ops...),
		usercacheOf(t, o.cache),
		servertest.SpawnProtectionRadius(o.spawnProtection),
	)
	if s.host == nil {
		t.Fatal("plugintest: the server did not enable its plugins")
	}
	return s
}

// usercacheOf seeds the server's user cache with entries, in the shape of
// vanilla's usercache.json.
func usercacheOf(t *testing.T, entries []plugin.Profile) func(*server.Server) {
	return func(s *server.Server) {
		if len(entries) == 0 {
			return
		}
		type record struct {
			UUID      string `json:"uuid"`
			Name      string `json:"name"`
			ExpiresOn string `json:"expiresOn"`
		}
		expires := time.Now().AddDate(0, 1, 0).Format("2006-01-02 15:04:05 -0700")
		records := make([]record, len(entries))
		for i, e := range entries {
			records[i] = record{UUID: e.ID.String(), Name: e.Name, ExpiresOn: expires}
		}
		data, err := json.Marshal(records)
		if err != nil {
			t.Fatalf("plugintest: encode usercache: %v", err)
		}
		path := filepath.Join(t.TempDir(), "usercache.json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("plugintest: write usercache: %v", err)
		}
		s.SetUsercacheFile(path)
	}
}

type capturePlugin struct{ s *Server }

func (c capturePlugin) Enable(h plugin.Server) error { c.s.host = h; return nil }
func (capturePlugin) Disable()                       {}

// Host is the plugin API as a plugin sees it, for reading or changing the
// server's state from the test.
func (s *Server) Host() plugin.Server { return s.host }

// Console runs a console command and returns its output lines as plain text.
func (s *Server) Console(line string) []string {
	return plainLines(s.srv.RunLine(line))
}

// Run calls f on the tick goroutine, where a plugin's handles may be used,
// and returns once it has run, after one tick.
func (s *Server) Run(f func()) {
	s.t.Helper()
	ran := false
	s.host.Scheduler().RunTask(func() { f(); ran = true })
	s.RunTicks(1)
	if !ran {
		s.t.Fatal("plugintest: the task did not run in a tick")
	}
}

// RunTicks returns after n whole ticks have run.
func (s *Server) RunTicks(n int) {
	s.t.Helper()
	servertest.RunTicks(s.t, s.srv, n)
}

// HoldTicks stops the tick loop from advancing by itself: from here a tick
// runs only in RunTicks or a Player reader (Messages, Read and the like). Join
// players first.
//
// While ticks are held, a plugin call that waits for the tick loop to run
// (World.Spawn and the other calls that hand work to the main thread) blocks
// until the next RunTicks, so call it from a goroutine that the test then
// advances with RunTicks, or before HoldTicks.
func (s *Server) HoldTicks() {
	s.t.Helper()
	servertest.HoldTicks(s.t, s.srv)
}

// Join connects a client under name and returns once it is in the world.
func (s *Server) Join(name string) *Player {
	s.t.Helper()
	c := servertest.DialServer(s.t, s.srv)
	w := servertest.WindowedOf(s.t, name, c)
	return &Player{s: s, c: c, w: w, name: name}
}

func plainLines(texts []protocol.Text) []string {
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = t.Plain()
	}
	return out
}
