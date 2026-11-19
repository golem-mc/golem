package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

func stackOf(t *testing.T, name string) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("no %s", name)
	}
	return protocol.ItemStack{ItemID: id, Count: 1}
}

// netheriteSet is a whole set worn, in player.EquipmentSlots order.
func netheriteSet(t *testing.T) player.Equipment {
	return player.Equipment{2: stackOf(t, "minecraft:netherite_boots"), 3: stackOf(t, "minecraft:netherite_leggings"),
		4: stackOf(t, "minecraft:netherite_chestplate"), 5: stackOf(t, "minecraft:netherite_helmet")}
}

// TestArmourFromTheAttributesMatchesThePieces checks the armour formula
// reads the same numbers off the attributes as off the pieces themselves,
// so a blow comes out as it did when the pieces were summed directly.
func TestArmourFromTheAttributesMatchesThePieces(t *testing.T) {
	for _, set := range []string{"leather", "iron", "diamond", "netherite"} {
		var now player.Equipment
		var points, toughness float64
		for i, piece := range []string{"boots", "leggings", "chestplate", "helmet"} {
			now[2+i] = stackOf(t, "minecraft:"+set+"_"+piece)
			p, tough := registry.ArmorOf(now[2+i].ItemID)
			points += float64(p)
			toughness += float64(tough)
		}
		var a player.Attributes
		UpdateEquipmentAttributes(&a, now)
		gotPoints, gotToughness := ArmourValue(&a)
		if gotPoints != points || gotToughness != toughness {
			t.Errorf("%s: attributes give %v/%v, the pieces %v/%v", set, gotPoints, gotToughness, points, toughness)
		}
		for _, damage := range []float64{1, 7, 20} {
			if got, want := DamageAfterArmour(damage, gotPoints, gotToughness), DamageAfterArmour(damage, points, toughness); got != want {
				t.Errorf("%s, %v damage: %v through the attributes, %v through the pieces", set, damage, got, want)
			}
		}
	}
}

func TestEquipmentModifiersComeAndGo(t *testing.T) {
	var a player.Attributes
	UpdateEquipmentAttributes(&a, netheriteSet(t))
	if got := KnockbackScale(&a); got != 1-4*float64(float32(0.1)) {
		t.Errorf("knockback left through netherite = %v", got)
	}

	// A sword in hand adds its speed; the set comes off, the sword stays.
	sword := stackOf(t, "minecraft:diamond_sword")
	UpdateEquipmentAttributes(&a, player.Equipment{0: sword})
	if p, tough := ArmourValue(&a); p != 0 || tough != 0 || KnockbackScale(&a) != 1 {
		t.Errorf("after taking the set off: armour %v/%v, knockback %v", p, tough, KnockbackScale(&a))
	}
	if got, want := a.Value(registry.AttributeAttackSpeed), 4+float64(float32(-2.4)); got != want {
		t.Errorf("attack speed = %v, want %v", got, want)
	}
	if got := a.Value(registry.AttributeAttackDamage); got != 7 {
		t.Errorf("attack damage = %v, want 7", got)
	}

	// In the off hand the sword is no weapon.
	UpdateEquipmentAttributes(&a, player.Equipment{1: sword})
	if got := a.Value(registry.AttributeAttackSpeed); got != 4 {
		t.Errorf("attack speed with the sword in the off hand = %v, want 4", got)
	}

	// A worn-out piece adds nothing.
	broken := stackOf(t, "minecraft:iron_chestplate")
	broken.Damage = registry.MaxDamage(broken.ItemID)
	UpdateEquipmentAttributes(&a, player.Equipment{4: broken})
	if p, _ := ArmourValue(&a); p != 0 {
		t.Errorf("a broken chestplate gives %v points", p)
	}
}

// TestEffectModifiersComeAndGo pins the modifiers the active effects put on
// the attributes: jump boost adds a block a level to safe_fall_distance, and
// the modifier comes off when the effect ends.
func TestEffectModifiersComeAndGo(t *testing.T) {
	jump := registry.MustMobEffect("minecraft:jump_boost")
	var a player.Attributes

	UpdateEffectAttributes(&a, []effect.Instance{effect.New(jump, 100, 0, false, true, true)})
	if got := a.Value(registry.AttributeSafeFallDistance); got != 4 {
		t.Errorf("jump boost I gives a safe fall distance of %v, want 4", got)
	}
	const id = "minecraft:effect.jump_boost"
	found := false
	if in := a.Instance(registry.AttributeSafeFallDistance); in != nil {
		for _, m := range in.Modifiers {
			if m.ID == id {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("jump boost did not put the %s modifier on the safe fall distance", id)
	}

	UpdateEffectAttributes(&a, []effect.Instance{effect.New(jump, 100, 2, false, true, true)})
	if got := a.Value(registry.AttributeSafeFallDistance); got != 6 {
		t.Errorf("jump boost III gives a safe fall distance of %v, want 6", got)
	}

	UpdateEffectAttributes(&a, nil)
	if got := a.Value(registry.AttributeSafeFallDistance); got != 3 {
		t.Errorf("safe fall distance after jump boost ends = %v, want the default 3", got)
	}
}

// TestSpeedAndSlownessScaleTheMovementSpeed checks the movement speed's
// effect modifiers: speed multiplies the total by 0.2 a level, slowness
// takes 0.15 a level off it, and both come off when the effect ends.
// vanilla: MobEffects.SPEED, MobEffects.SLOWNESS
func TestSpeedAndSlownessScaleTheMovementSpeed(t *testing.T) {
	speed := registry.MustMobEffect("minecraft:speed")
	slowness := registry.MustMobEffect("minecraft:slowness")
	var a player.Attributes
	base := float64(float32(0.1))

	UpdateEffectAttributes(&a, []effect.Instance{effect.New(speed, 100, 1, false, true, true)})
	if got, want := a.Value(registry.AttributeMovementSpeed), base*(1+float64(float32(0.2))*2); got != want {
		t.Errorf("movement speed on Speed II = %v, want %v", got, want)
	}
	UpdateEffectAttributes(&a, []effect.Instance{effect.New(slowness, 100, 0, false, true, true)})
	if got, want := a.Value(registry.AttributeMovementSpeed), base*(1-float64(float32(0.15))); got != want {
		t.Errorf("movement speed on Slowness I = %v, want %v", got, want)
	}
	UpdateEffectAttributes(&a, nil)
	if got := a.Value(registry.AttributeMovementSpeed); got != base {
		t.Errorf("movement speed after the effects end = %v, want the base %v", got, base)
	}
}
