package plugins_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// fireworkListener counts launches and spawns of rockets and cancels the
// launch while cancel is set.
type fireworkListener struct {
	launches, spawns int
	cancel           bool
}

func (f *fireworkListener) listen(s plugin.Server) {
	plugin.Listen(s, func(e *plugin.ProjectileLaunchEvent) {
		if e.Projectile.Type() == protocol.EntityTypeFireworkRocket {
			f.launches++
			e.SetCancelled(f.cancel)
		}
	})
	plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
		if e.Entity.Type() == protocol.EntityTypeFireworkRocket {
			f.spawns++
		}
	})
}

// TestPluginFireworkLaunchEvents checks a crossbow-shot, a used and a
// dispensed rocket reach ProjectileLaunchEvent and EntitySpawnEvent, and a
// cancelled launch removes the rocket.
// vanilla: Projectile.spawnProjectile, CrossbowItem.createProjectile, FireworkRocketItem.useOn
func TestPluginFireworkLaunchEvents(t *testing.T) {
	t.Parallel()
	rocket := func(t *testing.T) protocol.ItemStack { return servertest.ItemStack(t, "minecraft:firework_rocket", 1) }
	scenes := map[string]func(t *testing.T, l *fireworkListener) (*server.Server, func()){
		"used": func(t *testing.T, l *fireworkListener) (*server.Server, func()) {
			srv, _, c := blockScene(t, "minecraft:firework_rocket", false, l.listen)
			d := servertest.DigTarget
			return srv, func() { c.PlaceBlock(d[0], d[1]-1, d[2], protocol.FaceTop) }
		},
		"dispensed": func(t *testing.T, l *fireworkListener) (*server.Server, func()) {
			srv, host, c := blockScene(t, "minecraft:dispenser", false, l.listen)
			d := servertest.DigTarget
			c.PlaceBlock(d[0], d[1]-1, d[2], protocol.FaceTop)
			c.Settle()
			return srv, func() {
				if !srv.SetInventoryAt(d[0], d[1], d[2], 0, rocket(t)) {
					t.Fatal("the dispenser has no block entity")
				}
				// The visit runs on the tick goroutine, as a redstone edge's would.
				host.Scheduler().RunTask(func() { srv.Dispense(d[0], d[1], d[2], protocol.FaceTop) })
			}
		},
		"crossbow": func(t *testing.T, l *fireworkListener) (*server.Server, func()) {
			srv := servertest.NewTestServerWithPlugins(t, servertest.BowTestConfig(), []plugin.Registration{{Name: "fw", Plugin: &testPlugin{enable: func(s plugin.Server) error {
				l.listen(s)
				return nil
			}}}})
			c := servertest.DialServer(t, srv)
			c.JoinAs("Archer")
			srv.GiveSlot("Archer", 36, servertest.ItemStack(t, "minecraft:crossbow", 1))
			srv.GiveSlot("Archer", player.OffHandSlot, servertest.ItemStack(t, "minecraft:firework_rocket", 10))
			c.MoveTo(0.5, 64, 0.5, -90, 0)
			servertest.DrawBow(c, -90, 0)
			servertest.HoldDraw(t, srv, "Archer", 27)
			servertest.ReleaseBow(c)
			servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.BowTicks("Archer") == 0 })
			return srv, func() { servertest.DrawBow(c, -90, 0) }
		},
	}
	for name, scene := range scenes {
		for _, cancel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/cancel=%v", name, cancel), func(t *testing.T) {
				t.Parallel()
				l := &fireworkListener{cancel: cancel}
				srv, fire := scene(t, l)
				fire()
				// The packet may not be read yet, so the ticks are paced in real time.
				for i := 0; l.launches == 0; i++ {
					if i == 2000 {
						t.Fatal("no rocket launched")
					}
					servertest.RunTicks(t, srv, 1)
					time.Sleep(5 * time.Millisecond)
				}
				servertest.RunTicks(t, srv, 2)
				if l.launches != 1 || l.spawns != boolInt(!cancel) {
					t.Errorf("launches %d spawns %d", l.launches, l.spawns)
				}
				if got, want := srv.FireworkCount(), boolInt(!cancel); got != want {
					t.Errorf("%d rockets fly after cancel=%v, want %d", got, cancel, want)
				}
			})
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
