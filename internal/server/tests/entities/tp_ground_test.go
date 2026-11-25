package entities

import (
	"bytes"
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

// A stand that a /tp takes out of one watcher's range and that breaks or
// falls out of the world in the same tick sends that watcher one removal;
// the watcher still in range gets the destination sync, then one removal.
// vanilla: ChunkMap.TrackedEntity.updatePlayer, ServerEntity.removePairing
func TestTeleportedStandThatEndsSendsOneRemovalPerWatcher(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, nbt, dest string }{
		{"broken", "Health:1.0f,Fire:20s", "60.5 150 0.5"},
		{"voided", "", "60.5 -200 0.5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect, alice := servertest.AdminSolo(t, mannequinConfig())
			bob := connect().Join("Bob")
			alice.ExpectPlayerAdded("Bob")
			servertest.HoldTicks(t, srv)
			srv.RunLine(`tp Bob 40 150 0`)
			srv.RunLine(`summon armor_stand 20.5 150 0.5 {Tags:[b],NoGravity:1b,` + tc.nbt + `}`)
			spawn := alice.ExpectWithinTicks(srv, 6, protocol.IDPlaySpawnEntity)
			bob.ExpectWithinTicks(srv, 0, protocol.IDPlaySpawnEntity)
			// The removal of this stand: one id, the stand's.
			standID := protocol.NewReader(spawn).ReadVarInt()
			removal := []byte{1, byte(standID)} // ids below 128 are one VarInt byte
			// On a movement tick the next /tp is told before the step.
			srv.RunLine(`tp @e[tag=b,limit=1] ` + tc.dest)
			if tc.name == "broken" {
				srv.RunLine(`data merge entity @e[tag=b,limit=1] {Fire:20s,Health:1.0f}`)
			}
			servertest.RunTicks(t, srv, 1)
			count := func(c *servertest.Client) (removals, syncs int) {
				c.ReadTicks(srv, 0, func(pid int32, d []byte) bool {
					switch pid {
					case protocol.IDPlayRemoveEntities:
						if bytes.Equal(d, removal) {
							removals++
						}
					case protocol.IDPlayTeleportEntity:
						syncs++
					}
					return false
				})
				return
			}
			if r, _ := count(alice); r != 1 {
				t.Errorf("out-of-range watcher got %d removals, want 1", r)
			}
			if r, _ := count(bob); r != 1 {
				t.Errorf("in-range watcher got %d removals, want 1", r)
			}
		})
	}
}
