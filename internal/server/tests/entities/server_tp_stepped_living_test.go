package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// An armor stand or mannequin, both LivingEntity, reaches the client on a
// stepped path whose duration is the ticks since its previous movement tick,
// idle ones included, so an idle entity's next teleport takes one interval.
// vanilla: SteppedInterpolationTracker, ServerEntity.sendChanges
func TestTeleportCommandSteppedPathLastsOneTrackingInterval(t *testing.T) {
	t.Parallel()
	for _, kind := range []struct {
		name     string
		interval int32
	}{{"armor_stand", 3}, {"mannequin", 2}} {
		for _, frozen := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/frozen=%v", kind.name, frozen), func(t *testing.T) {
				t.Parallel()
				cfg := servertest.HostileTestConfig()
				cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
				srv, c := servertest.Join(t, cfg, "Watcher")
				servertest.HoldTicks(t, srv)
				srv.RunLine(fmt.Sprintf(`summon %s 0.5 100 0.5 {Tags:[s],NoGravity:1b}`, kind.name))
				servertest.RunTicks(t, srv, 1)
				c.Settle()
				if frozen {
					srv.RunLine(`tick freeze`)
				}
				var sync servertest.EntityPositionSync
				var id int32
				for i, y := range []float64{103, 112} {
					if i > 0 {
						// A multiple of both intervals, so the teleport lands
						// between two scheduled gates.
						servertest.RunTicks(t, srv, 6)
					}
					srv.RunLine(fmt.Sprintf(`tp @e[tag=s,limit=1] 0.5 %v 0.5`, y))
					seen := c.ReadTicks(srv, 8, func(pid int32, data []byte) bool {
						if pid != protocol.IDPlayTeleportEntity {
							return false
						}
						sync = servertest.ParseEntityPositionSync(data)
						return sync.Y == y && (i == 0 || sync.ID == id)
					})
					if i == 0 {
						id = sync.ID
					}
					if !seen {
						t.Fatalf("teleport %d: no Teleport Entity at y %v", i, y)
					}
				}
				if !sync.Stepped || sync.StepTicks != kind.interval {
					t.Errorf("second sync = %+v, want stepped over %d ticks", sync, kind.interval)
				}
			})
		}
	}
}

// teleportWithin runs up to n ticks one at a time and returns the Teleport
// Entity at y and the ticks it took; ok is false when none came.
func teleportWithin(srv *server.Server, c *servertest.Client, y float64, n int) (sync servertest.EntityPositionSync, ticks int, ok bool) {
	for ticks = 1; ticks <= n; ticks++ {
		if c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
			if pid != protocol.IDPlayTeleportEntity {
				return false
			}
			sync = servertest.ParseEntityPositionSync(data)
			return sync.Y == y
		}) {
			return sync, ticks, true
		}
	}
	return sync, ticks, false
}

// A change to an armor stand's or mannequin's synched data, or a /data load,
// opens one movement tick off the period and is consumed by whichever tick
// opens: the teleport that follows waits for the next scheduled tick, and its
// path lasts the ticks since the pass the change opened.
// vanilla: ServerEntity.sendChanges, Entity.needsSync, SteppedInterpolationTracker
func TestDirtyDataOpensOneMovementTick(t *testing.T) {
	t.Parallel()
	actions := []struct {
		name string
		line string
	}{
		{"glow", `effect give @e[tag=s,limit=1] glowing infinite 0 true`},
		{"load", `data merge entity @e[tag=s,limit=1] {Invulnerable:1b}`},
		{"health", `damage @e[tag=s,limit=1] 1 generic`},
	}
	for _, kind := range []struct {
		name     string
		interval int
	}{{"armor_stand", 3}, {"mannequin", 2}} {
		for _, act := range actions {
			if kind.name == "armor_stand" && act.name == "health" {
				continue
			}
			for _, scheduled := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/scheduled=%v", kind.name, act.name, scheduled), func(t *testing.T) {
					t.Parallel()
					cfg := servertest.HostileTestConfig()
					cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
					srv, c := servertest.Join(t, cfg, "Watcher")
					servertest.HoldTicks(t, srv)
					srv.RunLine(fmt.Sprintf(`summon %s 0.5 100 0.5 {Tags:[s],NoGravity:1b,Health:20f}`, kind.name))
					servertest.RunTicks(t, srv, 1)
					c.Settle()
					// Calibrate: a lone teleport arrives on a scheduled tick.
					srv.RunLine(`tp @e[tag=s,limit=1] 0.5 101 0.5`)
					if _, _, ok := teleportWithin(srv, c, 101, kind.interval); !ok {
						t.Fatal("no calibration teleport")
					}
					// The next scheduled tick is interval ticks on.
					idle := kind.interval - 1
					if !scheduled {
						idle = 0
					}
					servertest.RunTicks(t, srv, idle)
					srv.RunLine(act.line)
					c.Settle()
					servertest.RunTicks(t, srv, 1) // the tick the change opens or shares
					c.Settle()
					srv.RunLine(`tp @e[tag=s,limit=1] 0.5 105 0.5`)
					// The dirty pass is consumed, so the teleport waits for
					// the next scheduled tick, the ticks since the pass.
					sync, _, ok := teleportWithin(srv, c, 105, kind.interval)
					if !ok {
						t.Fatal("no Teleport Entity")
					}
					want := int32(kind.interval)
					if !scheduled {
						want = int32(kind.interval - 1)
					}
					if !sync.Stepped || sync.StepTicks != want {
						t.Errorf("sync = %+v, want stepped over %d ticks", sync, want)
					}
				})
			}
		}
	}
}
