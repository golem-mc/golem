package terrain

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

func TestNetherBiomeLastResultTie(t *testing.T) {
	t.Parallel()
	r := NewRandomState(Nether, 0).Router
	// Vanilla's saved quart column (-95,29) is crimson forest. Its temperature
	// quantizes to 2000, exactly halfway between the two climate entries.
	if got := NetherBiomeAt(r, -95, 0, 29, levelgen.CrimsonForest); got != levelgen.CrimsonForest {
		t.Fatalf("cached crimson candidate lost tie: %v", got)
	}
	if got := NetherBiomeAt(r, -95, 0, 29, levelgen.NetherWastes); got != levelgen.NetherWastes {
		t.Fatalf("cached wastes candidate lost tie: %v", got)
	}
	if got := NetherBiomeAt(r, -95, 0, 28, levelgen.NetherWastes); got != levelgen.CrimsonForest {
		t.Fatalf("non-nearest candidate won: %v", got)
	}
}
