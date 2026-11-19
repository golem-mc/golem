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

// TestAGuardianTakesNoTargetWithinThreeBlocks: whenever a guardian takes a
// target, the player stands more than three blocks off.
// vanilla: Guardian.GuardianAttackSelector
func TestAGuardianTakesNoTargetWithinThreeBlocks(t *testing.T) {
	w := tank(t, rules.DifficultyNormal)
	g := NewMob(TypeGuardian, 1, 0.5, 63, 0.5, 5)
	players := []Player{{X: 0.5, Y: 64, Z: 2.5, EntityID: 7, Huntable: true}}
	acquired := 0
	for range 1200 {
		had := g.TargetID
		g.Tick(w, players)
		if had == 0 && g.TargetID != 0 {
			acquired++
			if d := Dist2(g.X, g.Y, g.Z, 0.5, 64, 2.5); d <= 9 {
				t.Fatalf("a guardian took a target at squared distance %v", d)
			}
		}
	}
	if acquired == 0 {
		t.Error("the guardian never took the player as a target in 1200 ticks, so the test checked nothing")
	}
}

// TestAGuardianLooksAtAnotherGuardianWithinTwelveBlocks: the second look goal
// starts about one tick in a hundred on the nearest other guardian within
// twelve blocks, never on a farther one or on another kind, and turns the
// head square on it.
// vanilla: Guardian.registerGoals, LookAtPlayerGoal
func TestAGuardianLooksAtAnotherGuardianWithinTwelveBlocks(t *testing.T) {
	w := tank(t, rules.DifficultyNormal)
	starts := func(other *Mob) (bool, *Mob, *lookAtGuardian) {
		g := NewMob(TypeGuardian, 1, 0.5, 61, 0.5, 5)
		goal := &lookAtGuardian{m: g}
		view := &tickView{w: w, others: []*Mob{g, other}}
		for range 3000 {
			if goal.CanStart(view) {
				goal.Start(view)
				goal.Tick(view)
				return true, g, goal
			}
		}
		return false, g, goal
	}
	ok, g, _ := starts(NewMob(TypeElderGuardian, 2, 8.5, 61, 0.5, 6))
	if !ok {
		t.Fatal("the guardian never looked at a guardian 8 blocks away in 3000 ticks")
	}
	if g.HeadYaw != -90 {
		t.Errorf("head yaw %v after looking at a guardian to the east, want -90", g.HeadYaw)
	}
	if ok, _, _ := starts(NewMob(TypeGuardian, 2, 13.5, 61, 0.5, 6)); ok {
		t.Error("the guardian looked at a guardian 13 blocks away")
	}
	if ok, _, _ := starts(NewMob(TypeSquid, 2, 4.5, 61, 0.5, 6)); ok {
		t.Error("the guardian looked at a squid with the guardian look goal")
	}
}

// TestASpawnedGuardianWiresTheGuardianLookGoal: a guardian built by NewMob
// carries the look-at-guardian goal after the look-at-player one.
// vanilla: Guardian.registerGoals
func TestASpawnedGuardianWiresTheGuardianLookGoal(t *testing.T) {
	for _, kind := range []MobType{TypeGuardian, TypeElderGuardian} {
		g := NewMob(kind, 1, 0.5, 61, 0.5, 5)
		found := false
		for _, goal := range g.ai.goals {
			if _, ok := goal.(*lookAtGuardian); ok {
				found = true
			}
		}
		if !found {
			t.Errorf("%s has no look-at-guardian goal", kind)
		}
	}
}

// TestAGuardianWithAHomeStrollsOnlyWithinIt: with a home set and the guardian
// near enough for it to bind, every cell the stroll picks lies inside the
// home's radius.
// vanilla: DefaultRandomPos.getPos, GoalUtils.mobRestricted
func TestAGuardianWithAHomeStrollsOnlyWithinIt(t *testing.T) {
	w := tank(t, rules.DifficultyNormal)
	g := NewMob(TypeElderGuardian, 1, 0.5, 61, 0.5, 5)
	g.RestrictTo(0, 61, 0, 6)
	picked := 0
	for range 300 {
		c, ok := guardianStrollPos(w, g)
		if !ok {
			continue
		}
		picked++
		if !g.withinHome(c) {
			t.Fatalf("the stroll picked (%d, %d, %d), outside the home of radius 6 at the origin", c.X, c.Y, c.Z)
		}
	}
	if picked == 0 {
		t.Fatal("the stroll never picked a cell")
	}
}

// TestAGuardianTargetsASquidOnlyBeyondThreeBlocks: a squid or glow squid
// within the follow range and farther than three blocks becomes the target,
// one closer does not, and a plain cod never does.
// vanilla: Guardian.GuardianAttackSelector
func TestAGuardianTargetsASquidOnlyBeyondThreeBlocks(t *testing.T) {
	w := tank(t, rules.DifficultyNormal)
	targets := func(kind MobType, dx float64) bool {
		g := NewMob(TypeGuardian, 1, 0.5, 61, 0.5, 5)
		goal := &guardianTargetGoal{nearestTargetGoal{m: g, priority: 1, accept: guardianOkTarget(g)}}
		view := &tickView{w: w, others: []*Mob{g, NewMob(kind, 2, 0.5+dx, 61, 0.5, 6)}}
		for range 500 {
			if goal.CanStart(view) {
				return g.TargetID == 2
			}
		}
		return false
	}
	for _, kind := range []MobType{TypeSquid, TypeGlowSquid} {
		if !targets(kind, 5) {
			t.Errorf("the guardian never targeted a %s 5 blocks away", kind)
		}
		if targets(kind, 2) {
			t.Errorf("the guardian targeted a %s 2 blocks away", kind)
		}
	}
	if targets(TypeCod, 5) {
		t.Error("the guardian targeted a cod")
	}
}

// TestMobsWireTheirVanillaGlanceRange: the look-at-player goal of each kind
// carries the range its registerGoals passes.
// vanilla: Cow/Pig/Sheep/Chicken/AbstractHorse/Llama/SnowGolem/Cat/Ocelot.registerGoals
func TestMobsWireTheirVanillaGlanceRange(t *testing.T) {
	want := map[MobType]float64{
		TypeCow: 6, TypePig: 6, TypeSheep: 6, TypeChicken: 6, TypeHorse: 6,
		TypeLlama: 6, TypeSnowGolem: 6, TypeCat: 10, TypeOcelot: 10,
	}
	for kind, r := range want {
		m := NewMob(kind, 1, 0.5, 61, 0.5, 5)
		found := false
		for _, goal := range m.ai.goals {
			if l, ok := goal.(*lookAtPlayer); ok {
				found = true
				if l.lookRange() != r {
					t.Errorf("%s look range = %v, want %v", kind, l.lookRange(), r)
				}
			}
		}
		if !found {
			t.Errorf("%s has no look-at-player goal", kind)
		}
	}
}
