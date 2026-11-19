package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestDrinkPotionAppliesTheBrewingsInOrder drinks a turtle master's potion
// carrying a custom effect: slowness IV then resistance III in the brewing's
// own order, then the custom speed, hidden twin and all, each effect told
// as it lands.
func TestDrinkPotionAppliesTheBrewingsInOrder(t *testing.T) {
	speedID := registry.MustMobEffect("minecraft:speed")
	var contents protocol.PotionContents
	contents.Potion = registry.MustPotion("minecraft:turtle_master")
	contents.CustomEffects = []protocol.PotionEffectSpec{{
		ID: speedID, Duration: 100, Ambient: true,
		Hidden: &protocol.PotionEffectSpec{ID: speedID, Duration: 400, ShowIcon: true},
	}}

	var v player.Vitals
	var s effect.Set
	var hurts []Hurt
	changes := DrinkPotion(&v, &s, nil, contents, 1, &hurts)
	if len(changes) != 3 {
		t.Fatalf("drink came to %d changes, want 3: %+v", len(changes), changes)
	}
	if len(hurts) != 0 {
		t.Errorf("a potion of carried effects hurt %v, want none", hurts)
	}
	for i, want := range []int32{
		registry.MustMobEffect("minecraft:slowness"),
		registry.MustMobEffect("minecraft:resistance"),
		speedID,
	} {
		if changes[i].Instance.ID != want {
			t.Errorf("change %d = effect %d, want %d", i, changes[i].Instance.ID, want)
		}
	}
	custom, ok := s.Get(speedID)
	if !ok {
		t.Fatal("the custom speed effect did not land")
	}
	if !custom.Ambient || custom.Duration != 100 || custom.ShowIcon {
		t.Errorf("custom speed = %+v, want ambient for 100 ticks, no icon", custom)
	}
	if custom.Hidden == nil || custom.Hidden.Duration != 400 || !custom.Hidden.ShowIcon {
		t.Errorf("custom speed's hidden twin = %+v, want 400 ticks with an icon", custom.Hidden)
	}

	// A component naming a potion out of the registry carries no brewing
	// of its own: the customs are the whole drink.
	var alone effect.Set
	if changes := DrinkPotion(&v, &alone, nil, protocol.PotionContents{Potion: 999}, 1, &hurts); len(changes) != 0 {
		t.Errorf("an unknown potion handed %+v, want nothing", changes)
	}
}

// TestInstantPotionLandsOnTheSpot drinks a strong healing and a harming
// potion: the heal of eight and the blow of six land on the drinking tick,
// neither left as a countdown instance for the next tick to carry out.
func TestInstantPotionLandsOnTheSpot(t *testing.T) {
	v := FreshVitals(0)
	v.Health = 4
	var s effect.Set
	var hurts []Hurt
	changes := DrinkPotion(&v, &s, nil, protocol.PotionContents{Potion: registry.MustPotion("minecraft:strong_healing")}, 1, &hurts)
	if v.Health != 12 {
		t.Errorf("health right after the drink = %v, want 12 at once", v.Health)
	}
	if len(changes) != 0 {
		t.Errorf("the heal told %+v, want nothing", changes)
	}
	if len(hurts) != 0 {
		t.Errorf("the heal hurt %v, want none", hurts)
	}
	if _, ok := s.Get(registry.MustMobEffect("minecraft:instant_health")); ok {
		t.Error("the heal left an instant_health instance behind")
	}

	var harm effect.Set
	hurts = nil
	changes = DrinkPotion(&v, &harm, nil, protocol.PotionContents{Potion: registry.MustPotion("minecraft:harming")}, 1, &hurts)
	if len(hurts) != 1 || hurts[0].DamageType != DamageTypeIndirectMagic || hurts[0].Amount != 6 {
		t.Errorf("harming's blow = %v, want one indirect magic hurt of 6", hurts)
	}
	if len(changes) != 0 {
		t.Errorf("the harm told %+v, want nothing", changes)
	}
	if _, ok := harm.Get(registry.MustMobEffect("minecraft:instant_damage")); ok {
		t.Error("the harm left an instant_damage instance behind")
	}
}
