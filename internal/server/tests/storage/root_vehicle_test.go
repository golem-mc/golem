package storage

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server"
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
	writeRiderFile(t, cfg.LevelName, name)

	srv, connect := servertest.Start(t, cfg)
	connect().Join(name)
	awaitSeatedOnFileBoat(t, srv, name)
}

// TestARiderLeavingBeforeTheRemountKeepsRootVehicle: a rider who leaves while
// their file's boat still waits for the loading screen to close leaves the
// file's RootVehicle in place, and the next join seats them on it.
// vanilla: PlayerList.placeNewPlayer seats the rider before any save
func TestARiderLeavingBeforeTheRemountKeepsRootVehicle(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()
	const name = "Ferry"
	writeRiderFile(t, cfg.LevelName, name)

	srv, connect := servertest.Start(t, cfg)
	c := joinStillLoading(t, srv, connect(), name)
	c.Disconnect()
	assertFileKeepsBoat(t, cfg.LevelName, name)
	if n := srv.BoatCount(); n != 0 {
		t.Errorf("%d boats stand after the rider left, want none", n)
	}

	connect().Join(name)
	awaitSeatedOnFileBoat(t, srv, name)
}

// TestASaveBeforeTheRemountKeepsRootVehicle: a save while a rider's file
// boat still waits for the loading screen to close writes the file's
// RootVehicle, and the remount after it still seats them, on one boat.
func TestASaveBeforeTheRemountKeepsRootVehicle(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()
	const name = "Ferry"
	writeRiderFile(t, cfg.LevelName, name)

	srv, connect := servertest.Start(t, cfg)
	c := joinStillLoading(t, srv, connect(), name)
	if !srv.SaveAll(false) {
		t.Fatal("the save failed")
	}
	assertFileKeepsBoat(t, cfg.LevelName, name)

	c.MarkLoaded()
	awaitSeatedOnFileBoat(t, srv, name)
	if n := srv.BoatCount(); n != 1 {
		t.Errorf("%d boats stand after the remount, want the file's one", n)
	}
}

var fileBoat = uuid.MustParse("0b0a7000-0000-4000-8000-00000000b0a7")

// writeRiderFile writes a player file the vanilla way: RootVehicle with
// Attach naming the boat by its UUID.
func writeRiderFile(t *testing.T, level, name string) {
	t.Helper()
	pos := nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(4.5), nbt.Double(64), nbt.Double(4.5)}}
	servertest.WritePlayerFile(t, servertest.PlayerFile(level, name), nbt.Compound{
		"Pos": pos,
		"RootVehicle": nbt.Compound{
			"Attach": nbt.UUIDToIntArray(fileBoat),
			"Entity": nbt.Compound{
				"id":   nbt.String("minecraft:oak_boat"),
				"UUID": nbt.UUIDToIntArray(fileBoat),
				"Pos":  pos,
			},
		},
	})
}

// joinStillLoading joins name with the loading screen kept up, so the file's
// boat waits to be raised, and checks a tick has passed without it.
func joinStillLoading(t *testing.T, srv *server.Server, c *servertest.Client, name string) *servertest.Client {
	t.Helper()
	// The join's own timeout would count the client loaded after 60 ticks.
	srv.PlayerJoinBodiesBetween(func() { srv.HoldPlayerUnloaded(name) })
	c.Login(name)
	c.Configure()
	c.ReadThroughJoin(name)
	servertest.RunTicks(t, srv, 2)
	if n := srv.BoatCount(); n != 0 {
		t.Fatalf("%d boats stand behind the loading screen, want the file's still waiting", n)
	}
	return c
}

func assertFileKeepsBoat(t *testing.T, level, name string) {
	t.Helper()
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(level, name))
	saved, ok := nbt.Get[nbt.Compound](root, "RootVehicle")
	if !ok {
		t.Fatal("no RootVehicle in the file: the boat the rider joined on is gone")
	}
	if attach, _ := nbt.Get[nbt.IntArray](saved, "Attach"); !slices.Equal(attach, nbt.UUIDToIntArray(fileBoat)) {
		t.Errorf("Attach = %v, want the file's boat %v", attach, nbt.UUIDToIntArray(fileBoat))
	}
}

func awaitSeatedOnFileBoat(t *testing.T, srv *server.Server, name string) {
	t.Helper()
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

// TestARiderLeavingOnAMinecartWritesRootVehicleAndRestoresOnRejoin: a player
// riding a minecart saves RootVehicle with Attach naming the cart's UUID and
// Entity holding the cart, and rejoining restores the seat without duplicating
// the cart.
// vanilla: ServerPlayer.saveParentVehicle, ServerPlayer.loadAndSpawnParentVehicle
func TestARiderLeavingOnAMinecartWritesRootVehicleAndRestoresOnRejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Rail")
	c.Settle()
	cartID := srv.SpawnMinecart(4.5, 64, 4.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	c.UseOn(cartID)
	servertest.AwaitPassengersFor(c, cartID)
	tag, ok := srv.MinecartSaveTag(cartID)
	if !ok {
		t.Fatal("no save tag for the ridden minecart")
	}
	identity, _ := nbt.Get[nbt.IntArray](tag, "UUID")
	wantUUID := srv.MinecartsInDimension("minecraft:overworld")[0].UUID

	if !srv.SaveAll(false) {
		t.Fatal("SaveAll failed")
	}
	assertSavedMinecart(t, cfg.LevelName, "Rail", identity)
	if n := len(srv.MinecartsInDimension("minecraft:overworld")); n != 1 {
		t.Fatalf("%d minecarts stand after SaveAll, want 1", n)
	}

	c.Disconnect()
	servertest.UntilTicks(t, srv, 200, "the minecart leaves with its rider", func() bool {
		return len(srv.MinecartsInDimension("minecraft:overworld")) == 0
	})
	assertSavedMinecart(t, cfg.LevelName, "Rail", identity)

	connect().Join("Rail")
	servertest.UntilTicks(t, srv, 200, "the rider seated back in the minecart", func() bool {
		for _, cart := range srv.MinecartsInDimension("minecraft:overworld") {
			if rider, ok := srv.MinecartRider(cart.ID); ok && rider == "Rail" {
				return true
			}
		}
		return false
	})
	carts := srv.MinecartsInDimension("minecraft:overworld")
	if len(carts) != 1 {
		t.Fatalf("%d minecarts stand after rejoin, want 1", len(carts))
	}
	if carts[0].UUID != wantUUID {
		t.Errorf("restored minecart UUID = %q, want %q", carts[0].UUID, wantUUID)
	}
}

func assertSavedMinecart(t *testing.T, level, name string, wantUUID nbt.IntArray) {
	t.Helper()
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(level, name))
	saved, ok := nbt.Get[nbt.Compound](root, "RootVehicle")
	if !ok {
		t.Fatal("no RootVehicle in the file of a rider on a minecart")
	}
	attach, ok := nbt.Get[nbt.IntArray](saved, "Attach")
	if !ok || len(wantUUID) != 4 || !slices.Equal(attach, wantUUID) {
		t.Errorf("Attach = %v (%v), want the minecart's UUID %v", attach, ok, wantUUID)
	}
	ent, ok := nbt.Get[nbt.Compound](saved, "Entity")
	if !ok {
		t.Fatal("RootVehicle has no Entity")
	}
	if id, _ := nbt.Get[nbt.String](ent, "id"); id != "minecraft:minecart" {
		t.Errorf("RootVehicle entity = %q, want minecraft:minecart", id)
	}
	if _, ok := ent["Passengers"]; ok {
		t.Errorf("the saved minecart carries Passengers %v, want the player left out", ent["Passengers"])
	}
}

// TestAMinecartRootVehicleReattachesStandingCartWithoutDuplicating: a player
// whose file names a minecart already standing in the world seats back into
// that cart instead of spawning a second one.
// vanilla: ServerPlayer.loadAndSpawnParentVehicle
func TestAMinecartRootVehicleReattachesStandingCartWithoutDuplicating(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	cartID := srv.SpawnMinecart(4.5, 64, 4.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	tag, ok := srv.MinecartSaveTag(cartID)
	if !ok {
		t.Fatal("no save tag for the standing minecart")
	}
	cartUUID, _ := nbt.Get[nbt.IntArray](tag, "UUID")
	pos := nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(4.5), nbt.Double(64), nbt.Double(4.5)}}
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Rail"), nbt.Compound{
		"Pos": pos,
		"RootVehicle": nbt.Compound{
			"Attach": cartUUID,
			"Entity": nbt.Compound{
				"id":   nbt.String("minecraft:minecart"),
				"UUID": cartUUID,
				"Pos":  pos,
			},
		},
	})

	connect().Join("Rail")
	servertest.UntilTicks(t, srv, 200, "the rider seated in the standing minecart", func() bool {
		rider, ok := srv.MinecartRider(cartID)
		return ok && rider == "Rail"
	})
	if n := len(srv.MinecartsInDimension("minecraft:overworld")); n != 1 {
		t.Errorf("%d minecarts stand after rejoin, want the 1 already in the world", n)
	}
}
