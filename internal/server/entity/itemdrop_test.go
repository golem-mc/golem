package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/google/uuid"
)

// TestSpillSplitsEachStackIntoTenToThirty spills a full stack and a small
// one: every piece holds ten to thirty items (or the remainder), the counts
// add back up, and the pieces of one stack share one spot inside the block.
func TestSpillSplitsEachStackIntoTenToThirty(t *testing.T) {
	stacks := []protocol.ItemStack{{ItemID: 1, Count: 64}, {}, {ItemID: 2, Count: 5}}
	for seed := range int64(200) {
		for _, p := range Spill(rules.NewJavaRandom(seed), 3, 70, -2, stacks) {
			if p.Y < 70 {
				t.Fatalf("seed %d: piece at y=%v, below the block", seed, p.Y)
			}
		}
	}
	pieces := Spill(rules.NewJavaRandom(42), 3, 70, -2, stacks)
	totals := map[int32]int32{}
	spots := map[int32][3]float64{}
	for _, p := range pieces {
		if p.Stack.Count < 1 || p.Stack.Count > 30 {
			t.Errorf("piece of %d items, want 1..30", p.Stack.Count)
		}
		totals[p.Stack.ItemID] += p.Stack.Count
		spot := [3]float64{p.X, p.Y, p.Z}
		if was, ok := spots[p.Stack.ItemID]; ok && was != spot {
			t.Errorf("item %d pieces at %v and %v, want one spot", p.Stack.ItemID, was, spot)
		}
		spots[p.Stack.ItemID] = spot
		if p.X < 3 || p.X > 4 || p.Y < 70+ItemHalfSize || p.Y >= 70.75+ItemHalfSize || p.Z < -2 || p.Z > -1 {
			t.Errorf("piece at (%v, %v, %v), outside the block", p.X, p.Y, p.Z)
		}
	}
	if totals[1] != 64 || totals[2] != 5 || len(totals) != 2 {
		t.Errorf("spilled %v, want 64 of item 1 and 5 of item 2", totals)
	}
}

// TestFaceDropThrowsOutOfTheClickedFace checks the top face's drop: half a
// block above the centre (the item's feet at the top), pushed up a tenth plus the lift.
func TestFaceDropThrowsOutOfTheClickedFace(t *testing.T) {
	_, py, _, _, vy, _ := FaceDrop(rand.New(rand.NewSource(1)), 0, 64, 0, protocol.FaceTop)
	if want := 64 + 0.5 + 0.5; py != want {
		t.Errorf("y = %v, want %v", py, want)
	}
	if want := 2 * ItemPopScatter; vy != want {
		t.Errorf("vy = %v, want %v", vy, want)
	}
}

// TestPopVelocityRisesAtThePopSpeed checks the constructor's toss: the
// fixed rise and a sideways scatter within a tenth either way.
func TestPopVelocityRisesAtThePopSpeed(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for range 100 {
		vx, vy, vz := PopVelocity(r)
		if vy != ItemPopSpeed || vx < -ItemPopScatter || vx > ItemPopScatter || vz < -ItemPopScatter || vz > ItemPopScatter {
			t.Fatalf("PopVelocity = (%v, %v, %v)", vx, vy, vz)
		}
	}
}

// TestItemAllowsPickupByTarget checks the owner rule: no target is anyone's,
// a target is that player's alone.
func TestItemAllowsPickupByTarget(t *testing.T) {
	it := NewItem(1, protocol.ItemStack{ItemID: 1, Count: 1}, 0, 0, 0, 0, 0, 0, 0, 1)
	a, b := uuid.New(), uuid.New()
	if !it.AllowsPickup(a) {
		t.Error("untargeted item refused a player")
	}
	it.Target = &a
	if !it.AllowsPickup(a) || it.AllowsPickup(b) {
		t.Error("targeted item: want only its target allowed")
	}
}

// TestOrbTakesAwardOnlyOfItsValueAndShift checks the award merge's gates:
// the value, the id shift, the box and a dead orb.
func TestOrbTakesAwardOnlyOfItsValueAndShift(t *testing.T) {
	o := NewOrb(41, 0.5, 64, 0.5, 0, 0, 0, 3, 1)
	if !o.TakesAward(0.5, 64, 0.5, 3, 1) {
		t.Error("orb refused a matching award")
	}
	if o.TakesAward(0.5, 64, 0.5, 7, 1) {
		t.Error("orb took an award of another value")
	}
	if o.TakesAward(0.5, 64, 0.5, 3, 2) {
		t.Error("orb took an award the shift does not name")
	}
	if o.TakesAward(5, 64, 0.5, 3, 1) {
		t.Error("orb took an award outside its box")
	}
	o.Dead = true
	if o.TakesAward(0.5, 64, 0.5, 3, 1) {
		t.Error("dead orb took an award")
	}
}
