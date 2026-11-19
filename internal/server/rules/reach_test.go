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

func TestBlockReachFollowsTheRange(t *testing.T) {
	// The block's box spans 0..1 on x; eyes 8 past its face.
	if WithinBlockInteractionRange(4.5, 9, 0.5, 0.5, 0, 0, 0) {
		t.Error("the default range reaches 8 blocks")
	}
	if !WithinBlockInteractionRange(10, 9, 0.5, 0.5, 0, 0, 0) {
		t.Error("a range of 10 does not reach 8 blocks")
	}
}

func TestAttackRangeWithFallsBackForARemovedComponent(t *testing.T) {
	spear := protocol.ItemStack{ItemID: int32(registry.ItemWoodenSpear), Count: 1}
	if r := AttackRangeWith(spear, 3); r.MaxReach != 4.5 || r.MobFactor != 0.5 {
		t.Errorf("wooden spear range = %+v, want its component's 4.5 at mob factor 0.5", r)
	}
	spear.Remove(protocol.DataComponentAttackRange)
	want := registry.AttackRange{MaxReach: 3, MaxCreativeReach: 3, MobFactor: 1}
	if r := AttackRangeWith(spear, 3); r != want {
		t.Errorf("wooden spear without attack_range = %+v, want %+v", r, want)
	}
	if _, ok := StackAttackRange(spear); ok {
		t.Error("a stack that removes attack_range still has one")
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

func TestAttackReachFollowsTheHeldAttackRange(t *testing.T) {
	spear := protocol.ItemStack{ItemID: int32(registry.ItemIronSpear), Count: 1}
	for _, tc := range []struct {
		name     string
		held     protocol.ItemStack
		creative bool
		rng      float64
		reach    float64 // the farthest a box's near face is reached, inclusive
	}{
		{"empty hand", protocol.ItemStack{}, false, 3, 6},
		{"empty creative hand", protocol.ItemStack{}, true, 5, 8},
		{"empty hand, raised range", protocol.ItemStack{}, false, 10, 13},
		// 4.5 plus the 0.125 margin plus the buffer; the attribute is not read.
		{"spear", spear, false, 10, 7.625},
		{"creative spear", spear, true, 3, 9.625},
	} {
		// A unit box whose near face is d along x from eyes at the origin.
		in := func(d float64) bool {
			return WithinAttackRange(tc.held, tc.creative, tc.rng, 0, 0.5, 0.5, d, 0, 0, d+1, 1, 1)
		}
		if !in(tc.reach) || in(tc.reach+0.001) {
			t.Errorf("%s: reached %v: %v, %v: %v; want only the first", tc.name, tc.reach, in(tc.reach), tc.reach+0.001, in(tc.reach+0.001))
		}
		// The spear's minimum reach of 2, less margin and buffer, is below zero.
		if !in(0) {
			t.Errorf("%s: a box at the eyes is out of reach", tc.name)
		}
	}
}
