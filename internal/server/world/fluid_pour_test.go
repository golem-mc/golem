package world

import (
	"fmt"
	"math/bits"
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

type fluidPourTestReader struct {
	cells   map[[3]int32]uint16
	reads   [][3]int32
	convert bool
}

func (r *fluidPourTestReader) BlockAt(x, y, z int32) uint16 {
	pos := [3]int32{x, y, z}
	r.reads = append(r.reads, pos)
	if state, ok := r.cells[pos]; ok {
		return state
	}
	return airState
}

func (r *fluidPourTestReader) BoolRule(GameRuleIndex) bool { return r.convert }

func TestDownwardLiquidMatchesNewLiquid(t *testing.T) {
	waterloggedFence, ok := registry.WithBool(registry.BlockOakFence.DefaultState(), registry.PropWaterlogged, true)
	if !ok {
		t.Fatal("oak fence has no waterlogged state")
	}
	const x, y, z = 8, 79, 8
	floor := [3]int32{x, y - 1, z}
	for _, fluid := range []struct {
		name        string
		kind, other fluidKind
	}{
		{"water", fluidWater, fluidLava},
		{"lava", fluidLava, fluidWater},
	} {
		b := fluidBehaviours[fluid.kind]
		falling := fluidStateAt(fluid.kind, 8)
		source := fluidStateAt(fluid.kind, 0)
		otherSource := fluidStateAt(fluid.other, 0)
		type pourCase struct {
			name          string
			origin, floor uint16
			floorOnly     bool
		}
		cases := []pourCase{
			{"air floor", falling, airState, true},
			{"solid floor", falling, stoneState, false},
			{"source below", falling, source, false},
			{"falling fluid below", falling, falling, true},
			{"other source below", falling, otherSource, true},
			{"origin cleared", airState, airState, false},
			{"origin replaced", otherSource, airState, false},
		}
		if fluid.kind == fluidWater {
			cases = append(cases, pourCase{"waterlogged origin", waterloggedFence, airState, true})
		}
		for _, convert := range []bool{false, true} {
			for _, tc := range cases {
				t.Run(fmt.Sprintf("%s/conversion=%t/%s", fluid.name, convert, tc.name), func(t *testing.T) {
					if !canMaybePassThrough(fluid.kind, tc.origin, 1, false, airState, fluidState{}) {
						t.Fatal("fixture does not allow a downward pour")
					}
					r := &fluidPourTestReader{convert: convert, cells: map[[3]int32]uint16{
						{x, y + 1, z}: tc.origin,
						{x, y, z - 1}: source,
						{x + 1, y, z}: source,
						floor:         tc.floor,
					}}
					want := newLiquidAt(r, x, y, z, airState, b)
					r.reads = nil
					got := downwardLiquidAt(r, x, y, z, tc.origin, airState, b)
					if got != want {
						t.Errorf("destination fluid = %+v, want %+v", got, want)
					}
					if convert && (tc.floor == stoneState || tc.floor == source) && !got.isSource() {
						t.Errorf("supported destination fluid = %+v, want a source", got)
					}
					if tc.floorOnly && !slices.Equal(r.reads, [][3]int32{floor}) {
						t.Errorf("destination read %v, want only the floor %v", r.reads, floor)
					}
				})
			}
		}
	}
}

func TestPourSurroundedBySources(t *testing.T) {
	const x, y, z = 8, 80, 8
	for _, kind := range []fluidKind{fluidWater, fluidLava} {
		other := fluidLava
		if kind == fluidLava {
			other = fluidWater
		}
		for mask := range 16 {
			r := &fluidPourTestReader{cells: make(map[[3]int32]uint16)}
			neighbors := [4][3]int32{}
			nonSources := [4]uint16{airState, fluidStateAt(kind, 8), fluidStateAt(other, 0), fluidStateAt(kind, 1)}
			for i, d := range horizontal4 {
				pos := [3]int32{x + d[0], y, z + d[1]}
				neighbors[i] = pos
				if mask&(1<<i) != 0 {
					r.cells[pos] = fluidStateAt(kind, 0)
				} else if mask != 0 {
					r.cells[pos] = nonSources[i]
				}
			}
			want := bits.OnesCount(uint(mask)) >= 3
			if got := pourSurroundedBySources(r, x, y, z, kind); got != want {
				t.Errorf("fluid %d, mask %04b: surrounded = %t, want %t", kind, mask, got, want)
			}
			wantReads := 4
			firstThree := bits.OnesCount(uint(mask & 7))
			switch {
			case mask&3 == 0:
				wantReads = 2
			case firstThree == 1 || firstThree == 3:
				wantReads = 3
			}
			if !slices.Equal(r.reads, neighbors[:wantReads]) {
				t.Errorf("fluid %d, mask %04b: read %v, want %v", kind, mask, r.reads, neighbors[:wantReads])
			}
		}
	}
}
