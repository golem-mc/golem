package entities

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

const lootBoatTable = "minecraft:chests/buried_treasure"

// summonLootBoat summons a chest boat carrying the loot table and returns its
// entity ID. The tick loop is stopped so the hull stays where it is.
func summonLootBoat(t *testing.T, srv *server.Server, tag string) int32 {
	t.Helper()
	srv.StopTick()
	srv.RunLine("summon minecraft:oak_chest_boat 4.5 64 4.5 " + tag)
	ids := srv.BoatIDs()
	if len(ids) != 1 {
		t.Fatalf("%d boats stand after the summon, want 1", len(ids))
	}
	return ids[0]
}

// cargoCount adds up the items of the named kind a boat carries.
func cargoCount(srv *server.Server, id int32, itemID int32) int32 {
	var n int32
	for slot := range 27 {
		if stack := srv.BoatCargoAt(id, slot); stack.ItemID == itemID {
			n += stack.Count
		}
	}
	return n
}

// TestAChestBoatRollsItsLootTableOnFirstOpen: a summoned chest boat holds
// nothing until it is opened, then the table's loot is in its slots, and a
// second open does not roll again.
// vanilla: ContainerEntity.unpackChestVehicleLootTable
func TestAChestBoatRollsItsLootTableOnFirstOpen(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Looter", 0)
	boatID := summonLootBoat(t, srv, `{LootTable:"`+lootBoatTable+`",LootTableSeed:7L}`)
	heart := servertest.MustItemID(t, "minecraft:heart_of_the_sea")
	if got := cargoCount(srv, boatID, heart); got != 0 {
		t.Fatalf("the unopened boat holds %d hearts, want none", got)
	}

	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(boatID)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.5, 0.5, 0.5)
		b.PutBool(true) // sneaking
	})
	awaitBoatCargo(t, srv, c)
	if got := cargoCount(srv, boatID, heart); got != 1 {
		t.Errorf("the opened boat holds %d hearts of the sea, want the table's one", got)
	}
	tag, _ := srv.BoatSaveTag(boatID)
	if _, ok := tag["LootTable"]; ok {
		t.Error("the opened boat still saves its loot table")
	}
	if _, ok := nbt.Get[nbt.List](tag, "Items"); !ok {
		t.Error("the opened boat saves no Items")
	}
}

// TestABrokenChestBoatSpillsItsRolledLootTable: breaking an unopened hull
// rolls the table and spills what it gave.
// vanilla: ContainerEntity.unpackChestVehicleLootTable, Containers.dropContents
func TestABrokenChestBoatSpillsItsRolledLootTable(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "survival", "Breaker", 0)
	summonLootBoat(t, srv, `{LootTable:"`+lootBoatTable+`"}`)

	srv.RunLine("kill @e[type=minecraft:oak_chest_boat]")
	servertest.PollUntil(t, 10*time.Second, func() bool { srv.TickBoats(1); return srv.BoatCount() == 0 })

	items := srv.ItemsInDimension(world.DimensionName)
	if got := servertest.ItemCountOf(items, "minecraft:heart_of_the_sea"); got != 1 {
		t.Errorf("the broken hull spilled %d hearts of the sea, want the table's one", got)
	}
}
