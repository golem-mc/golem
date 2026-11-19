package entities

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

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

// TestNetheriteBringsToughnessAndKnockbackResistance checks the other two
// attributes netherite carries: the toughness goes to the client with the
// points, the knockback resistance - not a synced attribute - stays on the
// server, where a whole set of it takes four tenths off a shove. The blow
// itself is what the armour formula makes of the points and toughness the
// four pieces add up to.
func TestNetheriteBringsToughnessAndKnockbackResistance(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)

	victim := connect()
	victimID := victim.JoinAs("Victim")

	attacker := connect()
	attacker.JoinAs("Attacker")
	victim.ExpectPlayerAdded("Attacker")

	attacker.MoveToGround(0.5, 64, 0.5, true)
	victim.MoveToGround(3.5, 64, 0.5, true)
	attacker.Settle()
	victim.Settle()
	srv.SetFood("Victim", 17)

	var points, toughness float64
	pieces := []string{"helmet", "chestplate", "leggings", "boots"}
	for i, piece := range pieces {
		id := servertest.MustItemID(t, "minecraft:netherite_"+piece)
		p, tough := registry.ArmorOf(id)
		points += float64(p)
		toughness += float64(tough)
		srv.GiveSlot("Victim", player.ArmorSlot+i, protocol.ItemStack{ItemID: id, Count: 1})
	}

	entries := victim.AwaitAttributes(victimID, servertest.AttrArmor, func(e servertest.AttrEntry) bool { return e.Value() == points })
	if got := entries[servertest.AttrArmorToughness].Value(); got != toughness {
		t.Errorf("toughness = %v, want %v", got, toughness)
	}
	for _, id := range []int32{servertest.AttrKnockbackResistance, servertest.AttrAttackDamage} {
		if _, ok := entries[id]; ok {
			t.Errorf("attribute %d was sent; vanilla does not sync it", id)
		}
	}

	sword := servertest.MustItemID(t, "minecraft:iron_sword")
	srv.GiveSlot("Attacker", player.HotbarSlot, protocol.ItemStack{ItemID: sword, Count: 1})
	servertest.AwaitFullSwing(t, srv, "Attacker")
	victim.DrainSurvivalTraffic()

	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victimID) })

	damage := servertest.FistDamageTestBase + float64(registry.AttackDamage(sword))
	reduction := math.Min(20, math.Max(points/5, points-4*damage/(toughness+8))) / 25
	got := victim.ExpectHealthish()
	if want := float32(servertest.MaxHealthTestBase - damage*(1-reduction)); math.Abs(float64(got.Health-want)) > 0.01 {
		t.Errorf("health behind netherite = %v, want %v", got.Health, want)
	}

	var motion []byte
	for id, data := range victim.Packets(servertest.ExpectTimeout) {
		if id == protocol.IDPlaySetEntityMotion {
			motion = data
			break
		}
	}
	r := protocol.NewReader(motion)
	r.ReadVarInt()
	vx, vy, _ := r.ReadLpVec3()
	// Four pieces of a tenth each, as floats widened.
	scale := 1 - 4*float64(float32(0.1))
	if math.Abs(vx-0.4*scale) > 0.01 || math.Abs(vy-(0.4*scale-0.0784/2)) > 0.01 {
		t.Errorf("knockback = (%v, %v), want (%v, %v): the bare shove less the resistance", vx, vy, 0.4*scale, 0.4*scale-0.0784/2)
	}
}

// TestKnockbackEnchantmentFacesNetheriteResistance checks where the knockback
// resistance takes its share of an enchanted blow: vanilla applies it inside
// each knockback call, so the enchantment's shove folds to a horizontal of
// (0.2+0.5) of what the six tenths of a full netherite set leave, and the
// lift stays at the four tenths the vanilla knockback caps - where a
// resistance applied to the folded result would scale the lift down too.
func TestKnockbackEnchantmentFacesNetheriteResistance(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)

	victim := connect()
	victimID := victim.JoinAs("Victim")

	attacker := connect()
	attacker.JoinAs("Attacker")
	victim.ExpectPlayerAdded("Attacker")

	// Facing east, the way the victim is, so the attack's extra rides the
	// same line and its amount is what the test reads.
	attacker.MoveToGrounded(0.5, 64, 0.5, -90, 0, true)
	victim.MoveToGround(3.5, 64, 0.5, true)
	attacker.Settle()
	victim.Settle()
	srv.SetFood("Victim", 17)

	var points float64
	pieces := []string{"helmet", "chestplate", "leggings", "boots"}
	for i, piece := range pieces {
		id := servertest.MustItemID(t, "minecraft:netherite_"+piece)
		p, _ := registry.ArmorOf(id)
		points += float64(p)
		srv.GiveSlot("Victim", player.ArmorSlot+i, protocol.ItemStack{ItemID: id, Count: 1})
	}
	victim.AwaitAttributes(victimID, servertest.AttrArmor, func(e servertest.AttrEntry) bool { return e.Value() == points })

	srv.GiveSlot("Attacker", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:knockback", 1))
	servertest.AwaitFullSwing(t, srv, "Attacker")
	victim.DrainSurvivalTraffic()

	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victimID) })

	var motion []byte
	for id, data := range victim.Packets(servertest.ExpectTimeout) {
		if id == protocol.IDPlaySetEntityMotion {
			motion = data
			break
		}
	}
	r := protocol.NewReader(motion)
	r.ReadVarInt()
	vx, vy, _ := r.ReadLpVec3()
	// (0.2+0.5) of the six tenths full netherite leaves, the lift capped.
	if math.Abs(vx-0.42) > 0.01 || math.Abs(vy-0.4) > 0.01 {
		t.Errorf("knockback = (%v, %v), want (0.42, 0.4)", vx, vy)
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

// TestRespawnTellsTheNewBodyItsArmour checks the respawn's side: the
// client's new body starts from its defaults, so the server tells it the
// whole map again, the chestplate kept by keep_inventory already on it.
func TestRespawnTellsTheNewBodyItsArmour(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c, id := servertest.JoinAs(t, cfg, "Phoenix", servertest.OpsListing(t, "Phoenix"))
	c.ChatCommand("gamerule keep_inventory true")
	c.ExpectCommandFeedback("commands.gamerule.set", "keep_inventory", "true")

	chestplate := servertest.MustItemID(t, "minecraft:iron_chestplate")
	srv.GiveSlot("Phoenix", player.ArmorSlot+1, protocol.ItemStack{ItemID: chestplate, Count: 1})
	c.AwaitAttributes(id, servertest.AttrArmor, func(e servertest.AttrEntry) bool { return e.Value() == 6 })

	c.ChatCommand("kill")
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayCombatDeath); !ok {
		t.Fatal("no death")
	}
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn")
	}
	entries := c.AwaitAttributes(id, servertest.AttrArmor, func(servertest.AttrEntry) bool { return true })
	if e := entries[servertest.AttrArmor]; e.Value() != 6 || !hasModifier(e, "minecraft:armor.chestplate") {
		t.Errorf("armour told the respawned body = %+v, want the kept chestplate's 6", e)
	}
	if len(entries) < 4 {
		t.Errorf("respawn attributes = %+v, want the three starts and the armour", entries)
	}
}

// TestMovementSpeedEffectModifiersFollowTheEffect checks the movement speed's
// effect modifiers: the sprint's Update Attributes lists effect.speed next to
// sprinting, stopping the sprint leaves effect.speed on, and clearing the
// effect takes it off. Slowness carries its own -0.15 a level.
// vanilla: MobEffects.SPEED and MobEffects.SLOWNESS attribute modifiers
func TestMovementSpeedEffectModifiersFollowTheEffect(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Runner", servertest.OpsListing(t, "Runner"))
	c.Settle()
	srv.StopTick()
	c.Settle()

	const (
		sprint   = "minecraft:sprinting"
		speed    = "minecraft:effect.speed"
		slowness = "minecraft:effect.slowness"
	)

	srv.GiveEffect("Runner", "minecraft:speed", 6000, 1, true)
	c.AwaitMobEffect()
	c.SendSprint(id, true)
	c.Settle()
	srv.TickSurvival()

	sprinting := func(e servertest.AttrEntry) bool { return hasModifier(e, sprint) }
	e := c.AwaitAttributes(id, servertest.AttrMovementSpeed, sprinting)[servertest.AttrMovementSpeed]
	want := servertest.AttrMod(speed, float64(float32(0.2))*2, 2)
	if m, ok := servertest.Modifier(e, speed); !ok || e.Base != float64(float32(0.1)) || m != want {
		t.Fatalf("movement speed while sprinting on Speed II = %+v, want base 0.1 and %+v next to %s", e, want, sprint)
	}

	// Stopping the sprint keeps the effect's modifier.
	c.SendSprint(id, false)
	c.Settle()
	srv.TickSurvival()
	e = c.AwaitAttributes(id, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool { return !hasModifier(e, sprint) })[servertest.AttrMovementSpeed]
	if m, ok := servertest.Modifier(e, speed); !ok || m != want {
		t.Errorf("movement speed after stopping the sprint = %+v, want %+v kept", e, want)
	}

	// Clearing the effect takes it off.
	c.ChatCommand("effect clear @s minecraft:speed")
	c.Settle()
	srv.TickSurvival()
	e = c.AwaitAttributes(id, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool { return !hasModifier(e, sprint) })[servertest.AttrMovementSpeed]
	if hasModifier(e, speed) {
		t.Errorf("movement speed after clearing speed = %+v, want %s gone", e, speed)
	}

	// Slowness is the same attribute, with its own negative modifier.
	srv.GiveEffect("Runner", "minecraft:slowness", 6000, 1, true)
	c.AwaitMobEffect()
	srv.TickSurvival()
	e = c.AwaitAttributes(id, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool { return hasModifier(e, slowness) })[servertest.AttrMovementSpeed]
	want = servertest.AttrMod(slowness, float64(float32(-0.15))*2, 2)
	if m, ok := servertest.Modifier(e, slowness); !ok || e.Base != float64(float32(0.1)) || m != want {
		t.Errorf("movement speed on Slowness II = %+v, want %+v on base 0.1", e, want)
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

// TestSoulSpeedBootsPutTheirModifiersOn checks the wiring of the per-tick
// update: boots with Soul Speed over soul sand add the speed modifier under
// the data file's slot-suffixed id, and it comes off with the boots.
// vanilla: EnchantmentAttributeEffect.idForSlot
func TestSoulSpeedBootsPutTheirModifiersOn(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c, id := servertest.JoinAs(t, cfg, "Runner")

	pos := srv.PlayerPosition("Runner")
	sand, ok := registry.FindState("minecraft:soul_sand", nil)
	if !ok {
		t.Fatal("the block registry has no soul sand")
	}
	srv.SetBlock(int32(math.Floor(pos.X)), int32(math.Floor(pos.Y-0.5)), int32(math.Floor(pos.Z)), sand)

	boots, ok := registry.ItemID("minecraft:iron_boots")
	if !ok {
		t.Fatal("the item registry has no iron boots")
	}
	soulSpeed, ok := registry.EnchantmentID("minecraft:soul_speed")
	if !ok {
		t.Fatal("the enchantment registry has no soul speed")
	}
	srv.GiveSlot("Runner", player.ArmorSlot+3, protocol.ItemStack{ItemID: boots, Count: 1, Enchantments: map[int32]int32{soulSpeed: 1}})
	running := func(e servertest.AttrEntry) bool { return hasModifier(e, "minecraft:enchantment.soul_speed/feet") }
	e := c.AwaitAttributes(id, servertest.AttrMovementSpeed, running)[servertest.AttrMovementSpeed]
	want := servertest.AttrMod("minecraft:enchantment.soul_speed/feet", float64(float32(0.0405)), 0)
	if len(e.Modifiers) != 1 || e.Modifiers[0] != want {
		t.Errorf("movement speed on soul sand = %+v, want %+v", e, want)
	}

	srv.GiveSlot("Runner", player.ArmorSlot+3, protocol.ItemStack{})
	c.AwaitAttributes(id, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool { return !running(e) })
}
