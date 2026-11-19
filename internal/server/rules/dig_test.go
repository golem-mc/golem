package rules

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// held is one plain stack of the item named, the empty hand for "".
func held(t *testing.T, item string) protocol.ItemStack {
	t.Helper()
	if item == "" {
		return protocol.ItemStack{}
	}
	id, ok := registry.ItemID(item)
	if !ok {
		t.Fatalf("the item registry has no %s", item)
	}
	return protocol.ItemStack{ItemID: id, Count: 1}
}

// mainHand is a digger on the ground holding the stack, its attributes at
// their bases.
func mainHand(stack protocol.ItemStack) Digger {
	return Digger{Held: stack, BlockBreakSpeed: 1, SubmergedMiningSpeed: 0.2, OnGround: true}
}

func TestDestroyProgressMatchesVanillasNumbers(t *testing.T) {
	efficiency := func(level int32) Digger {
		d := mainHand(held(t, "minecraft:diamond_pickaxe"))
		d.MiningEfficiency = float64(level*level + 1) // Efficiency's levels_squared + 1
		return d
	}
	offGround := mainHand(held(t, "minecraft:shears"))
	offGround.OnGround = false
	submerged := mainHand(held(t, "minecraft:shears"))
	submerged.EyesInWater = true
	aqua := submerged
	aqua.SubmergedMiningSpeed = 1 // Aqua Affinity's multiplier of 5
	haste := mainHand(held(t, "minecraft:shears"))
	haste.Haste = 2
	fatigue := mainHand(held(t, "minecraft:shears"))
	fatigue.MiningFatigue = 1
	fatigueIII := mainHand(held(t, "minecraft:iron_pickaxe"))
	fatigueIII.MiningFatigue = 3

	for _, tc := range []struct {
		name  string
		block string
		d     Digger
		want  float32
	}{
		// 15 / 0.2 / 30
		{"shears on leaves", "minecraft:oak_leaves", mainHand(held(t, "minecraft:shears")), 2.5},
		// 6 / 0.2 / 30, exactly one in float32
		{"iron hoe on leaves", "minecraft:oak_leaves", mainHand(held(t, "minecraft:iron_hoe")), 1},
		{"stone hoe on leaves", "minecraft:oak_leaves", mainHand(held(t, "minecraft:stone_hoe")), float32(4) / 0.2 / 30},
		// stone wants a pickaxe: / 100
		{"hand on stone", "minecraft:stone", mainHand(held(t, "")), float32(1) / 1.5 / 100},
		{"wooden pickaxe on stone", "minecraft:stone", mainHand(held(t, "minecraft:wooden_pickaxe")), float32(2) / 1.5 / 30},
		// a rail needs no tool, cobweb wants shears
		{"hand on rail", "minecraft:rail", mainHand(held(t, "")), float32(1) / 0.7 / 30},
		{"hand on cobweb", "minecraft:cobweb", mainHand(held(t, "")), float32(1) / 4 / 100},
		// 8 + 5*5+1
		{"efficiency V on stone", "minecraft:stone", efficiency(5), float32(34) / 1.5 / 30},
		{"efficiency II on netherrack", "minecraft:netherrack", efficiency(2), float32(13) / 0.4 / 30},
		// Efficiency adds nothing where the tool's speed is one.
		{"efficiency V on dirt", "minecraft:dirt", efficiency(5), float32(1) / 0.5 / 30},
		{"off the ground", "minecraft:oak_leaves", offGround, 0.5},
		{"eyes in water", "minecraft:oak_leaves", submerged, 0.5},
		{"eyes in water, aqua affinity", "minecraft:oak_leaves", aqua, 2.5},
		{"haste II", "minecraft:oak_leaves", haste, 3.5},
		{"mining fatigue I", "minecraft:oak_leaves", fatigue, 0.75},
		// 6 * 0.3^3 / 1.5 / 30
		{"mining fatigue III", "minecraft:stone", fatigueIII, float32(6*0.027) / 1.5 / 30},
		{"bedrock", "minecraft:bedrock", mainHand(held(t, "minecraft:netherite_pickaxe")), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DestroyProgress(blockNamed(tc.block), tc.d)
			if math.Abs(float64(got-tc.want)) > 1e-5 {
				t.Errorf("DestroyProgress = %v, want %v", got, tc.want)
			}
		})
	}
}
