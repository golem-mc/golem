package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The enderman over the wire: it spawns with the vanilla attributes, and a
// player's gaze at its eyes sets the screaming and staring flags of its
// metadata.

func TestAStareAtAnEndermanSetsItsScreamingFlags(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Starer")
	srv.SetPaused(true)
	// The gaze is a narrow cone and the mob acts from the tick it spawns
	// on: on a free loop a rotation that lands late finds the stare over.
	servertest.HoldTicks(t, srv)

	id := srv.SpawnMob("minecraft:enderman", 8.5, 64, 0.5)
	// The eyes at 66.55 against the player's 65.62, eight blocks east: yaw
	// -90 faces +x and the pitch lifts 0.93 blocks over those eight.
	c.SendRotation(-90, -6.63, true)
	c.SettleKeeping()
	c.ExpectMobSpawn(protocol.EntityTypeEnderman)

	var screaming, staring bool
	pumpTicks(c, srv, 100, servertest.HostileSkip, func(packetID int32, data []byte) bool {
		if packetID != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		seen := servertest.VariantSeen{Meta: map[uint8][]int32{}}
		c.ReadVariantMetadata(r, &seen)
		if v, ok := seen.Meta[protocol.MetadataIndexEndermanScreaming]; ok && v[0] == 1 {
			screaming = true
		}
		if v, ok := seen.Meta[protocol.MetadataIndexEndermanStaring]; ok && v[0] == 1 {
			staring = true
		}
		return screaming && staring
	})
	if !staring {
		t.Error("the stare never set the enderman's staring flag")
	}
	if !screaming {
		t.Error("the stare never turned into a target and the screaming flag")
	}
}
