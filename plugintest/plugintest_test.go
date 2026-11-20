package plugintest_test

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/plugintest"
	"github.com/golem-mc/golem/registry"
)

func TestOpRunsCommandsAndHandleIsLive(t *testing.T) {
	srv := plugintest.Start(t, plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}))
	srv.HoldTicks()
	bob := srv.Join("Bob")

	bob.Run("/gamemode creative")
	if msgs := bob.Messages(2); len(msgs) == 0 || !strings.Contains(msgs[0], "Unknown or incomplete command") {
		t.Fatalf("a non-op got %q, want the refusal", msgs)
	}

	bob.Op()
	bob.Run("/gamemode creative")
	if msgs := bob.Messages(2); len(msgs) == 0 || strings.Contains(msgs[0], "Unknown") {
		t.Fatalf("an op got %q, want the gamemode feedback", msgs)
	}
	if got := bob.Handle().Name(); got != "Bob" {
		t.Fatalf("handle name = %q", got)
	}
	if out := srv.Console("list"); !strings.Contains(strings.Join(out, "\n"), "Bob") {
		t.Fatalf("console list = %q, want Bob", out)
	}
}

type noop struct{}

func (noop) Enable(plugin.Server) error { return nil }
func (noop) Disable()                   {}

type disableRecorder struct{ calls *atomic.Int32 }

func (disableRecorder) Enable(plugin.Server) error { return nil }
func (r disableRecorder) Disable()                 { r.calls.Add(1) }

func TestDisableOnCleanup(t *testing.T) {
	var calls atomic.Int32
	t.Run("server", func(t *testing.T) {
		plugintest.Start(t, plugintest.WithPlugins(plugin.Registration{Name: "rec", Plugin: disableRecorder{&calls}}))
		if calls.Load() != 0 {
			t.Fatal("disabled while running")
		}
	})
	if got := calls.Load(); got != 1 {
		t.Fatalf("Disable called %d times on cleanup, want 1", got)
	}
}

func TestClientActionsReachPlugins(t *testing.T) {
	srv := plugintest.Start(t, plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}), plugintest.Ops("Alice"), plugintest.WithGameMode("creative"))
	alice := srv.Join("Alice")
	bob := srv.Join("Bob")

	var placed, dug, clicked, attacked, interacted, used atomic.Int32
	plugin.Listen(srv.Host(), func(e *plugin.BlockPlaceEvent) { placed.Add(1) })
	plugin.Listen(srv.Host(), func(e *plugin.BlockBreakEvent) { dug.Add(1) })
	plugin.Listen(srv.Host(), func(e *plugin.InventoryClickEvent) { clicked.Add(1) })
	plugin.Listen(srv.Host(), func(e *plugin.PlayerAttackEvent) { attacked.Add(1) })
	plugin.Listen(srv.Host(), func(e *plugin.PlayerInteractEntityEvent) { interacted.Add(1) })
	plugin.Listen(srv.Host(), func(e *plugin.PlayerInteractEvent) { used.Add(1) })

	alice.Handle().Inventory().SetItem(0, plugin.NewStack(registry.ItemStone, 8))
	alice.PlaceBlock(3, 63, 0, 1)
	alice.Read(2)
	if placed.Load() != 1 {
		t.Fatalf("BlockPlaceEvent fired %d times, want 1", placed.Load())
	}

	alice.StartBreak(3, 64, 0)
	alice.FinishBreak(3, 64, 0)
	alice.Read(40)
	if dug.Load() == 0 {
		t.Fatal("no BlockBreakEvent for the dig")
	}

	alice.Click(36, 0, 0)
	alice.Drag(false, 36, 37)
	alice.Read(1)
	if clicked.Load() == 0 {
		t.Fatal("no InventoryClickEvent")
	}

	alice.Attack(bob.Handle().ID())
	alice.Interact(bob.Handle().ID())
	alice.SelectHotbar(0)
	alice.UseItem()
	alice.Read(2)
	if attacked.Load() != 1 || interacted.Load() != 1 || used.Load() == 0 {
		t.Fatalf("attack %d, interact %d, use %d", attacked.Load(), interacted.Load(), used.Load())
	}
}

func TestSwingIsALeftClickInTheAir(t *testing.T) {
	srv := plugintest.Start(t, plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}))
	alice := srv.Join("Alice")

	var airs, others, swings atomic.Int32
	plugin.Listen(srv.Host(), func(e *plugin.PlayerInteractEvent) {
		if e.Action == plugin.LeftClickAir {
			airs.Add(1)
		} else {
			others.Add(1)
		}
	})
	plugin.Listen(srv.Host(), func(e *plugin.PlayerAnimationEvent) { swings.Add(1) })

	alice.Swing()
	alice.Read(2)
	if airs.Load() != 1 || others.Load() != 0 || swings.Load() != 1 {
		t.Fatalf("left click air %d, other interacts %d, animation %d; want 1, 0, 1", airs.Load(), others.Load(), swings.Load())
	}
}

func TestDisconnectAndKick(t *testing.T) {
	srv := plugintest.Start(t, plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}))
	srv.HoldTicks()
	alice := srv.Join("Alice")
	bob := srv.Join("Bob")

	bob.Handle().Kick(plugin.Plain("bye"))
	if msgs := bob.Messages(2); len(msgs) != 0 {
		t.Fatalf("a kicked player read %q", msgs)
	}
	if reason, ok := bob.Kicked(); !ok || reason != "bye" {
		t.Fatalf("Kicked = %q, %v, want bye", reason, ok)
	}
	bob.Messages(1) // reading on after a kick is not an error

	alice.Disconnect()
	if _, ok := srv.Host().PlayerNamed("Alice"); ok {
		t.Fatal("Alice is still online after Disconnect")
	}
}

func TestUsercacheAndPacketReaders(t *testing.T) {
	id := plugin.UUID{1, 2, 3}
	srv := plugintest.Start(t, plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}),
		plugintest.WithUsercache(plugin.Profile{ID: id, Name: "Ghost"}))
	alice := srv.Join("Alice")
	bob := srv.Join("Bob")

	if p, ok := srv.Host().OfflinePlayerNamed("Ghost"); !ok || p.UUID() != id {
		t.Fatalf("OfflinePlayerNamed(Ghost) = %v, %v, want the cached profile", p, ok)
	}

	bob.Handle().SendActionBar(plugin.Plain("bar"))
	bob.Handle().SendTitle(plugin.Plain("head"), plugin.Plain("sub"), 5, 20, 5)
	if got := bob.ActionBars(1); len(got) != 1 || got[0] != "bar" {
		t.Fatalf("action bars = %q", got)
	}
	if titles, subs := bob.Titles(1); len(titles) != 1 || titles[0] != "head" || len(subs) != 1 || subs[0] != "sub" {
		t.Fatalf("titles = %q %q", titles, subs)
	}
	alice.Run("hello")
	if got := bob.Chat(2); len(got) != 1 || got[0].Message != "hello" {
		t.Fatalf("chat = %+v", got)
	}
}

func TestExplosionAndSoundReaders(t *testing.T) {
	srv := plugintest.Start(t, plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}))
	alice := srv.Join("Alice")
	w := alice.Handle().World()

	w.Explode(plugin.Vec3{X: 0.5, Y: 70, Z: 5.5}, 3, false, plugin.ExplosionKeep, nil)
	if got := alice.Explosions(2); len(got) != 1 || got[0].Radius != 3 {
		t.Fatalf("explosions = %+v, want one of radius 3", got)
	}
	w.PlaySound(plugin.Vec3{X: 0.5, Y: 64, Z: 0.5}, registry.SoundEntityGenericExplode, plugin.SoundBlocks, 1, 1)
	got := alice.Sounds(2)
	found := false
	for _, s := range got {
		found = found || s.Name == "minecraft:entity.generic.explode"
	}
	if !found {
		t.Fatalf("sounds = %+v, want the generic explode", got)
	}
}

func TestSchedulerOnTick(t *testing.T) {
	srv := plugintest.Start(t, plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}))
	host := srv.Host()
	sched := host.Scheduler()

	if sched.OnTick() {
		t.Fatal("OnTick = true on the test goroutine, want false")
	}

	var cmdOnTick, consoleOnTick, listenerOnTick, taskOnTick, asyncOnTick atomic.Bool
	cmdRan := make(chan struct{}, 2)
	if err := host.Commands().Register(plugin.Command{
		Name: "ontick",
		Run: func(ctx *plugin.CommandContext) (int, error) {
			if _, ok := ctx.Sender.(plugin.Player); ok {
				cmdOnTick.Store(sched.OnTick())
			} else {
				consoleOnTick.Store(sched.OnTick())
			}
			cmdRan <- struct{}{}
			return 1, nil
		},
	}); err != nil {
		t.Fatalf("register command: %v", err)
	}

	listenerDone := make(chan struct{}, 1)
	plugin.Listen(host, func(*plugin.PlayerAnimationEvent) {
		listenerOnTick.Store(sched.OnTick())
		select {
		case listenerDone <- struct{}{}:
		default:
		}
	})

	alice := srv.Join("Alice")
	alice.Run("/ontick")
	alice.Read(2)
	<-cmdRan

	srv.Console("ontick")
	<-cmdRan

	alice.Swing()
	alice.Read(2)
	<-listenerDone

	taskDone := make(chan struct{})
	sched.RunTask(func() {
		taskOnTick.Store(sched.OnTick())
		close(taskDone)
	})
	<-taskDone

	asyncDone := make(chan struct{})
	sched.RunAsync(func() {
		asyncOnTick.Store(sched.OnTick())
		close(asyncDone)
	})
	<-asyncDone

	if !cmdOnTick.Load() {
		t.Error("OnTick = false inside a player command's Run, want true")
	}
	if !consoleOnTick.Load() {
		t.Error("OnTick = false inside a console command's Run, want true")
	}
	if !listenerOnTick.Load() {
		t.Error("OnTick = false inside an event listener, want true")
	}
	if !taskOnTick.Load() {
		t.Error("OnTick = false inside a RunTask task, want true")
	}
	if asyncOnTick.Load() {
		t.Error("OnTick = true inside a RunAsync task, want false")
	}
}

type enableFunc func(plugin.Server) error

func (f enableFunc) Enable(s plugin.Server) error { return f(s) }
func (enableFunc) Disable()                       {}

func TestCommandFallbackPrefixWhenNameTaken(t *testing.T) {
	say := func(msg string) func(*plugin.CommandContext) (int, error) {
		return func(ctx *plugin.CommandContext) (int, error) {
			if arg := plugin.Arg[string](ctx, "arg"); arg != "" {
				ctx.Sender.SendMessage(plugin.Plain(msg + ":" + arg))
				return 1, nil
			}
			ctx.Sender.SendMessage(plugin.Plain(msg))
			return 1, nil
		}
	}
	godCmd := func(msg string, aliases ...string) plugin.Command {
		return plugin.Command{
			Name:     "god",
			Aliases:  aliases,
			Run:      say(msg),
			Children: []plugin.Node{{Arg: plugin.WordArg("arg"), Run: say(msg)}},
		}
	}

	var bErr, cErr, dupErr, vanillaErr error
	srv := plugintest.Start(t, plugintest.WithPlugins(
		plugin.Registration{Name: "a", Plugin: enableFunc(func(s plugin.Server) error {
			if err := s.Commands().Register(godCmd("from-a")); err != nil {
				return err
			}
			// Registering the same command again from the same plugin leaves no free name.
			dupErr = s.Commands().Register(godCmd("from-a-again"))
			return nil
		})},
		plugin.Registration{Name: "b", Plugin: enableFunc(func(s plugin.Server) error {
			bErr = s.Commands().Register(godCmd("from-b", "godmode"))
			vanillaErr = s.Commands().Register(plugin.Command{Name: "seed", Run: say("b-seed")})
			return nil
		})},
		plugin.Registration{Name: "c", Plugin: enableFunc(func(s plugin.Server) error {
			cErr = s.Commands().Register(godCmd("from-c"))
			return cErr
		})},
		plugin.Registration{Name: "d", Plugin: enableFunc(func(s plugin.Server) error {
			_ = s.Commands().Register(plugin.Command{Name: "dcmd", Run: say("from-d")})
			return errors.New("unrelated enable failure")
		})},
	))

	if !errors.Is(bErr, plugin.ErrNameTaken) || !strings.Contains(bErr.Error(), "god") {
		t.Fatalf("plugin b Register(god) = %v, want ErrNameTaken naming god", bErr)
	}
	if !errors.Is(cErr, plugin.ErrNameTaken) || !strings.Contains(cErr.Error(), "god") {
		t.Fatalf("plugin c Register(god) = %v, want ErrNameTaken naming god", cErr)
	}
	if dupErr == nil || errors.Is(dupErr, plugin.ErrNameTaken) {
		t.Fatalf("duplicate Register(god) by same plugin = %v, want non-ErrNameTaken failure", dupErr)
	}
	if !errors.Is(vanillaErr, plugin.ErrNameTaken) || !strings.Contains(vanillaErr.Error(), "seed") {
		t.Fatalf("plugin b Register(seed) = %v, want ErrNameTaken naming seed", vanillaErr)
	}

	enabled := make(map[string]bool)
	for _, info := range srv.Host().Plugins() {
		enabled[info.Name] = info.Enabled
	}
	for _, name := range []string{"a", "b", "c"} {
		if !enabled[name] {
			t.Errorf("plugin %q Enabled = false, want true", name)
		}
	}
	if isEnabled, ok := enabled["d"]; !ok || isEnabled {
		t.Errorf("plugin d enabled=%v (present=%v), want false (present=true)", isEnabled, ok)
	}
	for _, cmd := range srv.Host().Commands().All() {
		if cmd.Plugin == "d" {
			t.Errorf("disabled plugin d still has registered command %q", cmd.Name)
		}
	}

	alice := srv.Join("Alice")
	for _, tc := range []struct {
		cmd  string
		want string
	}{
		{"/god", "from-a"},
		{"/a:god", "from-a"},
		{"/b:god", "from-b"},
		{"/b:god on", "from-b:on"},
		{"/godmode", "from-b"},
		{"/godmode on", "from-b:on"},
		{"/b:godmode", "from-b"},
		{"/b:seed", "b-seed"},
		{"/c:god", "from-c"},
		{"/c:god on", "from-c:on"},
	} {
		alice.Run(tc.cmd)
		if msgs := alice.Messages(2); len(msgs) != 1 || msgs[0] != tc.want {
			t.Errorf("%s -> %q, want [%q]", tc.cmd, msgs, tc.want)
		}
	}
	// Vanilla /seed stays untouched.
	if out := srv.Console("seed"); !strings.Contains(strings.Join(out, "\n"), "Seed:") {
		t.Errorf("console seed = %q, want vanilla Seed output", out)
	}
}

type hostCapture struct{ host *plugin.Server }

func (c hostCapture) Enable(s plugin.Server) error { *c.host = s; return nil }
func (hostCapture) Disable()                       {}

func TestSpawnProtection(t *testing.T) {
	startWithConfig := func(t *testing.T, modify func(*config.Config)) plugin.Server {
		t.Helper()
		cfg := config.Default()
		cfg.OnlineMode = false
		cfg.LevelName = t.TempDir()
		cfg.LevelType = server.TestFlatLevelType
		cfg.SimulationDistance = 4
		cfg.PauseWhenEmptySeconds = 0
		cfg.SyncChunkWrites = false
		if modify != nil {
			modify(cfg)
		}
		var host plugin.Server
		srv, err := server.NewSeededSharingNether(cfg, 0, server.SharedNetherSeed,
			plugin.Registration{Name: "capture", Plugin: hostCapture{&host}},
		)
		if err != nil {
			t.Fatalf("create server: %v", err)
		}
		t.Cleanup(func() {
			if err := srv.CloseWorlds(); err != nil {
				t.Errorf("close worlds: %v", err)
			}
		})
		return host
	}

	t.Run("default properties", func(t *testing.T) {
		host := startWithConfig(t, nil)
		if got := host.SpawnProtection(); got != 16 {
			t.Fatalf("SpawnProtection() = %d, want 16 (vanilla default)", got)
		}
	})

	t.Run("disabled in properties", func(t *testing.T) {
		host := startWithConfig(t, func(cfg *config.Config) { cfg.SpawnProtection = 0 })
		if got := host.SpawnProtection(); got != 0 {
			t.Fatalf("SpawnProtection() = %d, want 0", got)
		}
	})

	t.Run("plugintest option", func(t *testing.T) {
		srvDefault := plugintest.Start(t, plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}))
		if got := srvDefault.Host().SpawnProtection(); got != 0 {
			t.Fatalf("plugintest default SpawnProtection() = %d, want 0", got)
		}
		srvSet := plugintest.Start(t,
			plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}),
			plugintest.WithSpawnProtection(16),
		)
		if got := srvSet.Host().SpawnProtection(); got != 16 {
			t.Fatalf("plugintest WithSpawnProtection(16) = %d, want 16", got)
		}
		srvOff := plugintest.Start(t,
			plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}),
			plugintest.WithSpawnProtection(0),
		)
		if got := srvOff.Host().SpawnProtection(); got != 0 {
			t.Fatalf("plugintest WithSpawnProtection(0) = %d, want 0", got)
		}
	})
}

func TestDefaultGameMode(t *testing.T) {
	srv := plugintest.Start(t, plugintest.WithGameMode("creative"), plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}))
	srv.HoldTicks()
	h := srv.Host()
	if got := h.DefaultGameMode(); got != plugin.Creative {
		t.Fatalf("DefaultGameMode = %d, want creative", got)
	}
	srv.Console("defaultgamemode adventure")
	if got := h.DefaultGameMode(); got != plugin.Adventure {
		t.Fatalf("after /defaultgamemode = %d, want adventure", got)
	}
	h.SetDefaultGameMode(plugin.Spectator)
	if got := h.DefaultGameMode(); got != plugin.Spectator {
		t.Fatalf("after Set = %d, want spectator", got)
	}
	h.SetDefaultGameMode(plugin.GameMode(9))
	if got := h.DefaultGameMode(); got != plugin.Spectator {
		t.Fatalf("out-of-range Set changed the mode to %d", got)
	}
}

// vanilla: Arrow.getPotionContents, Projectile.spawnProjectile
func TestTippedArrowAndLaunchRefusal(t *testing.T) {
	srv := plugintest.Start(t, plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: noop{}}))
	srv.HoldTicks()
	bob := srv.Join("Bob")

	var launched atomic.Int32
	var cancel atomic.Bool
	plugin.Listen(srv.Host(), func(e *plugin.ProjectileLaunchEvent) {
		launched.Add(1)
		e.SetCancelled(cancel.Load())
	})

	arrow, err := bob.ShootTippedArrow(plugin.Key{Namespace: "minecraft", Path: "poison"})
	if err != nil {
		t.Fatal(err)
	}
	if launched.Load() != 1 {
		t.Fatalf("ProjectileLaunchEvent fired %d times, want 1", launched.Load())
	}
	key, effects, ok := arrow.Potion()
	if !ok || key.String() != "minecraft:poison" || len(effects) != 1 || effects[0].Duration <= 0 {
		t.Fatalf("Potion() = %v %v %v, want poison with one effect", key, effects, ok)
	}

	if _, err := bob.ShootTippedArrow(plugin.Key{Namespace: "minecraft", Path: "nonsense"}); err == nil {
		t.Fatal("an unknown potion tipped an arrow")
	}

	cancel.Store(true)
	if _, err := bob.ShootTippedArrow(plugin.Key{Namespace: "minecraft", Path: "poison"}); err == nil {
		t.Fatal("a cancelled launch returned an arrow")
	}
	if launched.Load() != 3 {
		t.Fatalf("ProjectileLaunchEvent fired %d times, want 3", launched.Load())
	}
}
