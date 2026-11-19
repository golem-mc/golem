package command

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
)

func TestSelectorUnfilteredPredicateAllocations(t *testing.T) {
	sel, err := parsedSelector("@s", true)
	if err != nil {
		t.Fatal(err)
	}
	var predicate func(Entity) bool
	allocs := testing.AllocsPerRun(100, func() {
		predicate = sel.predicate(nil, player.Position{})
	})
	if allocs != 0 {
		t.Fatalf("unfiltered predicate allocated %g times, want 0", allocs)
	}
	if !predicate(&forkBody{}) {
		t.Fatal("unfiltered predicate rejected an entity")
	}
}
