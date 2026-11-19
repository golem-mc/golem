package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func sculkTestPhysics(t *testing.T) (*World, *Physics) {
	t.Helper()
	w := newTestWorld(t, newFlatGenerator("minecraft:plains", []flatLayer{{state: stoneState, height: GroundLevel - worldMinY}}))
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{X: 0, Z: 0})
	p := NewPhysics(w)
	p.rng = rand.New(rand.NewSource(3))
	return w, p
}

func TestACatalystTakesAtMostThirtyTwoCursors(t *testing.T) {
	w, p := sculkTestPhysics(t)
	catalyst, ok := registry.FindState(sculkCatalyst, map[string]string{"bloom": "false"})
	if !ok {
		t.Fatal("no catalyst state")
	}
	setBlock(t, w, p, 8, GroundLevel+1, 8, catalyst)
	for range 40 {
		if taken, _ := p.SculkCatalystDeath(9.5, GroundLevel+1, 9.5, func() int { return 5 }); !taken {
			t.Fatal("the catalyst took no death")
		}
	}
	if got := len(p.catalysts[blockKey{8, GroundLevel + 1, 8}]); got != sculkMaxCursors {
		t.Errorf("after 40 deaths the catalyst holds %d cursors, want %d", got, sculkMaxCursors)
	}
}

func TestCursorsRoundTripThroughNBTAndTheCodecLimitsHold(t *testing.T) {
	in := []chargeCursor{
		{x: 1, y: -2, z: 3, charge: 40, decayDelay: 0, updateDelay: 5, facings: 1<<1 | 1<<4, hasFacings: true},
		{x: 4, y: 5, z: 6, charge: 7, decayDelay: 1},
	}
	got := cursorsFromNBT(nbt.Compound{"cursors": cursorsToNBT(in)})
	if len(got) != 2 || got[0] != in[0] || got[1] != in[1] {
		t.Errorf("round trip gave %+v, want %+v", got, in)
	}
	over := make([]chargeCursor, sculkMaxCursors+1)
	if got := cursorsFromNBT(nbt.Compound{"cursors": cursorsToNBT(over)}); got != nil {
		t.Errorf("a list of %d cursors loaded %d", len(over), len(got))
	}
	if got := cursorsFromNBT(nbt.Compound{"cursors": cursorsToNBT([]chargeCursor{{charge: sculkMaxCharge + 1}})}); got != nil {
		t.Errorf("a charge above %d loaded", sculkMaxCharge)
	}
}
