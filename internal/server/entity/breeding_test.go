package entity

import (
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
					want = 61 + NewDice(seed).Intn(50) // Include the start tick before the duration elapses.
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
