package mobs

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The enderman over the wire: it spawns with the vanilla attributes, and a
// player's gaze at its eyes sets the screaming and staring flags of its
// metadata.

func TestAnEndermanSpawnsWithTheVanillaAttributes(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Watcher")
	srv.SetPaused(true)

	id := srv.SpawnMob("minecraft:enderman", 6.5, 64, 0.5)
	m := c.ExpectMobSpawn(protocol.EntityTypeEnderman)
	if m.EntityID != id {
		t.Fatalf("spawned enderman's entity ID = %d, want %d", m.EntityID, id)
	}
	if m.Health != 40 {
		t.Errorf("spawned enderman's health = %v, want 40", m.Health)
	}
	if m.SpeedAttribute != float64(float32(0.3)) {
		t.Errorf("spawned enderman's speed = %v, want the vanilla 0.3", m.SpeedAttribute)
	}
}

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

// TestAnEndermanThatTakesATargetResendsItsAttackingSpeed stares at an
// enderman and expects Update Attributes carrying the attacking modifier once
// it has a target, after a spawn packet without it.
func TestAnEndermanThatTakesATargetResendsItsAttackingSpeed(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Starer")
	srv.SetPaused(true)
	servertest.HoldTicks(t, srv)

	id := srv.SpawnMob("minecraft:enderman", 8.5, 64, 0.5)
	c.SendRotation(-90, -6.63, true)
	c.SettleKeeping()
	c.ExpectMobSpawn(protocol.EntityTypeEnderman)

	var with bool
	pumpTicks(c, srv, 100, servertest.HostileSkip, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlayUpdateAttributes && protocol.NewReader(data).ReadVarInt() == id &&
			bytes.Contains(data, []byte("minecraft:attacking")) {
			with = true
		}
		return with
	})
	if !with {
		t.Error("an enderman with a target never sent the attacking speed modifier")
	}
}
