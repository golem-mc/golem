package rules

import (
	"math/rand"
	"testing"
)

// TestTheCatMorningGiftTableHoldsSevenItems: the table rolls one item per
// roll from rabbit hide, rabbit foot, chicken, feather, rotten flesh and
// string at weight 10 and phantom membrane at 2.
// vanilla: data/minecraft/loot_table/gameplay/cat_morning_gift.json
func TestTheCatMorningGiftTableHoldsSevenItems(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	const rolls = 6200
	seen := map[string]int{}
	for range rolls {
		drops := rollNames(t, RollLoot("minecraft:gameplay/cat_morning_gift", &LootContext{}, rng))
		if len(drops) != 1 || drops[0].count != 1 {
			t.Fatalf("a roll gave %v, want one item", drops)
		}
		seen[drops[0].item]++
	}
	for _, item := range []string{"rabbit_hide", "rabbit_foot", "chicken", "feather", "rotten_flesh", "string"} {
		if n := seen["minecraft:"+item]; n < 800 || n > 1200 {
			t.Errorf("%s rolled %d times in %d, want about 1000", item, n, rolls)
		}
	}
	if n := seen["minecraft:phantom_membrane"]; n < 120 || n > 280 {
		t.Errorf("phantom membrane rolled %d times in %d, want about 200", n, rolls)
	}
	if len(seen) != 7 {
		t.Errorf("rolled %d distinct items, want 7: %v", len(seen), seen)
	}
}
