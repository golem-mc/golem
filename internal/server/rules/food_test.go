package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// foodOf resolves one meal by name for the tests below.
func foodOf(t *testing.T, name string) registry.Food {
	t.Helper()
	food, ok := registry.FoodOf(itemID(t, name))
	if !ok {
		t.Fatalf("the item registry has no meal %s", name)
	}
	return food
}

// TestEatAppliesNutritionAndSaturation runs vanilla's order of a finished
// meal: the nutrition fills the food bar to its cap, and the saturation
// follows it but no fuller than the bar itself was left by the meal.
func TestEatAppliesNutritionAndSaturation(t *testing.T) {
	bread := foodOf(t, "minecraft:bread") // five nutrition, six saturation
	if bread.Nutrition != 5 {
		t.Fatalf("bread's nutrition is %v, want the vanilla five", bread.Nutrition)
	}

	// Halfway hungry: both bars take the whole meal.
	v := player.Vitals{Food: 10, Saturation: 1}
	Eat(&v, bread)
	if v.Food != 15 {
		t.Errorf("the meal left the food bar at %v, want fifteen", v.Food)
	}
	if v.Saturation != 7 {
		t.Errorf("the meal left the saturation at %v, want seven", v.Saturation)
	}

	// A bar nearly full: the food caps at twenty, and the saturation
	// stops at the bar the meal left, fuller than vanilla's old five.
	v = player.Vitals{Food: 19, Saturation: 4}
	Eat(&v, bread)
	if v.Food != MaxFood {
		t.Errorf("the meal left the food bar at %v, want it capped", v.Food)
	}
	if v.Saturation != 10 {
		t.Errorf("the meal left the saturation at %v, want it capped by the food bar", v.Saturation)
	}

	// On a full bar the meal is all saturation, up to the cap.
	v = player.Vitals{Food: MaxFood, Saturation: 0}
	Eat(&v, bread)
	if v.Food != MaxFood || v.Saturation != 6 {
		t.Errorf("a meal on a full bar left %+v, want the saturation alone", v)
	}
}

// TestEatTicksRunsOnTheGameClock turns a consumable's seconds into the
// ticks the meal is counted down on: twenty to the second, truncated to the
// whole tick the vanilla server's consume time keeps - the 1.6 seconds a
// meal has always taken being 32 ticks, not the 33 a rounding up would
// give - with the multiplication itself on the 32-bit floats the component
// arrives as.
func TestEatTicksRunsOnTheGameClock(t *testing.T) {
	if got, want := EatTicks(foodOf(t, "minecraft:bread")), int32(32); got != want {
		t.Errorf("eating bread takes %v ticks, want %v", got, want)
	}
	// The other consume times the vanilla components carry: the dried
	// kelp's eight-tenths of a second, the honey bottle's two.
	if got, want := EatTicks(foodOf(t, "minecraft:dried_kelp")), int32(16); got != want {
		t.Errorf("eating dried kelp takes %v ticks, want %v", got, want)
	}
	if got, want := EatTicks(foodOf(t, "minecraft:honey_bottle")), int32(40); got != want {
		t.Errorf("drinking the honey bottle takes %v ticks, want %v", got, want)
	}
	// A fraction of a second costs no tick it does not reach.
	if got := EatTicks(registry.Food{ConsumeSeconds: 0.05}); got != 1 {
		t.Errorf("a twentieth-second meal takes %v ticks, want the one it truncates to", got)
	}
	// Whole seconds land on whole ticks.
	if got := EatTicks(registry.Food{ConsumeSeconds: 2}); got != 40 {
		t.Errorf("a two-second meal takes %v ticks, want forty", got)
	}
}

// TestSlotOfHand maps a hand onto the slot it holds, the hotbar slot the
// player has selected being the main hand and the off hand its own slot
// at the end of the window.
func TestSlotOfHand(t *testing.T) {
	inv := player.Inventory{Selected: 3}
	if got := SlotOfHand(&inv, protocol.HandMain); got != player.HotbarSlot+3 {
		t.Errorf("the main hand holds slot %v, want the selected hotbar slot", got)
	}
	if got := SlotOfHand(&inv, protocol.HandOff); got != player.OffHandSlot {
		t.Errorf("the off hand holds slot %v, want its own", got)
	}
}
