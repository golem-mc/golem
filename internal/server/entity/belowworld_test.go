package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func TestBodiesBelowTheWorldAreDiscarded(t *testing.T) {
	w := newTestWorld()
	below := float64(w.MinY()) - 64
	tests := []struct {
		name string
		dead func(y float64) bool
	}{
		{"item", func(y float64) bool {
			it := NewItem(1, protocol.ItemStack{ItemID: 1, Count: 1}, 0.5, y, 0.5, 0, 0, 0, 0, 1)
			it.Step(w, nil)
			return it.Dead
		}},
		{"arrow", func(y float64) bool {
			a := NewArrow(1, 0, y, 0, 0, 0, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)
			a.Step(w, nil, nil)
			return a.Dead
		}},
		{"thrown", func(y float64) bool {
			th := NewThrown(1, ThrownSnowball, 0, y, 0, 0, 0, 0, 0, 1)
			th.Step(w, nil, nil)
			return th.Dead
		}},
		{"bobber", func(y float64) bool {
			b := NewBobber(1, 0, y, 0, 0, 0, 7, 3, 0, 0)
			b.Y = y
			b.Step(w, &BobberOwner{Fishing: true}, nil, nil)
			return b.Dead
		}},
	}
	for _, tc := range tests {
		if !tc.dead(below - 1) {
			t.Errorf("%s below the world's floor was not discarded", tc.name)
		}
		if tc.dead(below + 1) {
			t.Errorf("%s above the discard line was discarded", tc.name)
		}
	}
}
