package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestTakeDeathEquipmentKeepsAllayHandAndGolemCarry(t *testing.T) {
	a := NewMob(TypeAllay, 1, 0, 64, 0, 1)
	a.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: 3}
	got := a.TakeDeathEquipment()
	if len(got) != 1 || got[0].Count != 3 || !a.MainHand.Empty() {
		t.Errorf("allay took %+v, hand %+v, want 3 diamonds and an empty hand", got, a.MainHand)
	}
	g := NewMob(TypeCopperGolem, 2, 0, 64, 0, 2)
	g.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemCobblestone), Count: 5}
	g.DropChances[0] = 2
	if got := g.TakeDeathEquipment(); len(got) != 1 || got[0].Count != 5 {
		t.Errorf("golem took %+v, want 5 cobblestone", got)
	}
}
