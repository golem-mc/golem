package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func stackWithMaxDamage(item Item, damage, maxDamage int32) protocol.ItemStack {
	var b protocol.Buffer
	b.PutVarInt(maxDamage)
	s := protocol.ItemStack{ItemID: int32(item), Count: 1, Damage: damage}
	s.SetRaw(protocol.RawComponent{Type: protocol.DataComponentMaxDamage, Data: b.Bytes()})
	return s
}

// TestRepairCraftFollowsTheStacksOwnMaxDamage: two pickaxes at max_damage 20
// repair against 20, not the prototype's 1561, and the result keeps that
// max_damage.
// vanilla: RepairItemRecipe.assemble
func TestRepairCraftFollowsTheStacksOwnMaxDamage(t *testing.T) {
	t.Parallel()
	cells := []protocol.ItemStack{
		stackWithMaxDamage(ItemDiamondPickaxe, 15, 20),
		stackWithMaxDamage(ItemDiamondPickaxe, 18, 20),
	}
	got, ok := repairItem(append(cells, protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{}))
	if !ok {
		t.Fatal("two damaged pickaxes did not repair")
	}
	// remaining 5 + 2 + 20*5/100 = 8 of 20.
	if StackMaxDamage(got) != 20 || got.Damage != 12 {
		t.Fatalf("repaired pickaxe: max_damage %d, damage %d; want 20, 12", StackMaxDamage(got), got.Damage)
	}
}
