package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A /tp sets the ground flag of a body in the air, gravity or not, and a
// command in the first movement tick tells that flag before the step moves
// the body. setup is the tick count before the command.
// vanilla: TeleportCommand.performTeleport, ServerChunkCache.tick
func TestTeleportCommandSetsStandAndMannequinOnGround(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, kind, nbt string
		setup           int
	}{
		{"armor_stand", "armor_stand", "NoGravity:1b", 0},
		{"mannequin", "mannequin", "NoGravity:1b", 0},
		{"falling_armor_stand", "armor_stand", "NoGravity:0b", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, mannequinConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			srv.RunLine(`summon ` + tc.kind + ` 0.5 150 0.5 {Tags:[b],` + tc.nbt + `}`)
			servertest.RunTicks(t, srv, tc.setup)
			c.Settle()
			srv.RunLine(`tp @e[tag=b,limit=1] 0.5 155 0.5`)
			var sync servertest.EntityPositionSync
			seen := c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
				if pid != protocol.IDPlayTeleportEntity {
					return false
				}
				sync = servertest.ParseEntityPositionSync(data)
				return true
			})
			if !seen || sync.Y != 155 || !sync.OnGround {
				t.Errorf("seen %v, sync %+v, want a position sync at y 155 with onGround", seen, sync)
			}
		})
	}
}

// A teleport between a stand's movement ticks, however short, is told on its
// next movement tick.
// vanilla: ServerEntity.sendChanges
func TestTeleportedStandShortHopIsToldOnItsNextGate(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, mannequinConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon armor_stand 0.5 150 0.5 {Tags:[b],NoGravity:1b}`)
	// One tick: the command lands just after the first movement tick.
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	srv.RunLine(`tp @e[tag=b,limit=1] 0.5005 150 0.5`)
	var sync servertest.EntityPositionSync
	// Silent for the two ticks before the next movement tick (every 3rd).
	c.ExpectNoneWithinTicks(srv, 2, servertest.PacketIs(protocol.IDPlayTeleportEntity))
	seen := c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayTeleportEntity {
			return false
		}
		sync = servertest.ParseEntityPositionSync(data)
		return true
	})
	if !seen || sync.X != 0.5005 || !sync.OnGround {
		t.Errorf("seen %v, sync %+v, want a position sync at x 0.5005 with onGround", seen, sync)
	}
}
