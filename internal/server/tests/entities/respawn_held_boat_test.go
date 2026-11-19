package entities

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestASameDimensionRespawnRemovesAHeldBoatBeforeTheRespawn kills a player
// beside a boat and respawns them in the same dimension: the boat's Remove
// Entities comes ahead of the Respawn, as the removal of the old player takes
// every body it held out first, and the boat is shown again after it.
// vanilla: PlayerList.respawn, ChunkMap.removeEntity, ServerEntity.removePairing
func TestASameDimensionRespawnRemovesAHeldBoatBeforeTheRespawn(t *testing.T) {
	t.Parallel()
	boatType, ok := registry.EntityTypeID("minecraft:oak_boat")
	if !ok {
		t.Fatal("no entity type minecraft:oak_boat")
	}
	srv, c, playerID := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Rower", servertest.OpsListing(t, "Rower"))
	boat := srv.SpawnBoat(3.5, 64, 3.5, "minecraft:oak_boat")
	if boat == 0 {
		t.Fatal("the boat was not laid")
	}
	if got := c.AwaitSpawn(boatType).EntityID; got != boat {
		t.Fatalf("the boat was shown as entity %d, want %d", got, boat)
	}

	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, playerID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Rower")},
	})
	c.PerformRespawn()

	// The order the client is told the boat in, around the Respawn.
	var order []string
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		switch id {
		case protocol.IDPlayRemoveEntities:
			if slices.Contains(servertest.RemovedIDs(t, data), boat) {
				order = append(order, "remove boat")
			}
		case protocol.IDPlayRespawn:
			order = append(order, "respawn")
		case protocol.IDPlaySpawnEntity:
			if protocol.NewReader(data).ReadVarInt() == boat {
				order = append(order, "spawn boat")
			}
		}
		if slices.Contains(order, "spawn boat") {
			break
		}
	}
	if want := []string{"remove boat", "respawn", "spawn boat"}; !slices.Equal(order, want) {
		t.Errorf("the boat was told in the order %v around the respawn, want %v", order, want)
	}
}
