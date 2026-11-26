package rules

import "testing"

// TestABlazeDropsRodsOnlyForAPlayerAndGrowsWithLooting: the rod's nought to
// one needs a player's kill, and looting III stretches it to at most four.
// vanilla: entities/blaze.json
func TestABlazeDropsRodsOnlyForAPlayerAndGrowsWithLooting(t *testing.T) {
	for _, drops := range rollMany(t, "minecraft:entities/blaze", LootContext{}, 200, 701) {
		if len(drops) != 0 {
			t.Fatalf("a death nobody is behind dropped %v", drops)
		}
	}
	plain := countsOf(rollMany(t, "minecraft:entities/blaze", LootContext{KilledByPlayer: true}, 400, 702), "minecraft:blaze_rod")
	most := 0
	for _, c := range plain {
		most = max(most, int(c))
	}
	if most != 1 {
		t.Errorf("a plain kill dropped at most %d rods, want 1", most)
	}
	looted := countsOf(rollMany(t, "minecraft:entities/blaze", LootContext{KilledByPlayer: true,
		Attacker: LootEntity{Type: "minecraft:player",
			Gear: &LootGear{Mainhand: toolWith("minecraft:iron_sword", map[string]int32{"minecraft:looting": 3})}}}, 400, 703), "minecraft:blaze_rod")
	most = 0
	for _, c := range looted {
		most = max(most, int(c))
	}
	if most < 2 || most > 4 {
		t.Errorf("a looting III kill dropped at most %d rods, want 2 to 4", most)
	}
}
