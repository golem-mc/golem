package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestNoSaveEntitiesAreNotWritten saves a bobber, a leash knot and a
// lightning bolt next to a snowball, and finds only the snowball in the
// chunk's entity file.
// vanilla: EntityType.noSave, Entity.shouldBeSaved
func TestNoSaveEntitiesAreNotWritten(t *testing.T) {
	t.Parallel()
	_, level := restartAround(t, func(srv *server.Server) {
		srv.ThrowThrown(entity.ThrownSnowball, 3.5, 100, 3.5, 0, 0, 0, 1)
		srv.SpawnBobberEntity(4.5, 100, 3.5)
		srv.SpawnKnotEntity(5, 100, 3)
		srv.StrikeLightning(6.5, 100, 3.5)
	}, nil)
	pos := world.ChunkPos{X: 0, Z: 0}
	servertest.EntityFileIs(t, level, pos, "minecraft:snowball", func(nbt.Compound) {})
	for _, c := range servertest.ReadChunkEntities(t, level, pos) {
		id, _ := nbt.Get[nbt.String](c, "id")
		switch id {
		case "minecraft:fishing_bobber", "minecraft:leash_knot", "minecraft:lightning_bolt":
			t.Errorf("the entity file holds a %s, which vanilla never saves", id)
		}
	}
}
