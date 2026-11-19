package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// huntWorld is a nether floor with the given piglins and hoglins on it, the
// mobs at x = 0.5 + 2i.
func huntWorld(t *testing.T, piglins, hoglins int) (testWorld, []*Mob, []*Mob) {
	t.Helper()
	w := newTestWorld()
	w.nether = true
	floor64(w, t)
	var ps, hs []*Mob
	id := int32(1)
	for i := range piglins {
		ps = append(ps, NewMob(TypePiglin, id, -3.5+float64(i), 64, -2.5, int64(id)))
		id++
	}
	for i := range hoglins {
		hs = append(hs, NewMob(TypeHoglin, id, -3.5+float64(i), 64, 2.5, int64(id)))
		id++
	}
	return w, ps, hs
}

func runHunt(w testWorld, mobs []*Mob, ticks int) (events []MobEvent) {
	for range ticks {
		for _, m := range mobs {
			events = append(events, m.tick(w, nil, mobs)...)
		}
	}
	return events
}

func all(ps, hs []*Mob) []*Mob { return append(append([]*Mob{}, ps...), hs...) }

func TestAPiglinTakesAnAdultHoglinAsItsTargetAndRests(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 1)
	runHunt(w, all(ps, hs), 40)
	if ps[0].TargetID != hs[0].ID {
		t.Fatalf("target %d, want the hoglin %d", ps[0].TargetID, hs[0].ID)
	}
	if ps[0].barter.huntRest < 560 {
		t.Errorf("hunt rest %d, want 30 seconds less the ticks run", ps[0].barter.huntRest)
	}
}

func TestAPiglinLeavesBabyHoglinsAndBrutesNeverHunt(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 1)
	hs[0].SetBaby(true)
	brute := NewMob(TypePiglinBrute, 9, -1.5, 64, -2.5, 9)
	runHunt(w, append(all(ps, hs), brute), 80)
	if ps[0].TargetID != 0 {
		t.Errorf("the piglin hunts a baby hoglin: %d", ps[0].TargetID)
	}
	hs[0].SetBaby(false)
	runHunt(w, append(all(ps, hs), brute), 80)
	if brute.TargetID == hs[0].ID {
		t.Error("the brute hunts a hoglin")
	}
}

func TestTheHuntCallsTheAdultPiglinsAround(t *testing.T) {
	w, ps, hs := huntWorld(t, 3, 1)
	runHunt(w, all(ps, hs), 40)
	for i, p := range ps {
		if p.TargetID != hs[0].ID {
			t.Errorf("piglin %d target %d, want the hoglin", i, p.TargetID)
		}
		if p.barter.huntRest == 0 {
			t.Errorf("piglin %d did not rest from hunting", i)
		}
	}
}

func TestAPiglinNextToAHuntingOneStartsNoHunt(t *testing.T) {
	w, ps, hs := huntWorld(t, 2, 1)
	ps[1].barter.huntRest = 1000
	ps[0].Age = 0
	runHunt(w, all(ps, hs), 40)
	if ps[0].TargetID != 0 {
		t.Errorf("a piglin beside a resting one hunts: %d", ps[0].TargetID)
	}
}

func TestAPiglinRetreatsFromHoglinsThatOutnumberIt(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 3)
	ps[0].barter.huntRest = 5000
	ps[0].retaliate = hs[0].ID
	events := runHunt(w, all(ps, hs), 4)
	if ps[0].barter.retreat == 0 || ps[0].barter.from != hs[0].ID {
		t.Fatalf("retreat %d from %d, want a retreat from the hoglin", ps[0].barter.retreat, ps[0].barter.from)
	}
	if ps[0].TargetID != 0 {
		t.Errorf("the retreating piglin targets %d", ps[0].TargetID)
	}
	if ps[0].barter.retreat < piglinRetreatMin-5 {
		t.Errorf("retreat %d under the 5 second minimum", ps[0].barter.retreat)
	}
	var retreatSound bool
	for _, ev := range events {
		retreatSound = retreatSound || ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.piglin.retreat"
	}
	if !retreatSound {
		t.Error("no retreat sound")
	}
}

func TestAPiglinThatCannotHuntStillRetreatsFromOutnumberingHoglins(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 3)
	ps[0].barter.CannotHunt = true
	ps[0].barter.huntRest = 5000
	ps[0].retaliate = hs[0].ID
	runHunt(w, all(ps, hs), 4)
	if ps[0].barter.retreat == 0 || ps[0].barter.from != hs[0].ID {
		t.Fatalf("retreat %d from %d target %d, want a retreat from the hoglin", ps[0].barter.retreat, ps[0].barter.from, ps[0].TargetID)
	}
}

func TestAPiglinFightsBackAgainstAHoglinItsGroupMatches(t *testing.T) {
	w, ps, hs := huntWorld(t, 2, 2)
	ps[0].barter.huntRest = 5000
	ps[0].retaliate = hs[0].ID
	runHunt(w, all(ps, hs), 4)
	if ps[0].barter.retreat != 0 {
		t.Error("a piglin with an equal group retreats")
	}
	if ps[0].TargetID != hs[0].ID {
		t.Errorf("target %d, want the hoglin that struck", ps[0].TargetID)
	}
}

func TestAStruckPiglinCallsTheOthersAfterItsAttacker(t *testing.T) {
	w, ps, _ := huntWorld(t, 2, 0)
	attacker := Player{X: -3.5, Y: 64, Z: 0.5, EntityID: 77, Huntable: true}
	ps[0].retaliate = attacker.EntityID
	for range 4 {
		for _, m := range ps {
			m.tick(w, []Player{attacker}, ps)
		}
	}
	if ps[1].TargetID != attacker.EntityID {
		t.Errorf("the second piglin targets %d, want the attacker", ps[1].TargetID)
	}
}

func TestAPiglinGrowlsWhenItTakesATarget(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 1)
	var angry bool
	for _, ev := range runHunt(w, all(ps, hs), 40) {
		angry = angry || ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.piglin.angry"
	}
	if !angry {
		t.Error("no angry sound")
	}
}

func TestARetreatingPiglinWalksAwayFromTheHoglins(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 3)
	ps[0].barter.huntRest = 5000
	ps[0].retaliate = hs[0].ID
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-20); x <= 20; x++ {
		for z := int32(-20); z <= 20; z++ {
			w.set(x, 63, z, stone)
		}
	}
	for _, h := range hs {
		h.NoAI = true
	}
	start := Dist2(ps[0].X, ps[0].Y, ps[0].Z, hs[0].X, hs[0].Y, hs[0].Z)
	runHunt(w, all(ps, hs), 40)
	if d := Dist2(ps[0].X, ps[0].Y, ps[0].Z, hs[0].X, hs[0].Y, hs[0].Z); d <= start {
		t.Errorf("distance² %.2f after the retreat, was %.2f", d, start)
	}
}

func TestAPiglinDoesNotAnswerAPiglinsBlow(t *testing.T) {
	w, ps, _ := huntWorld(t, 2, 0)
	ps[0].retaliate = ps[1].ID
	runHunt(w, ps, 4)
	if ps[0].TargetID != 0 {
		t.Errorf("target %d after a piglin's blow, want none", ps[0].TargetID)
	}
}

func TestTheHuntRestIsThirtyToOneTwentySeconds(t *testing.T) {
	for seed := int64(1); seed <= 50; seed++ {
		m := NewMob(TypePiglin, 1, 0.5, 64, 0.5, seed)
		if n := m.huntRestTicks(); n < 600 || n > 2400 {
			t.Fatalf("seed %d: rest %d ticks outside 600 to 2400", seed, n)
		}
	}
}

func holdGold(m *Mob) {
	m.OffHand = protocol.ItemStack{ItemID: int32(registry.ItemGoldIngot), Count: 1}
	m.barter.admiring = piglinAdmireTicks
}

func TestARetreatLeavesTheBystandersAdmiringAndTheStruckOneDropsIt(t *testing.T) {
	w, ps, hs := huntWorld(t, 2, 4)
	holdGold(ps[0])
	holdGold(ps[1])
	ps[0].setRetaliate(hs[0].ID)
	runHunt(w, all(ps, hs), 4)
	if ps[1].barter.retreat == 0 {
		t.Fatal("the bystander did not retreat")
	}
	if ps[1].barter.admiring == 0 || ps[1].OffHand.ItemID != int32(registry.ItemGoldIngot) {
		t.Errorf("the bystander admires %d holding %v, want it to keep admiring", ps[1].barter.admiring, ps[1].OffHand)
	}
	if ps[0].barter.admiring != 0 || !ps[0].OffHand.Empty() {
		t.Errorf("the struck piglin admires %d holding %v, want it to let go", ps[0].barter.admiring, ps[0].OffHand)
	}
}

func TestAFreshPiglinRestsFromHunting(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		m := NewMob(TypePiglin, 1, 0.5, 64, 0.5, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:nether_wastes"), nil, rules.LocalDifficulty{})
		if r := m.barter.huntRest; r < piglinHuntRestMin || r > piglinHuntRestMax {
			t.Fatalf("seed %d: fresh piglin rests %d ticks, want %d to %d", seed, r, piglinHuntRestMin, piglinHuntRestMax)
		}
	}
}

func TestABruteDoesNotAnswerAPiglinOrBrutesBlow(t *testing.T) {
	w, ps, _ := huntWorld(t, 1, 0)
	brutes := []*Mob{NewMob(TypePiglinBrute, 8, -1.5, 64, -2.5, 8), NewMob(TypePiglinBrute, 9, -0.5, 64, -2.5, 9)}
	mobs := append(append([]*Mob{}, ps...), brutes...)
	for _, striker := range []*Mob{ps[0], brutes[1]} {
		brutes[0].retaliate = striker.ID
		runHunt(w, mobs, 4)
		if brutes[0].TargetID != 0 {
			t.Errorf("a brute answers a %s's blow: target %d", striker.Type, brutes[0].TargetID)
		}
	}
}

func TestAStruckBruteCallsPiglinsAndAStruckPiglinCallsBrutes(t *testing.T) {
	attacker := Player{X: -3.5, Y: 64, Z: 0.5, EntityID: 77, Huntable: true}
	for _, striker := range []MobType{TypePiglin, TypePiglinBrute} {
		w, _, _ := huntWorld(t, 0, 0)
		other := TypePiglinBrute
		if striker == TypePiglinBrute {
			other = TypePiglin
		}
		a := NewMob(striker, 1, -3.5, 64, -2.5, 1)
		b := NewMob(other, 2, -2.5, 64, -2.5, 2)
		a.retaliate = attacker.EntityID
		mobs := []*Mob{a, b}
		for range 4 {
			for _, m := range mobs {
				m.tick(w, []Player{attacker}, mobs)
			}
		}
		if b.TargetID != attacker.EntityID {
			t.Errorf("a struck %s did not call the %s: target %d", striker, other, b.TargetID)
		}
	}
}

func TestAHoglinsBiteCallsNoBrute(t *testing.T) {
	w, ps, hs := huntWorld(t, 2, 2)
	brute := NewMob(TypePiglinBrute, 9, -2.5, 64, -2.5, 9)
	ps[0].barter.huntRest = 5000
	ps[0].retaliate = hs[0].ID
	runHunt(w, append(all(ps, hs), brute), 4)
	if brute.TargetID == hs[0].ID {
		t.Error("a brute was called after a hoglin")
	}
	if ps[1].TargetID != hs[0].ID {
		t.Errorf("the second piglin targets %d, want the hoglin", ps[1].TargetID)
	}
}

func TestAPiglinFightingAPlayerRetreatsWhenHoglinsOutnumberItAndBite(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 3)
	for _, h := range hs {
		h.NoAI = true
	}
	ps[0].barter.huntRest = 5000
	attacker := Player{X: -3.5, Y: 64, Z: 0.5, EntityID: 77, Huntable: true}
	mobs := all(ps, hs)
	step := func(n int) {
		for range n {
			for _, m := range mobs {
				m.tick(w, []Player{attacker}, mobs)
			}
		}
	}
	ps[0].retaliate = attacker.EntityID
	step(6)
	if ps[0].TargetID != attacker.EntityID {
		t.Fatalf("target %d, want the player that struck", ps[0].TargetID)
	}
	ps[0].retaliate = hs[0].ID
	step(6)
	if ps[0].barter.retreat == 0 || ps[0].TargetID != 0 {
		t.Errorf("retreat %d, target %d: a fighting piglin ignores the hoglin's bite", ps[0].barter.retreat, ps[0].TargetID)
	}
}

func TestAFightingBruteGrowlsNowAndThen(t *testing.T) {
	w := newTestWorld()
	w.nether = true
	floor64(w, t)
	brute := NewMob(TypePiglinBrute, 1, -3.5, 64, -2.5, 1)
	player := Player{X: -3.5, Y: 64, Z: -0.5, EntityID: 77, Huntable: true}
	brute.retaliate = player.EntityID
	var growls int
	for range 1600 {
		for _, ev := range brute.tick(w, []Player{player}, []*Mob{brute}) {
			if ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.piglin_brute.angry" {
				growls++
			}
		}
	}
	// One for taking the target, then 1.25% of the ticks after.
	if growls < 8 || growls > 40 {
		t.Errorf("%d growls in 1600 ticks of fighting, want about 21", growls)
	}
}

func TestAStruckPiglinSwitchesNeighboursToTheNearerAttacker(t *testing.T) {
	w, _, _ := huntWorld(t, 0, 0)
	attacker := Player{X: -3.5, Y: 64, Z: 0.5, EntityID: 77, Huntable: true}
	for _, tc := range []struct {
		name   string
		farZ   float64
		wantID int32
	}{
		{"farther target is dropped", 8.5, 77},
		{"nearer target is kept", -0.5, 78},
	} {
		far := Player{X: -2.5, Y: 64, Z: tc.farZ, EntityID: 78, Huntable: true}
		a := NewMob(TypePiglin, 1, -3.5, 64, -2.5, 1)
		b := NewMob(TypePiglin, 2, -2.5, 64, -2.5, 2)
		a.retaliate = attacker.EntityID
		b.TargetID = far.EntityID
		mobs := []*Mob{a, b}
		for range 4 {
			for _, m := range mobs {
				m.tick(w, []Player{attacker, far}, mobs)
			}
		}
		if b.TargetID != tc.wantID {
			t.Errorf("%s: target %d, want %d", tc.name, b.TargetID, tc.wantID)
		}
	}
}

func TestTheAngerCallReachesTheFollowRangeUpAndNoFurther(t *testing.T) {
	attacker := Player{X: -3.5, Y: 64, Z: 0.5, EntityID: 77, Huntable: true}
	// Safe armour prevents the receiver from independently targeting the player.
	// vanilla: PiglinAi.findNearestValidAttackTarget, isWearingSafeArmor
	attacker.Worn[0] = protocol.ItemStack{ItemID: int32(registry.ItemGoldenBoots), Count: 1}
	for _, tc := range []struct {
		name    string
		striker MobType
		y       float64
		want    int32
	}{
		{"piglin/inside", TypePiglin, 79, 77}, {"piglin/outside", TypePiglin, 83, 0},
		{"brute/inside", TypePiglinBrute, 76, 77}, {"brute/outside", TypePiglinBrute, 78, 0},
	} {
		for _, struck := range []bool{false, true} {
			name := "unprovoked"
			if struck {
				name = "struck"
			}
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				w, _, _ := huntWorld(t, 0, 0)
				a := NewMob(tc.striker, 1, -3.5, 64, -2.5, 1)
				b := NewMob(TypePiglin, 2, -3.5, tc.y, -2.5, 2)
				// Keep the receiver from falling into the brute's expanded box.
				b.NoGravity = true
				want := int32(0)
				if struck {
					a.retaliate = attacker.EntityID
					want = tc.want
				}
				mobs := []*Mob{a, b}
				for range 4 {
					for _, m := range mobs {
						m.tick(w, []Player{attacker}, mobs)
					}
				}
				if b.TargetID != want {
					t.Errorf("piglin %v blocks up: target %d, want %d", tc.y-64, b.TargetID, want)
				}
			})
		}
	}
}

func TestARetreatAvoidsTheNearestOfTheOldAndNewHoglin(t *testing.T) {
	for _, tc := range []struct {
		name        string
		oldZ, newZ  float64
		wantFromOld bool
	}{
		{"nearer new hoglin replaces the old", 8.5, 3.5, false},
		{"nearer old hoglin is kept", 3.5, 8.5, true},
	} {
		w, ps, _ := huntWorld(t, 1, 0)
		oldH := NewMob(TypeHoglin, 8, -3.5, 64, tc.oldZ, 8)
		newH := NewMob(TypeHoglin, 9, -3.5, 64, tc.newZ, 9)
		mobs := []*Mob{ps[0], oldH, newH}
		ps[0].barter.retreat, ps[0].barter.from = 200, oldH.ID
		ps[0].retreatFrom(&tickView{w: w, others: mobs}, newH)
		want := newH.ID
		if tc.wantFromOld {
			want = oldH.ID
		}
		if ps[0].barter.from != want || ps[0].barter.retreat < piglinRetreatMin {
			t.Errorf("%s: avoids %d for %d ticks, want %d", tc.name, ps[0].barter.from, ps[0].barter.retreat, want)
		}
	}
}

func TestAPiglinsAmbientSoundFollowsWhatItSees(t *testing.T) {
	w, ps, _ := huntWorld(t, 1, 0)
	m := ps[0]
	m.tick(w, []Player{{X: 0.5, Y: 64, Z: -2.5, HeldItem: protocol.ItemStack{ItemID: int32(registry.ItemGoldIngot), Count: 1}}}, ps)
	if got := m.Sounds().Ambient.Name(); got != "minecraft:entity.piglin.jealous" {
		t.Errorf("ambient = %q, want jealous", got)
	}
	m.tick(w, nil, ps)
	if got := m.Sounds().Ambient.Name(); got != "minecraft:entity.piglin.ambient" {
		t.Errorf("ambient = %q, want the plain one", got)
	}
}

func TestAStruckBabyPiglinFleesInsteadOfFightingBack(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 1)
	ps[0].SetBaby(true)
	ps[0].retaliate = hs[0].ID
	runHunt(w, all(ps, hs), 4)
	if ps[0].barter.from != hs[0].ID || ps[0].barter.retreat == 0 || ps[0].barter.retreat > piglinBabyFlee {
		t.Errorf("retreat %d from %d, want a flight from the attacker", ps[0].barter.retreat, ps[0].barter.from)
	}
	if ps[0].TargetID != 0 {
		t.Errorf("the baby targets %d", ps[0].TargetID)
	}
}

func TestABabyPiglinAvoidsAWitherSkeleton(t *testing.T) {
	w, ps, _ := huntWorld(t, 1, 0)
	ps[0].SetBaby(true)
	ws := NewMob(TypeWitherSkeleton, 9, 0.5, 64, 2.5, 9)
	runHunt(w, []*Mob{ps[0], ws}, 25)
	if ps[0].barter.from != ws.ID || ps[0].barter.retreat == 0 {
		t.Errorf("retreat %d from %d, want a flight from the wither skeleton", ps[0].barter.retreat, ps[0].barter.from)
	}
}

func TestAPiglinKeepsAvoidingAZombifiedPiglinItCannotLeave(t *testing.T) {
	w, ps, _ := huntWorld(t, 1, 0)
	p := ps[0]
	z := NewMob(TypeZombifiedPiglin, 5, p.X+2, 64, p.Z, 5)
	for tick := range 400 {
		p.X, p.Z = -3.5, -2.5
		p.tick(w, nil, []*Mob{p, z})
		if tick >= 20 && p.barter.retreat == 0 {
			t.Fatalf("the avoid memory ran out at tick %d with the zombified piglin 2 blocks away", tick)
		}
	}
}

func TestAFreshPiglinPlaysItsIdleSoundOnceOnTheFirstTick(t *testing.T) {
	w, ps, _ := huntWorld(t, 1, 0)
	count := func(n int) (c int) {
		for _, ev := range runHunt(w, ps[:1], n) {
			if ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.piglin.ambient" {
				c++
			}
		}
		return c
	}
	if got := count(1); got != 1 {
		t.Errorf("first tick played %d ambient sounds, want 1", got)
	}
	if got := count(5); got != 0 {
		t.Errorf("later idle ticks played %d ambient sounds, want 0", got)
	}
}
