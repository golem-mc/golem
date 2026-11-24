package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.calculatePassengerTransition
func TestTeleportKeepsMobPassengerFacingRelativeToMount(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	c.ChatCommand(`summon chicken 3.5 64 3.5 {NoAI:1b,NoGravity:1b,Rotation:[90f,0f],Passengers:[{id:"minecraft:pig",NoAI:1b,NoGravity:1b,Rotation:[0f,0f]}]}`)
	byType := make(map[int32]int32)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySpawnEntity {
			r := protocol.NewReader(data)
			body := r.ReadVarInt()
			r.ReadUUID()
			byType[r.ReadVarInt()] = body
		}
	})
	chicken, pig := byType[protocol.EntityTypeChicken], byType[protocol.EntityTypePig]
	if chicken == 0 || pig == 0 {
		t.Fatalf("spawned types = %v", byType)
	}
	c.ChatCommand(`tp @e[type=chicken] 6.5 64 6.5 180 0`)
	c.Settle()
	if y, _ := srv.MobLook(chicken); y != -180 {
		t.Fatalf("chicken yaw = %g, want -180", y)
	}
	if y, p := srv.MobLook(pig); y != -270 || p != 0 {
		t.Errorf("pig look = (%g, %g), want (-270, 0)", y, p)
	}
}
