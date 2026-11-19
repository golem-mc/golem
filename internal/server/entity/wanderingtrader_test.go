package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestAWanderingTraderIsALeadlessCreature: vanilla refuses it a lead
// (AbstractVillager.canBeLeashed) and files it under CREATURE.
func TestAWanderingTraderIsALeadlessCreature(t *testing.T) {
	if Leashable(TypeWanderingTrader) {
		t.Error("a wandering trader can be leashed")
	}
	if !CreatureKind(TypeWanderingTrader) {
		t.Error("a wandering trader is not a creature-category kind")
	}
	m := NewMob(TypeWanderingTrader, 1, 0.5, 64, 0.5, 1)
	if m.MaxHealth != 20 || m.eyeHeight() != 1.62 || m.width() != 0.6 || m.height() != 1.95 {
		t.Errorf("health %v eye %v box %v x %v", m.MaxHealth, m.eyeHeight(), m.width(), m.height())
	}
}

// TestAWanderingTraderLeavesWhenItsDespawnDelayRunsOut: the delay counts
// down only while no one trades, and the tick it reaches zero removes the
// trader without a death.
func TestAWanderingTraderLeavesWhenItsDespawnDelayRunsOut(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -10, -10, 10, 10, 63, stone)
	m := NewMob(TypeWanderingTrader, 1, 0.5, 64, 0.5, 1)
	m.NoAI = true
	m.DespawnDelay = 3
	vanished := func() bool {
		for _, ev := range m.Tick(w, nil) {
			if ev.Kind == MobVanished {
				return true
			}
		}
		return false
	}
	m.Trading = true
	for range 10 {
		if vanished() {
			t.Fatal("a trading wandering trader left")
		}
	}
	m.Trading = false
	for i := range 2 {
		if vanished() {
			t.Fatalf("left on tick %d of a 3-tick delay", i+1)
		}
	}
	if !vanished() {
		t.Errorf("still here with delay %d after 3 ticks", m.DespawnDelay)
	}
}

// TestAWanderingTraderWithNoDelayStays: a zero DespawnDelay, a summoned
// trader's, never runs out.
func TestAWanderingTraderWithNoDelayStays(t *testing.T) {
	m := NewMob(TypeWanderingTrader, 1, 0.5, 64, 0.5, 1)
	for range 100 {
		if m.traderDespawnStep() {
			t.Fatal("a trader with no despawn delay left")
		}
	}
}

// TestAWanderingTraderSavesItsDelayAndOffers round-trips DespawnDelay, the
// wander target and the Offers compound.
func TestAWanderingTraderSavesItsDelayAndOffers(t *testing.T) {
	now := func() int64 { return 0 }
	m := NewMob(TypeWanderingTrader, 1, 0.5, 64, 0.5, 1)
	m.DespawnDelay = 48000
	m.WanderTarget = &Waypoint{5, 64, -7}
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound}}
	tag := nbt.Compound{}
	m.SaveTo(tag, now)
	if d, _ := nbt.Get[nbt.Int](tag, "DespawnDelay"); d != 48000 {
		t.Errorf("saved DespawnDelay %v, want 48000", tag["DespawnDelay"])
	}
	back := NewMob(TypeWanderingTrader, 2, 0.5, 64, 0.5, 2)
	back.LoadFrom(tag, nil, now)
	if back.DespawnDelay != 48000 || back.Offers == nil {
		t.Errorf("loaded delay %d offers %v", back.DespawnDelay, back.Offers)
	}
	if back.WanderTarget == nil || *back.WanderTarget != (Waypoint{5, 64, -7}) {
		t.Errorf("loaded wander target %v, want 5 64 -7", back.WanderTarget)
	}
	back.WanderTarget = nil
	tag = nbt.Compound{}
	back.SaveTo(tag, now)
	if _, ok := tag["wander_target"]; ok {
		t.Error("a trader with no wander target saved one")
	}
}

// TestAWanderingTraderRewardsOnlyAnOrb: a trade drops a 3-6 orb unless the
// offer withholds it, and the trader gains no experience.
func TestAWanderingTraderRewardsOnlyAnOrb(t *testing.T) {
	m := NewMob(TypeWanderingTrader, 1, 0.5, 64, 0.5, 1)
	level := m.Villager.Level
	for range 20 {
		if orb, _, _ := m.RewardTrade(10, true); orb < 3 || orb > 6 {
			t.Fatalf("orb %d, want 3-6", orb)
		}
	}
	if orb, _, _ := m.RewardTrade(10, false); orb != 0 {
		t.Errorf("orb %d for an offer without reward, want none", orb)
	}
	if m.VillagerXp != 0 || m.Villager.Level != level {
		t.Errorf("trader xp %d, level %d, want %d", m.VillagerXp, m.Villager.Level, level)
	}
}

// TestAWanderingTraderDrinksInvisibilityAtNightAndMilkAtDawn: in the dark it
// drinks the potion and turns invisible, in the light the milk clears it;
// each drink ends with an empty hand and its sound.
// vanilla: WanderingTrader.registerGoals, UseItemGoal
func TestAWanderingTraderDrinksInvisibilityAtNightAndMilkAtDawn(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -10, -10, 10, 10, 63, stone)
	m := NewMob(TypeWanderingTrader, 1, 0.5, 64, 0.5, 1)
	drink := func(w testWorld, done string) {
		t.Helper()
		heard := false
		for range 60 {
			for _, ev := range m.Tick(w, nil) {
				heard = heard || ev.Kind == MobSounded && ev.SoundID.Name() == done
			}
		}
		if !heard {
			t.Errorf("no %s", done)
		}
		if m.UsingItem || !m.MainHand.Empty() {
			t.Errorf("still drinking %v with %v in hand", m.UsingItem, m.MainHand)
		}
	}
	w.dark = true
	drink(w, "minecraft:entity.wandering_trader.disappeared")
	if !m.hasEffect(mobEffectInvisibility) {
		t.Fatal("not invisible after the night's potion")
	}
	w.dark, w.bright = false, true
	drink(w, "minecraft:entity.wandering_trader.reappeared")
	if m.hasEffect(mobEffectInvisibility) {
		t.Error("still invisible after the dawn's milk")
	}
}

// TestAWanderingTraderWalksToItsWanderTarget: it heads for the target in
// legs and forgets it within 2 blocks.
// vanilla: WanderingTrader.WanderToPositionGoal
func TestAWanderingTraderWalksToItsWanderTarget(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -10, -10, 40, 10, 63, stone)
	m := NewMob(TypeWanderingTrader, 1, 0.5, 64, 0.5, 1)
	m.WanderTarget = &Waypoint{25, 64, 0}
	for range 2400 {
		m.Tick(w, nil)
		if m.WanderTarget == nil {
			break
		}
	}
	if m.WanderTarget != nil {
		t.Fatalf("still bound for its target at %.1f %.1f %.1f", m.X, m.Y, m.Z)
	}
	if dx, dz := m.X-25.5, m.Z-0.5; dx*dx+dz*dz > 3*3 {
		t.Errorf("forgot its target at %.1f %.1f, not near 25.5 0.5", m.X, m.Z)
	}
}
