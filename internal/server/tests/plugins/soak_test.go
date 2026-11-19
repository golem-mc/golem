package plugins_test

import (
	"fmt"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// soakServer starts a survival server whose plugin registers a command and a
// no-op listener for every hook.
func soakServer(t *testing.T) *server.Server {
	t.Helper()
	return servertest.NewTestServerWithPlugins(t, servertest.SurvivalTestConfig(), []plugin.Registration{{Name: "soak", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		for _, typ := range pluginruntime.HookEventTypes() {
			s.Events().Register(typ, func(plugin.Event) {}, plugin.ListenOptions{Priority: plugin.Normal})
		}
		return s.Commands().Register(plugin.Command{Name: "soakcmd", Run: func(*plugin.CommandContext) (int, error) { return 1, nil }})
	}}}})
}

// watchdogTicks runs n ticks and fails with a goroutine dump if one takes over 5 s.
func watchdogTicks(t *testing.T, srv *server.Server, n int) {
	t.Helper()
	for range n {
		done := make(chan struct{})
		go func() {
			defer close(done)
			servertest.RunTicks(t, srv, 1)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 2)
			t.Fatal("a tick took over 5s")
		}
	}
}

// actTicking runs f, which may wait for a tick, while ticking the held loop.
func actTicking(t *testing.T, srv *server.Server, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	for {
		select {
		case <-done:
			return
		default:
			watchdogTicks(t, srv, 1)
		}
	}
}

// Respawn runs on the tick goroutine when a plugin command is registered.
func TestPluginSoakRespawnWithCommand(t *testing.T) {
	srv := soakServer(t)
	c := servertest.DialServer(t, srv)
	c.Join("Soaker")
	if !srv.Hurt("Soaker", 50) {
		t.Fatal("lethal hurt refused")
	}
	servertest.HoldTicks(t, srv)
	actTicking(t, srv, func() {
		c.PerformRespawn()
		c.ExpectRespawnThroughTeleport()
	})
	c.Chat("hello")
	c.ChatCommand("soakcmd")
	watchdogTicks(t, srv, 10)
	servertest.ReleaseTicks(t, srv)
}

// A few hundred ticks of everything a plugin listens to, with a command
// registered, must never stall a tick.
func TestPluginSoakScenario(t *testing.T) {
	srv := soakServer(t)
	srv.SetDifficulty(3)
	c := servertest.DialServer(t, srv)
	c.Join("Soaker")
	c.MoveToGround(0.5, 64, 0.5, true)
	other := servertest.DialServer(t, srv)
	other.Join("Other")
	servertest.HoldTicks(t, srv)
	connected := true
	act := func(f func()) { actTicking(t, srv, f) }
	run := func(ticks int) {
		watchdogTicks(t, srv, ticks)
		if connected {
			// Connection handlers wait for a tick to run plugin callbacks.
			act(func() { c.Drain(); other.Drain() })
		}
	}
	line := func(l string) { act(func() { srv.RunLine(l) }) }
	spawn := func(kind string, x, z float64) {
		line(fmt.Sprintf("summon minecraft:%s %.1f 64 %.1f", kind, x, z))
	}

	// Spectate a mob and teleport to another player.
	var cow int32
	act(func() { cow = srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5) })
	act(func() { srv.SetPlayerGameMode("Soaker", protocol.GameModeSpectator) })
	run(2)
	c.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(cow + 1) })
	c.Send(protocol.IDPlayTeleportToEntity, func(b *protocol.Buffer) { b.PutUUID(protocol.OfflineUUID("Other")) })
	run(10)
	act(func() { srv.SetPlayerGameMode("Soaker", protocol.GameModeSurvival) })
	run(5)

	// Mobs fighting each other.
	for i, pair := range [][2]string{{"zombie", "villager"}, {"wolf", "skeleton"}, {"iron_golem", "zombie"}} {
		x := 60 + 8*float64(i)
		spawn(pair[0], x, 60)
		spawn(pair[1], x+1.5, 60)
	}
	run(100)

	// Blasts near mobs, items and an armour stand.
	spawn("armor_stand", 6.5, 6.5)
	line("summon minecraft:item 7.5 64 6.5 {Item:{id:\"minecraft:stone\",count:3}}")
	spawn("cow", 8.5, 6.5)
	line("summon minecraft:zombie 5.5 64 8.5 {NoAI:1b}")
	line("summon minecraft:creeper 7.5 64 8.5 {Fuse:1,ignited:1b}")
	line("summon minecraft:tnt 6.5 64 5.5 {fuse:1}")
	run(40)

	// Blocks and containers.
	act(func() {
		srv.GiveSlot("Soaker", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:chest"), Count: 4})
	})
	act(func() { srv.SetBlock(0, 63, 3, servertest.BlockState(t, "minecraft:stone")) })
	c.PlaceBlock(0, 63, 3, protocol.FaceTop)
	run(5)
	c.UseBlock(0, 64, 3)
	run(5)
	c.Send(protocol.IDPlayCloseContainerIn, func(b *protocol.Buffer) { b.PutVarInt(1) })
	c.BreakBlock(0, 64, 3)
	run(10)
	c.Chat("hello")
	c.ChatCommand("soakcmd")
	run(10)

	// Leave and rejoin.
	connected = false
	act(other.Disconnect)
	act(c.Disconnect)
	run(10)
	servertest.ReleaseTicks(t, srv)
	again := servertest.DialServer(t, srv)
	again.Join("Soaker")
	watchdogTicks(t, srv, 20)
}
