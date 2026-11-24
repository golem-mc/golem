package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
)

// The hopper's absorb of an item entity as the wire tells it: a stack taken
// whole is a Remove Entities for its watchers, a partly taken one keeps its
// body and has its count sent again as Set Entity Metadata. These watch a
// second client, whose view is where a body left drawn by a silent take
// would stay.

// absorbedItemWatcher enters two players and stops the tick loop, so the
// hopper's turn is driven by hand. The first is the placer, the second the
// watcher whose wire the tests read.
func absorbedItemWatcher(t *testing.T) (*server.Server, *servertest.Client, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	placer := connect().Join("Placer")
	watcher := connect().Join("Watcher")
	placer.Settle()
	watcher.Settle()
	srv.StopTick()
	return srv, placer, watcher
}

// removeEntitiesContain reports whether a Remove Entities packet takes the
// entity named.
func removeEntitiesContain(data []byte, id int32) bool {
	r := protocol.NewReader(data)
	for n := r.ReadVarInt(); n > 0; n-- {
		if r.ReadVarInt() == id {
			return true
		}
	}
	return false
}

// TestAHopperRemovesAnAbsorbedItemForItsWatchers stands a single dirt on a
// hopper's mouth and ticks it once: the stack goes into the hopper, and the
// watcher is told the item entity is gone rather than left drawing a copy on
// top of the hopper.
func TestAHopperRemovesAnAbsorbedItemForItsWatchers(t *testing.T) {
	t.Parallel()
	srv, placer, watcher := absorbedItemWatcher(t)

	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("the hopper would not stand")
	}
	itemID := srv.SpawnItemEntity(4.5, 65.0, 0.5, "minecraft:dirt", 1)
	if itemID == 0 {
		t.Fatal("the item entity did not spawn")
	}
	watcher.ExpectDroppedItem()
	placer.ExpectDroppedItem()

	servertest.TickBlockEntitiesByHand(srv, 1)

	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Fatalf("the hopper holds %+v, want the absorbed dirt", got)
	}
	gone := false
	watcher.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id == protocol.IDPlayRemoveEntities && removeEntitiesContain(data, itemID) {
			gone = true
		}
		return gone
	})
	if !gone {
		t.Errorf("the watcher was never told the absorbed item %d is gone", itemID)
	}
	if got := srv.ItemsInDimension(world.DimensionName); len(got) != 0 {
		t.Errorf("the world still holds %+v, want the item absorbed", got)
	}
}

// TestAHopperPicksUpTheLowerXSectionFirst registers the diamond before the
// straddling emerald and moves it into the eastern chunk: vanilla visits the
// lower-X section first, so the emerald wins the one free slot.
func TestAHopperPicksUpTheLowerXSectionFirst(t *testing.T) {
	t.Parallel()
	srv := servertest.NewTestServer(t, servertest.BlockTestConfig())
	srv.StopTick()
	t.Cleanup(srv.HoldChunk(15, 0))
	t.Cleanup(srv.HoldChunk(16, 0))

	if !srv.PlaceContainer(16, 64, 0, "minecraft:hopper") {
		t.Fatal("the hopper would not stand")
	}
	cobble := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 64}
	for slot := range 4 {
		if !srv.SetInventoryAt(16, 64, 0, slot, cobble) {
			t.Fatalf("could not fill hopper slot %d", slot)
		}
	}

	firstID := srv.SpawnItemEntity(14.5, 65.125, 0.5, "minecraft:diamond", 1)
	secondID := srv.SpawnItemEntity(15.95, 65.125, 0.5, "minecraft:emerald", 1)
	if firstID == 0 || secondID == 0 {
		t.Fatalf("drops did not spawn: first=%d second=%d", firstID, secondID)
	}
	srv.RunLine("tp @e[type=item,x=14.5,y=65,z=0.5,distance=..1] 16.5 65 0.5")

	servertest.TickBlockEntitiesByHand(srv, 1)

	emerald := servertest.MustItemID(t, "minecraft:emerald")
	if got := srv.InventoryAt(16, 64, 0)[4]; got.ItemID != emerald || got.Count != 1 {
		t.Fatalf("hopper slot 4 holds %+v, want straddling emerald (%d)", got, emerald)
	}
	rem := srv.ItemsInDimension(world.DimensionName)
	if len(rem) != 1 || rem[0].ID != firstID {
		t.Fatalf("remaining items = %+v, want only diamond %d", rem, firstID)
	}
}

// TestAHopperTellsWatchersTheRemainderOfAPartlyTakenItem fills four slots and
// the fifth to 63, blocks the outlet and drops two stone over it: the slot
// reaches 64 and the player is sent the same item entity's remaining stack of
// one, since a partial pickup changes the entity without removing it.
func TestAHopperTellsWatchersTheRemainderOfAPartlyTakenItem(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	watcher.Settle()
	srv.StopTick()

	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("the hopper would not stand")
	}
	srv.SetBlock(4, 63, 0, servertest.MustState(t, "minecraft:stone", nil)) // false if the floor is already stone
	cobble := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 64}
	for slot := range 4 {
		if !srv.SetInventoryAt(4, 64, 0, slot, cobble) {
			t.Fatalf("could not fill hopper slot %d", slot)
		}
	}
	stone := servertest.MustItemID(t, "minecraft:stone")
	if !srv.SetInventoryAt(4, 64, 0, 4, protocol.ItemStack{ItemID: stone, Count: 63}) {
		t.Fatal("could not set the fifth slot")
	}
	itemID := srv.SpawnItemEntity(4.5, 65.0, 0.5, "minecraft:stone", 2)
	if itemID == 0 {
		t.Fatal("the item entity did not spawn")
	}
	itemSlot := func() *servertest.MetaEntry {
		var slot *servertest.MetaEntry
		watcher.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			if id != protocol.IDPlayEntityMetadata {
				return false
			}
			m := protocol.NewReader(data)
			if m.ReadVarInt() != itemID {
				return false
			}
			entries, ok := servertest.ReadMetadata(m)
			if !ok {
				return false
			}
			slot = servertest.MetaOf(entries, protocol.MetadataIndexItemSlot)
			return slot != nil
		})
		return slot
	}
	if slot := itemSlot(); slot == nil || slot.SlotVal.Count != 2 {
		t.Fatalf("the spawn sent %+v for the item, want two stone", slot)
	}

	servertest.TickBlockEntitiesByHand(srv, 1)

	if got := srv.InventoryAt(4, 64, 0)[4]; got.ItemID != stone || got.Count != 64 {
		t.Fatalf("hopper slot 4 holds %+v, want 64 stone", got)
	}
	slot := itemSlot()
	if slot == nil {
		t.Fatal("the watcher was sent no item slot for the item")
	}
	if slot.SlotVal.ItemID != stone || slot.SlotVal.Count != 1 {
		t.Errorf("the watcher was sent %+v for the item, want one stone", slot.SlotVal)
	}
}

// TestAHopperPicksUpAcrossAChunkBorder covers an item whose centre is in
// the next chunk, including arrivals after another hopper searched that chunk.
// vanilla: HopperBlockEntity.getItemsAtAndAbove
func TestAHopperPicksUpAcrossAChunkBorder(t *testing.T) {
	t.Parallel()
	for _, arrival := range []string{"present", "spawned", "teleported"} {
		t.Run(arrival, func(t *testing.T) {
			srv := servertest.NewTestServer(t, servertest.BlockTestConfig())
			srv.StopTick()
			t.Cleanup(srv.HoldChunk(15, 0))
			t.Cleanup(srv.HoldChunk(16, 0))

			// The first hopper searches both chunks before the second takes its turn.
			for _, z := range []int32{1, 0} {
				if !srv.PlaceContainer(16, 64, z, "minecraft:hopper") {
					t.Fatalf("could not place hopper at z=%d", z)
				}
			}
			spawn := func(x float64) {
				t.Helper()
				if srv.SpawnItemEntity(x, 65, 0.5, "minecraft:stone", 1) == 0 {
					t.Fatal("the drop did not spawn")
				}
			}
			switch arrival {
			case "present":
				spawn(15.95)
			case "teleported":
				spawn(17.5)
			}
			srv.BlockEntityTickBetween(func(be *world.BlockEntity) {
				if be.X != 16 || be.Y != 64 || be.Z != 0 {
					return
				}
				switch arrival {
				case "spawned":
					spawn(15.95)
				case "teleported":
					srv.RunLine("tp @e[type=item] 15.95 65 0.5")
				}
			})
			servertest.TickBlockEntitiesByHand(srv, 1)

			stone := servertest.MustItemID(t, "minecraft:stone")
			if got := srv.InventoryAt(16, 64, 0)[0]; got.ItemID != stone || got.Count != 1 {
				t.Fatalf("hopper slot 0 holds %+v, want the straddling stone", got)
			}
		})
	}
}
