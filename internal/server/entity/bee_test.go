package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// TestAStungBeeDiesAtTheLatestOnTheTwelveHundredthTick holds Bee.
// customServerAiStep's worst case: the death roll is made on every fifth tick
// since the sting and cannot miss once the countdown has run out.
func TestAStungBeeDiesAtTheLatestOnTheTwelveHundredthTick(t *testing.T) {
	for seed := range int64(100) {
		m := NewMob(TypeBee, 1, 0, 64, 0, seed)
		m.Sting()
		tv := &tickView{w: newTestWorld()}
		died := 0
		for tick := 1; tick <= 1200 && died == 0; tick++ {
			m.beeStep(tv)
			if m.Dead {
				died = tick
			}
		}
		if died == 0 {
			t.Fatalf("seed %d: the bee is alive 1200 ticks after its sting", seed)
		}
		if died%5 != 0 {
			t.Fatalf("seed %d: the bee died on tick %d, want a multiple of 5", seed, died)
		}
	}
}

// TestABeeDrownsAfterTwentyTicksInWater: the first bite comes on the 21st
// tick in water, Bee.customServerAiStep's underWaterTicks > 20.
func TestABeeDrownsAfterTwentyTicksInWater(t *testing.T) {
	m := NewMob(TypeBee, 1, 0, 64, 0, 1)
	m.waterFluid.height = 1
	tv := &tickView{w: newTestWorld()}
	for range 20 {
		m.beeStep(tv)
	}
	if m.Health != 10 {
		t.Fatalf("health after 20 ticks in water = %v, want 10", m.Health)
	}
	m.beeStep(tv)
	if m.Health != 9 {
		t.Errorf("health on the 21st tick in water = %v, want 9", m.Health)
	}
	if len(tv.events) != 1 || tv.events[0].DamageType != rules.DamageTypeDrown {
		t.Errorf("events = %+v, want one drowning bite", tv.events)
	}
}

func TestABeeSting(t *testing.T) {
	m := NewMob(TypeBee, 1, 0, 64, 0, 1)
	m.SetAnger(100, [16]byte{}, false)
	m.Sting()
	if !m.HasStung() || m.Angry() {
		t.Errorf("after the sting: stung %v, angry %v; want stung and calm", m.HasStung(), m.Angry())
	}
	if got := m.BeeFlags(); got != BeeFlagStung {
		t.Errorf("flags = %#x, want %#x", got, BeeFlagStung)
	}
	m.SetBeeState(false, true)
	if got := m.BeeFlags(); got != BeeFlagNectar {
		t.Errorf("flags = %#x, want %#x", got, BeeFlagNectar)
	}
	if a := m.kind().attackDamage; a != 2 {
		t.Errorf("attack damage = %v, want 2", a)
	}
}

// TestABeesStingPoisonsByDifficulty: ten seconds on normal, eighteen on hard,
// none on easy.
func TestABeesStingPoisonsByDifficulty(t *testing.T) {
	m := NewMob(TypeBee, 1, 0, 64, 0, 1)
	for _, c := range []struct {
		difficulty uint8
		ticks      int32
		ok         bool
	}{
		{rules.DifficultyPeaceful, 0, false},
		{rules.DifficultyEasy, 0, false},
		{rules.DifficultyNormal, 200, true},
		{rules.DifficultyHard, 360, true},
	} {
		name, ticks, ok := m.BlowEffect(c.difficulty, 1)
		if ok != c.ok || ticks != c.ticks || (ok && name != "minecraft:poison") {
			t.Errorf("difficulty %d: %q %d %v, want poison %d %v", c.difficulty, name, ticks, ok, c.ticks, c.ok)
		}
	}
}
