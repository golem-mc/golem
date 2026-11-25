package blocks

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestDoublePlantShapeUpdateChecksSurvival checks the survival question a
// tall grass half answers to a shape update: a lower half on stone breaks
// when a side neighbour is written or its upper half is placed, and one on
// dirt stays; a neighbour notification alone breaks nothing.
// vanilla: DoublePlantBlock.updateShape, VegetationBlock.updateShape
func TestDoublePlantShapeUpdateChecksSurvival(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()

	state := func(name string, props map[string]string) uint16 {
		s, ok := registry.FindState(name, props)
		if !ok {
			t.Fatalf("the registry has no %s with %v", name, props)
		}
		return s
	}
	lower := state("minecraft:tall_grass", map[string]string{"half": "lower"})

	for i, tc := range []struct {
		name    string
		floor   string
		command string
		broken  bool
	}{
		{"side write on stone", "minecraft:stone", "setblock 2 100 %d stone", true},
		{"side write on dirt", "minecraft:dirt", "setblock 2 100 %d stone", false},
		{"upper placed on stone", "minecraft:stone", "setblock 1 101 %d tall_grass[half=upper]", true},
		{"upper placed on dirt", "minecraft:dirt", "setblock 1 101 %d tall_grass[half=upper]", false},
		{"side air destroy on air", "minecraft:stone", "setblock 2 100 %d air destroy", false},
	} {
		z := int32(4 * i)
		srv.SetBlock(1, 99, z, state(tc.floor, nil))
		srv.SetBlock(1, 100, z, lower)
		c.ChatCommand(fmt.Sprintf(tc.command, z))
		c.Settle()
		got := srv.BlockAt(1, 100, z)
		if tc.broken && got != 0 {
			t.Errorf("%s: tall grass = %d, want air", tc.name, got)
		}
		if !tc.broken && got != lower {
			t.Errorf("%s: tall grass = %d, want %d", tc.name, got, lower)
		}
	}
}
