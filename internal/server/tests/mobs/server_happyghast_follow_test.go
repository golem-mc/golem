package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAGhastlingFollowsAGrownCow: with no player in its follow window, a
// ghastling eight blocks from a grown cow flies over to it, since the cow is
// in followable_friendly_mobs.
// vanilla: AdultSensorAnyType.setNearestVisibleAdult, BabyFollowAdult.create
func TestAGhastlingFollowsAGrownCow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	c.ChatCommand(`summon minecraft:cow 30.5 64 0.5 {NoAI:1b}`)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow).EntityID
	c.ChatCommand(`summon minecraft:happy_ghast 38.5 66 0.5 {Age:-24000}`)
	ghastling := c.ExpectMobSpawn(protocol.EntityTypeHappyGhast).EntityID
	cx, cy, cz, _ := srv.MobPos(cow)
	servertest.UntilTicks(t, srv, 400, "the ghastling comes within 4 blocks of the cow", func() bool {
		x, y, z, ok := srv.MobPos(ghastling)
		return ok && math.Sqrt((x-cx)*(x-cx)+(y-cy)*(y-cy)+(z-cz)*(z-cz)) < 4
	})
}
