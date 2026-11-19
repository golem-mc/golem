package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestPlacedConcretePowderSetsConcrete covers the placements a concrete
// powder solidifies in: one set with water beside it, one of another colour,
// one set into a water cell, and one water arrives beside after it stood.
// Each leaves the concrete of its colour rather than the powder.
// vanilla: ConcretePowderBlock.getStateForPlacement, updateShape
func TestPlacedConcretePowderSetsConcrete(t *testing.T) {
	t.Parallel()

	water, ok := registry.FindState("minecraft:water", map[string]string{"level": "0"})
	if !ok {
		t.Fatal("the registry has no water source")
	}
	powder, ok := registry.FindState("minecraft:white_concrete_powder", nil)
	if !ok {
		t.Fatal("the registry has no white concrete powder")
	}
	powderItem, ok := registry.ItemID("minecraft:white_concrete_powder")
	if !ok {
		t.Fatal("the item registry has no white concrete powder")
	}
	concrete, ok := registry.FindState("minecraft:white_concrete", nil)
	if !ok {
		t.Fatal("the registry has no white concrete")
	}

	t.Run("water beside the placement", func(t *testing.T) {
		srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

		c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: powderItem, Count: 1})
		c.Settle()

		// A water source in the cell east of the one the powder will fill.
		if !srv.SetBlock(4, 64, 3, water) {
			t.Fatal("could not stand the water")
		}

		c.PlaceBlock(3, 63, 3, protocol.FaceTop)
		servertest.WaitForBlock(t, srv, 3, 64, 3, func(s uint16) bool { return s == concrete })
	})

	t.Run("another colour beside the placement", func(t *testing.T) {
		redItem, ok := registry.ItemID("minecraft:red_concrete_powder")
		if !ok {
			t.Fatal("the item registry has no red concrete powder")
		}
		redConcrete, ok := registry.FindState("minecraft:red_concrete", nil)
		if !ok {
			t.Fatal("the registry has no red concrete")
		}

		srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

		c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: redItem, Count: 1})
		c.Settle()

		if !srv.SetBlock(4, 64, 3, water) {
			t.Fatal("could not stand the water")
		}

		c.PlaceBlock(3, 63, 3, protocol.FaceTop)
		servertest.WaitForBlock(t, srv, 3, 64, 3, func(s uint16) bool { return s == redConcrete })
	})

	t.Run("placed into water", func(t *testing.T) {
		srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

		c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: powderItem, Count: 1})
		c.Settle()

		// A water source in the very cell the placement fills: the clicked
		// stone is beneath it, and the water is replaceable.
		if !srv.SetBlock(3, 64, 3, water) {
			t.Fatal("could not stand the water")
		}

		c.PlaceBlock(3, 63, 3, protocol.FaceTop)
		servertest.WaitForBlock(t, srv, 3, 64, 3, func(s uint16) bool { return s == concrete })
	})

	t.Run("water arrives beside it", func(t *testing.T) {
		srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

		c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: powderItem, Count: 1})
		c.Settle()

		// The powder stands on dry ground first.
		c.PlaceBlock(1, 63, 3, protocol.FaceTop)
		servertest.WaitForBlock(t, srv, 1, 64, 3, func(s uint16) bool { return s == powder })

		// Water set beside it solidifies it where it stands.
		c.ChatCommand("setblock 2 64 3 minecraft:water")
		servertest.WaitForBlock(t, srv, 1, 64, 3, func(s uint16) bool { return s == concrete })
	})
}
