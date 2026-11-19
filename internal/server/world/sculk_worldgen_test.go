package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

type sculkMovementWorld map[blockKey]uint16

func (w sculkMovementWorld) get(x, y, z int32) uint16 {
	if state, ok := w[blockKey{x, y, z}]; ok {
		return state
	}
	return airState
}
func (w sculkMovementWorld) set(x, y, z int32, state uint16, _ bool) bool {
	w[blockKey{x, y, z}] = state
	return true
}
func (sculkMovementWorld) sound(int32, int32, int32, registry.Sound) {}
func (sculkMovementWorld) placedSculk(int32, int32, int32, uint16)   {}

type sculkMovementRandom struct{}

func (sculkMovementRandom) NextIntN(n int32) int32 { return n - 1 }
func (sculkMovementRandom) Perm(n int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	return order
}

func TestSculkWorldgenMovementBoundaryAndDischarge(t *testing.T) {
	for _, tc := range []struct {
		name           string
		start, target  blockKey
		worldGen, move bool
	}{
		{"positive boundary", blockKey{11, 0, 0}, blockKey{12, 0, 0}, true, true},
		{"negative boundary", blockKey{-11, 0, 0}, blockKey{-12, 0, 0}, true, true},
		{"outside boundary", blockKey{12, 0, 0}, blockKey{13, 0, 0}, true, false},
		{"diagonal outside circle", blockKey{11, 0, 4}, blockKey{12, 0, 4}, true, false},
		{"vertical at boundary", blockKey{12, 10, 0}, blockKey{12, 11, 0}, true, true},
		{"gameplay outside boundary", blockKey{12, 0, 0}, blockKey{13, 0, 0}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := sculkMovementWorld{tc.start: registry.BlockSculk.DefaultState(), tc.target: registry.BlockSculk.DefaultState()}
			tuning := worldGenSculkTuning
			tuning.worldGen = tc.worldGen
			s := sculkSpreader{w: w, rng: sculkMovementRandom{}, t: &tuning}
			cur := chargeCursor{x: tc.start.x, y: tc.start.y, z: tc.start.z, charge: 10}
			s.update(&cur, false)
			want := tc.start
			if tc.move {
				want = tc.target
			}
			if got := (blockKey{cur.x, cur.y, cur.z}); got != want || (cur.charge > 0) != tc.move {
				t.Fatalf("cursor %v charge %d, want %v moving=%v", got, cur.charge, want, tc.move)
			}
		})
	}
	for _, worldGen := range []bool{false, true} {
		w := sculkMovementWorld{blockKey{}: registry.BlockSculk.DefaultState()}
		tuning := worldGenSculkTuning
		tuning.worldGen = worldGen
		s := sculkSpreader{w: w, rng: sculkMovementRandom{}, t: &tuning}
		cur := chargeCursor{charge: 10}
		s.update(&cur, false)
		if (cur.charge == 0) != worldGen {
			t.Fatalf("worldGen=%v: stranded cursor charge %d", worldGen, cur.charge)
		}
	}
}
