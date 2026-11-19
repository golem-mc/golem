package storage

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// TestAVanillaRootVehicleSeatsTheRiderOnRejoin: a file written the vanilla
// way, RootVehicle with Attach naming the boat by its UUID, stands the boat
// up and seats the player on it.
// vanilla: ServerPlayer.loadAndSpawnParentVehicle
func TestAVanillaRootVehicleSeatsTheRiderOnRejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	const name = "Ferry"
	boat := uuid.MustParse("0b0a7000-0000-4000-8000-00000000b0a7")
	pos := nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(4.5), nbt.Double(64), nbt.Double(4.5)}}
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"Pos": pos,
		"RootVehicle": nbt.Compound{
			"Attach": nbt.UUIDToIntArray(boat),
			"Entity": nbt.Compound{
				"id":   nbt.String("minecraft:oak_boat"),
				"UUID": nbt.UUIDToIntArray(boat),
				"Pos":  pos,
			},
		},
	})

	srv, connect := servertest.Start(t, cfg)
	connect().Join(name)
	servertest.UntilTicks(t, srv, 200, "the rider seated on the file's boat", func() bool {
		for _, id := range srv.BoatIDs() {
			if rider, ok := srv.BoatRider(id); ok && rider == name {
				return true
			}
		}
		return false
	})
}

// TestARiderLeavingOnABoatWritesRootVehicle: the file of a rider who leaves
// on a boat holds RootVehicle, Attach the boat's own UUID as an int array and
// Entity the boat, with no player saved among its passengers.
// vanilla: ServerPlayer.saveParentVehicle
func TestARiderLeavingOnABoatWritesRootVehicle(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Ferry")
	c.Settle()
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	tag, ok := srv.BoatSaveTag(boatID)
	if !ok {
		t.Fatal("no save tag for the ridden boat")
	}
	identity, _ := nbt.Get[nbt.IntArray](tag, "UUID")
	c.Disconnect()
	servertest.UntilTicks(t, srv, 200, "the boat leaves with its rider", func() bool { return srv.BoatCount() == 0 })

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Ferry"))
	if _, ok := root["root_vehicle"]; ok {
		t.Error("the file still carries root_vehicle")
	}
	saved, ok := nbt.Get[nbt.Compound](root, "RootVehicle")
	if !ok {
		t.Fatal("no RootVehicle in the file of a rider who left on a boat")
	}
	attach, ok := nbt.Get[nbt.IntArray](saved, "Attach")
	if !ok || len(identity) != 4 || !slices.Equal(attach, identity) {
		t.Errorf("Attach = %v (%v), want the boat's UUID %v", attach, ok, identity)
	}
	entity, ok := nbt.Get[nbt.Compound](saved, "Entity")
	if !ok {
		t.Fatal("RootVehicle has no Entity")
	}
	if id, _ := nbt.Get[nbt.String](entity, "id"); id != "minecraft:oak_boat" {
		t.Errorf("RootVehicle entity = %q, want the oak boat", id)
	}
	if _, ok := entity["Passengers"]; ok {
		t.Errorf("the saved boat carries Passengers %v, want the player left out", entity["Passengers"])
	}
}
