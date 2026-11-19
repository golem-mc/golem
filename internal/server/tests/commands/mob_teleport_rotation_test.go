package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: TeleportCommand.performTeleport, Entity.teleportSetPosition
func TestTeleportTurnsANoAIMobsHead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon fox 6.5 100 6.5 {NoAI:1b,NoGravity:1b,Age:0,Rotation:[0f,0f]}`)
	fox := c.AwaitSpawn(protocol.EntityTypeFox)
	servertest.RunTicks(t, srv, 2)
	c.Settle()

	for _, tc := range []struct {
		rotation string
		yaw      float32
		head     byte
	}{
		{"90", 90, 64},
		{"-90", -90, 192},
		{"~180", -180, 128},
	} {
		c.ChatCommand("tp @e[type=fox,limit=1] 6.5 100 6.5 " + tc.rotation + " 0")
		c.SettleKeeping()
		servertest.RunTicks(t, srv, 3) // one fox tracking interval
		bodyTurned, headTurned := false, false
		c.SettleAndCollect(func(packetID int32, data []byte) {
			if packetID != protocol.IDPlayTeleportEntity && packetID != protocol.IDPlayHeadRotation {
				return
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() != fox.EntityID {
				return
			}
			if packetID == protocol.IDPlayHeadRotation {
				headTurned = true
				if got := r.ReadU8(); got != tc.head {
					t.Errorf("rotation %s: head yaw = %d, want %d", tc.rotation, got, tc.head)
				}
			} else {
				bodyTurned = true
				r.ReadVarInt() // position path
				x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
				yaw, pitch := r.ReadF32(), r.ReadF32()
				r.ReadBool() // on ground
				if x != 6.5 || y != 100 || z != 6.5 || yaw != tc.yaw || pitch != 0 {
					t.Errorf("rotation %s: teleport = (%v, %v, %v), yaw/pitch %v/%v", tc.rotation, x, y, z, yaw, pitch)
				}
			}
			if r.Err() != nil || r.Remaining() != 0 {
				t.Errorf("invalid rotation packet: %v, %d trailing bytes", r.Err(), r.Remaining())
			}
		})
		if !bodyTurned || !headTurned {
			t.Errorf("rotation %s: body update = %v, head update = %v; want both", tc.rotation, bodyTurned, headTurned)
		}
	}
}
