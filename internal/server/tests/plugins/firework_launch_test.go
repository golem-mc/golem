package plugins_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// fireworkListener counts launches and spawns of rockets and cancels the
// launch or the spawn while its flag is set.
type fireworkListener struct {
	launches, spawns          int
	cancelLaunch, cancelSpawn bool
}

func (f *fireworkListener) listen(s plugin.Server) {
	plugin.Listen(s, func(e *plugin.ProjectileLaunchEvent) {
		if e.Projectile.Type() == protocol.EntityTypeFireworkRocket {
			f.launches++
			e.SetCancelled(f.cancelLaunch)
		}
	})
	plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
		if e.Entity.Type() == protocol.EntityTypeFireworkRocket {
			f.spawns++
			e.SetCancelled(f.cancelSpawn)
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

// run fires one shot and returns the packet IDs the client got, once both
// events the shot reaches have run: the loop is held, so a tick runs only
// when a call is pending.
func (r *rocketShot) run(t *testing.T, l *fireworkListener) (spawnPackets int) {
	t.Helper()
	queued := r.srv.PluginCallsQueued()
	select {
	case <-queued:
	default:
	}
	launches, spawns := l.launches, l.spawns
	r.fire()
	step := func(n *int, want int) {
		for *n < want {
			if r.byPacket {
				select {
				case <-queued:
				case <-time.After(servertest.ExpectTimeout):
					t.Fatal("no plugin call queued")
				}
			}
			servertest.RunTicks(t, r.srv, 1)
		}
	}
	step(&l.launches, launches+1)
	if !l.cancelLaunch {
		step(&l.spawns, spawns+1)
	}
	r.c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySpawnEntity {
			spawnPackets++
		}
	})
	return spawnPackets
}

func chargedCrossbow(t *testing.T, enchantment string) protocol.ItemStack {
	t.Helper()
	crossbow := servertest.ItemStack(t, "minecraft:crossbow", 1)
	if enchantment != "" {
		crossbow = servertest.EnchantedStack(t, "minecraft:crossbow", enchantment, 1)
	}
	crossbow.SetChargedProjectiles([]protocol.ItemStack{servertest.ItemStack(t, "minecraft:firework_rocket", 1)})
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
	for name, scene := range scenes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			l := &fireworkListener{cancelLaunch: true}
			shot := scene(t, l)
			if n := shot.run(t, l); n != 0 || shot.srv.FireworkCount() != 0 {
				t.Errorf("a cancelled launch sent %d spawns and left %d rockets", n, shot.srv.FireworkCount())
			}
			l.cancelLaunch, l.cancelSpawn = false, true
			if n := shot.run(t, l); n != 0 || shot.srv.FireworkCount() != 0 {
				t.Errorf("a cancelled spawn sent %d spawns and left %d rockets", n, shot.srv.FireworkCount())
			}
			l.cancelSpawn = false
			if n := shot.run(t, l); n != 1 || shot.srv.FireworkCount() != 1 {
				t.Errorf("an allowed shot sent %d spawns and left %d rockets, want 1 and 1", n, shot.srv.FireworkCount())
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

// fxPack writes a data pack whose crossbow enchantment adds one to a score
// each time a projectile it shot is spawned, and returns the level directory.
// The registry tables are process-wide, so the test is not parallel.
func fxPack(t *testing.T) string {
	t.Helper()
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackEnchantments(nil) })
	dir := t.TempDir()
	pack := filepath.Join(dir, "datapacks", "fx")
	files := map[string]string{
		"pack.mcmeta": `{"pack":{"description":"fx"}}`,
		"data/pk/enchantment/fx.json": `{"anvil_cost":1,"description":{"text":"Fx"},"max_level":1,"weight":1,
			"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":1,"per_level_above_first":0},
			"slots":["mainhand"],"supported_items":"minecraft:crossbow",
			"effects":{"minecraft:projectile_spawned":[{"effect":{"type":"minecraft:run_function","function":"pack:count"}}]}}`,
		"data/pack/function/count.mcfunction": "scoreboard players add #n fx 1\n",
	}
	for name, text := range files {
		path := filepath.Join(pack, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestPluginCrossbowRocketSpawnEffect checks the crossbow's projectile_spawned
// effect runs once for an accepted rocket and not for one either event refuses.
// vanilla: Projectile.spawnProjectile, Projectile.applyOnProjectileSpawned
func TestPluginCrossbowRocketSpawnEffect(t *testing.T) {
	cfg := servertest.BowTestConfig()
	cfg.LevelName = fxPack(t)
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
