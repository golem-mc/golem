package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func protectionBoots(t *testing.T, enchantment string, level int32) protocol.ItemStack {
	t.Helper()
	item, ok := registry.ItemID("minecraft:leather_boots")
	if !ok {
		t.Fatal("no leather boots")
	}
	ench, ok := registry.EnchantmentID(enchantment)
	if !ok {
		t.Fatalf("no enchantment %s", enchantment)
	}
	return protocol.ItemStack{ItemID: item, Count: 1, Enchantments: map[int32]int32{ench: level}}
}

// healthLost is what one plainHurt of the damage type takes off a zombie
// wearing the given boots.
func healthLost(t *testing.T, boots protocol.ItemStack, damageType registry.DamageType) float64 {
	t.Helper()
	zombie := aimless(NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1))
	zombie.NoAI = true
	zombie.Worn[wornFeet] = boots
	before := zombie.Health
	zombie.plainHurt(10, damageType, false)
	return before - zombie.Health
}

// TestWornProtectionCutsAMobsDamage checks that enchantment protection on a
// mob's armour scales a blow after armour, and that Fire, Blast and
// Projectile Protection answer only their own damage types.
// vanilla: LivingEntity.getDamageAfterMagicAbsorb
func TestWornProtectionCutsAMobsDamage(t *testing.T) {
	plain := protectionBoots(t, "minecraft:unbreaking", 1)
	cases := []struct {
		name, ench string
		damageType registry.DamageType
		protected  bool
	}{
		{"protection vs mob attack", "minecraft:protection", rules.DamageTypeMobAttack, true},
		{"fire protection vs fire", "minecraft:fire_protection", rules.DamageTypeInFire, true},
		{"fire protection vs mob attack", "minecraft:fire_protection", rules.DamageTypeMobAttack, false},
		{"blast protection vs explosion", "minecraft:blast_protection", rules.DamageTypeExplosion, true},
		{"blast protection vs mob attack", "minecraft:blast_protection", rules.DamageTypeMobAttack, false},
		{"projectile protection vs arrow", "minecraft:projectile_protection", rules.DamageTypeArrow, true},
		{"projectile protection vs fire", "minecraft:projectile_protection", rules.DamageTypeInFire, false},
	}
	for _, tc := range cases {
		base := healthLost(t, plain, tc.damageType)
		got := healthLost(t, protectionBoots(t, tc.ench, 4), tc.damageType)
		if tc.protected && !(got < base-0.01) {
			t.Errorf("%s: lost %v, want less than the unprotected %v", tc.name, got, base)
		}
		if !tc.protected && math.Abs(got-base) > 1e-9 {
			t.Errorf("%s: lost %v, want the unprotected %v", tc.name, got, base)
		}
	}
	// Protection IV is four points, four twenty-fifths off.
	base := healthLost(t, plain, rules.DamageTypeMobAttack)
	got := healthLost(t, protectionBoots(t, "minecraft:protection", 4), rules.DamageTypeMobAttack)
	if want := base * (1 - 4.0/25); math.Abs(got-want) > 1e-6 {
		t.Errorf("protection IV took %v, want %v", got, want)
	}
}
