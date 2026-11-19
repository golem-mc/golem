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

func TestACatalystBeyondEightBlocksTakesNoDeath(t *testing.T) {
	w, p := sculkTestPhysics(t)
	catalyst, _ := registry.FindState(sculkCatalyst, map[string]string{"bloom": "false"})
	setBlock(t, w, p, 8, GroundLevel+1, 8, catalyst)
	asked := false
	if taken, _ := p.SculkCatalystDeath(8.5, GroundLevel+1, 17.5, func() int { asked = true; return 5 }); taken || asked {
		t.Error("a death nine blocks away was taken")
	}
	if taken, _ := p.SculkCatalystDeath(8.5, GroundLevel+1, 16.5, func() int { return 5 }); !taken {
		t.Error("a death eight blocks away was not taken")
	}
}

// Two cursors on one cell become one; past the cap of 1000 they stay two.
func TestCursorsOnOneCellMergeUnderTheCap(t *testing.T) {
	cases := []struct {
		name    string
		charges []int
		want    []int
	}{
		{"under the cap", []int{10, 20}, []int{30}},
		{"over the cap", []int{600, 700}, []int{600, 700}},
		// The third lands on the smaller of the two, as vanilla's map
		// holds the cursor with the lower charge after a failed merge.
		{"merge target is the smaller", []int{700, 600, 300}, []int{700, 900}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, p := sculkTestPhysics(t)
			origin := blockKey{8, GroundLevel + 1, 8}
			catalyst, _ := registry.FindState(sculkCatalyst, map[string]string{"bloom": "false"})
			setBlock(t, w, p, origin.x, origin.y, origin.z, catalyst)
			for _, c := range tc.charges {
				p.catalysts[origin] = append(p.catalysts[origin],
					chargeCursor{x: 9, y: GroundLevel + 1, z: 9, charge: c, updateDelay: 5})
			}
			var changes []BlockChange
			p.updateCursors(origin, &changes)
			var got []int
			for _, c := range p.catalysts[origin] {
				got = append(got, c.charge)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("cursors after the merge: %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cursors after the merge: %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// A growth on sculk is a sensor or a shrieker that cannot summon, and plays
// the block's own place sound.
func TestSculkGrowthSoundAndShriekerCannotSummon(t *testing.T) {
	w, p := sculkTestPhysics(t)
	sounds := map[string]bool{}
	var changes []BlockChange
	for i := range int32(200) {
		x, z := i%16, i/16
		p.levelSpreader(blockKey{}, &changes).growth(x, GroundLevel+1, z)
		name, props, _ := registry.BlockStateName(w.BlockAt(x, GroundLevel+1, z))
		switch name {
		case "minecraft:sculk_shrieker":
			if registry.ParseStateProps(props)["can_summon"] != "false" {
				t.Fatalf("a shrieker grown by a cursor has can_summon %s", registry.ParseStateProps(props)["can_summon"])
			}
		case "minecraft:sculk_sensor":
		default:
			t.Fatalf("growth placed %s", name)
		}
	}
	for _, e := range p.effects {
		sounds[e.SoundID.Name()] = true
	}
	for _, want := range []string{"minecraft:block.sculk_sensor.place", "minecraft:block.sculk_shrieker.place"} {
		if !sounds[want] {
			t.Errorf("no %s among %v", want, sounds)
		}
		if _, ok := registry.SoundEventID(want); !ok {
			t.Errorf("%s is not a sound event", want)
		}
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
