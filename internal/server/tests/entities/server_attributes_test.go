package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// What equipment does to a player's attributes, and what the clients are
// told of it: the armour bar and the attack-cooldown indicator are drawn
// from the attributes the server sends with Update Attributes.

func hasModifier(e servertest.AttrEntry, id string) bool {
	for _, m := range e.Modifiers {
		if m.Id == id {
			return true
		}
	}
	return false
}

// TestWearingAChestplateSendsTheArmour checks the armour bar's source: an
// iron chestplate put on reaches the wearer, and everyone who can see them,
// as the armor attribute's chestplate modifier of six; taken off, the
// attribute comes back with no modifier at all.
func TestWearingAChestplateSendsTheArmour(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)

	watcher := connect()
	watcher.JoinAs("Watcher")

	wearer := connect()
	wearerID := wearer.JoinAs("Wearer")
	watcher.ExpectPlayerAdded("Wearer")

	chestplate := servertest.MustItemID(t, "minecraft:iron_chestplate")
	srv.GiveSlot("Wearer", player.ArmorSlot+1, protocol.ItemStack{ItemID: chestplate, Count: 1})

	on := func(e servertest.AttrEntry) bool { return hasModifier(e, "minecraft:armor.chestplate") }
	for _, c := range []*servertest.Client{wearer, watcher} {
		e := c.AwaitAttributes(wearerID, servertest.AttrArmor, on)[servertest.AttrArmor]
		want := []servertest.AttrModifier{servertest.AttrMod("minecraft:armor.chestplate", 6, 0)}
		if e.Base != 0 || len(e.Modifiers) != 1 || e.Modifiers[0] != want[0] {
			t.Errorf("armour with the chestplate on = %+v, want base 0 and %+v", e, want)
		}
	}

	srv.GiveSlot("Wearer", player.ArmorSlot+1, protocol.ItemStack{})
	off := wearer.AwaitAttributes(wearerID, servertest.AttrArmor, func(e servertest.AttrEntry) bool { return !on(e) })
	if e := off[servertest.AttrArmor]; e.Value() != 0 || len(e.Modifiers) != 0 {
		t.Errorf("armour with the chestplate off = %+v, want zero and no modifier", e)
	}
}

// TestHoldingASwordSetsTheAttackSpeed checks what the attack-cooldown
// indicator is drawn from: a diamond sword in hand puts its attack speed
// modifier on the attribute the client is sent, its attack damage staying
// on the server, and putting it away takes the modifier back off.
func TestHoldingASwordSetsTheAttackSpeed(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c, id := servertest.JoinAs(t, cfg, "Swordsman")

	sword := servertest.MustItemID(t, "minecraft:diamond_sword")
	srv.GiveSlot("Swordsman", player.HotbarSlot, protocol.ItemStack{ItemID: sword, Count: 1})

	held := func(e servertest.AttrEntry) bool { return hasModifier(e, "minecraft:base_attack_speed") }
	entries := c.AwaitAttributes(id, servertest.AttrAttackSpeed, held)
	e := entries[servertest.AttrAttackSpeed]
	want := servertest.AttrMod("minecraft:base_attack_speed", float64(registry.AttackSpeed(sword)), 0)
	if e.Base != 4 || len(e.Modifiers) != 1 || e.Modifiers[0] != want {
		t.Errorf("attack speed with the sword = %+v, want base 4 and %+v", e, want)
	}
	if _, ok := entries[servertest.AttrAttackDamage]; ok {
		t.Error("attack damage was sent; vanilla does not sync it")
	}

	srv.GiveSlot("Swordsman", player.HotbarSlot, protocol.ItemStack{})
	if e := c.AwaitAttributes(id, servertest.AttrAttackSpeed, func(e servertest.AttrEntry) bool { return !held(e) })[servertest.AttrAttackSpeed]; e.Value() != 4 {
		t.Errorf("attack speed with the sword put away = %+v, want 4", e)
	}
}

// TestEnchantedGearPutsItsAttributeModifiersOn checks the attributes the
// client reads its movement and dig prediction from: each enchantment's
// data-file modifier follows the piece carrying it under the file's own
// id over the slot, reaches the wearer and everyone watching them, and
// comes off with the piece.
// vanilla: LivingEntity.collectEquipmentChanges, EnchantmentHelper.forEachModifier
func TestEnchantedGearPutsItsAttributeModifiersOn(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)

	watcher := connect()
	watcher.JoinAs("Watcher")

	wearer := connect()
	wearerID := wearer.JoinAs("Wearer")
	watcher.ExpectPlayerAdded("Wearer")
	clients := []*servertest.Client{wearer, watcher}

	// Depth Strider III: three thirds of water movement efficiency.
	srv.GiveSlot("Wearer", player.ArmorSlot+3, servertest.EnchantedStack(t, "minecraft:iron_boots", "minecraft:depth_strider", 3))
	striding := func(e servertest.AttrEntry) bool { return hasModifier(e, "minecraft:enchantment.depth_strider/feet") }
	for _, c := range clients {
		e := c.AwaitAttributes(wearerID, servertest.AttrWaterMovementEfficiency, striding)[servertest.AttrWaterMovementEfficiency]
		want := servertest.AttrMod("minecraft:enchantment.depth_strider/feet", 1, 0)
		if e.Base != 0 || len(e.Modifiers) != 1 || e.Modifiers[0] != want || e.Value() != 1 {
			t.Errorf("water movement efficiency with the depth strider boots = %+v, want base 0 and %+v", e, want)
		}
	}

	// Swift Sneak III: three fifteens over the three tenths the attribute
	// starts at, in the float the vanilla calculation runs in - which
	// rounds above the float32 nearest to 0.45.
	srv.GiveSlot("Wearer", player.ArmorSlot+2, servertest.EnchantedStack(t, "minecraft:iron_leggings", "minecraft:swift_sneak", 3))
	sneaking := func(e servertest.AttrEntry) bool { return hasModifier(e, "minecraft:enchantment.swift_sneak/legs") }
	for _, c := range clients {
		e := c.AwaitAttributes(wearerID, servertest.AttrSneakingSpeed, sneaking)[servertest.AttrSneakingSpeed]
		want := servertest.AttrMod("minecraft:enchantment.swift_sneak/legs", float64(float32(0.15)*2+float32(0.15)), 0)
		if e.Base != 0.3 || len(e.Modifiers) != 1 || e.Modifiers[0] != want {
			t.Errorf("sneaking speed with the swift sneak leggings = %+v, want base 0.3 and %+v", e, want)
		}
	}

	// Efficiency V: the level squared plus one of mining efficiency.
	srv.GiveSlot("Wearer", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:efficiency", 5))
	digging := func(e servertest.AttrEntry) bool { return hasModifier(e, "minecraft:enchantment.efficiency/mainhand") }
	for _, c := range clients {
		e := c.AwaitAttributes(wearerID, servertest.AttrMiningEfficiency, digging)[servertest.AttrMiningEfficiency]
		want := servertest.AttrMod("minecraft:enchantment.efficiency/mainhand", 26, 0)
		if e.Base != 0 || len(e.Modifiers) != 1 || e.Modifiers[0] != want || e.Value() != 26 {
			t.Errorf("mining efficiency with the pickaxe = %+v, want base 0 and %+v", e, want)
		}
	}

	srv.GiveSlot("Wearer", player.HotbarSlot, protocol.ItemStack{})
	for _, c := range clients {
		e := c.AwaitAttributes(wearerID, servertest.AttrMiningEfficiency, func(e servertest.AttrEntry) bool { return !digging(e) })[servertest.AttrMiningEfficiency]
		if e.Value() != 0 || len(e.Modifiers) != 0 {
			t.Errorf("mining efficiency with the pickaxe away = %+v, want no modifiers", e)
		}
	}
	srv.GiveSlot("Wearer", player.ArmorSlot+2, protocol.ItemStack{})
	for _, c := range clients {
		e := c.AwaitAttributes(wearerID, servertest.AttrSneakingSpeed, func(e servertest.AttrEntry) bool { return !sneaking(e) })[servertest.AttrSneakingSpeed]
		if e.Value() != 0.3 || len(e.Modifiers) != 0 {
			t.Errorf("sneaking speed with the leggings away = %+v, want the bare base", e)
		}
	}
	srv.GiveSlot("Wearer", player.ArmorSlot+3, protocol.ItemStack{})
	for _, c := range clients {
		e := c.AwaitAttributes(wearerID, servertest.AttrWaterMovementEfficiency, func(e servertest.AttrEntry) bool { return !striding(e) })[servertest.AttrWaterMovementEfficiency]
		if e.Value() != 0 || len(e.Modifiers) != 0 {
			t.Errorf("water movement efficiency with the boots away = %+v, want no modifiers", e)
		}
	}
}

// TestEffectModifiersReachTheAttributes checks the effect attribute table
// past the movement speed: Luck a point and Unluck one off the luck
// attribute, absorption its four a level on max_absorption, and haste and
// mining fatigue their tenth on attack_speed, all as attributes the client
// reads off Update Attributes.
// vanilla: MobEffects.LUCK, UNLUCK, ABSORPTION, HASTE, MINING_FATIGUE
func TestEffectModifiersReachTheAttributes(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Lucky")
	c.Settle()
	srv.StopTick()
	c.Settle()

	const (
		luck          = "minecraft:effect.luck"
		unluck        = "minecraft:effect.unluck"
		absorption    = "minecraft:effect.absorption"
		haste         = "minecraft:effect.haste"
		miningFatigue = "minecraft:effect.mining_fatigue"
	)
	for _, e := range []struct {
		name string
		amp  int32
	}{
		{"minecraft:luck", 0},
		{"minecraft:unluck", 0},
		{"minecraft:absorption", 1},
		{"minecraft:haste", 1},
		{"minecraft:mining_fatigue", 0},
	} {
		srv.GiveEffect("Lucky", e.name, 6000, e.amp, true)
		c.AwaitMobEffect()
	}
	srv.TickSurvival()

	entries := c.AwaitAttributes(id, servertest.AttrLuck, func(e servertest.AttrEntry) bool { return hasModifier(e, luck) && hasModifier(e, unluck) })
	e := entries[servertest.AttrLuck]
	l, hasLuck := servertest.Modifier(e, luck)
	u, hasUnluck := servertest.Modifier(e, unluck)
	if e.Base != 0 || !hasLuck || l != (servertest.AttrMod(luck, 1, 0)) || !hasUnluck || u != (servertest.AttrMod(unluck, -1, 0)) {
		t.Errorf("luck with Luck and Unluck = %+v, want +1 and -1 add_value on base 0", e)
	}
	if m, ok := servertest.Modifier(entries[servertest.AttrMaxAbsorption], absorption); !ok || m != (servertest.AttrMod(absorption, 8, 0)) {
		t.Errorf("absorption's max_absorption modifier = %+v, %v, want +8 add_value", m, ok)
	}
	if m, ok := servertest.Modifier(entries[servertest.AttrAttackSpeed], haste); !ok || m != (servertest.AttrMod(haste, float64(float32(0.1))*2, 2)) {
		t.Errorf("haste's attack speed modifier = %+v, %v, want +0.2 multiply_total", m, ok)
	}
	if m, ok := servertest.Modifier(entries[servertest.AttrAttackSpeed], miningFatigue); !ok || m != (servertest.AttrMod(miningFatigue, float64(float32(-0.1)), 2)) {
		t.Errorf("mining fatigue's attack speed modifier = %+v, %v, want -0.1 multiply_total", m, ok)
	}
}

// TestHealthBoostRaisesTheMaxHealthTheClientDraws checks the attribute the
// hearts bar is drawn from: Health Boost I puts its four a level on
// max_health as an add_value modifier beside the twenty base, and the
// modifier comes off again when the effect runs out.
// vanilla: MobEffects.HEALTH_BOOST, LivingEntity.onEffectsRemoved
func TestHealthBoostRaisesTheMaxHealthTheClientDraws(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Boosted")
	c.Settle()
	srv.StopTick()
	c.Settle()

	const boost = "minecraft:effect.health_boost"
	srv.GiveEffect("Boosted", "minecraft:health_boost", 20, 0, true)
	srv.TickSurvival()
	raised := c.AwaitAttributes(id, protocol.AttributeMaxHealth, func(e servertest.AttrEntry) bool { return hasModifier(e, boost) })
	e := raised[protocol.AttributeMaxHealth]
	want := servertest.AttrMod(boost, 4, 0)
	if e.Base != 20 || len(e.Modifiers) != 1 || e.Modifiers[0] != want {
		t.Errorf("max health under Health Boost I = %+v, want the twenty base and %+v", e, want)
	}

	// Twenty ticks after the effect was given, the one whose countdown
	// reaches zero removes it and the modifier with it.
	for range 19 {
		srv.TickSurvival()
	}
	ended := c.AwaitAttributes(id, protocol.AttributeMaxHealth, func(e servertest.AttrEntry) bool { return !hasModifier(e, boost) })
	if e := ended[protocol.AttributeMaxHealth]; e.Base != 20 || len(e.Modifiers) != 0 || e.Value() != 20 {
		t.Errorf("max health after the boost ran out = %+v, want the bare twenty", e)
	}
}
