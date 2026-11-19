package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// fakeLevel is a LootLevel with fixed answers.
type fakeLevel struct {
	raining, thundering bool
	ticks               int64
	scores              map[string]int32
	env                 float32
	stored              map[string]nbt.Tag // by storage + "/" + path
}

func (l fakeLevel) IsRaining() bool    { return l.raining }
func (l fakeLevel) IsThundering() bool { return l.thundering }
func (l fakeLevel) ClockTicks(clock string) (int64, bool) {
	return l.ticks, clock == "minecraft:overworld"
}
func (l fakeLevel) Score(holder, objective string) (int32, bool) {
	v, ok := l.scores[holder+"/"+objective]
	return v, ok
}
func (l fakeLevel) EnvironmentFloat(string) float32 { return l.env }
func (l fakeLevel) StorageNumber(storage, path string) (nbt.Tag, bool) {
	t, ok := l.stored[storage+"/"+path]
	return t, ok
}

func (l fakeLevel) Storage(string) nbt.Compound { return nil }

func TestLootLevelConditions(t *testing.T) {
	c := func(v float64) registry.LootNumber { return registry.LootConstant{Value: v} }
	day := registry.LootRange{Min: c(0), Max: c(12000)}
	level := fakeLevel{raining: true, ticks: 3*24000 + 6000, scores: map[string]int32{"Steve/pts": 5}, env: 15}
	ctx := &LootContext{Level: level, This: LootEntity{Type: "minecraft:player", ScoreHolder: "Steve"}}
	cases := []struct {
		name string
		cond registry.LootCondition
		ctx  *LootContext
		want bool
	}{
		{"time in the period", registry.LootTimeCheck{Clock: "minecraft:overworld", Period: 24000, Value: day}, ctx, true},
		{"time without a period", registry.LootTimeCheck{Clock: "minecraft:overworld", Value: day}, ctx, false},
		{"unknown clock", registry.LootTimeCheck{Clock: "minecraft:nope", Period: 24000, Value: day}, ctx, false},
		{"raining", registry.LootWeatherCheck{Raining: new(true)}, ctx, true},
		{"thundering", registry.LootWeatherCheck{Raining: new(true), Thundering: new(true)}, ctx, false},
		{"no level", registry.LootWeatherCheck{}, &LootContext{}, false},
		{"score in range", registry.LootEntityScores{Entity: "this", Scores: []registry.LootScoreRange{{Objective: "pts", Range: registry.LootRange{Exact: c(5)}}}}, ctx, true},
		{"score missing", registry.LootEntityScores{Entity: "this", Scores: []registry.LootScoreRange{{Objective: "x", Range: registry.LootRange{}}}}, ctx, false},
		{"no attacker", registry.LootEntityScores{Entity: "attacker"}, ctx, false},
		{"environment equal", registry.LootEnvironmentCheck{Attribute: "minecraft:gameplay/sky_light_level", Value: 15}, ctx, true},
		{"environment differs", registry.LootEnvironmentCheck{Attribute: "minecraft:gameplay/sky_light_level", Value: 4}, ctx, false},
		{"enchantment active absent", registry.LootEnchantmentActive{Active: false}, ctx, false},
		{"int value", registry.LootValueCheck{Value: c(4), Range: registry.LootRange{Max: c(4)}}, ctx, true},
		{"float value", registry.LootValueCheck{Value: c(0.5), Range: registry.LootRange{Min: c(0.75)}, Float: true}, ctx, false},
	}
	for _, tc := range cases {
		if got := TestLootCondition(tc.cond, tc.ctx, rand.New(rand.NewSource(1))); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// fakeLevelWorld is a LevelWorld whose every attribute reads env.
type fakeLevelWorld struct{ env float32 }

func (fakeLevelWorld) IsRaining() bool    { return false }
func (fakeLevelWorld) IsThundering() bool { return false }
func (fakeLevelWorld) ClockTime(string) (int64, int64, bool) {
	return 0, 0, false
}
func (w fakeLevelWorld) EnvironmentFloat(string, float64, float64, float64) float32 { return w.env }

func TestLevelWithoutOriginEnvironment(t *testing.T) {
	w := fakeLevelWorld{env: 0.5}
	const positional = "minecraft:gameplay/creature_world_gen_spawn_probability" // default 0.1
	const dimensional = "minecraft:gameplay/sky_light_level"
	if got := LevelWithoutOrigin(w, nil).EnvironmentFloat(positional); got != 0.1 {
		t.Errorf("positional without origin = %v, want the default 0.1", got)
	}
	if got := LevelWithoutOrigin(w, nil).EnvironmentFloat(dimensional); got != 0.5 {
		t.Errorf("dimensional without origin = %v, want the world's 0.5", got)
	}
	if got := LevelAt(w, nil, 0, 0, 0).EnvironmentFloat(positional); got != 0.5 {
		t.Errorf("positional at an origin = %v, want the world's 0.5", got)
	}
}
