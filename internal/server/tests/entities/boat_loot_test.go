package entities

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

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

// TestOpeningALootChestBoatObtainsItsCriterion: the open that rolls a chest
// boat's bastion table is a player_generates_container_loot for that table
// alone.
// vanilla: ContainerEntity.unpackChestVehicleLootTable, GenerateContainerLootTrigger
func TestOpeningALootChestBoatObtainsItsCriterion(t *testing.T) {
	t.Parallel()
	const name, adv = "Looter", "minecraft:nether/loot_bastion"
	srv, c, _ := servertest.BoatPlayer(t, "survival", name, 0)
	boatID := summonLootBoat(t, srv, `{LootTable:"minecraft:chests/bastion_bridge"}`)
	if srv.AdvancementObtained(name, adv, "loot_bastion_bridge") {
		t.Fatal("loot_bastion_bridge was obtained before the boat was opened")
	}

	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(boatID)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.5, 0.5, 0.5)
		b.PutBool(true) // sneaking
	})
	awaitBoatCargo(t, srv, c)
	if !srv.AdvancementObtained(name, adv, "loot_bastion_bridge") {
		t.Error("opening the loot boat did not obtain loot_bastion_bridge")
	}
	if srv.AdvancementObtained(name, adv, "loot_bastion_treasure") {
		t.Error("opening the loot boat obtained loot_bastion_treasure")
	}
}

// TestAChestBoatKeepsAnUnrolledLootTableInItsSave: before the first open the
// compound carries the table and its seed and no Items.
// vanilla: ContainerEntity.addChestVehicleSaveData
func TestAChestBoatKeepsAnUnrolledLootTableInItsSave(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "creative", "Saver", 0)
	boatID := summonLootBoat(t, srv, `{LootTable:"`+lootBoatTable+`",LootTableSeed:7L}`)
	tag, ok := srv.BoatSaveTag(boatID)
	if !ok {
		t.Fatal("the boat has no save tag")
	}
	if table, _ := nbt.Get[nbt.String](tag, "LootTable"); string(table) != lootBoatTable {
		t.Errorf("saved LootTable = %q, want %q", table, lootBoatTable)
	}
	if seed, _ := nbt.LongValue(tag["LootTableSeed"]); seed != 7 {
		t.Errorf("saved LootTableSeed = %d, want 7", seed)
	}
	if _, ok := tag["Items"]; ok {
		t.Error("the unrolled boat saves Items beside its table")
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

// TestAnEmptyChestBoatSavesAnEmptyItemsList: a chest boat with no loot table
// and no cargo still saves an empty Items list.
// vanilla: ContainerHelper.saveAllItems (alsoWhenEmpty)
func TestAnEmptyChestBoatSavesAnEmptyItemsList(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "creative", "Saver", 0)
	boatID := summonLootBoat(t, srv, `{}`)
	tag, ok := srv.BoatSaveTag(boatID)
	if !ok {
		t.Fatal("the boat has no save tag")
	}
	items, ok := nbt.Get[nbt.List](tag, "Items")
	if !ok || len(items.Items) != 0 {
		t.Errorf("saved Items = %v (present %v), want an empty list", items.Items, ok)
	}
}
