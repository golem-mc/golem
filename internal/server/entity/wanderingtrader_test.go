package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

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
