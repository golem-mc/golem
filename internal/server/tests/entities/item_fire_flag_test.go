package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestABurningItemSendsTheOnFireFlag: an item lying in fire carries bit 0x01
// of the shared flags to its watchers, and loses it once water puts it out.
// vanilla: Entity.baseTick, setSharedFlagOnFire
func TestABurningItemSendsTheOnFireFlag(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Stoker")
	servertest.HoldTicks(t, srv)

	if !srv.SetBlock(4, 64, 4, registry.MustState("minecraft:fire", nil)) {
		t.Fatal("the world did not take the fire")
	}
	// Resting on the ground inside the fire's cell.
	id := srv.SpawnItemStack(4.5, 64+entity.ItemHalfSize, 4.5,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:stick"), Count: 1})

	flags := func(what string) uint8 {
		t.Helper()
		e, ok := c.MetaEntryWithinTicks(srv, 5, id, protocol.MetadataIndexEntityFlags)
		if !ok {
			t.Fatalf("no entity flags for the item within 5 ticks %s", what)
		}
		return e.ByteVal
	}
	if got := flags("of landing in the fire"); got&protocol.EntityFlagOnFire == 0 {
		t.Fatalf("flags = %#x after the fire, want the on-fire bit", got)
	}
	// The fire bites one health a tick: douse it before the item burns up.
	if !srv.SetBlock(4, 64, 4, registry.MustState("minecraft:water", nil)) {
		t.Fatal("the world did not take the water")
	}
	if got := flags("of the water"); got&protocol.EntityFlagOnFire != 0 {
		t.Fatalf("flags = %#x after the water, want the on-fire bit clear", got)
	}
}
