package entities

import (
	"math"
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
			if tc.nbt != "NoGravity:0b" {
				return
			}
			// The next gate, three ticks on, tells the pose after three
			// steps of the fall, the ground flag cleared.
			seen = c.ReadTicks(srv, 3, func(pid int32, data []byte) bool {
				if pid != protocol.IDPlayTeleportEntity {
					return false
				}
				sync = servertest.ParseEntityPositionSync(data)
				return true
			})
			if !seen || math.Abs(sync.Y-154.766368) > 1e-9 || sync.OnGround {
				t.Errorf("seen %v, next sync %+v, want y 154.766368 without onGround", seen, sync)
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

// A stand that a /tp takes out of one watcher's range and that breaks, falls
// out of the world or enters an End portal in the same tick sends that
// watcher one removal; the watcher still in range gets the destination sync
// at its position with onGround, then one removal.
// vanilla: ChunkMap.TrackedEntity.updatePlayer, ServerEntity.removePairing
func TestTeleportedStandThatEndsSendsOneRemovalPerWatcher(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, nbt, dest string
		y               float64
	}{
		{"broken", "Health:1.0f,Fire:20s", "33.5 150 0.5", 150},
		{"voided", "", "33.5 -200 0.5", -200},
		// The portal block lies at the destination; the stand has physics.
		{"portal", "", "33.5 150 0.5", 150},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect, alice := servertest.AdminSolo(t, mannequinConfig())
			bob := connect().Join("Bob")
			alice.ExpectPlayerAdded("Bob")
			servertest.HoldTicks(t, srv)
			srv.RunLine(`tp Bob 8.5 150 0.5`)
			gravity := "NoGravity:1b,"
			if tc.name == "portal" {
				gravity = ""
				srv.RunLine(`setblock 33 150 0 end_portal`)
			}
			srv.RunLine(`summon armor_stand 20.5 150 0.5 {Tags:[b],` + gravity + tc.nbt + `}`)
			standID := protocol.NewReader(alice.ExpectWithinTicks(srv, 6, protocol.IDPlaySpawnEntity)).ReadVarInt()
			bob.ExpectWithinTicks(srv, 0, protocol.IDPlaySpawnEntity)
			// On a movement tick the next /tp is told before the step.
			srv.RunLine(`tp @e[tag=b,limit=1] ` + tc.dest)
			if tc.name == "broken" {
				srv.RunLine(`data merge entity @e[tag=b,limit=1] {Fire:20s,Health:1.0f}`)
			}
			servertest.RunTicks(t, srv, 1)
			// events are the stand's packets in arrival order: "sync" or "removal".
			events := func(c *servertest.Client) (got []string) {
				c.ReadTicks(srv, 0, func(pid int32, d []byte) bool {
					switch pid {
					case protocol.IDPlayRemoveEntities:
						for _, id := range servertest.RemovedIDs(t, d) {
							if id == standID {
								got = append(got, "removal")
							}
						}
					case protocol.IDPlayTeleportEntity:
						if sync := servertest.ParseEntityPositionSync(d); sync.ID == standID {
							if sync.X != 33.5 || sync.Y != tc.y || sync.Z != 0.5 || !sync.OnGround {
								t.Errorf("sync %+v, want (33.5, %v, 0.5) with onGround", sync, tc.y)
							}
							got = append(got, "sync")
						}
					}
					return false
				})
				return
			}
			if got := events(alice); len(got) != 1 || got[0] != "removal" {
				t.Errorf("out-of-range watcher got %v, want one removal", got)
			}
			if got := events(bob); len(got) != 2 || got[0] != "sync" || got[1] != "removal" {
				t.Errorf("in-range watcher got %v, want a sync then one removal", got)
			}
		})
	}
}
