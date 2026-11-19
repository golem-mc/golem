package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// TestRespawnAndCrossingsResendTheSpawnPosition checks that a client whose
// Respawn cleared its world is told where the world spawn is again: behind a
// death's Respawn and behind every Respawn a crossing into another dimension
// sends. The packet has to follow the Respawn, that being what made the client
// forget it. vanilla: PlayerList.respawn, PlayerList.sendLevelInfo
func TestRespawnAndCrossingsResendTheSpawnPosition(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Revenant")

	// A world spawn the test can tell apart from the one the join named by
	// its rotation, and one in the chunk the join already loaded, so a
	// respawn onto it generates no fresh region.
	c.ChatCommand("setworldspawn 4 64 4 90 -15")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"4", "64", "4", "90.0", "-15.0", "minecraft:overworld")
	// The move's own broadcast, read here so it is not mistaken for a
	// respawn's.
	if x, y, z, yaw, pitch := servertest.ReadSpawnPosition(t, c.ExpectPlay(protocol.IDPlaySpawnPosition)); x != 4 || y != 64 || z != 4 || yaw != 90 || pitch != -15 {
		t.Fatalf("setworldspawn broadcast = (%d, %d, %d) yaw %v pitch %v, want (4, 64, 4) yaw 90 pitch -15",
			x, y, z, yaw, pitch)
	}

	// A death's respawn, the same-dimension path.
	c.ChatCommand("kill")
	c.AwaitPacket(protocol.IDPlayCombatDeath)
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	expectSpawnPositionAfterRespawn(t, srv, c, 4, 64, 4, "the death respawn")
	// A respawn waits for the client's Player Loaded before the next hurt
	// lands.
	c.MarkLoaded()

	// A living crossing into the nether, the /execute in ... run tp form.
	c.ChatCommand("execute in minecraft:the_nether run tp @s 8 40 8")
	expectSpawnPositionAfterRespawn(t, srv, c, 4, 64, 4, "the crossing into the nether")

	// A death in the nether: the Respawn crosses the player home through the
	// dimension-changing branch rather than the same-dimension one above.
	c.ChatCommand("kill")
	c.AwaitPacket(protocol.IDPlayCombatDeath)
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	expectSpawnPositionAfterRespawn(t, srv, c, 4, 64, 4, "the death in the nether")
	c.Disconnect()
}

// expectSpawnPositionAfterRespawn reads a client's packets until a Set Default
// Spawn Position arrives behind a Respawn, and checks the spot and rotation it
// names.
func expectSpawnPositionAfterRespawn(t *testing.T, srv *server.Server, c *servertest.Client, x, y, z int32, how string) {
	t.Helper()
	sawRespawn := false
	var spawn []byte
	if !pumpTicks(c, srv, 400, nil, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayRespawn:
			sawRespawn = true
		case protocol.IDPlaySpawnPosition:
			if sawRespawn {
				spawn = data
				return true
			}
		}
		return false
	}) {
		if !sawRespawn {
			t.Fatalf("%s: no Respawn arrived", how)
		}
		t.Fatalf("%s: no Set Default Spawn Position arrived behind the Respawn", how)
	}
	if gotX, gotY, gotZ, yaw, pitch := servertest.ReadSpawnPosition(t, spawn); gotX != x || gotY != y || gotZ != z || yaw != 90 || pitch != -15 {
		t.Errorf("%s: spawn position = (%d, %d, %d) yaw %v pitch %v, want (%d, %d, %d) yaw 90 pitch -15",
			how, gotX, gotY, gotZ, yaw, pitch, x, y, z)
	}
}
