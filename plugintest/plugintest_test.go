package plugintest_test

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

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

	var bErr, dupErr, vanillaErr error
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
	))

	if !errors.Is(bErr, plugin.ErrNameTaken) || !strings.Contains(bErr.Error(), "god") {
		t.Fatalf("plugin b Register(god) = %v, want ErrNameTaken naming god", bErr)
	}
	if dupErr == nil || errors.Is(dupErr, plugin.ErrNameTaken) {
		t.Fatalf("duplicate Register(god) by same plugin = %v, want non-ErrNameTaken failure", dupErr)
	}
	if !errors.Is(vanillaErr, plugin.ErrNameTaken) || !strings.Contains(vanillaErr.Error(), "seed") {
		t.Fatalf("plugin b Register(seed) = %v, want ErrNameTaken naming seed", vanillaErr)
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
