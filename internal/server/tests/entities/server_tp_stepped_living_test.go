package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
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

// A change to an armor stand's or mannequin's synched data opens a movement
// tick off the period, so a teleport commanded in that tick is sent on the
// next one whatever the schedule's phase.
// vanilla: ServerEntity.sendChanges
func TestSyncedDataChangeOpensMovementTick(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"armor_stand", "mannequin"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.HostileTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Watcher")
			servertest.HoldTicks(t, srv)
			srv.RunLine(fmt.Sprintf(`summon %s 0.5 100 0.5 {Tags:[s],NoGravity:1b}`, kind))
			servertest.RunTicks(t, srv, 1)
			c.Settle()
			// Every phase of both intervals.
			for i := range 6 {
				servertest.RunTicks(t, srv, 1)
				y := float64(101 + i)
				// Toggling the glow changes the entity flags each phase.
				if i%2 == 0 {
					srv.RunLine(`effect give @e[tag=s,limit=1] glowing infinite 0 true`)
				} else {
					srv.RunLine(`effect clear @e[tag=s,limit=1]`)
				}
				srv.RunLine(fmt.Sprintf(`tp @e[tag=s,limit=1] 0.5 %v 0.5`, y))
				if !c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
					return pid == protocol.IDPlayTeleportEntity && servertest.ParseEntityPositionSync(data).Y == y
				}) {
					t.Fatalf("phase %d: no Teleport Entity at y %v on the next tick", i, y)
				}
			}
		})
	}
}

// Loading entity data, with no synched value changing, also opens a movement
// tick off the period.
// vanilla: Entity.load, ServerEntity.sendChanges
func TestDataLoadOpensMovementTick(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"armor_stand", "mannequin"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.HostileTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Watcher")
			servertest.HoldTicks(t, srv)
			srv.RunLine(fmt.Sprintf(`summon %s 0.5 100 0.5 {Tags:[s],NoGravity:1b}`, kind))
			servertest.RunTicks(t, srv, 1)
			c.Settle()
			for i := range 6 {
				servertest.RunTicks(t, srv, 1)
				y := float64(101 + i)
				srv.RunLine(fmt.Sprintf(`data merge entity @e[tag=s,limit=1] {Invulnerable:%db}`, (i+1)%2))
				srv.RunLine(fmt.Sprintf(`tp @e[tag=s,limit=1] 0.5 %v 0.5`, y))
				if !c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
					return pid == protocol.IDPlayTeleportEntity && servertest.ParseEntityPositionSync(data).Y == y
				}) {
					t.Fatalf("phase %d: no Teleport Entity at y %v on the next tick", i, y)
				}
			}
		})
	}
}
