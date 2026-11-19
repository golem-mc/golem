package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func mossProps(t *testing.T, s uint16) map[string]string {
	t.Helper()
	_, raw, _ := registry.BlockStateName(s)
	return registry.ParseStateProps(raw)
}

// TestMossyCarpetSidesGrowTallAndTopperLosesBaselessSides covers the tall
// side under a topper and the topper losing a side its base lacks.
func TestMossyCarpetSidesGrowTallAndTopperLosesBaselessSides(t *testing.T) {
	stone, _ := registry.FindState("minecraft:stone", nil)
	topper, _ := registry.FindState(paleMossCarpet, map[string]string{
		"bottom": "false", "north": "none", "south": "none", "east": "low", "west": "none",
	})
	w := testWorld{{1, 0, 0}: stone, {1, 1, 0}: stone, {0, 1, 0}: topper}
	base := world.MossyCarpetState(w, registry.BlockStates[paleMossCarpet], 0, 0, 0, true)
	if p := mossProps(t, base); p["east"] != "tall" || p["west"] != "none" {
		t.Errorf("base under a topper has %v, want east=tall", p)
	}
	w[[3]int32{0, 0, 0}] = base
	got := world.MossyCarpetState(w, topper, 0, 1, 0, false)
	if p := mossProps(t, got); p["east"] != "low" {
		t.Errorf("topper over a tall base has %v, want east=low", p)
	}
	delete(w, [3]int32{1, 0, 0})
	nobase := world.MossyCarpetState(w, base, 0, 0, 0, false)
	w[[3]int32{0, 0, 0}] = nobase
	if p := mossProps(t, world.MossyCarpetState(w, topper, 0, 1, 0, false)); p["east"] != "none" {
		t.Errorf("topper over a base without east has %v, want east=none", p)
	}
}
