package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
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
	servertest.UntilTicks(t, srv, 100, "boarding a boat a goat sits in obtains ride_a_boat_with_a_goat", func() bool {
		return srv.AdvancementObtained(name, goat, "ride_a_boat_with_a_goat")
	})
}

// TestBoostingAStriderObtainsThisBoatHasLegs rides a saddled strider: the
// mount alone does not obtain ride_strider, the fungus stick's wear from a
// boost does. vanilla: ItemStack.applyDamage, ItemDurabilityTrigger
func TestBoostingAStriderObtainsThisBoatHasLegs(t *testing.T) {
	t.Parallel()
	const name, legs = "Rider", "minecraft:nether/ride_strider"
	srv, c, strider := striderRider(t, name)
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:warped_fungus_on_a_stick"), Count: 1})
	c.Settle()
	if srv.AdvancementObtained(name, legs, "used_warped_fungus_on_a_stick") {
		t.Fatal("mounting a strider obtained ride_strider")
	}
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if got := srv.InventorySlot(name, player.HotbarSlot).Damage; got != 1 {
		t.Fatalf("stick wear %d after a boost, want 1", got)
	}
	if !srv.AdvancementObtained(name, legs, "used_warped_fungus_on_a_stick") {
		t.Errorf("the boost's wear on the stick, riding strider %d, did not obtain ride_strider", strider)
	}
}

// TestRidingAStriderFiftyBlocksThroughLavaObtainsFeelsLikeHome carries a
// rider's strider through lava: 20 blocks from where it entered does not
// obtain ride_strider_in_overworld_lava, 60 does.
// vanilla: ServerPlayer.trackEnteredOrExitedLavaOnVehicle, DistanceTrigger
func TestRidingAStriderFiftyBlocksThroughLavaObtainsFeelsLikeHome(t *testing.T) {
	t.Parallel()
	const name, home = "Rider", "minecraft:nether/ride_strider_in_overworld_lava"
	lava := servertest.BlockState(t, "minecraft:lava")
	srv, c := servertest.Join(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	for _, x := range []int32{5, 25, 65} {
		for dx := int32(-2); dx <= 2; dx++ {
			for dz := int32(3); dz <= 7; dz++ {
				if !srv.SetBlock(x+dx, 63, dz, lava) {
					t.Fatalf("chunk of (%d, %d) not loaded", x+dx, dz)
				}
			}
		}
	}
	c.ChatCommand("summon minecraft:strider 5.5 63.5 5.5 {NoAI:1b,equipment:{saddle:{id:\"minecraft:saddle\",count:1}}}")
	strider := c.ExpectMobSpawn(striderType(t)).EntityID
	c.UseOn(strider)
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 3)

	srv.MoveMob(strider, 25.5, 5.5)
	servertest.RunTicks(t, srv, 3)
	if srv.AdvancementObtained(name, home, "ride_entity_distance") {
		t.Fatal("riding a strider 20 blocks through lava obtained ride_strider_in_overworld_lava")
	}
	srv.MoveMob(strider, 65.5, 5.5)
	servertest.RunTicks(t, srv, 3)
	if !srv.AdvancementObtained(name, home, "ride_entity_distance") {
		t.Errorf("riding a strider 60 blocks through lava did not obtain ride_strider_in_overworld_lava (rider at %+v)",
			srv.PlayerPosition(name))
	}
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
