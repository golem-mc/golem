package rules

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestTradeDyesColourOneItemAndFilterUndyedResults(t *testing.T) {
	id, _ := registry.ItemID("minecraft:leather_chestplate")
	stack := protocol.ItemStack{ItemID: id, Count: 4}
	filter := registry.LootFiltered{Predicate: registry.LootItemPredicate{HasDyedColor: true}, OnFail: registry.LootDiscard{}}
	for _, count := range []float64{0, 1} {
		got, cost := ApplyTradeModifiers([]registry.LootFunction{
			registry.LootSetRandomDyes{Count: registry.LootConstant{Value: count}}, filter,
		}, stack, rand.New(rand.NewSource(1)), nil)
		if cost != 0 {
			t.Fatal("dyeing added a price surcharge")
		}
		if count == 0 {
			if !got.Empty() {
				t.Fatal("an undyed result passed its filter")
			}
			continue
		}
		// Seed 1 picks orange, DyeColor.ORANGE's texture colour.
		if rgb, ok := got.DyedColorOf(); !ok || rgb != 16351261 || got.Count != 1 {
			t.Fatalf("dyed stack %+v, RGB %x, present %v", got, rgb, ok)
		}
	}
	if stack.HasDyedColor() || stack.Count != 4 {
		t.Fatal("changed the input stack")
	}
	stack.SetDyedColor(0xFFFFFF)
	got := ApplyItemModifier(registry.LootSetRandomDyes{Count: registry.LootConstant{Value: 1}}, stack, rand.New(rand.NewSource(1)), nil)
	// White and orange: channel averages (252,191,142), average intensity 252.
	if rgb, _ := got.DyedColorOf(); rgb != 0xFCBF8E {
		t.Fatalf("white + orange = %x", rgb)
	}
	if rgb, _ := stack.DyedColorOf(); rgb != 0xFFFFFF {
		t.Fatal("overwrote the input's colour")
	}
}

// TestTradeAddSumsIntInputs checks the dye count's add: an exact int sum of
// its inputs, where an overflow throws and reads as 0.
// vanilla: ints.Sum.getIntUnsafe, ContextIntProvider.getInt
func TestTradeAddSumsIntInputs(t *testing.T) {
	r := lootRoller{rng: rand.New(rand.NewSource(1))}
	c := func(v float64) registry.LootNumber { return registry.LootConstant{Value: v} }
	if got := r.numInt(registry.LootSum{}); got != 0 {
		t.Fatalf("empty add = %d", got)
	}
	for _, p := range []float64{0, 1} {
		sum := registry.LootSum{Summands: []registry.LootNumber{c(1), registry.LootBinomial{N: c(2), P: c(p)}}}
		if got := r.numInt(sum); got != 1+int32(2*p) {
			t.Fatalf("p=%v: dye count %d", p, got)
		}
	}
	over := registry.LootSum{Summands: []registry.LootNumber{c(2147483647), c(1)}}
	if _, err := r.intOf(over); err == nil || err.Error() != "Value 2147483648 can't be safely converted to int" {
		t.Fatalf("overflow error %v", err)
	}
	if got := r.numInt(over); got != 0 {
		t.Fatalf("overflowing add = %d, want 0", got)
	}
}

func TestTradePotionsPreserveCustomContentsAndRespectOptions(t *testing.T) {
	id, _ := registry.ItemID("minecraft:tipped_arrow")
	stack := protocol.ItemStack{ItemID: id, Count: 5}
	colour, name := int32(0x123456), "custom"
	original := protocol.PotionContents{Potion: registry.MustPotion("minecraft:water"), CustomColor: &colour, CustomName: &name,
		CustomEffects: []protocol.PotionEffectSpec{{ID: registry.MustMobEffect("minecraft:poison"), Duration: 40}}}
	stack.SetPotionContents(original)
	for _, fn := range []registry.LootFunction{
		registry.LootSetPotion{Potion: "minecraft:long_invisibility"},
		registry.LootSetRandomPotion{Options: []string{"minecraft:long_invisibility"}},
	} {
		got, _ := ApplyTradeModifiers([]registry.LootFunction{fn}, stack, rand.New(rand.NewSource(1)), nil)
		want := original
		want.Potion = registry.MustPotion("minecraft:long_invisibility")
		contents, _ := got.PotionContentsOf()
		if !reflect.DeepEqual(contents, want) || got.Count != 5 {
			t.Fatalf("contents %+v, count %d", contents, got.Count)
		}
		if contents, _ := stack.PotionContentsOf(); !reflect.DeepEqual(contents, original) {
			t.Fatal("changed the input's potion")
		}
	}
	for _, options := range [][]string{nil, {}, {"minecraft:healing", "minecraft:poison"}} {
		seen := map[int32]bool{}
		for seed := range int64(100) {
			got := ApplyItemModifier(registry.LootSetRandomPotion{Options: options}, stack, rand.New(rand.NewSource(seed)), nil)
			contents, _ := got.PotionContentsOf()
			if options != nil && len(options) == 0 {
				if contents.Potion != original.Potion {
					t.Fatal("empty options changed the potion")
				}
			} else {
				if _, ok := registry.PotionName(contents.Potion); !ok {
					t.Fatal("chose an unknown potion")
				}
				if len(options) > 0 && contents.Potion != registry.MustPotion(options[0]) && contents.Potion != registry.MustPotion(options[1]) {
					t.Fatal("chose outside the options")
				}
				seen[contents.Potion] = true
			}
		}
		if len(options) == 2 && len(seen) != 2 || options == nil && len(seen) < 3 {
			t.Fatalf("choices %v", seen)
		}
	}
}

func TestTradeStewAppendsEffectsAndConvertsOnlyLastingDurations(t *testing.T) {
	id, _ := registry.ItemID("minecraft:suspicious_stew")
	stack := protocol.ItemStack{ItemID: id, Count: 1}
	first := protocol.StewEffect{Effect: registry.MustMobEffect("minecraft:blindness"), Duration: 10}
	stack.SetSuspiciousStewEffects([]protocol.StewEffect{first})
	for _, name := range []string{"minecraft:night_vision", "minecraft:saturation"} {
		fn := registry.LootSetStewEffect{Effects: []registry.LootStewEffect{{Effect: name, Duration: registry.LootConstant{Value: 7}}}}
		got, _ := ApplyTradeModifiers([]registry.LootFunction{fn}, stack, rand.New(rand.NewSource(1)), nil)
		effects := got.SuspiciousStewEffectsOf()
		duration := int32(140)
		if name == "minecraft:saturation" {
			duration = 7
		}
		if len(effects) != 2 || effects[0] != first || effects[1] != (protocol.StewEffect{Effect: registry.MustMobEffect(name), Duration: duration}) {
			t.Fatalf("effects %v", effects)
		}
		if !reflect.DeepEqual(stack.SuspiciousStewEffectsOf(), []protocol.StewEffect{first}) {
			t.Fatal("changed the input's effects")
		}
		bread, _ := registry.ItemID("minecraft:bread")
		got = ApplyItemModifier(fn, protocol.ItemStack{ItemID: bread, Count: 1}, rand.New(rand.NewSource(1)), nil)
		if len(got.SuspiciousStewEffectsOf()) != 0 {
			t.Fatal("gave bread a stew effect")
		}
	}
}

func TestTradeComponentModifiersAlsoRunInLootTables(t *testing.T) {
	functions := []registry.LootFunction{
		registry.LootSetRandomDyes{Count: registry.LootConstant{Value: 1}},
		registry.LootFiltered{Predicate: registry.LootItemPredicate{HasDyedColor: true}, OnFail: registry.LootDiscard{}},
		registry.LootSetRandomPotion{Options: []string{"minecraft:healing"}},
	}
	got := RollLootTable("test", entryTable("minecraft:leather_chestplate", functions), &LootContext{}, rand.New(rand.NewSource(1)))
	if len(got) != 1 || !got[0].HasDyedColor() {
		t.Fatalf("loot %v", got)
	}
	if c, ok := got[0].PotionContentsOf(); !ok || c.Potion != registry.MustPotion("minecraft:healing") {
		t.Fatalf("potion %+v, present %v", c, ok)
	}
}
