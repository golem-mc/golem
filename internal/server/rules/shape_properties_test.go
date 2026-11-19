package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestWithDefinedPropertiesKeepsUnspecifiedAndSkipsAbsent(t *testing.T) {
	state, _ := registry.FindState("minecraft:oak_stairs", map[string]string{
		"facing": "east", "half": "top", "waterlogged": "true",
	})
	want, _ := registry.FindState("minecraft:oak_stairs", map[string]string{
		"facing": "east", "half": "top", "waterlogged": "true", "shape": "inner_left",
	})
	for _, tc := range []struct {
		name    string
		defined map[string]string
		want    uint16
	}{
		{"empty", nil, state},
		{"shape only", map[string]string{"shape": "inner_left"}, want},
		{"unknown property", map[string]string{"missing": "true", "shape": "inner_left"}, registry.BlockOakStairs.DefaultState()},
		{"other block property", map[string]string{"powered": "true", "shape": "inner_left"}, want},
		{"invalid value", map[string]string{"half": "sideways", "shape": "inner_left"}, registry.BlockOakStairs.DefaultState()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WithDefinedProperties(state, tc.defined); got != tc.want {
				t.Errorf("state = %d, want %d", got, tc.want)
			}
		})
	}
	if got := WithDefinedProperties(0xffff, map[string]string{"half": "top"}); got != 0xffff {
		t.Errorf("invalid state = %d, want unchanged", got)
	}
}
