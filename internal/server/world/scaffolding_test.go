package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The scaffolding, engine-side: the distance a piece stands at following the
// blocks around it — sturdy ground, the column below, the closest side one
// past itself — and the placement survival gate that refuses the ceiling.

// scaffoldingTestState resolves a scaffolding state, failing the test when
// the registry has none with the properties.
func scaffoldingTestState(t *testing.T, overrides map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:scaffolding", overrides)
	if !ok {
		t.Fatalf("the registry has no scaffolding with %v", overrides)
	}
	return state
}

// checkScaffold asserts the scaffolding at a position carries exactly the
// distance and bottom given.
func checkScaffold(t *testing.T, w *World, x, y, z int32, distance string, bottom string, label string) {
	t.Helper()
	name, raw, ok := registry.BlockStateName(w.BlockAt(x, y, z))
	if !ok || name != "minecraft:scaffolding" {
		t.Fatalf("%s: block at (%d, %d, %d) = %d, not scaffolding",
			label, x, y, z, w.BlockAt(x, y, z))
	}
	props := registry.ParseStateProps(raw)
	if props["distance"] != distance || props["bottom"] != bottom {
		t.Errorf("%s: scaffolding at (%d, %d, %d) carries distance=%s bottom=%s, want distance=%s bottom=%s",
			label, x, y, z, props["distance"], props["bottom"], distance, bottom)
	}
}

// TestScaffoldingDistanceFollowsTheBlocksAroundIt checks the walk the
// stability visit recomputes: a piece stands at zero on anything with a
// sturdy top face, inherits the distance of the column below it at most one
// past the closest side, and a piece over air hangs at one past the piece
// beside it — the state each was set down with counting for nothing once
// the walk has spoken.
// vanilla: ScaffoldingBlock.getDistance
func TestScaffoldingDistanceFollowsTheBlocksAroundIt(t *testing.T) {
	w, p := newPhysicsWorld(t)
	// Every piece arrives carrying a distance the blocks around it do not
	// give, the placement of each booking its own stability visit over the
	// distance of the last.
	scaffold := scaffoldingTestState(t, map[string]string{"distance": "5"})
	setBlock(t, w, p, 0, GroundLevel, 0, scaffold)
	setBlock(t, w, p, 0, GroundLevel+1, 0, scaffold)
	setBlock(t, w, p, 0, GroundLevel+2, 0, scaffold)
	setBlock(t, w, p, 1, GroundLevel+2, 0, scaffold)

	var clock int64
	tickToQuiet(t, p, &clock, 100)

	// The stone under the first holds it at zero, the column above inherits
	// it, and the side over air hangs one past the top of the column.
	checkScaffold(t, w, 0, GroundLevel, 0, "0", "false", "on the ground")
	checkScaffold(t, w, 0, GroundLevel+1, 0, "0", "false", "on the column")
	checkScaffold(t, w, 0, GroundLevel+2, 0, "0", "false", "on the column's top")
	checkScaffold(t, w, 1, GroundLevel+2, 0, "1", "true", "over air beside it")
}
