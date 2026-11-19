package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestFrogAcceptsLandingCaveAirAboveTrapdoor: cave air counts as air, so a
// trapdoor below it is an acceptable landing.
func TestFrogAcceptsLandingCaveAirAboveTrapdoor(t *testing.T) {
	w := testWorld{blocks: &blockGrid{}}
	w.blocks.set(0, 64, 0, registry.BlockOakTrapdoor.DefaultState())
	w.blocks.set(0, 65, 0, registry.BlockCaveAir.DefaultState())
	if ok, _ := frogAcceptsLanding(w, Waypoint{X: 0, Y: 65, Z: 0}); !ok {
		t.Fatal("cave air above a trapdoor refused")
	}
}
