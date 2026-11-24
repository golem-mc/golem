package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// lodestoneWorld is a stub LodestoneWorld holding the lodestones in its blocks.
type lodestoneWorld struct {
	stubBlocks
}

func (w lodestoneWorld) LodestoneAt(x, y, z int32) bool {
	return registry.BlockOf(w.BlockAt(x, y, z)) == registry.BlockLodestone
}

// TestLodestoneLost checks when a tracked compass loses its target.
func TestLodestoneLost(t *testing.T) {
	const dim = "minecraft:overworld"
	target := func(y int32) protocol.LodestoneTracker {
		return protocol.LodestoneTracker{Tracked: true,
			Target: &protocol.LodestoneTarget{Dimension: dim, X: 1, Y: y, Z: 2}}
	}
	stone := lodestoneWorld{stubBlocks: stubBlocks{{1, 64, 2}: stateOf(t, "minecraft:lodestone")}}
	empty := lodestoneWorld{stubBlocks: stubBlocks{}}
	far := func(x, z int32) protocol.LodestoneTracker {
		return protocol.LodestoneTracker{Tracked: true,
			Target: &protocol.LodestoneTarget{Dimension: dim, X: x, Y: 64, Z: z}}
	}
	for _, tc := range []struct {
		name    string
		w       lodestoneWorld
		tracker protocol.LodestoneTracker
		dim     string
		want    bool
	}{
		{"standing lodestone", stone, target(64), dim, false},
		{"broken lodestone", empty, target(64), dim, true},
		{"unsaved chunk", empty, target(64), dim, true},
		{"out of bounds", stone, target(400), dim, true},
		{"past the east edge", stone, far(30000000, 0), dim, true},
		{"past the north edge", stone, far(0, -30000001), dim, true},
		{"on the west edge, no lodestone", empty, far(-30000000, 29999999), dim, true},
		{"other dimension", empty, target(64), "minecraft:the_nether", false},
		{"untracked", empty, protocol.LodestoneTracker{Target: target(64).Target}, dim, false},
		{"no target", empty, protocol.LodestoneTracker{Tracked: true}, dim, false},
	} {
		if got := LodestoneLost(tc.w, tc.tracker, tc.dim, -64, 320); got != tc.want {
			t.Errorf("%s: LodestoneLost = %v, want %v", tc.name, got, tc.want)
		}
	}
}
