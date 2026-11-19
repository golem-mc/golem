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
