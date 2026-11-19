package rules

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
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

func TestDestroyProgressThresholds(t *testing.T) {
	// The client insta-mines at one and above; these sit on either side.
	if p := DestroyProgress(blockNamed("minecraft:oak_leaves"), mainHand(held(t, "minecraft:iron_hoe"))); p < 1 {
		t.Errorf("iron hoe on leaves = %v, want at least 1", p)
	}
	if p := DestroyProgress(blockNamed("minecraft:short_grass"), mainHand(held(t, ""))); !math.IsInf(float64(p), 1) {
		t.Errorf("hand on short grass = %v, want +Inf (hardness zero)", p)
	}
	if p := DestroyProgress(blockNamed("minecraft:bamboo"), mainHand(held(t, "minecraft:iron_sword"))); p < 1 {
		t.Errorf("sword on bamboo = %v, want at least 1", p)
	}
}

func TestDigEffects(t *testing.T) {
	eff := func(name string, amplifier int32) effect.Instance {
		return effect.New(registry.MustMobEffect(name), 100, amplifier, false, false, false)
	}
	for _, tc := range []struct {
		name           string
		effects        []effect.Instance
		haste, fatigue int
	}{
		{"none", nil, 0, 0},
		{"haste III", []effect.Instance{eff("minecraft:haste", 2)}, 3, 0},
		{"conduit power II", []effect.Instance{eff("minecraft:conduit_power", 1)}, 2, 0},
		{"the higher of haste and conduit power", []effect.Instance{
			eff("minecraft:haste", 1), eff("minecraft:conduit_power", 3),
		}, 4, 0},
		{"mining fatigue II", []effect.Instance{eff("minecraft:mining_fatigue", 1)}, 0, 2},
		{"haste and mining fatigue", []effect.Instance{
			eff("minecraft:haste", 0), eff("minecraft:mining_fatigue", 0),
		}, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			haste, fatigue := DigEffects(tc.effects)
			if haste != tc.haste || fatigue != tc.fatigue {
				t.Errorf("DigEffects = (%d, %d), want (%d, %d)", haste, fatigue, tc.haste, tc.fatigue)
			}
		})
	}
}

func TestDestroyStageIsTenthsOfProgress(t *testing.T) {
	for _, c := range []struct {
		progress float32
		want     int32
	}{{0, 0}, {0.09, 0}, {0.1, 1}, {0.55, 5}, {0.99, 9}, {1, 10}} {
		if got := DestroyStage(c.progress); got != c.want {
			t.Errorf("DestroyStage(%v) = %d, want %d", c.progress, got, c.want)
		}
	}
}
