package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestBlockReachMeasuresFromThePoseEye(t *testing.T) {
	// The block at y 59 has its top face at 60: 4.4 below a swimming eye at
	// 64.4, 5.62 below a standing one, past the 4.5 range plus the 1.0 buffer.
	if !WithinBlockInteractionRange(4.5, 0, 64+PlayerEyeHeight(protocol.PoseSwimming), 0, 0, 59, 0) {
		t.Error("a swimming eye 4.4 above the block is refused")
	}
	if WithinBlockInteractionRange(4.5, 0, 64+1.62, 0, 0, 59, 0) {
		t.Error("a standing eye 5.62 above the block is accepted")
	}
}

func TestAttackRangeWithUsesTheStackOverride(t *testing.T) {
	held := protocol.ItemStack{ItemID: int32(registry.ItemWoodenSpear), Count: 1}
	want := registry.AttackRange{MinReach: 1, MaxReach: 9, MinCreativeReach: 2, MaxCreativeReach: 10, HitboxMargin: 0.7, MobFactor: 1.5}
	held.SetAttackRange(protocol.AttackRangeComponent(want))
	if got := AttackRangeWith(held, 20); got != want {
		t.Errorf("patched spear range = %+v, want %+v", got, want)
	}
	held.Remove(protocol.DataComponentAttackRange)
	if got := AttackRangeWith(held, 20); got != (registry.AttackRange{MaxReach: 20, MaxCreativeReach: 20, MobFactor: 1}) {
		t.Errorf("removed override = %+v, want the attribute's 20", got)
	}
	held.SetAttackRange(protocol.AttackRangeComponent(want))
	held.Count = 0
	if _, ok := StackAttackRange(held); ok {
		t.Error("an empty stack kept its override")
	}
}
