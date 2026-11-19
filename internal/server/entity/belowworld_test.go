package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
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
			b.Step(w, &BobberOwner{Fishing: true}, nil)
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

// TestAMobBelowTheWorldTakesTheVoidEveryTenTicks checks the first hit lands
// on the first tick below minY - 64, the window paces the rest, and the fifth
// hit kills a zombie.
func TestAMobBelowTheWorldTakesTheVoidEveryTenTicks(t *testing.T) {
	w := newTestWorld()
	z := NewMob(TypeZombie, 1, 0.5, float64(w.MinY())-70, 0.5, 1)
	var hits []int
	last := z.Health
	for tick := 0; tick < 60 && !z.Dead; tick++ {
		z.Tick(w, nil)
		if z.Health < last {
			hits = append(hits, tick)
			if last-z.Health != rules.VoidDamage {
				t.Errorf("hit at tick %d took %v, want %v", tick, last-z.Health, rules.VoidDamage)
			}
			last = z.Health
		}
	}
	want := []int{0, 10, 20, 30, 40}
	if len(hits) != len(want) {
		t.Fatalf("void hits at ticks %v, want %v", hits, want)
	}
	for i := range want {
		if hits[i] != want[i] {
			t.Fatalf("void hits at ticks %v, want %v", hits, want)
		}
	}
	if !z.Dead {
		t.Error("the zombie survived five void hits")
	}

	above := NewMob(TypeZombie, 2, 0.5, float64(w.MinY())-60, 0.5, 1)
	above.Tick(w, nil)
	if above.Health != above.MaxHealth {
		t.Errorf("a zombie above the void line took damage: %v", above.Health)
	}
}
