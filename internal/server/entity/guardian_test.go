package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// tank is a lake the guardians swim in: water from y 60 to 63 over a stone
// floor, sixteen blocks across.
func tank(t *testing.T, difficulty uint8) testWorld {
	t.Helper()
	w := newTestWorld()
	w.diffUp, w.diff = true, difficulty
	water, stone := defaultState(t, "minecraft:water"), defaultState(t, "minecraft:stone")
	for x := int32(-8); x <= 8; x++ {
		for z := int32(-8); z <= 8; z++ {
			w.set(x, 59, z, stone)
			for y := int32(60); y <= 63; y++ {
				w.set(x, y, z, water)
			}
		}
	}
	return w
}

// beamFrom ticks a guardian against a player on the shore until its beam
// finishes charging and returns the tick the target was told on, the tick
// the beam landed on and the beam event.
func beamFrom(t *testing.T, w testWorld, kind MobType) (told, landed int, beam MobEvent) {
	t.Helper()
	g := NewMob(kind, 1, 0.5, 61, 0.5, 5)
	players := []Player{{X: 6.5, Y: 64, Z: 0.5, EntityID: 7, Huntable: true}}
	told, landed = -1, -1
	for tick := range 400 {
		for _, ev := range g.Tick(w, players) {
			if ev.Kind == MobGuardianBeam {
				landed, beam = tick, ev
			}
		}
		if told < 0 && g.GuardianTarget() == 7 {
			told = tick
		}
		if landed >= 0 {
			return told, landed, beam
		}
	}
	t.Fatalf("the %s's beam never landed in 400 ticks", kind)
	return
}

// TestAGuardianBeamsMagicDamageGrowsWithDifficultyAndElderhood: the beam is
// 1, plus 2 on hard, plus 2 for the elder.
// vanilla: Guardian.GuardianAttackGoal.tick
func TestAGuardianBeamsMagicDamageGrowsWithDifficultyAndElderhood(t *testing.T) {
	cases := []struct {
		kind       MobType
		difficulty uint8
		want       float64
		blow       float64
		duration   int
	}{
		{TypeGuardian, rules.DifficultyNormal, 1, 6, 80},
		{TypeGuardian, rules.DifficultyHard, 3, 6, 80},
		{TypeElderGuardian, rules.DifficultyNormal, 3, 8, 60},
		{TypeElderGuardian, rules.DifficultyHard, 5, 8, 60},
	}
	for _, tc := range cases {
		told, landed, beam := beamFrom(t, tank(t, tc.difficulty), tc.kind)
		if beam.Magic != tc.want {
			t.Errorf("%s on difficulty %d: beam magic %v, want %v", tc.kind, tc.difficulty, beam.Magic, tc.want)
		}
		if beam.Damage != tc.blow {
			t.Errorf("%s: the blow after the beam is %v, want its attack damage %v", tc.kind, beam.Damage, tc.blow)
		}
		if landed-told != tc.duration {
			t.Errorf("%s: the beam landed %d ticks after its target was told, want %d", tc.kind, landed-told, tc.duration)
		}
	}
}
