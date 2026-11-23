package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A /tp sets the ground flag of a body in the air, gravity or not, and the
// destination sync carries it before the next step moves the body.
// vanilla: TeleportCommand.performTeleport, ServerChunkCache.tick
func TestTeleportCommandSetsStandAndMannequinOnGround(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, kind, nbt string }{
		{"armor_stand", "armor_stand", "NoGravity:1b"},
		{"mannequin", "mannequin", "NoGravity:1b"},
		{"falling_armor_stand", "armor_stand", "NoGravity:0b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			srv.RunLine(`summon ` + tc.kind + ` 0.5 150 0.5 {Tags:[b],` + tc.nbt + `}`)
			servertest.RunTicks(t, srv, 2)
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
	// 11 ticks fall about 4 blocks, enough for fall damage on a landing.
	servertest.RunTicks(t, srv, 11)
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
	if len(syncs) != 1 || syncs[0].Y != 155 || !syncs[0].OnGround {
		t.Errorf("syncs in the /tp tick = %+v, want one at y 155 with onGround", syncs)
	}
	if got := mannequinDataNow(t, srv, "Health"); got != health {
		t.Errorf("Health after the first tick = %s, want %s (no fall damage)", got, health)
	}
	if got := mannequinDataNow(t, srv, "OnGround"); got != "0b" {
		t.Errorf("OnGround after a midair tick = %s, want 0b", got)
	}
	var next servertest.EntityPositionSync
	seen := c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
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
