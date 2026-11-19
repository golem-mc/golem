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

// TestAVanillaRootVehicleSeatsTheRiderOnAPassenger: Attach naming a pig that
// rides the saved boat stands the boat up with the pig aboard and seats the
// player on the pig.
// vanilla: ServerPlayer.loadAndSpawnParentVehicle
func TestAVanillaRootVehicleSeatsTheRiderOnAPassenger(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()

	const name = "Swineherd"
	boat := uuid.MustParse("0b0a7000-0000-4000-8000-00000000b0a8")
	pig := uuid.MustParse("0b0a7000-0000-4000-8000-0000000091a9")
	pos := nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(4.5), nbt.Double(64), nbt.Double(4.5)}}
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"Pos": pos,
		"RootVehicle": nbt.Compound{
			"Attach": nbt.UUIDToIntArray(pig),
			"Entity": nbt.Compound{
				"id":   nbt.String("minecraft:oak_boat"),
				"UUID": nbt.UUIDToIntArray(boat),
				"Pos":  pos,
				"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
					"id":   nbt.String("minecraft:pig"),
					"UUID": nbt.UUIDToIntArray(pig),
					"Pos":  pos,
				}}},
			},
		},
	})

	srv, connect := servertest.Start(t, cfg)
	connect().Join(name)
	servertest.UntilTicks(t, srv, 200, "the rider seated on the pig in the file's boat", func() bool {
		pigs := srv.MobIDsOfKind("minecraft:pig")
		ridden := srv.RiddenMobIDs()
		return len(pigs) == 1 && len(ridden) == 1 && ridden[0] == pigs[0]
	})
	pigID := srv.MobIDsOfKind("minecraft:pig")[0]
	boats := srv.BoatIDs()
	if len(boats) != 1 || !slices.Contains(srv.BoatPassengerIDs(boats[0]), pigID) {
		t.Errorf("boats %v, want the one boat carrying pig %d", boats, pigID)
	}
}

// TestARiderLeavingWithAPigAboardSavesAndRaisesThePig: a pig sharing the
// rider's boat is saved among the root's Passengers, leaves the world with
// the rider and comes back aboard on the rejoin.
// vanilla: ServerPlayer.saveParentVehicle, PlayerList.remove
func TestARiderLeavingWithAPigAboardSavesAndRaisesThePig(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()

	const name = "Ferryman"
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, name))
	c := connect().Join(name)
	c.Settle()
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.ChatCommand("summon minecraft:pig 4.5 64 4.5")
	c.ChatCommand("ride @e[type=minecraft:pig,limit=1] mount @e[type=minecraft:oak_boat,limit=1]")
	servertest.UntilTicks(t, srv, 100, "the pig aboard", func() bool { return len(srv.BoatPassengerIDs(boatID)) == 1 })
	c.UseOn(boatID)
	servertest.UntilTicks(t, srv, 100, "the player aboard", func() bool {
		rider, ok := srv.BoatRider(boatID)
		return ok && rider == name
	})
	c.Disconnect()
	servertest.UntilTicks(t, srv, 200, "the boat and pig leave with the rider", func() bool {
		return srv.BoatCount() == 0 && len(srv.MobIDsOfKind("minecraft:pig")) == 0
	})

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, name))
	saved, _ := nbt.Get[nbt.Compound](root, "RootVehicle")
	entity, _ := nbt.Get[nbt.Compound](saved, "Entity")
	list, _ := nbt.Get[nbt.List](entity, "Passengers")
	if len(list.Items) != 1 {
		t.Fatalf("saved boat Passengers = %v, want the pig alone", entity["Passengers"])
	}
	if id, _ := nbt.Get[nbt.String](list.Items[0].(nbt.Compound), "id"); id != "minecraft:pig" {
		t.Errorf("saved passenger = %q, want the pig", id)
	}

	connect().Join(name)
	servertest.UntilTicks(t, srv, 200, "the rider and the pig back aboard", func() bool {
		boats, pigs := srv.BoatIDs(), srv.MobIDsOfKind("minecraft:pig")
		if len(boats) != 1 || len(pigs) != 1 {
			return false
		}
		rider, ok := srv.BoatRider(boats[0])
		return ok && rider == name && slices.Contains(srv.BoatPassengerIDs(boats[0]), pigs[0])
	})
}

// TestALeaverSharingABoatLeavesItToTheOtherRider: a boat carrying two
// players is no player's root vehicle, so the leaver's file holds no
// RootVehicle and the boat stays in the world with the other rider aboard.
// vanilla: ServerPlayer.saveParentVehicle (hasExactlyOnePlayerPassenger)
func TestALeaverSharingABoatLeavesItToTheOtherRider(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	stay := connect().Join("Helmsman")
	stay.Settle()
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	stay.UseOn(boatID)
	servertest.UntilTicks(t, srv, 100, "the first player aboard", func() bool {
		rider, ok := srv.BoatRider(boatID)
		return ok && rider == "Helmsman"
	})
	leave := connect().Join("Deckhand")
	leave.Settle()
	leave.UseOn(boatID)
	servertest.UntilTicks(t, srv, 100, "both players aboard", func() bool { return len(srv.BoatPassengerIDs(boatID)) == 2 })
	leave.Disconnect()
	servertest.UntilTicks(t, srv, 200, "the leaver saved and off the boat", func() bool {
		return len(srv.BoatPassengerIDs(boatID)) == 1
	})

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Deckhand"))
	if _, ok := root["RootVehicle"]; ok {
		t.Errorf("the leaver's file carries RootVehicle %v, want none for a shared boat", root["RootVehicle"])
	}
	if ids := srv.BoatIDs(); len(ids) != 1 || ids[0] != boatID {
		t.Fatalf("boats %v, want boat %d kept", ids, boatID)
	}
	if rider, ok := srv.BoatRider(boatID); !ok || rider != "Helmsman" {
		t.Errorf("boat rider = %q (%v), want Helmsman still aboard", rider, ok)
	}
}

// TestARiderOnACamelWithAPigAboardTakesBothAlong: a rider sharing a camel
// with a pig saves the camel as RootVehicle.Entity with the pig among its
// Passengers, takes both out of the world on leaving and stands both up on
// the rejoin, seated on the camel Attach names.
// vanilla: ServerPlayer.saveParentVehicle, ServerPlayer.loadAndSpawnParentVehicle, PlayerList.remove
func TestARiderOnACamelWithAPigAboardTakesBothAlong(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()

	const name = "Caravaneer"
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, name))
	c := connect().Join(name)
	c.Settle()
	c.ChatCommand("summon minecraft:camel 4.5 64 4.5")
	c.ChatCommand("summon minecraft:pig 4.5 64 4.5")
	servertest.UntilTicks(t, srv, 100, "the camel and the pig", func() bool {
		return len(srv.MobIDsOfKind("minecraft:camel")) == 1 && len(srv.MobIDsOfKind("minecraft:pig")) == 1
	})
	camelID, pigID := srv.MobIDsOfKind("minecraft:camel")[0], srv.MobIDsOfKind("minecraft:pig")[0]
	c.ChatCommand("ride @e[type=minecraft:pig,limit=1] mount @e[type=minecraft:camel,limit=1]")
	servertest.UntilTicks(t, srv, 100, "the pig on the camel", func() bool { return srv.MobVehicleID(pigID) == camelID })
	c.ChatCommand("ride @s mount @e[type=minecraft:camel,limit=1]")
	servertest.UntilTicks(t, srv, 100, "the player on the camel", func() bool { return len(srv.MobPassengerIDs(camelID)) == 2 })
	c.Disconnect()
	servertest.UntilTicks(t, srv, 200, "the camel and the pig leave with the rider", func() bool {
		return len(srv.MobIDsOfKind("minecraft:camel")) == 0 && len(srv.MobIDsOfKind("minecraft:pig")) == 0
	})

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, name))
	saved, _ := nbt.Get[nbt.Compound](root, "RootVehicle")
	entity, ok := nbt.Get[nbt.Compound](saved, "Entity")
	if !ok {
		t.Fatalf("no RootVehicle.Entity in the file, RootVehicle = %v", root["RootVehicle"])
	}
	if id, _ := nbt.Get[nbt.String](entity, "id"); id != "minecraft:camel" {
		t.Errorf("RootVehicle entity = %q, want the camel", id)
	}
	list, _ := nbt.Get[nbt.List](entity, "Passengers")
	if len(list.Items) != 1 {
		t.Fatalf("saved camel Passengers = %v, want the pig alone", entity["Passengers"])
	}
	if id, _ := nbt.Get[nbt.String](list.Items[0].(nbt.Compound), "id"); id != "minecraft:pig" {
		t.Errorf("saved passenger = %q, want the pig", id)
	}
	attach, _ := nbt.Get[nbt.IntArray](saved, "Attach")
	if camelUUID, _ := nbt.Get[nbt.IntArray](entity, "UUID"); len(camelUUID) != 4 || !slices.Equal(attach, camelUUID) {
		t.Errorf("Attach = %v, want the camel's UUID %v", attach, camelUUID)
	}

	self := connect().JoinAs(name)
	servertest.UntilTicks(t, srv, 200, "the camel and the pig back, the rider on the camel", func() bool {
		camels, pigs := srv.MobIDsOfKind("minecraft:camel"), srv.MobIDsOfKind("minecraft:pig")
		if len(camels) != 1 || len(pigs) != 1 {
			return false
		}
		return srv.MobVehicleID(pigs[0]) == camels[0] && slices.Contains(srv.MobPassengerIDs(camels[0]), self)
	})
}

// TestARootVehiclePigAndItsRiderJoinTheirTeamOnRejoin: a file's RootVehicle
// pig and the zombie riding it, each with Team "red", are both on red once
// the player rejoins.
// vanilla: ServerPlayer.loadAndSpawnParentVehicle, EntityType.loadEntityRecursive, Entity.load
func TestARootVehiclePigAndItsRiderJoinTheirTeamOnRejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()

	const name = "Swineherd"
	pig := uuid.MustParse("0b0a7000-0000-4000-8000-0000000091b1")
	zombie := uuid.MustParse("0b0a7000-0000-4000-8000-0000000020b1")
	pos := nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(4.5), nbt.Double(64), nbt.Double(4.5)}}
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"Pos": pos,
		"RootVehicle": nbt.Compound{
			"Attach": nbt.UUIDToIntArray(pig),
			"Entity": nbt.Compound{
				"id":   nbt.String("minecraft:pig"),
				"UUID": nbt.UUIDToIntArray(pig),
				"Pos":  pos,
				"Team": nbt.String("red"),
				"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
					"id":   nbt.String("minecraft:zombie"),
					"UUID": nbt.UUIDToIntArray(zombie),
					"Pos":  pos,
					"NoAI": nbt.Byte(1),
					"Team": nbt.String("red"),
				}}},
			},
		},
	})

	srv, connect := servertest.Start(t, cfg)
	srv.PutOnTeam("red", name)
	connect().Join(name)
	servertest.UntilTicks(t, srv, 200, "the pig and its zombie rider on red", func() bool {
		for _, id := range []uuid.UUID{pig, zombie} {
			if team, ok := srv.TeamOf(id.String()); !ok || team != "red" {
				return false
			}
		}
		return true
	})
	if pigs, zombies := srv.MobIDsOfKind("minecraft:pig"), srv.MobIDsOfKind("minecraft:zombie"); len(pigs) != 1 || len(zombies) != 1 {
		t.Errorf("pigs %v, zombies %v, want one of each", pigs, zombies)
	}
}

// TestARootVehicleBoatJoinsItsTeamOnRejoin: a file's RootVehicle boat with
// Team "red" is on red once the player rejoins on it.
// vanilla: ServerPlayer.loadAndSpawnParentVehicle, Entity.load
func TestARootVehicleBoatJoinsItsTeamOnRejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	const name = "Ferry"
	boat := uuid.MustParse("0b0a7000-0000-4000-8000-00000000b0b2")
	pos := nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(4.5), nbt.Double(64), nbt.Double(4.5)}}
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"Pos": pos,
		"RootVehicle": nbt.Compound{
			"Attach": nbt.UUIDToIntArray(boat),
			"Entity": nbt.Compound{
				"id":   nbt.String("minecraft:oak_boat"),
				"UUID": nbt.UUIDToIntArray(boat),
				"Pos":  pos,
				"Team": nbt.String("red"),
			},
		},
	})

	srv, connect := servertest.Start(t, cfg)
	srv.PutOnTeam("red", name)
	connect().Join(name)
	servertest.UntilTicks(t, srv, 200, "the file's boat on red", func() bool {
		team, ok := srv.TeamOf(boat.String())
		return ok && team == "red"
	})
}
