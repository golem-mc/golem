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

// A spawn egg's calf goes through getBreedOffspring with the parent as both
// parents, so a screaming parent's calf screams.
func TestAGoatEggCalfInheritsTheScreamingVariant(t *testing.T) {
	for seed := range int64(50) {
		parent := newGoat(seed)
		parent.Goat.Screaming = true
		calf := newGoat(seed + 100)
		parent.InheritInto(calf)
		if !calf.Goat.Screaming {
			t.Fatalf("seed %d: an egg used on a screaming goat gave a plain calf", seed)
		}
	}
}

// Goat.calculateFallDamage takes ten off the fall damage: a fall that would
// cost a cow eleven hearts of health costs the goat one.
func TestAGoatTakesTenLessFallDamage(t *testing.T) {
	w := newTestWorld()
	for _, tc := range []struct {
		kind MobType
		fall float64
		want float64
	}{
		{TypeCow, 14, 11},
		{TypeGoat, 14, 1},
		{TypeGoat, 12, 0},
	} {
		m := NewMob(tc.kind, 1, 0.5, 64, 0.5, 1)
		m.Health, m.MaxHealth = 20, 20
		if got, _, _, _ := m.fallBlow(w, tc.fall); got != tc.want {
			t.Errorf("%s fell %v blocks for %v damage, want %v", tc.kind, tc.fall, got, tc.want)
		}
	}
}

// A ram is mob_attack_no_aggro, in no_anger: the victim keeps no grudge.
// vanilla: DamageSources.noAggroMobAttack
func TestARamLeavesNoGrudgeButAMobBlowDoes(t *testing.T) {
	goat := newGoat(1)
	goat.ID = 9
	victim := NewMob(TypeCow, 2, 0, 64, 0, 1)
	if !victim.HurtByRam(2, goat) || victim.retaliate != 0 {
		t.Errorf("after a ram retaliate = %d, want 0", victim.retaliate)
	}
	other := NewMob(TypeCow, 3, 0, 64, 0, 1)
	other.HurtByMob(2, goat)
	if other.retaliate != goat.ID {
		t.Errorf("after a mob blow retaliate = %d, want %d", other.retaliate, goat.ID)
	}
}

// A calf follows an adult from beyond the animals' 8-block search: its brain
// follows from 5 to 17 blocks with a sensor reach of 16.
// vanilla: GoatAi.ADULT_FOLLOW_RANGE, BabyFollowAdult
func TestAGoatCalfFollowsAnAdultTwelveBlocksOff(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-4); x <= 20; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, stone)
		}
	}
	calf := NewMob(TypeGoat, 1, 0.5, 64, 0.5, 1)
	calf.SetBaby(true)
	adult := NewMob(TypeGoat, 2, 12.5, 64, 0.5, 2)
	start := Dist2(calf.X, calf.Y, calf.Z, adult.X, adult.Y, adult.Z)
	for range 60 {
		calf.tick(w, nil, []*Mob{calf, adult})
	}
	if now := Dist2(calf.X, calf.Y, calf.Z, adult.X, adult.Y, adult.Z); now >= start-4 {
		t.Errorf("squared distance to the adult %.1f from %.1f, want the calf walking toward it", now, start)
	}
}
