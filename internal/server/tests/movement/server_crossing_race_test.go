package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
)

// A dimension change's Respawn clears the client's world, and the server
// forgets what it had shown there. Anything about the old world that reaches
// the client after the Respawn stays in the new one for good: a spawn there
// is a ghost nothing will ever remove.

// spawnsAroundRespawn reads c's stream through a dimension change, its chunk
// batch and a settle, and reports whether entity id was spawned before the
// Respawn and after it.
func spawnsAroundRespawn(t *testing.T, c *servertest.Client, id int32) (before, after bool) {
	t.Helper()
	respawned := false
	see := func(packetID int32, data []byte) {
		switch packetID {
		case protocol.IDPlayRespawn:
			respawned = true
		case protocol.IDPlaySpawnEntity:
			if protocol.NewReader(data).ReadVarInt() != id {
				return
			}
			if respawned {
				after = true
			} else {
				before = true
			}
		}
	}
	if !c.Pump(servertest.ExpectTimeout, nil, func(packetID int32, data []byte) bool {
		see(packetID, data)
		return respawned && packetID == protocol.IDPlayChunkBatchFinished
	}) {
		t.Fatal("the nether never arrived for the crossing player")
	}
	// The settle's answer is queued behind anything sent late.
	c.SettleAndCollect(see)
	return before, after
}
