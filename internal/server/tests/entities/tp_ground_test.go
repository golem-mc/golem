package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A /tp sets the ground flag of a body in the air, even one without gravity.
// vanilla: TeleportCommand.performTeleport
func TestTeleportCommandSetsStandAndMannequinOnGround(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"armor_stand", "mannequin"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			srv.RunLine(`summon ` + kind + ` 0.5 150 0.5 {Tags:[b],NoGravity:1b}`)
			servertest.RunTicks(t, srv, 2)
			c.Settle()
			srv.RunLine(`tp @e[tag=b,limit=1] 0.5 155 0.5`)
			var sync servertest.EntityPositionSync
			seen := c.ReadTicks(srv, 5, func(pid int32, data []byte) bool {
				if pid != protocol.IDPlayTeleportEntity {
					return false
				}
				sync = servertest.ParseEntityPositionSync(data)
				return sync.Y == 155
			})
			if !seen || !sync.OnGround {
				t.Errorf("seen %v, sync %+v, want a position sync at y 155 with onGround", seen, sync)
			}
		})
	}
}

// A /tp of a falling mannequin zeroes its vertical velocity and lands it at
// the destination with the ground flag, instead of letting the tick carry on
// the fall first.
// vanilla: TeleportCommand.performTeleport
func TestTeleportCommandStopsFallingMannequin(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Watcher")
	srv.RunLine(`summon mannequin 0.5 150 0.5 {Tags:[b]}`)
	servertest.RunTicks(t, srv, 14)
	c.Settle()
	if got := dataOfMannequin(t, c, srv, "Motion[1]"); !strings.HasPrefix(got, "-") {
		t.Fatalf("Motion[1] before /tp = %s, want a falling (negative) value", got)
	}
	srv.RunLine(`tp @e[tag=b,limit=1] 0.5 155 0.5`)
	if got := dataOfMannequin(t, c, srv, "Motion[1]"); got != "0.0d" {
		t.Errorf("Motion[1] after /tp = %s, want 0.0d", got)
	}
	health := dataOfMannequin(t, c, srv, "Health")
	var sync servertest.EntityPositionSync
	seen := c.ReadTicks(srv, 5, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayTeleportEntity {
			return false
		}
		sync = servertest.ParseEntityPositionSync(data)
		return true
	})
	if !seen || sync.Y != 155 || !sync.OnGround {
		t.Errorf("seen %v, sync %+v, want a position sync at y 155 with onGround", seen, sync)
	}
	if got := dataOfMannequin(t, c, srv, "Health"); got != health {
		t.Errorf("Health after the first tick = %s, want %s (no fall damage)", got, health)
	}
}
