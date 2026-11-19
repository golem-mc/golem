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
