package entity

import (
	"math/rand"
	"testing"
)

// The breeding goal's plain decisions, stepped against mobs placed by hand:
// the black-box breeding in internal/server/server_breeding_test.go proves a pair the
// server actually spawns, these pin the selection rules BreedGoal keeps.

// lover is a mob in love, at the given horizontal place.
func lover(id int32, x float64, kind MobType) *Mob {
	m := NewMob(kind, id, x, 64, 0.5, int64(id))
	m.InLove = loveTicks
	return m
}

// breedTickView is the tick's view over the mobs given, nothing else set.
func breedTickView(mobs ...*Mob) *tickView {
	return &tickView{w: testWorld{}, others: mobs}
}

// TestBreedPartnerMustBeNearAndOfTheKind: getFreePartner's reach is eight
// blocks and its mate the same kind, so a body past the reach or of another
// species is no partner.
func TestBreedPartnerMustBeNearAndOfTheKind(t *testing.T) {
	near := lover(1, 0.5, TypeCow)
	if mate := lover(2, 6.5, TypeCow); (&breedGoal{m: near}).freePartner(breedTickView(near, mate)) != mate {
		t.Error("a cow six blocks off is no partner, want it found within the eight-block reach")
	}
	if mate := lover(3, 9.5, TypeCow); (&breedGoal{m: near}).freePartner(breedTickView(near, mate)) != nil {
		t.Error("a cow nine blocks off was taken as a partner, want the scan's eight-block reach")
	}
	if mate := lover(4, 2.5, TypePig); (&breedGoal{m: near}).freePartner(breedTickView(near, mate)) != nil {
		t.Error("a pig was taken as a cow's partner, want the kind checked")
	}
}

// TestAPanickingMateIsRefused: BreedGoal skips a panicking partner both when
// choosing one and while a courtship runs, PathfinderMob.isPanicking over
// the running panic goal.
func TestAPanickingMateIsRefused(t *testing.T) {
	a := lover(1, 0.5, TypeCow)
	b := lover(2, 1.5, TypeCow)
	tv := breedTickView(a, b)

	g := &breedGoal{m: a}
	if p := g.freePartner(tv); p != b {
		t.Fatal("the loving cow beside the other was no partner, want it found")
	}
	b.ai.running = append(b.ai.running, &panicGoal{m: b})
	if p := g.freePartner(tv); p != nil {
		t.Error("a panicking cow was taken as a partner, want it refused")
	}
	g.partner = b
	if g.ShouldContinue(tv) {
		t.Error("a courtship kept a partner that panicked, want it let go")
	}
}

// TestABreedCourtshipGivesUpAfterSixtyTicks: BreedGoal.canContinueToUse
// stops a courtship once its own count reaches the sixty-tick give-up.
func TestABreedCourtshipGivesUpAfterSixtyTicks(t *testing.T) {
	a := lover(1, 0.5, TypeCow)
	b := lover(2, 1.5, TypeCow)
	tv := breedTickView(a, b)

	g := &breedGoal{m: a, partner: b, loveTime: breedGiveUp - 1}
	if !g.ShouldContinue(tv) {
		t.Fatalf("the courtship let go at %d goal-ticks, want the %d-tick give-up", g.loveTime, breedGiveUp)
	}
	g.loveTime = breedGiveUp
	if g.ShouldContinue(tv) {
		t.Errorf("the courtship ran at %d goal-ticks, want the %d-tick give-up", g.loveTime, breedGiveUp)
	}
}

// TestBreedCourtshipTiming counts real selector ticks through MobBred.
func TestBreedCourtshipTiming(t *testing.T) {
	for _, kind := range []MobType{TypeCow, TypeAxolotl} {
		t.Run(kind.Name(), func(t *testing.T) {
			seen := map[int]bool{}
			for seed := range int64(100) {
				a, b := lover(2, 0.5, kind), lover(3, 1.5, kind)
				a.Rand.Seed(seed)
				want := 59 // 30 alternate updates, including the starting tick.
				if kind == TypeAxolotl {
					want = 61 + rand.New(rand.NewSource(seed)).Intn(50) // Include the start tick before the duration elapses.
				}
				tv := breedTickView(a, b)
				var bredAt int
				for tick := 1; tick <= 120; tick++ {
					a.Age = 99 + tick // Start on a full selection, past the first-tick exception.
					a.ai.run(a, tv)
					for _, ev := range tv.events {
						if ev.Kind == MobBred {
							bredAt = tick
						}
					}
					if bredAt != 0 {
						break
					}
				}
				if bredAt != want {
					t.Fatalf("seed %d: bred on tick %d, want %d", seed, bredAt, want)
				}
				seen[bredAt] = true
			}
			if kind == TypeAxolotl && len(seen) < 2 {
				t.Fatal("brain courtship duration did not vary across seeds")
			}
		})
	}
}

func TestBrainCourtshipGivesUpAtBirthTime(t *testing.T) {
	a, b := lover(1, 0.5, TypeAxolotl), lover(2, 4.5, TypeAxolotl)
	a.Rand.Seed(0)
	want := 61 + rand.New(rand.NewSource(0)).Intn(50)
	tv := breedTickView(a, b)
	g := &breedGoal{m: a}
	if !g.CanStart(tv) {
		t.Fatal("no partner within the courtship range")
	}
	g.Start(tv)
	for ticks := 0; ticks <= 110; ticks++ {
		if !g.ShouldContinue(tv) {
			if ticks != want {
				t.Fatalf("gave up after %d full ticks, want %d", ticks, want)
			}
			if len(tv.events) != 0 {
				t.Fatal("bred with a partner outside breeding distance")
			}
			g.Stop(tv)
			if g.partner != nil || g.loveTime != 0 {
				t.Fatal("stopped courtship retained its partner or timer")
			}
			return
		}
		g.Tick(tv)
	}
	t.Fatal("courtship continued past its birth time")
}

// TestABredLambWearsItsParentsMixedColour: BreedInto's sheep half is
// Sheep.getBreedOffspring, the lamb wearing the colour getMixedColor rolls
// for its parents — the data pack's mix where the pair has one, red and
// blue breeding purple, and one of the parents where it has none, blue
// and yellow never breeding green.
func TestABredLambWearsItsParentsMixedColour(t *testing.T) {
	mother, partner := lover(1, 0.5, TypeSheep), lover(2, 1.5, TypeSheep)
	mother.Colour, partner.Colour = "red", "blue"
	lamb := NewMob(TypeSheep, 3, 0.5, 64, 0.5, 8)
	mother.BreedInto(lamb, partner)
	if lamb.Colour != "purple" {
		t.Errorf("a red and a blue sheep bred %q, want purple", lamb.Colour)
	}

	for s := range 50 {
		mother, partner := NewMob(TypeSheep, 1, 0.5, 64, 0.5, int64(s)), lover(2, 1.5, TypeSheep)
		mother.Colour, partner.Colour = "blue", "yellow"
		lamb := NewMob(TypeSheep, 3, 0.5, 64, 0.5, 8)
		mother.BreedInto(lamb, partner)
		if lamb.Colour != "blue" && lamb.Colour != "yellow" {
			t.Errorf("seed %d: a blue and a yellow sheep bred %q, want one of the parents", s, lamb.Colour)
		}
	}
}
