package rules

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestContextProvidersEvaluate(t *testing.T) {
	c := func(v float64) registry.LootNumber { return registry.LootConstant{Value: v} }
	level := fakeLevel{scores: map[string]int32{"Steve/pts": 5, "fixed/pts": 9}, env: 15,
		stored: map[string]nbt.Tag{"minecraft:s/a": nbt.Double(-2.7), "minecraft:s/b": nbt.Long(1 << 32)}}
	ctx := &LootContext{Level: level, This: LootEntity{Type: "minecraft:player", ScoreHolder: "Steve"}}
	ints := []struct {
		name string
		n    registry.LootNumber
		want int32
	}{
		{"score of this", registry.LootScore{Target: "this", Objective: "pts"}, 5},
		{"fixed score", registry.LootScore{Target: "fixed", Fixed: true, Objective: "pts"}, 9},
		{"absent holder falls back", registry.LootScore{Target: "attacker", Objective: "pts", Fallback: c(7)}, 7},
		{"missing score is zero", registry.LootScore{Target: "this", Objective: "none"}, 0},
		{"storage truncates", registry.LootStorage{Storage: "minecraft:s", Path: "a"}, -2},
		{"storage narrows a long", registry.LootStorage{Storage: "minecraft:s", Path: "b"}, 0},
		{"storage falls back", registry.LootStorage{Storage: "minecraft:s", Path: "x", Fallback: c(3)}, 3},
		{"floor_div", registry.LootBinary{Op: "floor_div", Left: c(-7), Right: c(2)}, -4},
		{"floor_mod", registry.LootBinary{Op: "floor_mod", Left: c(-7), Right: c(2)}, 1},
		{"pow", registry.LootBinary{Op: "pow", Left: c(3), Right: c(4)}, 81},
		{"avg", registry.LootAggregate{Op: "avg", Inputs: []registry.LootNumber{c(1), c(2), c(4)}}, 2},
		{"from_float truncates", registry.LootConvert{Input: c(-2.5)}, -2},
		{"div by zero reads 0", registry.LootBinary{Op: "div", Left: c(1), Right: c(0)}, 0},
		{"conditional off", registry.LootConditional{Condition: registry.LootInverted{Term: registry.LootAllOf{}}, OnTrue: c(1)}, 0},
		{"dispatcher", registry.LootDispatcher{Cases: []registry.LootDispatchCase{
			{Condition: registry.LootAnyOf{}, Value: c(1)}, {Condition: registry.LootAllOf{}, Value: c(2)}}, Default: c(3)}, 2},
		{"weighted single", registry.LootWeightedList{Values: []registry.LootNumber{c(4)}, Weights: []int64{1}}, 4},
	}
	for _, tc := range ints {
		r := lootRoller{ctx: ctx, rng: rand.New(rand.NewSource(1))}
		if got := r.numInt(tc.n); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
	floats := []struct {
		name string
		n    registry.LootNumber
		want float64
	}{
		{"float32 add", registry.LootSum{Summands: []registry.LootNumber{c(16777216), c(1), c(1)}}, 16777216},
		{"environment", registry.LootEnvAttribute{Attribute: "minecraft:gameplay/sky_light_level"}, 15},
		{"sqrt", registry.LootUnary{Op: "sqrt", Input: c(9)}, 3},
		{"round", registry.LootUnary{Op: "round", Input: c(-2.5)}, -2},
		{"length", registry.LootAggregate{Op: "length", Inputs: []registry.LootNumber{c(3), c(4)}}, 5},
		{"non-finite reads 0", registry.LootBinary{Op: "div", Left: c(1), Right: c(0)}, 0},
		{"from_int", registry.LootConvert{Input: c(3)}, 3},
	}
	for _, tc := range floats {
		r := lootRoller{ctx: ctx, rng: rand.New(rand.NewSource(1))}
		if got := r.numFloat(tc.n); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if v, err := EvalFloatProvider(registry.LootBinary{Op: "div", Left: c(1), Right: c(0)}, ctx, nil); err != nil || !math.IsInf(float64(v), 1) {
		t.Errorf("unsafe 1/0 = %v, %v", v, err)
	}
	if _, err := EvalIntProvider(registry.LootBinary{Op: "div", Left: c(1), Right: c(0)}, ctx, nil); err != ErrDivideByZero {
		t.Errorf("int 1/0 error %v", err)
	}
	for in, want := range map[float64]string{
		1e30:  "Value 9223372036854775807 can't be safely converted to int",
		-1e30: "Value -9223372036854775808 can't be safely converted to int",
	} {
		if _, err := EvalIntProvider(registry.LootConvert{Input: c(in)}, ctx, nil); err == nil || err.Error() != want {
			t.Errorf("from_float %v error %v, want %q", in, err, want)
		}
	}
}
