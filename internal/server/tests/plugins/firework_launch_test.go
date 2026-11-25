package plugins_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// fireworkListener counts launches and spawns of rockets, writes plugin data
// to each at launch and cancels or removes it at the launch or the spawn
// while its flags are set.
type fireworkListener struct {
	launches, spawns          int
	cancelLaunch, cancelSpawn bool
	removeLaunch, removeSpawn bool
	// lastID is the UUID of the latest launched rocket; originless counts
	// launches whose projectile had no origin.
	lastID     plugin.UUID
	originless int
}

var dataKey = plugin.Key{Namespace: "fx", Path: "k"}

func (f *fireworkListener) listen(s plugin.Server) {
	plugin.Listen(s, func(e *plugin.ProjectileLaunchEvent) {
		if e.Projectile.Type() != protocol.EntityTypeFireworkRocket {
			return
		}
		f.launches++
		f.lastID = e.Projectile.UUID()
		if _, ok := e.Projectile.Origin(); !ok {
			f.originless++
		}
		e.Projectile.Data().Set(dataKey, "v")
		e.SetCancelled(f.cancelLaunch)
		if f.removeLaunch {
			e.Projectile.Remove()
		}
	})
	plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
		if e.Entity.Type() != protocol.EntityTypeFireworkRocket {
			return
		}
		f.spawns++
		e.SetCancelled(f.cancelSpawn)
		if f.removeSpawn {
			e.Entity.Remove()
		}
	})
}

// rocketShot is one scene that can fire a rocket again and again.
type rocketShot struct {
	srv *server.Server
	c   *servertest.Client
	// fire arms the scene and triggers one shot.
	fire func()
	// byPacket is set when the shot's events are queued by a connection.
	byPacket bool
}

// shotResult is what a shot left behind.
type shotResult struct{ spawns, removes int }

// run fires one shot, lets the tick goroutine run both events it reaches and
// checks their counts: a cancelled or removed launch never reaches the spawn event. The
// loop is held; a packet's listeners are each asked at a tick's start, so a
// tick is run for every call its connection queues until the packet is done
// with, then one more as for a scheduled task.
func (r *rocketShot) run(t *testing.T, l *fireworkListener) (res shotResult) {
	t.Helper()
	queued := r.srv.PluginCallsQueued()
	select {
	case <-queued:
	default:
	}
	launches, spawns := l.launches, l.spawns
	record := func(id int32, _ []byte) {
		switch id {
		case protocol.IDPlaySpawnEntity:
			res.spawns++
		case protocol.IDPlayRemoveEntities:
			res.removes++
		}
	}
	r.fire()
	if r.byPacket {
		// The settle's ping is answered once the packet's last call has run;
		// res is only read here after it ends.
		settled := make(chan struct{})
		go func() {
			defer close(settled)
			r.c.SettleAndCollect(record)
		}()
		for waiting := true; waiting; {
			select {
			case <-queued:
				servertest.RunTicks(t, r.srv, 1)
			case <-settled:
				waiting = false
			}
		}
		if t.Failed() {
			t.FailNow()
		}
	}
	servertest.RunTicks(t, r.srv, 1)
	r.c.SettleAndCollect(record)
	wantSpawns := 1
	if l.cancelLaunch || l.removeLaunch {
		wantSpawns = 0
	}
	if l.launches-launches != 1 || l.spawns-spawns != wantSpawns {
		t.Fatalf("the shot ran %d launch and %d spawn events, want 1 and %d", l.launches-launches, l.spawns-spawns, wantSpawns)
	}
	return res
}

func chargedCrossbow(t *testing.T, enchantment string) protocol.ItemStack {
	t.Helper()
	crossbow := servertest.ItemStack(t, "minecraft:crossbow", 1)
	if enchantment != "" {
		crossbow = servertest.EnchantedStack(t, "minecraft:crossbow", enchantment, 1)
	}
	rocket := servertest.ItemStack(t, "minecraft:firework_rocket", 1)
	if enchantment != "" {
		rocket = servertest.EnchantedStack(t, "minecraft:firework_rocket", enchantment, 1)
	}
	crossbow.SetChargedProjectiles([]protocol.ItemStack{rocket})
	return crossbow
}

// TestPluginFireworkLaunchEvents checks a crossbow-shot, a used and a
// dispensed rocket reach ProjectileLaunchEvent and EntitySpawnEvent, and a
// refused rocket is never shown or kept.
// vanilla: Projectile.spawnProjectile, CrossbowItem.createProjectile, FireworkRocketItem.useOn
func TestPluginFireworkLaunchEvents(t *testing.T) {
	t.Parallel()
	scenes := map[string]func(t *testing.T, l *fireworkListener) *rocketShot{
		"used": func(t *testing.T, l *fireworkListener) *rocketShot {
			srv, _, c := blockScene(t, "minecraft:firework_rocket", false, l.listen)
			d := servertest.DigTarget
			return &rocketShot{srv: srv, c: c, byPacket: true, fire: func() {
				srv.GiveSlot("Miner", player.HotbarSlot, servertest.ItemStack(t, "minecraft:firework_rocket", 1))
				c.PlaceBlock(d[0], d[1]-1, d[2], protocol.FaceTop)
			}}
		},
		"dispensed": func(t *testing.T, l *fireworkListener) *rocketShot {
			srv, host, c := blockScene(t, "minecraft:dispenser", false, l.listen)
			d := servertest.DigTarget
			c.PlaceBlock(d[0], d[1]-1, d[2], protocol.FaceTop)
			c.Settle()
			return &rocketShot{srv: srv, c: c, fire: func() {
				if !srv.SetInventoryAt(d[0], d[1], d[2], 0, servertest.ItemStack(t, "minecraft:firework_rocket", 1)) {
					t.Fatal("the dispenser has no block entity")
				}
				// The visit runs on the tick goroutine, as a redstone edge's would.
				host.Scheduler().RunTask(func() { srv.Dispense(d[0], d[1], d[2], protocol.FaceTop) })
			}}
		},
		"crossbow": func(t *testing.T, l *fireworkListener) *rocketShot {
			srv, c := crossbowServer(t, servertest.BowTestConfig(), l)
			return &rocketShot{srv: srv, c: c, byPacket: true, fire: func() {
				srv.GiveSlot("Archer", 36, chargedCrossbow(t, ""))
				c.UseItemRightClicks(protocol.HandMain)
			}}
		},
	}
	rejections := []struct {
		name string
		set  func(l *fireworkListener)
	}{
		{"cancelled launch", func(l *fireworkListener) { l.cancelLaunch = true }},
		{"cancelled spawn", func(l *fireworkListener) { l.cancelSpawn = true }},
		{"removed in launch", func(l *fireworkListener) { l.removeLaunch = true }},
		{"removed in spawn", func(l *fireworkListener) { l.removeSpawn = true }},
	}
	for name, scene := range scenes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			l := &fireworkListener{}
			shot := scene(t, l)
			for _, rej := range rejections {
				*l = fireworkListener{launches: l.launches, spawns: l.spawns, originless: l.originless}
				rej.set(l)
				res := shot.run(t, l)
				if res != (shotResult{}) || shot.srv.FireworkCount() != 0 {
					t.Errorf("%s: sent %d spawns and %d removes and left %d rockets, want none", rej.name, res.spawns, res.removes, shot.srv.FireworkCount())
				}
				if n := shot.srv.PluginEntityDataKeys(l.lastID); n != 0 {
					t.Errorf("%s: %d plugin data keys left for the rocket", rej.name, n)
				}
			}
			*l = fireworkListener{launches: l.launches, spawns: l.spawns, originless: l.originless}
			res := shot.run(t, l)
			if res.spawns != 1 || res.removes != 0 || shot.srv.FireworkCount() != 1 {
				t.Errorf("an allowed shot sent %d spawns and %d removes and left %d rockets, want 1, 0 and 1", res.spawns, res.removes, shot.srv.FireworkCount())
			}
			if n := shot.srv.PluginEntityDataKeys(l.lastID); n != 1 {
				t.Errorf("an allowed shot has %d plugin data keys, want 1", n)
			}
			if l.originless != 0 {
				t.Errorf("%d launches saw a projectile with no origin", l.originless)
			}
		})
	}
}

// crossbowServer joins an archer to a server with the listener.
func crossbowServer(t *testing.T, cfg *config.Config, l *fireworkListener) (*server.Server, *servertest.Client) {
	t.Helper()
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "fw", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		l.listen(s)
		return nil
	}}}})
	c := servertest.DialServer(t, srv)
	c.JoinAs("Archer")
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return srv, c
}

// TestPluginCrossbowRocketSpawnEffect checks a projectile_spawned effect on the
// crossbow and on its rocket runs once for an accepted rocket and not for one either event refuses.
// vanilla: Projectile.spawnProjectile, Projectile.applyOnProjectileSpawned
func TestPluginCrossbowRocketSpawnEffect(t *testing.T) {
	cfg := servertest.BowTestConfig()
	cfg.LevelName = servertest.PackEnchantments(t, map[string]string{
		"data/pk/enchantment/fx.json": `{"anvil_cost":1,"description":{"text":"Fx"},"max_level":1,"weight":1,
			"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":1,"per_level_above_first":0},
			"slots":["mainhand"],"supported_items":["minecraft:crossbow","minecraft:firework_rocket"],
			"effects":{"minecraft:projectile_spawned":[{"effect":{"type":"minecraft:run_function","function":"pack:count"}}]}}`,
		"data/pack/function/count.mcfunction": "scoreboard players add #n fx 1\n",
	})
	l := &fireworkListener{}
	srv, c := crossbowServer(t, cfg, l)
	srv.RunLine("scoreboard objectives add fx dummy")
	srv.RunLine("scoreboard players set #n fx 0")
	shot := &rocketShot{srv: srv, c: c, byPacket: true, fire: func() {
		srv.GiveSlot("Archer", 36, chargedCrossbow(t, "pk:fx"))
		c.UseItemRightClicks(protocol.HandMain)
	}}
	score := func(n int) bool {
		return strings.Contains(fmt.Sprint(srv.RunLine(fmt.Sprintf("execute if score #n fx matches %d", n))), "conditional.pass")
	}
	l.cancelLaunch = true
	shot.run(t, l)
	l.cancelLaunch, l.cancelSpawn = false, true
	shot.run(t, l)
	if !score(0) {
		t.Fatal("a refused rocket ran the crossbow's spawn effect")
	}
	l.cancelSpawn = false
	shot.run(t, l)
	if !score(1) {
		t.Error("an accepted rocket did not run the spawn effect exactly once")
	}
}
