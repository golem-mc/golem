package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func rabbitTypesIn(biome string, dice int) map[int32]int {
	counts := map[int32]int{}
	rng := rand.New(rand.NewSource(7))
	for range dice {
		counts[rabbitTypeFor(testBiome(biome), GoDice{rng})]++
	}
	return counts
}

// TestRabbitVariantFollowsTheBiome checks Rabbit.getRandomRabbitVariant:
// white and white splotched in the snowy biomes, gold in the desert, brown,
// salt and black elsewhere.
func TestRabbitVariantFollowsTheBiome(t *testing.T) {
	snow := rabbitTypesIn("minecraft:snowy_plains", 2000)
	if len(snow) != 2 || snow[rabbitWhite] < 1400 || snow[rabbitWhiteSplotched] == 0 {
		t.Errorf("snowy plains variants = %v, want white (80%%) and white splotched", snow)
	}
	if desert := rabbitTypesIn("minecraft:desert", 200); len(desert) != 1 || desert[rabbitGold] != 200 {
		t.Errorf("desert variants = %v, want gold only", desert)
	}
	plains := rabbitTypesIn("minecraft:plains", 2000)
	if len(plains) != 3 || plains[rabbitBrown] < 900 || plains[rabbitSalt] < 650 || plains[rabbitBlack] == 0 {
		t.Errorf("plains variants = %v, want brown 50%%, salt 40%%, black 10%%", plains)
	}
}

// TestARabbitDropsHideAndMeat checks the loot table's hide, raw meat and,
// for a burning body, cooked meat.
func TestARabbitDropsHideAndMeat(t *testing.T) {
	seen := map[string]bool{}
	for seed := range int64(300) {
		m := NewMob(TypeRabbit, 1, 0, 64, 0, seed)
		m.IgniteForTicks(100)
		for _, s := range m.RollDrops(rules.LootKiller{}, nil) {
			name, _ := registry.ItemName(s.ItemID)
			seen[name] = true
		}
	}
	for _, want := range []string{"minecraft:rabbit_hide", "minecraft:cooked_rabbit"} {
		if !seen[want] {
			t.Errorf("300 burning deaths never dropped %s (saw %v)", want, seen)
		}
	}
	if seen["minecraft:rabbit"] {
		t.Errorf("a burning rabbit dropped raw meat (saw %v)", seen)
	}
	raw := false
	for seed := range int64(300) {
		for _, s := range NewMob(TypeRabbit, 1, 0, 64, 0, seed).RollDrops(rules.LootKiller{}, nil) {
			if name, _ := registry.ItemName(s.ItemID); name == "minecraft:rabbit" {
				raw = true
			}
		}
	}
	if !raw {
		t.Error("300 plain deaths never dropped raw rabbit")
	}
}

// TestARabbitIsBredIntoEitherParentsVariant checks Rabbit.getBreedOffspring:
// mostly a parent's variant, and one breeding in twenty the biome's roll.
func TestARabbitIsBredIntoEitherParentsVariant(t *testing.T) {
	mother := NewMob(TypeRabbit, 1, 0, 64, 0, 1)
	mate := NewMob(TypeRabbit, 2, 0, 64, 0, 2)
	mother.RabbitType, mate.RabbitType = rabbitWhite, rabbitBlack
	parents, other := map[int32]int{}, 0
	for seed := range int64(2000) {
		mother.Rand = rand.New(rand.NewSource(seed))
		baby := NewMob(TypeRabbit, 3, 0, 64, 0, 3)
		mother.BreedRabbit(baby, mate, testBiome("minecraft:desert"))
		switch baby.RabbitType {
		case rabbitWhite, rabbitBlack:
			parents[baby.RabbitType]++
		case rabbitGold:
			other++
		default:
			t.Fatalf("baby type %d, want a parent's or the desert's gold", baby.RabbitType)
		}
	}
	if parents[rabbitWhite] < 700 || parents[rabbitBlack] < 700 {
		t.Errorf("parent variants = %v, want about half each", parents)
	}
	if other < 40 || other > 180 {
		t.Errorf("%d of 2000 took the biome's variant, want about one in twenty", other)
	}
}

// TestARabbitSunkInPowderSnowClimbsOnTop checks ClimbOnTopOfPowderSnowGoal.canUse:
// a walker that was in powder snow offers the goal while the cell above is
// open, and a cow, which is no walker, never does.
func TestARabbitSunkInPowderSnowClimbsOnTop(t *testing.T) {
	w := newTestWorld()
	snow := defaultState(t, "minecraft:powder_snow")
	stone := defaultState(t, "minecraft:stone")
	w.set(0, 64, 0, snow)
	tv := &tickView{w: w}

	rabbit := NewMob(TypeRabbit, 1, 0.5, 64, 0.5, 1)
	goal := &climbPowderSnowGoal{rabbit}
	if goal.CanStart(tv) {
		t.Error("a rabbit that was not in powder snow offers the climb")
	}
	rabbit.wasInPowderSnow = true
	if !goal.CanStart(tv) {
		t.Error("a sunk rabbit with open air above does not offer the climb")
	}
	w.set(0, 65, 0, stone)
	if goal.CanStart(tv) {
		t.Error("a rabbit under a stone ceiling offers the climb")
	}
	w.set(0, 65, 0, snow)
	if !goal.CanStart(tv) {
		t.Error("a rabbit under more powder snow does not offer the climb")
	}
	cow := NewMob(TypeCow, 2, 0.5, 64, 0.5, 1)
	cow.wasInPowderSnow = true
	if (&climbPowderSnowGoal{cow}).CanStart(tv) {
		t.Error("a cow offers the climb")
	}
}
