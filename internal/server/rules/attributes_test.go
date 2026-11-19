package rules

import (
	"slices"
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

// TestGearCarriesItsEnchantmentsModifiers checks the enchantments' own side
// of the equipment-slot walk: each attribute effect the data files
// carry follows its piece under the file's id over the slot, at the amount
// vanilla's float calculate returns and only in the slots the enchantment
// works in. The amounts are the reference server's own update attributes.
// vanilla: EnchantmentHelper.forEachModifier
func TestGearCarriesItsEnchantmentsModifiers(t *testing.T) {
	cases := []struct {
		slot  string
		stack protocol.ItemStack
		want  registry.AttributeModifier
	}{
		{"feet", enchantedStack(t, "minecraft:iron_boots", "minecraft:depth_strider", 3),
			registry.AttributeModifier{Attribute: registry.AttributeWaterMovementEfficiency, ID: "minecraft:enchantment.depth_strider/feet", Amount: 1, Slot: "feet"}},
		{"feet", enchantedStack(t, "minecraft:iron_boots", "minecraft:blast_protection", 4),
			registry.AttributeModifier{Attribute: registry.AttributeExplosionKnockbackResistance, ID: "minecraft:enchantment.blast_protection/feet", Amount: float64(float32(0.6)), Slot: "feet"}},
		{"legs", enchantedStack(t, "minecraft:iron_leggings", "minecraft:swift_sneak", 3),
			registry.AttributeModifier{Attribute: registry.AttributeSneakingSpeed, ID: "minecraft:enchantment.swift_sneak/legs", Amount: float64(float32(0.15)*2 + float32(0.15)), Slot: "legs"}},
		{"chest", enchantedStack(t, "minecraft:iron_chestplate", "minecraft:fire_protection", 1),
			registry.AttributeModifier{Attribute: registry.AttributeBurningTime, ID: "minecraft:enchantment.fire_protection/chest", Amount: float64(float32(-0.15)), Operation: registry.AttributeAddMultipliedBase, Slot: "chest"}},
		{"head", enchantedStack(t, "minecraft:diamond_helmet", "minecraft:respiration", 3),
			registry.AttributeModifier{Attribute: registry.AttributeOxygenBonus, ID: "minecraft:enchantment.respiration/head", Amount: 3, Slot: "head"}},
		{"head", enchantedStack(t, "minecraft:diamond_helmet", "minecraft:aqua_affinity", 1),
			registry.AttributeModifier{Attribute: registry.AttributeSubmergedMiningSpeed, ID: "minecraft:enchantment.aqua_affinity/head", Amount: 4, Operation: registry.AttributeAddMultipliedTotal, Slot: "head"}},
		{"mainhand", enchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:efficiency", 5),
			registry.AttributeModifier{Attribute: registry.AttributeMiningEfficiency, ID: "minecraft:enchantment.efficiency/mainhand", Amount: 26, Slot: "mainhand"}},
		{"mainhand", enchantedStack(t, "minecraft:diamond_sword", "minecraft:sweeping_edge", 3),
			registry.AttributeModifier{Attribute: registry.AttributeSweepingDamageRatio, ID: "minecraft:enchantment.sweeping_edge/mainhand", Amount: float64(float32(0.75)), Slot: "mainhand"}},
	}
	for _, tc := range cases {
		if got := EquipmentModifiers(tc.stack, tc.slot); !slices.Contains(got, tc.want) {
			t.Errorf("modifiers at %s = %+v, want %+v among them", tc.slot, got, tc.want)
		}
	}

	// One level of depth strider: a single third, not the whole.
	one := enchantedStack(t, "minecraft:iron_boots", "minecraft:depth_strider", 1)
	want := registry.AttributeModifier{Attribute: registry.AttributeWaterMovementEfficiency, ID: "minecraft:enchantment.depth_strider/feet", Amount: float64(float32(0.33333334)), Slot: "feet"}
	if got := EquipmentModifiers(one, "feet"); !slices.Contains(got, want) {
		t.Errorf("depth strider I = %+v, want %+v among its modifiers", got, want)
	}

	// Only in the slots the enchantment works in: the strider boots put
	// nothing anywhere else, neither their armour nor the enchantment.
	for _, slot := range []string{"mainhand", "offhand", "legs", "chest", "head"} {
		if got := EquipmentModifiers(cases[0].stack, slot); len(got) != 0 {
			t.Errorf("depth strider boots at %s = %+v, want nothing", slot, got)
		}
	}

	// A worn-out piece puts none on, enchanted or not.
	broken := enchantedStack(t, "minecraft:iron_boots", "minecraft:depth_strider", 3)
	broken.Damage = registry.MaxDamage(broken.ItemID)
	if got := EquipmentModifiers(broken, "feet"); len(got) != 0 {
		t.Errorf("a broken piece's modifiers = %+v, want none", got)
	}
}

// TestStateModifiers pins the modifiers a player's state puts on: the creative
// reach, the sprint and the crouch that takes a player off the locator bar.
func TestStateModifiers(t *testing.T) {
	var a player.Attributes
	UpdateStateAttributes(&a, true, true, true)
	if got := a.Value(registry.AttributeBlockInteractionRange); got != 5 {
		t.Errorf("creative block reach = %v, want 5", got)
	}
	if got := a.Value(registry.AttributeEntityInteractionRange); got != 5 {
		t.Errorf("creative entity reach = %v, want 5", got)
	}
	if got, want := a.Value(registry.AttributeMovementSpeed), float64(float32(0.1))*(1+float64(float32(0.3))); got != want {
		t.Errorf("sprinting speed = %v, want %v", got, want)
	}
	UpdateStateAttributes(&a, false, false, false)
	if a.Value(registry.AttributeBlockInteractionRange) != 4.5 || a.Value(registry.AttributeMovementSpeed) != float64(float32(0.1)) {
		t.Error("the state modifiers did not come off")
	}
}

// TestCrouchingHidesTheWaypointTransmitRange checks the crouch's own modifier:
// the whole of the transmit range down while the pose holds, which is how the
// waypoint pass stops transmitting a crouching player.
// vanilla: ServerPlayer.WAYPOINT_TRANSMIT_RANGE_CROUCH_MODIFIER
func TestCrouchingHidesTheWaypointTransmitRange(t *testing.T) {
	var a player.Attributes
	UpdateStateAttributes(&a, false, false, true)
	if got := a.Value(registry.AttributeWaypointTransmitRange); got != 0 {
		t.Errorf("waypoint transmit range while crouching = %v, want 0", got)
	}
	UpdateStateAttributes(&a, false, false, false)
	if got := a.Value(registry.AttributeWaypointTransmitRange); got != 6e7 {
		t.Errorf("waypoint transmit range after standing = %v, want the base", got)
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

// TestOtherEffectAttributeModifiers checks the effects past the movement
// speed: haste and mining fatigue scale attack_speed, invisibility takes
// waypoint_transmit_range down whole, absorption adds to max_absorption, and
// each comes off when its effect ends.
// vanilla: MobEffects.HASTE, MINING_FATIGUE, INVISIBILITY, ABSORPTION
func TestOtherEffectAttributeModifiers(t *testing.T) {
	haste := registry.MustMobEffect("minecraft:haste")
	miningFatigue := registry.MustMobEffect("minecraft:mining_fatigue")
	invisibility := registry.MustMobEffect("minecraft:invisibility")
	absorption := registry.MustMobEffect("minecraft:absorption")
	var a player.Attributes

	UpdateEffectAttributes(&a, []effect.Instance{effect.New(haste, 100, 1, false, true, true)})
	if got, want := a.Value(registry.AttributeAttackSpeed), 4*(1+float64(float32(0.1))*2); got != want {
		t.Errorf("attack speed on Haste II = %v, want %v", got, want)
	}
	UpdateEffectAttributes(&a, []effect.Instance{effect.New(miningFatigue, 100, 0, false, true, true)})
	if got, want := a.Value(registry.AttributeAttackSpeed), 4*(1-float64(float32(0.1))); got != want {
		t.Errorf("attack speed on Mining Fatigue I = %v, want %v", got, want)
	}
	UpdateEffectAttributes(&a, []effect.Instance{effect.New(invisibility, 100, 0, false, true, true)})
	if got := a.Value(registry.AttributeWaypointTransmitRange); got != 0 {
		t.Errorf("waypoint transmit range on Invisibility = %v, want 0", got)
	}
	// Vanilla names this row after what it hides, not after the effect.
	const invisID = "minecraft:effect.waypoint_transmit_range_hide"
	found := false
	if in := a.Instance(registry.AttributeWaypointTransmitRange); in != nil {
		for _, m := range in.Modifiers {
			if m.ID == invisID {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("invisibility did not put the %s modifier on the waypoint transmit range", invisID)
	}
	UpdateEffectAttributes(&a, []effect.Instance{effect.New(absorption, 100, 1, false, true, true)})
	if got := a.Value(registry.AttributeMaxAbsorption); got != 8 {
		t.Errorf("max absorption on Absorption II = %v, want 8", got)
	}
	UpdateEffectAttributes(&a, nil)
	if a.Value(registry.AttributeAttackSpeed) != 4 || a.Value(registry.AttributeMaxAbsorption) != 0 || a.Value(registry.AttributeWaypointTransmitRange) != 6e7 {
		t.Errorf("after the effects end: attack speed %v, absorption %v, waypoint range %v",
			a.Value(registry.AttributeAttackSpeed), a.Value(registry.AttributeMaxAbsorption), a.Value(registry.AttributeWaypointTransmitRange))
	}
}
