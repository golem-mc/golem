package gamemode

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestCreativeSlotIgnoredOutsideCreative checks that a survival client cannot
// conjure items with the creative inventory packet, which the vanilla server
// only honours from a creative player.
func TestCreativeSlotIgnoredOutsideCreative(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Cheater")

	diamond, _ := registry.ItemID("minecraft:diamond")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 64})
	c.Settle()

	if got := srv.InventorySlot("Cheater", player.HotbarSlot); !got.Empty() {
		t.Errorf("survival player's hotbar after a creative slot = %+v, want empty", got)
	}
}

// TestCreativeSlotDropsRemovalsWithoutItemDefault checks that a creative stack
// removing a component its item has no value for stores the plain item, so it
// stacks with one; a removal of a component the item does have is kept.
// vanilla: PatchedDataComponentMap.fromPatch
func TestCreativeSlotDropsRemovalsWithoutItemDefault(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.ItemCreativeConfig(), "Builder")

	// SetCreativeSlot carries no removals, so the packet is written by hand.
	removing := func(item registry.Item, component int32) {
		c.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
			b.PutI16(player.HotbarSlot)
			b.PutVarInt(1)
			b.PutVarInt(int32(item))
			b.PutVarInt(0) // components to add
			b.PutVarInt(1) // components to remove
			b.PutVarInt(component)
		})
		c.Settle()
	}
	stone := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
	resistance, ok := registry.ComponentID("minecraft:damage_resistant")
	if !ok {
		t.Fatal("no damage_resistant component")
	}
	for _, component := range []int32{
		protocol.DataComponentDamage, protocol.DataComponentFood,
		protocol.DataComponentTool, resistance,
	} {
		removing(registry.ItemStone, component)
		if got := srv.InventorySlot("Builder", player.HotbarSlot); !got.Same(stone) {
			t.Errorf("stone removing component %d = %+v, want plain stone", component, got)
		}
	}

	removing(registry.ItemDiamondSword, protocol.DataComponentDamage)
	got := srv.InventorySlot("Builder", player.HotbarSlot)
	if !got.Removes(protocol.DataComponentDamage) {
		t.Errorf("diamond sword lost its damage removal: %+v", got)
	}
}
