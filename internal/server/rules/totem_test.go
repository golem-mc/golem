package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
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

// TestTotemSaveStandsTheBodyUpAtOneHalfHeart pins what the saving item does
// to the body: the health back at one and the dead flag down, with the three
// effects its death_protection names applied in the order the component
// lists them. The changes the save returns are the told ones.
// vanilla: LivingEntity.checkTotemDeathProtection, DeathProtection.applyEffects
func TestTotemSaveStandsTheBodyUpAtOneHalfHeart(t *testing.T) {
	_, death := TotemHand(DamageTypeMobAttack, totemOfUndying(), protocol.ItemStack{})
	v := FreshVitals(protocol.GameModeSurvival)
	v.Health = 0
	v.Dead = true
	var effects effect.Set
	changes := TotemSave(&v, &effects, nil, death, rand.New(rand.NewSource(1)))
	if v.Health != 1 || v.Dead {
		t.Errorf("after the save: health=%v dead=%v, want 1 and alive", v.Health, v.Dead)
	}
	wantEffects := []int32{
		registry.MustMobEffect("minecraft:regeneration"),
		registry.MustMobEffect("minecraft:absorption"),
		registry.MustMobEffect("minecraft:fire_resistance"),
	}
	if len(changes) != len(wantEffects) {
		t.Fatalf("the save told %d changes, want %d", len(changes), len(wantEffects))
	}
	for i, want := range wantEffects {
		if changes[i].Kind != effect.Added || changes[i].Instance.ID != want {
			t.Errorf("change %d = %d of effect %d, want the added %d",
				i, changes[i].Kind, changes[i].Instance.ID, want)
		}
	}
}
