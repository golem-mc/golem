package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// swiftnessPotion is a stack of minecraft:potion carrying a swiftness brewing
// as its potion_contents component. The value's shape is the wire's: the
// potion by its ID - swiftness is potion 13 of the registry the client keeps
// built into itself - then no custom colour, no custom effects and no
// custom name.
func swiftnessPotion(t *testing.T) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutBool(true)
	value.PutVarInt(13)
	value.PutBool(false) // no custom colour
	value.PutVarInt(0)   // no custom effects
	value.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:potion"), Count: 1,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentPotionContents, Data: value.Bytes()},
		},
	}
}

// brewedPotion is a stack of minecraft:potion carrying the brewing the given
// registry potion ID names, as its potion_contents component: the wire shape
// swiftnessPotion builds, for the tests that name their potion by ID rather
// than by hand.
func brewedPotion(t *testing.T, id int32) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutBool(true)
	value.PutVarInt(id)
	value.PutBool(false) // no custom colour
	value.PutVarInt(0)   // no custom effects
	value.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:potion"), Count: 1,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentPotionContents, Data: value.Bytes()},
		},
	}
}

// waterBottle is a stack of minecraft:potion carrying the water brewing as
// its potion_contents component: the potion alone, no colour, no effects and
// no custom name.
func waterBottle(t *testing.T) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutBool(true)
	value.PutVarInt(0)   // water
	value.PutBool(false) // no custom colour
	value.PutVarInt(0)   // no custom effects
	value.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:potion"), Count: 1,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentPotionContents, Data: value.Bytes()},
		},
	}
}

// TestDrinkingAWaterBottleUsesItUpAndLeavesTheBottle drinks a water bottle:
// water is brewed with no effects, so none is sent, and the emptied bottle
// takes the potion's place.
func TestDrinkingAWaterBottleUsesItUpAndLeavesTheBottle(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Drinker")
	c.Settle()

	srv.GiveSlot("Drinker", player.HotbarSlot, waterBottle(t))
	c.UseItemRightClicks(protocol.HandMain)

	bottle := servertest.MustItemID(t, "minecraft:glass_bottle")
	servertest.UntilTicks(t, srv, 100, "condition", func() bool {
		got := srv.InventorySlot("Drinker", player.HotbarSlot)
		return got.ItemID == bottle && got.Count == 1
	})
}

// TestDrinkingASwiftnessPotionAppliesItsEffectsAndLeavesTheBottle right-clicks
// a swiftness potion: the drinking player's hand goes up for the animation
// other clients play, the finished drink sends the speed it was brewed with
// for 3600 ticks, and the glass bottle takes the empty hand's place.
func TestDrinkingASwiftnessPotionAppliesItsEffectsAndLeavesTheBottle(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Drinker")
	c.Settle()

	srv.GiveSlot("Drinker", player.HotbarSlot, swiftnessPotion(t))
	c.UseItemRightClicks(protocol.HandMain)
	c.ExpectHandState(id, protocol.EntityFlagHandActive)

	if got, want := c.AwaitMobEffect(), (servertest.MobEffect{
		Entity: id, Effect: registry.MustMobEffect("minecraft:speed"),
		Amplifier: 0, Duration: 3600, Flags: 0x02 | 0x04 | 0x08,
	}); got != want {
		t.Errorf("update mob effect = %+v, want %+v", got, want)
	}
	bottle := servertest.MustItemID(t, "minecraft:glass_bottle")
	servertest.UntilTicks(t, srv, 100, "condition", func() bool {
		got := srv.InventorySlot("Drinker", player.HotbarSlot)
		return got.ItemID == bottle && got.Count == 1
	})
}

// TestPotionContentsSurviveARestart puts a swiftness potion in a surviving
// player's hotbar, restarts the server, and drinks it: the contents reached
// the file and came back, so the drink still gives Speed rather than only
// leaving the bottle.
func TestPotionContentsSurviveARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Drinker")
	c.Settle()
	srv.GiveSlot("Drinker", player.HotbarSlot, swiftnessPotion(t))
	c.Settle()
	srv.Shutdown()

	restarted := *cfg
	srv, again, id := servertest.JoinAs(t, &restarted, "Drinker")
	again.Settle()

	potion := servertest.MustItemID(t, "minecraft:potion")
	kept := srv.InventorySlot("Drinker", player.HotbarSlot)
	if kept.ItemID != potion || kept.Count != 1 {
		t.Fatalf("the rejoined hotbar holds %+v, want the potion that was put away", kept)
	}
	contents, ok := kept.PotionContentsOf()
	if !ok || contents.Potion != registry.MustPotion("minecraft:swiftness") {
		t.Fatalf("the rejoined potion's contents = %+v, %v; want swiftness", contents, ok)
	}

	again.UseItemRightClicks(protocol.HandMain)
	if got, want := again.AwaitMobEffect(), (servertest.MobEffect{
		Entity: id, Effect: registry.MustMobEffect("minecraft:speed"),
		Amplifier: 0, Duration: 3600, Flags: 0x02 | 0x04 | 0x08,
	}); got != want {
		t.Errorf("update mob effect = %+v, want %+v", got, want)
	}
}

// TestDrinkingAHealingPotionHealsAtOnceAndTellsNoEffect drinks a potion of
// healing: the instant health lands on the drinking tick itself, the bar
// climbing from ten to fourteen in one Set Health, and no effect is sent for
// it - an instant effect is carried out, not worn, so no Update Mob Effect
// and no Remove Mob Effect ever name it.
func TestDrinkingAHealingPotionHealsAtOnceAndTellsNoEffect(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Healed")
	c.Settle()
	srv.StopTick()
	c.Settle()

	srv.SetHealth("Healed", 10)
	srv.SetFood("Healed", 6)
	srv.GiveSlot("Healed", player.HotbarSlot, brewedPotion(t, 24)) // healing
	c.Settle()

	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	// The drink is thirty-two ticks; step well past its end by hand.
	for range 40 {
		srv.TickSurvival()
	}

	if got := srv.PlayerHealth("Healed"); got != 14 {
		t.Errorf("health after the healing potion = %v, want 14", got)
	}
	var updates, removals, healths int
	c.SettleAndCollect(func(packetID int32, data []byte) {
		switch packetID {
		case protocol.IDPlayUpdateMobEffect:
			updates++
		case protocol.IDPlayRemoveMobEffect:
			removals++
		case protocol.IDPlaySetHealth:
			healths++
		}
	})
	if updates != 0 || removals != 0 {
		t.Errorf("the instant heal sent %d effect updates and %d removals, want none", updates, removals)
	}
	if healths == 0 {
		t.Error("the instant heal sent no Set Health")
	}
}

// TestDrinkingAHarmingPotionKillsWithIndirectMagic drinks a harming potion on
// a body at two: the instant damage lands on the drinking tick as one blow of
// six of indirect magic, and the death is announced with the magic's own
// words, the drinker named for the source it has none of.
func TestDrinkingAHarmingPotionKillsWithIndirectMagic(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Doomed")
	c.Settle()
	srv.StopTick()
	c.Settle()

	// Keep the inventory so the death drops nothing: the item-entity spawns
	// of a drop would sit between the damage event and the death screen.
	srv.SetGameRule("keep_inventory", true)
	srv.SetHealth("Doomed", 2)
	srv.SetFood("Doomed", 6)
	srv.GiveSlot("Doomed", player.HotbarSlot, brewedPotion(t, 26)) // harming
	// The setters change health outside the tick, so the next tick sends
	// the Set Health of 2; take it before the drink.
	srv.TickSurvival()
	c.Settle()

	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	for range 40 {
		srv.TickSurvival()
	}

	if got := srv.PlayerHealth("Doomed"); got != 0 {
		t.Errorf("health after the harming potion = %v, want 0", got)
	}
	message := protocol.Text{
		Translate: "death.attack.indirectMagic",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Doomed"), servertest.PlayerDisplayComponent("Doomed")},
	}
	// The block-change acknowledgement the use carries rides in front of the
	// death's packets; read up to the Set Health, stepping over it.
	health := protocol.NewReader(c.ExpectWithinTicks(srv, 180, protocol.IDPlaySetHealth))
	if got := health.ReadF32(); got != 0 {
		t.Errorf("health at the death = %v, want 0", got)
	}
	c.ExpectDamageEvent(id, "minecraft:indirect_magic", id+1) // indirectMagic(drinker, drinker)
	c.ExpectHurtAnimation(id)
	c.ExpectCombatDeath(id, message)
	c.ExpectDeathMessage(message)
	c.Disconnect()
}
