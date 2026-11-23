package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// removalOf decodes a creative slot of item whose patch removes component.
func removalOf(t *testing.T, item registry.Item, component int32) protocol.ItemStack {
	t.Helper()
	var b protocol.Buffer
	b.PutVarInt(1)
	b.PutVarInt(int32(item))
	b.PutVarInt(0)
	b.PutVarInt(1)
	b.PutVarInt(component)
	stack, ok := protocol.NewReader(b.Bytes()).ReadUntrustedSlot()
	if !ok {
		t.Fatal("slot refused")
	}
	dropRemovalsWithoutDefault(&stack)
	return stack
}

func TestCreativeSlotDropsRemovalsWithoutItemDefault(t *testing.T) {
	plain := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
	for _, component := range []int32{protocol.DataComponentDamage, protocol.DataComponentFood, protocol.DataComponentTool} {
		if got := removalOf(t, registry.ItemStone, component); !got.Same(plain) {
			t.Errorf("stone removing %d does not stack with plain stone: %+v", component, got)
		}
	}
	sword := protocol.ItemStack{ItemID: int32(registry.ItemDiamondSword), Count: 1}
	if got := removalOf(t, registry.ItemDiamondSword, protocol.DataComponentDamage); got.Same(sword) || !got.Removes(protocol.DataComponentDamage) {
		t.Errorf("sword lost its damage removal: %+v", got)
	}
}
