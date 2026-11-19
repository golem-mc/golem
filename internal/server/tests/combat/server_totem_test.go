package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The totem of undying: a player holding one survives a blow past any
// health the bar can hold, at one half-heart with the three effects the
// item's death protection lists, the item spent, the animation (entity
// event 35) reaching the holder and their watchers both — and /kill, whose
// damage bypasses invulnerability, still kills through it.

// totemStack is one totem of undying, the one item whose default
// components carry minecraft:death_protection.
func totemStack(t *testing.T) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID("minecraft:totem_of_undying")
	if !ok {
		t.Fatal("the item registry has no totem of undying")
	}
	return protocol.ItemStack{ItemID: id, Count: 1}
}

// TestATotemOfUndyingSavesFromALethalBlow checks the whole save, holder and
// watcher both: the totem spent out of the hand, the three effects in the
// item's own words, the animation, the bar standing at the one half-heart
// the item leaves and the blow's damage event behind it.
func TestATotemOfUndyingSavesFromALethalBlow(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	// The watcher is in the world first, so the holder's arrival and the
	// totem's animation both reach it as they happen.
	srv, connect := servertest.Start(t, cfg)
	watcher := connect()
	watcher.JoinAs("Watcher")

	holder := connect()
	holderID := holder.JoinAs("Holder")
	if got := watcher.ExpectPlayerAdded("Holder"); got != holderID {
		t.Fatalf("entity ID disagreement: watcher saw %d, holder is %d", got, holderID)
	}
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Holder")

	// The loop is stopped so no vitals tick runs beside the blow: what
	// reaches the two clients in the reads below is the hurt's own order.
	srv.StopTick()

	srv.GiveSlot("Holder", player.HotbarSlot, totemStack(t))
	if !srv.Hurt("Holder", 40) {
		t.Fatal("the blow did not land")
	}

	// The save as the holder hears it: the totem spent out of the hand,
	// the three effects, the animation, then the bar at one half-heart and
	// the blow's own damage event.
	if got := servertest.ExpectEmptiedSlot(holder); got != player.HotbarSlot {
		t.Errorf("the spent slot = %d, want the hotbar's %d", got, player.HotbarSlot)
	}
	holder.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:regeneration"), 1, 900)
	holder.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:absorption"), 1, 100)
	holder.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:fire_resistance"), 0, 800)
	holder.ExpectEntityEvent(holderID, protocol.EntityStatusTotem)
	if got := holder.ExpectHealth(); got.Health != 1 || got.Food != 20 || got.Saturation != 5 {
		t.Errorf("bar after the save = %v health, %v food, %v saturation, want 1 with the bar fed",
			got.Health, got.Food, got.Saturation)
	}
	holder.ExpectDamageEvent(holderID, "minecraft:mob_attack", 1)

	// The watcher's half: the emptied hand, the animation, then the blow.
	entries := watcher.ExpectEquipment(holderID)
	if got := servertest.EquipEntry(entries, 0); !got.Empty() {
		t.Errorf("the watcher was shown the main hand holding %+v after the totem was spent", got)
	}
	watcher.ExpectEntityEvent(holderID, protocol.EntityStatusTotem)
	watcher.ExpectDamageEvent(holderID, "minecraft:mob_attack", 1)

	if got := srv.PlayerHealth("Holder"); got != 1 {
		t.Errorf("health after the save = %v, want 1", got)
	}
	if got := srv.InventorySlot("Holder", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hand still holds %+v after the save", got)
	}
}

// TestTheMainHandTotemIsSpentBeforeTheOffHand checks the order vanilla's
// hand pick reads the hands in: a totem in each hand spends the main one's,
// and once no main-hand item saves the body any more, the offhand's takes
// the next blow — clearing the effects the first save left before giving
// the three again. A body no hand saves dies all the same.
func TestTheMainHandTotemIsSpentBeforeTheOffHand(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Holder")
	srv.StopTick()

	srv.GiveSlot("Holder", player.HotbarSlot, totemStack(t))
	srv.GiveSlot("Holder", player.OffHandSlot, totemStack(t))
	holderID := c.PlayerEntityID
	if !srv.Hurt("Holder", 25) {
		t.Fatal("the blow did not land")
	}
	if got := servertest.ExpectEmptiedSlot(c); got != player.HotbarSlot {
		t.Errorf("the first spent slot = %d, want the main hand's %d", got, player.HotbarSlot)
	}
	if got := srv.InventorySlot("Holder", player.OffHandSlot); got.Count != 1 {
		t.Errorf("the offhand totem = %+v after a main-hand save, want one left", got)
	}
	c.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:regeneration"), 1, 900)
	c.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:absorption"), 1, 100)
	c.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:fire_resistance"), 0, 800)
	c.ExpectEntityEvent(holderID, protocol.EntityStatusTotem)
	if got := c.ExpectHealth(); got.Health != 1 {
		t.Errorf("bar after the first save = %v, want 1", got.Health)
	}
	c.ExpectDamageEvent(holderID, "minecraft:mob_attack", 1)
	c.ExpectHurtAnimation(holderID)

	// The window the first blow opened runs out on no tick while the loop
	// is stopped, so the second blow lands only what it beats the first by
	// — still enough to empty the bar, and around the offhand's totem.
	if !srv.Hurt("Holder", 100) {
		t.Fatal("the second blow was dropped by the first's window")
	}
	if got := servertest.ExpectEmptiedSlot(c); got != player.OffHandSlot {
		t.Errorf("the second spent slot = %d, want the offhand's %d", got, player.OffHandSlot)
	}
	// The save clears every effect first — the three the first save left —
	// then gives them again, the clear in effect-ID order.
	want := []int32{
		registry.MustMobEffect("minecraft:regeneration"),
		registry.MustMobEffect("minecraft:fire_resistance"),
		registry.MustMobEffect("minecraft:absorption"),
	}
	for _, effect := range want {
		if got := c.ExpectMobEffectRemoved(holderID); got != effect {
			t.Errorf("cleared effect = %d, want %d", got, effect)
		}
	}
	c.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:regeneration"), 1, 900)
	c.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:absorption"), 1, 100)
	c.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:fire_resistance"), 0, 800)
	c.ExpectEntityEvent(holderID, protocol.EntityStatusTotem)
	if got := srv.PlayerHealth("Holder"); got != 1 {
		t.Errorf("health after the offhand's save = %v, want 1", got)
	}

	// No hand saves the third blow, and the body dies in spite of the
	// bar the second save stood at one half-heart.
	if !srv.Hurt("Holder", 150) {
		t.Fatal("the third blow was dropped by the second's window")
	}
	if got := srv.PlayerHealth("Holder"); got != 0 {
		t.Errorf("health after a third blow past both totems = %v, want 0", got)
	}
}

// TestAnOffHandOnlyTotemSaves checks the fallback vanilla's hand pick makes
// when the main hand holds nothing that saves: the offhand's totem is spent,
// the effects land the same, and the absorption the item's second level names
// stands on the body.
func TestAnOffHandOnlyTotemSaves(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Holder")
	srv.StopTick()

	srv.GiveSlot("Holder", player.OffHandSlot, totemStack(t))
	holderID := c.PlayerEntityID
	if !srv.Hurt("Holder", 40) {
		t.Fatal("the blow did not land")
	}
	if got := servertest.ExpectEmptiedSlot(c); got != player.OffHandSlot {
		t.Errorf("the spent slot = %d, want the offhand's %d", got, player.OffHandSlot)
	}
	c.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:regeneration"), 1, 900)
	c.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:absorption"), 1, 100)
	c.ExpectMobEffectAdded(holderID, registry.MustMobEffect("minecraft:fire_resistance"), 0, 800)
	c.ExpectEntityEvent(holderID, protocol.EntityStatusTotem)
	if got := c.ExpectHealth(); got.Health != 1 {
		t.Errorf("bar after the save = %v, want 1", got.Health)
	}
	c.ExpectDamageEvent(holderID, "minecraft:mob_attack", 1)

	if got := srv.InventorySlot("Holder", player.OffHandSlot); !got.Empty() {
		t.Errorf("the offhand still holds %+v after the save", got)
	}
	if got := srv.PlayerAbsorption("Holder"); got != 8 {
		t.Errorf("absorption after the save = %v, want 8, the totem's Absorption II", got)
	}
}

// TestATotemSaveKeepsTheFallDistance checks that a body the totem saves keeps
// the fall it was carrying: a lethal blow taken mid-air leaves the banked
// distance alone, so the landing that follows still lands on the body. The
// absorbed part of the landing is what the test reads, the totem's extra
// hearts being the first thing the fall spends.
// vanilla: LivingEntity.checkTotemDeathProtection touches only the health
func TestATotemSaveKeepsTheFallDistance(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Faller")
	c.MarkLoaded()
	srv.StopTick()

	srv.GiveSlot("Faller", player.HotbarSlot, totemStack(t))

	// Rise to a known height, then bank eight blocks of fall mid-air.
	c.MoveToGround(0.5, 70, 0.5, false)
	c.MoveToGround(0.5, 62, 0.5, false)
	c.Settle()

	// The lethal blow lands mid-air: the totem stands the body up at one
	// half-heart, the fall still banked behind it.
	if !srv.Hurt("Faller", 40) {
		t.Fatal("the blow did not land")
	}
	if got := srv.PlayerHealth("Faller"); got != 1 {
		t.Fatalf("health after the save = %v, want 1", got)
	}

	// The save's own invulnerable window would turn the weaker landing
	// aside, so clear it, keeping the body at the half-heart.
	srv.SetHealth("Faller", 1)

	// Eight blocks past the free three are five damage, which the totem's
	// eight absorption eats first.
	c.MoveToGround(0.5, 62, 0.5, true)
	c.Settle()

	if got, want := srv.PlayerAbsorption("Faller"), float64(3); got != want {
		t.Errorf("absorption after the landing = %v, want %v: the fall's five spent on the totem's extra hearts", got, want)
	}
}

// TestKillStillKillsThroughATotem checks the bypass the totem cannot turn
// aside: /kill's damage_type minecraft:generic_kill sits in
// bypasses_invulnerability, so the body dies the whole death with the totem
// still in hand.
func TestKillStillKillsThroughATotem(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	// /kill is a gamemaster's command, so the runner is listed as an
	// operator.
	srv, c, entityID := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	srv.GiveSlot("Testificate", player.HotbarSlot, totemStack(t))

	c.ChatCommand("kill")
	killMessage := protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Testificate")},
	}
	servertest.ExpectDeathSequence(t, c, entityID, killMessage)
}
