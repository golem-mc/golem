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
