package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// An armor stand or mannequin, both LivingEntity, reaches the client on a
// stepped path from its second position sync on.
// vanilla: SteppedInterpolationTracker, ServerEntity.sendChanges
func TestTeleportCommandSteppesArmorStandAndMannequin(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"armor_stand", "mannequin"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, tpGroundConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			srv.RunLine(fmt.Sprintf(`summon %s 0.5 100 0.5 {Tags:[s],NoGravity:1b}`, kind))
			servertest.RunTicks(t, srv, 1)
			c.Settle()
			var sync servertest.EntityPositionSync
			for i, y := range []float64{103, 105} {
				srv.RunLine(fmt.Sprintf(`tp @e[tag=s,limit=1] 0.5 %v 0.5`, y))
				seen := c.ReadTicks(srv, 8, func(pid int32, data []byte) bool {
					if pid != protocol.IDPlayTeleportEntity {
						return false
					}
					sync = servertest.ParseEntityPositionSync(data)
					return sync.Y == y
				})
				if !seen {
					t.Fatalf("teleport %d: no Teleport Entity at y %v", i, y)
				}
			}
			if !sync.Stepped || sync.StepTicks < 1 {
				t.Errorf("second sync = %+v, want stepped", sync)
			}
		})
	}
}
