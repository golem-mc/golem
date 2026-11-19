package entities

import (
	"reflect"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// savedRider is a minimal mob compound for a vehicle's Passengers list.
func savedRider(id string, uuidWords nbt.Tag, x, y, z float64) nbt.Compound {
	return nbt.Compound{
		"id":   nbt.String(id),
		"UUID": uuidWords,
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(x), nbt.Double(y), nbt.Double(z),
		}},
		"Motion": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0), nbt.Double(0), nbt.Double(0)}},
		"Health": nbt.Float(20),
	}
}

// savedVehicle is a vehicle compound nesting one rider.
func savedVehicle(id string, uuidWords nbt.Tag, x, y, z float64, rider nbt.Compound) nbt.Compound {
	c := savedRider(id, uuidWords, x, y, z)
	delete(c, "Health")
	c["Passengers"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{rider}}
	return c
}

// passengerIDs is the ids of the compounds a saved vehicle nests.
func passengerIDs(compound nbt.Compound) []string {
	list, _ := nbt.Get[nbt.List](compound, "Passengers")
	var ids []string
	for _, item := range list.Items {
		if c, ok := item.(nbt.Compound); ok {
			id, _ := nbt.Get[nbt.String](c, "id")
			ids = append(ids, string(id))
		}
	}
	return ids
}

// TestASavedMinecartAndBoatWakeWithTheirRiders: a cart and a boat planted
// with a mob nested in their Passengers wake with it seated, and the save
// nests it again rather than dropping it.
// vanilla: EntityType.loadPassengersRecursive, Entity.addSaveData
func TestASavedMinecartAndBoatWakeWithTheirRiders(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	cart := savedVehicle("minecraft:minecart", servertest.UuidWords(servertest.FixtureUUID(4, 7, 6, 5)), 5.5, 70, 5.5,
		savedRider("minecraft:villager", servertest.UuidWords(servertest.FixtureUUID(4, 7, 6, 6)), 5.5, 70, 5.5))
	// A rider of a kind the server does not seat is carried and saved again.
	stand := savedRider("minecraft:armor_stand", servertest.UuidWords(servertest.FixtureUUID(4, 7, 6, 9)), 5.5, 70, 5.5)
	stand["Pose"] = nbt.Compound{"Head": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(10), nbt.Float(20), nbt.Float(30)}}}
	riders := cart["Passengers"].(nbt.List)
	riders.Items = append(riders.Items, stand)
	cart["Passengers"] = riders
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0},
		cart,
		savedVehicle("minecraft:oak_boat", servertest.UuidWords(servertest.FixtureUUID(4, 7, 6, 7)), 9.5, 70, 9.5,
			savedRider("minecraft:pig", servertest.UuidWords(servertest.FixtureUUID(4, 7, 6, 8)), 9.5, 70, 9.5)),
	)

	srv, _ := servertest.Join(t, cfg, "Keeper")
	servertest.WaitFor(t, srv, 200, func() bool {
		return slices.EqualFunc(srv.MinecartMobRidersInDimension(world.DimensionName), [][]string{{"minecraft:villager"}}, slices.Equal) &&
			slices.EqualFunc(srv.BoatMobRidersInDimension(world.DimensionName), [][]string{{"minecraft:pig"}}, slices.Equal)
	})
	servertest.HoldTicks(t, srv)

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:minecart", func(compound nbt.Compound) {
		got := passengerIDs(compound)
		slices.Sort(got)
		if !slices.Equal(got, []string{"minecraft:armor_stand", "minecraft:villager"}) {
			t.Errorf("the saved cart's Passengers = %v, want the armor stand and the villager", got)
		}
		list, _ := nbt.Get[nbt.List](compound, "Passengers")
		for _, rider := range list.Items {
			c, ok := rider.(nbt.Compound)
			if ok && c["id"] == nbt.String("minecraft:armor_stand") && !reflect.DeepEqual(c, stand) {
				t.Errorf("retained armor stand = %v, want %v", c, stand)
			}
		}
	})
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:oak_boat", func(compound nbt.Compound) {
		if got := passengerIDs(compound); !slices.Equal(got, []string{"minecraft:pig"}) {
			t.Errorf("the saved boat's Passengers = %v, want the pig", got)
		}
	})
}

// TestAVillagerInAMinecartIsStillSeatedAfterItsChunkReloads seats a villager
// in a cart, lets the chunk unload with the player's leaving, and finds it
// nested in the saved cart and seated again after the reload.
// vanilla: Entity.save, EntityType.loadPassengersRecursive
func TestAVillagerInAMinecartIsStillSeatedAfterItsChunkReloads(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Herder"))
	c := connect().Join("Herder")
	cart := srv.SpawnMinecart(5.5, 64, 5.5)
	mob := srv.SpawnMob("minecraft:villager", 6.5, 64, 5.5)
	servertest.RunTicks(t, srv, 5)
	if !srv.MountMobOnMinecart(mob, cart) {
		t.Fatal("the villager was not seated in the cart")
	}

	c.Disconnect()
	servertest.WaitFor(t, srv, 400, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:minecart", func(compound nbt.Compound) {
		if got := passengerIDs(compound); !slices.Equal(got, []string{"minecraft:villager"}) {
			t.Errorf("the saved cart's Passengers = %v, want the villager", got)
		}
	})

	connect().Join("Herder")
	servertest.WaitFor(t, srv, 400, func() bool {
		return srv.MobCount() == 1 &&
			slices.EqualFunc(srv.MinecartMobRidersInDimension(world.DimensionName), [][]string{{"minecraft:villager"}}, slices.Equal)
	})
}
