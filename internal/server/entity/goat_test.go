package entity

import (
	"math/rand"
	"testing"
)

func newGoat(seed int64) *Mob { return NewMob(TypeGoat, 1, 0, 64, 0, seed) }

// Goat.dropHorn takes one horn at a time, never from a calf, and nothing
// once both are gone.
func TestAGoatLosesOneHornAtATime(t *testing.T) {
	g := newGoat(1)
	for range 2 {
		if _, _, _, ok := g.dropHorn(); !ok {
			t.Fatal("a goat with a horn dropped none")
		}
	}
	if !g.Goat.NoLeftHorn || !g.Goat.NoRightHorn {
		t.Errorf("after two drops left gone %v, right gone %v, want both", g.Goat.NoLeftHorn, g.Goat.NoRightHorn)
	}
	if _, _, _, ok := g.dropHorn(); ok {
		t.Error("a hornless goat dropped a horn")
	}
	calf := newGoat(2)
	calf.SetBaby(true)
	if _, _, _, ok := calf.dropHorn(); ok {
		t.Error("a calf dropped a horn")
	}
	one := newGoat(3)
	one.Goat.NoLeftHorn = true
	one.dropHorn()
	if !one.Goat.NoRightHorn {
		t.Error("a goat with only the right horn kept it")
	}
}

// Goat.getBreedOffspring: a calf of a screaming parent it takes after is
// screaming; two plain parents give one in fifty.
func TestAGoatCalfInheritsTheScreamingVariant(t *testing.T) {
	mother, father := newGoat(4), newGoat(5)
	mother.Goat.Screaming, father.Goat.Screaming = true, true
	for range 50 {
		calf := newGoat(6)
		mother.breedGoat(calf, father)
		if !calf.Goat.Screaming {
			t.Fatal("two screaming parents bred a plain calf")
		}
	}
	mother.Goat.Screaming, father.Goat.Screaming = false, false
	mother.Rand = rand.New(rand.NewSource(9))
	screaming := 0
	for range 5000 {
		calf := newGoat(7)
		mother.breedGoat(calf, father)
		if calf.Goat.Screaming {
			screaming++
		}
	}
	if screaming < 50 || screaming > 160 {
		t.Errorf("%d of 5000 calves of plain parents screamed, want about 100", screaming)
	}
}
