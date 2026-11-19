package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// totemOfUndying is the one item a plain stack of holds a death_protection.
func totemOfUndying() protocol.ItemStack {
	return protocol.ItemStack{ItemID: registry.MustItem("minecraft:totem_of_undying"), Count: 1}
}

// TestTotemHandPicksTheSavingHand pins the hand vanilla's totem check reads
// first — the main hand's item before the offhand's — and the blows no item
// saves: the bypasses_invulnerability types and hurt from empty hands or
// items that carry no death_protection.
// vanilla: LivingEntity.checkTotemDeathProtection
func TestTotemHandPicksTheSavingHand(t *testing.T) {
	totem := totemOfUndying()
	sword := protocol.ItemStack{ItemID: registry.MustItem("minecraft:wooden_sword"), Count: 1}
	damageType := DamageTypeMobAttack
	cases := []struct {
		what        string
		damageType  registry.DamageType
		mainhand    protocol.ItemStack
		offhand     protocol.ItemStack
		wantHand    int32
		wantNoDeath bool
	}{
		{"a totem in the main hand", damageType, totem, protocol.ItemStack{}, protocol.HandMain, false},
		{"a totem in the offhand", damageType, protocol.ItemStack{}, totem, protocol.HandOff, false},
		{"a totem in each hand", damageType, totem, totem, protocol.HandMain, false},
		{"an empty hand", damageType, protocol.ItemStack{}, protocol.ItemStack{}, -1, true},
		{"a hand holding no totem", damageType, sword, protocol.ItemStack{}, -1, true},
		{"a totem against /kill", DamageTypeGenericKill, totem, totem, -1, true},
		{"a totem against the void", DamageTypeOutOfWorld, totem, totem, -1, true},
	}
	for _, c := range cases {
		hand, death := TotemHand(c.damageType, c.mainhand, c.offhand)
		if hand != c.wantHand {
			t.Errorf("TotemHand (%s) named hand %d, want %d", c.what, hand, c.wantHand)
		}
		if c.wantNoDeath != (death == nil) {
			t.Errorf("TotemHand (%s) death effects = %v, want the other", c.what, death)
		}
	}
}
