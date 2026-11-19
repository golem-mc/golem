package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestASpawnQueuedBeforeACrossingArrivesBeforeTheRespawn queues an item's
// spawn while the mailbox's writer is held, then crosses: the Respawn must
// wait for the queued spawn rather than overtake it.
func TestASpawnQueuedBeforeACrossingArrivesBeforeTheRespawn(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Crosser")
	c.Settle()

	release := srv.HoldMailbox("Crosser")
	t.Cleanup(release)
	id := srv.SpawnItemEntity(1.5, 64, 1.5, "minecraft:stone", 1)
	crossed := make(chan struct{})
	go func() {
		defer close(crossed)
		srv.MovePlayerToDimension("Crosser", world.NetherName, 8, 40, 8)
	}()
	// The held writer cannot answer a barrier ping, so the ticks run alone;
	// the read after the release shows the Respawn did not overtake.
	servertest.RunTicks(t, srv, servertest.QuietTicks)
	release()

	before, after := spawnsAroundRespawn(t, c, id)
	<-crossed
	if !before {
		t.Error("the item queued before the crossing never arrived ahead of the Respawn")
	}
	if after {
		t.Error("the overworld item's spawn arrived after the Respawn into the nether")
	}
}

// TestASpawnPairedBeforeACrossingIsNotSentAfterIt pairs an item with the
// player under the item lock, as the tick does, and queues the pairing only
// after the crossing: it must be dropped, not follow the Respawn.
func TestASpawnPairedBeforeACrossingIsNotSentAfterIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Crosser")
	id := srv.SpawnItemEntity(1.5, 64, 1.5, "minecraft:stone", 1)
	c.Settle()

	srv.ShowItemAcrossCrossing("Crosser", id, world.NetherName, 8, 40, 8)
	if _, after := spawnsAroundRespawn(t, c, id); after {
		t.Error("the overworld item's spawn arrived after the Respawn into the nether")
	}
}

// TestABlastDecidedBeforeACrossingDoesNotReachTheCrosser runs a blast the
// way the tick does up to its packets, crosses the player nearest it, and
// queues the packets only then: an Explosion packet that followed the
// Respawn would push the crosser with Player Delta Velocity in the world
// they arrived at, its sound and particles at old coordinates. The player
// who stayed keeps theirs.
func TestABlastDecidedBeforeACrossingDoesNotReachTheCrosser(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	srv, connect := servertest.Start(t, cfg)
	crosser := connect().Join("Crosser")
	stayer := connect().Join("Stayer")

	// The crosser inside the blast's own reach, the send that carries their
	// knockback; the stayer outside it but within the packet's 64, the
	// send without.
	crosser.MoveTo(4.5, 64, 4.5, 0, 0)
	crosser.Settle()
	stayer.MoveTo(20.5, 64, 8.5, 0, 0)
	stayer.Settle()

	srv.BlastAcrossCrossing("Crosser", 8.5, 64, 8.5, 4, world.NetherName, 8, 40, 8)

	respawned := false
	boomsAfter := 0
	see := func(packetID int32, data []byte) {
		switch packetID {
		case protocol.IDPlayRespawn:
			respawned = true
		case protocol.IDPlayExplosion:
			if respawned {
				boomsAfter++
			}
		}
	}
	if !crosser.Pump(servertest.ExpectTimeout, nil, func(packetID int32, data []byte) bool {
		see(packetID, data)
		return respawned && packetID == protocol.IDPlayChunkBatchFinished
	}) {
		t.Fatal("the nether never arrived for the crossing player")
	}
	// The settle's answer is queued behind anything sent late.
	crosser.SettleAndCollect(see)
	if boomsAfter != 0 {
		t.Errorf("the Explosion packet arrived %d times after the Respawn into the nether", boomsAfter)
	}

	// The player who stayed hears of the blast all the same, at the
	// coordinates it was decided at.
	boomSeen := false
	var boom servertest.Explosion
	stayer.ReadTicks(srv, 300, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlayExplosion {
			boom = servertest.ReadExplosion(stayer, data)
			boomSeen = true
		}
		return boomSeen
	})
	if !boomSeen {
		t.Fatal("the player who stayed was never told of the blast")
	}
	if boom.X != 8.5 || boom.Y != 64 || boom.Z != 8.5 {
		t.Errorf("the stayer's blast stood at (%v %v %v), want where it was decided", boom.X, boom.Y, boom.Z)
	}
}

// TestAKnockbackDecidedBeforeACrossingDoesNotReachTheCrosser lands a monster's
// blow the way the tick's landingBlow does, crosses the player it hurt, and
// queues the push only then: a Set Entity Motion that followed the Respawn
// would shove the body that arrived with a direction worked out of the old
// world's coordinates against its new-world position.
func TestAKnockbackDecidedBeforeACrossingDoesNotReachTheCrosser(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Crosser")
	c.MoveTo(4.5, 64, 4.5, 0, 0)
	c.Settle()

	srv.StrikeAcrossCrossing("Crosser", 8.5, 64, 8.5, 2, world.NetherName, 8, 40, 8)

	respawned := false
	motionsAfter := 0
	see := func(packetID int32, data []byte) {
		switch packetID {
		case protocol.IDPlayRespawn:
			respawned = true
		case protocol.IDPlaySetEntityMotion:
			if protocol.NewReader(data).ReadVarInt() != c.PlayerEntityID {
				return
			}
			if respawned {
				motionsAfter++
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
	if motionsAfter != 0 {
		t.Errorf("the victim's Set Entity Motion arrived %d times after the Respawn into the nether", motionsAfter)
	}
}
