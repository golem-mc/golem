package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestLootMatchBlock(t *testing.T) {
	lower := []registry.LootStateProperty{{Key: "half", Value: "lower"}}
	upper := []registry.LootStateProperty{{Key: "half", Value: "upper"}}
	door := registry.MustState("minecraft:oak_door", map[string]string{"half": "lower"})
	cases := []struct {
		name     string
		cond     registry.LootMatchBlock
		hasBlock bool
		want     bool
	}{
		{"match", registry.LootMatchBlock{Blocks: []string{"minecraft:oak_door"}, Properties: lower}, true, true},
		{"wrong block", registry.LootMatchBlock{Blocks: []string{"minecraft:stone"}}, true, false},
		{"wrong property", registry.LootMatchBlock{Blocks: []string{"minecraft:oak_door"}, Properties: upper}, true, false},
		{"no block in play", registry.LootMatchBlock{Blocks: []string{"minecraft:oak_door"}}, false, false},
		{"state without blocks", registry.LootMatchBlock{Properties: lower}, true, true},
		{"state without blocks, wrong", registry.LootMatchBlock{Properties: upper}, true, false},
		{"nothing set", registry.LootMatchBlock{}, true, true},
		{"empty tag", registry.LootMatchBlock{Blocks: []string{}}, true, false},
	}
	for _, c := range cases {
		r := &lootRoller{ctx: &LootContext{BlockState: door, HasBlock: c.hasBlock}}
		if got := r.condition(c.cond); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
