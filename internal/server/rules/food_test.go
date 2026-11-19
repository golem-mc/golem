package rules

import (
	"testing"

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
