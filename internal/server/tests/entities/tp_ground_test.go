package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A /tp sets the ground flag of a body in the air, gravity or not, and a
// command on the eve of a movement tick tells that flag before the step moves
// the body. setup puts the command on that eve; interval is the kind's.
// vanilla: TeleportCommand.performTeleport, ServerChunkCache.tick
func TestTeleportCommandSetsStandAndMannequinOnGround(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, kind, nbt string
		setup, interval int
	}{
		{"armor_stand", "armor_stand", "NoGravity:1b", 3, 3},
		{"mannequin", "mannequin", "NoGravity:1b", 2, 2},
		{"falling_armor_stand", "armor_stand", "NoGravity:0b", 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
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
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon armor_stand 0.5 150 0.5 {Tags:[b],NoGravity:1b}`)
	// 4 ticks: the command lands just after a movement tick (every 3rd).
	servertest.RunTicks(t, srv, 4)
	c.Settle()
	srv.RunLine(`tp @e[tag=b,limit=1] 0.5005 150 0.5`)
	var sync servertest.EntityPositionSync
	seen := c.ReadTicks(srv, 3, func(pid int32, data []byte) bool {
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

// mannequinDataNow reads a mannequin's /data path without running a tick.
func mannequinDataNow(t *testing.T, srv *server.Server, path string) string {
	t.Helper()
	for _, text := range srv.RunLine("data get entity @e[tag=b,limit=1] " + path) {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			return text.With[1].Plain()
		}
	}
	t.Fatalf("no /data get answer for %q", path)
	return ""
}

// A /tp of a falling mannequin zeroes its vertical velocity and lands it at
// the destination with the ground flag; the next step clears the flag in
// midair without counting the fall before the /tp as a landing.
// vanilla: TeleportCommand.performTeleport, Entity.move, Entity.checkFallDamage
func TestTeleportCommandStopsFallingMannequin(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Watcher")
	srv.RunLine(`summon mannequin 0.5 150 0.5 {Tags:[b]}`)
	// 12 ticks fall about 4.5 blocks, enough for fall damage on a landing,
	// and put the command on the eve of a movement tick (every 2nd).
	servertest.RunTicks(t, srv, 12)
	c.Settle()
	if got := mannequinDataNow(t, srv, "Motion[1]"); !strings.HasPrefix(got, "-") {
		t.Fatalf("Motion[1] before /tp = %s, want a falling (negative) value", got)
	}
	srv.RunLine(`tp @e[tag=b,limit=1] 0.5 155 0.5`)
	if got := mannequinDataNow(t, srv, "Motion[1]"); got != "0.0d" {
		t.Errorf("Motion[1] after /tp = %s, want 0.0d", got)
	}
	health := mannequinDataNow(t, srv, "Health")
	var syncs []servertest.EntityPositionSync
	c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayTeleportEntity {
			syncs = append(syncs, servertest.ParseEntityPositionSync(data))
		}
		return false
	})
	if len(syncs) != 1 || syncs[0].X != 0.5 || syncs[0].Y != 155 || syncs[0].Z != 0.5 || !syncs[0].OnGround {
		t.Errorf("syncs in the /tp tick = %+v, want one at 0.5 155 0.5 with onGround", syncs)
	}
	if got := mannequinDataNow(t, srv, "Health"); got != health {
		t.Errorf("Health after one tick = %s, want %s (no fall damage)", got, health)
	}
	if got := mannequinDataNow(t, srv, "OnGround"); got != "0b" {
		t.Errorf("OnGround after a midair tick = %s, want 0b", got)
	}
	var next servertest.EntityPositionSync
	seen := c.ReadTicks(srv, 2, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayTeleportEntity {
			return false
		}
		next = servertest.ParseEntityPositionSync(data)
		return true
	})
	if !seen || next.OnGround {
		t.Errorf("seen %v, next sync %+v, want one without onGround", seen, next)
	}
}
