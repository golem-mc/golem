package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The riding triggers: started_riding, item_durability_changed with a
// vehicle condition and ride_entity_in_lava.

// TestBoardingABoatAGoatSitsInObtainsGreatestOfAllTime boards an empty
// boat, which does not obtain ride_a_boat_with_a_goat, then one a goat sits
// in, which does. vanilla: Entity.startRiding, StartRidingTrigger
func TestBoardingABoatAGoatSitsInObtainsGreatestOfAllTime(t *testing.T) {
	t.Parallel()
	const name, goat = "Sailor", "minecraft:husbandry/ride_a_boat_with_a_goat"
	srv, c := servertest.Join(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	empty := srv.SpawnBoat(2.5, 64, 0.5, "minecraft:oak_boat")
	c.UseOn(empty)
	servertest.AwaitPassengersFor(c, empty)
	if srv.AdvancementObtained(name, goat, "ride_a_boat_with_a_goat") {
		t.Fatal("boarding an empty boat obtained ride_a_boat_with_a_goat")
	}
	c.ChatCommand("ride @s dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")

	laden := srv.SpawnBoat(0.5, 64, 2.5, "minecraft:birch_boat")
	c.ChatCommand("summon minecraft:goat 0.5 64 2.5")
	c.Settle()
	c.ChatCommand("ride @e[type=goat,limit=1] mount @e[type=birch_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.UseOn(laden)
	c.Settle()
	servertest.UntilTicks(t, srv, 100, "boarding a boat a goat sits in obtains ride_a_boat_with_a_goat", func() bool {
		return srv.AdvancementObtained(name, goat, "ride_a_boat_with_a_goat")
	})
}

// striderType is the strider's entity type ID.
func striderType(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:strider")
	if !ok {
		t.Fatal("no strider entity type")
	}
	return id
}

// striderRider seats the named player, with the loop stopped, on a saddled
// strider summoned at (5.5, 64, 5.5) and returns the strider's entity ID.
func striderRider(t *testing.T, name string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()
	c.ChatCommand(`summon minecraft:strider 5.5 64 5.5 {equipment:{saddle:{id:"minecraft:saddle",count:1}}}`)
	strider := c.ExpectMobSpawn(striderType(t)).EntityID
	c.UseOn(strider)
	c.Settle()
	return srv, c, strider
}
