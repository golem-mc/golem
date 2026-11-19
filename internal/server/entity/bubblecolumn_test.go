package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func bubbleColumnState(t *testing.T, drag bool) uint16 {
	t.Helper()
	d := "false"
	if drag {
		d = "true"
	}
	state, ok := registry.FindState("minecraft:bubble_column", map[string]string{"drag": d})
	if !ok {
		t.Fatal("no bubble column state")
	}
	return state
}

// bubblePool fills a 3x3 pool y=0..height-1 with either bubble column cells
// or plain water.
func bubblePool(t *testing.T, height int32, column, drag bool) testWorld {
	t.Helper()
	w := newTestWorld()
	fill := boatWater
	if column {
		fill = bubbleColumnState(t, drag)
	}
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			for y := range height {
				w.set(x, y, z, fill)
			}
		}
	}
	return w
}

func TestBubbleColumnSpeeds(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		fn       func(float64, bool) float64
		vy       float64
		dragDown bool
		want     float64
	}{
		{"inside lifts", rules.BubbleColumnInsideSpeed, 0, false, 0.06},
		{"inside lift cap", rules.BubbleColumnInsideSpeed, 0.69, false, 0.7},
		{"inside drags", rules.BubbleColumnInsideSpeed, 0, true, -0.03},
		{"inside drag cap", rules.BubbleColumnInsideSpeed, -0.29, true, -0.3},
		{"surface lifts", rules.BubbleColumnAboveSpeed, 0, false, 0.1},
		{"surface lift cap", rules.BubbleColumnAboveSpeed, 1.75, false, 1.8},
		{"surface drags", rules.BubbleColumnAboveSpeed, 0, true, -0.03},
		{"surface drag cap", rules.BubbleColumnAboveSpeed, -0.89, true, -0.9},
	} {
		if got := c.fn(c.vy, c.dragDown); !closeEnough(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAnItemInALiftingColumnRisesFasterThanInWater(t *testing.T) {
	t.Parallel()
	plain, lift := bubblePool(t, 12, false, false), bubblePool(t, 12, true, false)
	a := &Item{X: 0.5, Y: 3.5, Z: 0.5}
	b := &Item{X: 0.5, Y: 3.5, Z: 0.5}
	a.Step(plain, nil)
	b.Step(lift, nil)
	if got, want := b.VY-a.VY, 0.06*airDragFloat; !closeEnough(got, want) {
		t.Errorf("extra lift in one tick = %v, want %v", got, want)
	}
	for range 15 {
		a.Step(plain, nil)
		b.Step(lift, nil)
	}
	if b.Y < a.Y+2 {
		t.Errorf("after 16 ticks the item in the column is at %v, the one in water at %v", b.Y, a.Y)
	}
}

func TestAMobInALiftingColumnRises(t *testing.T) {
	t.Parallel()
	plain, lift := bubblePool(t, 20, false, false), bubblePool(t, 20, true, false)
	a := aimless(NewMob(TypeCow, 1, 0.5, 2, 0.5, 1))
	b := aimless(NewMob(TypeCow, 2, 0.5, 2, 0.5, 1))
	for range 20 {
		a.Tick(plain, nil)
		b.Tick(lift, nil)
	}
	if b.Y < a.Y+3 {
		t.Errorf("after 20 ticks the cow in the column is at %v, the one in water at %v", b.Y, a.Y)
	}
}

func TestAnUnriddenBoatAtAColumnsSurfaceIsThrownAfterSixtyTicks(t *testing.T) {
	t.Parallel()
	lift := bubblePool(t, 4, true, false)
	// The pool ends at y=4; the hull must straddle its surface.
	// vanilla: AbstractBoat.floatBoat, onAboveBubbleColumn
	b := NewBoat(1, 0.5, 4-boatBuoyancyDiv*BoatHeight, 0.5, 0)
	peak, firstThrow := 0.0, 0
	for i := 1; i <= 120; i++ {
		b.Step(lift, false)
		if b.VY > peak {
			peak = b.VY
		}
		if i <= boatBubbleTicks && b.BubbleTime != boatBubbleTicks-i {
			t.Errorf("tick %d: bubble timer = %d, want %d", i, b.BubbleTime, boatBubbleTicks-i)
		}
		if b.EjectPassengers {
			t.Fatal("a lifting column shook the riders loose")
		}
		if firstThrow == 0 && b.Thrown == BoatThrowLaunch {
			firstThrow = i
			if !closeEnough(b.VY, boatBubbleLift) {
				t.Errorf("launch speed = %v, want %v", b.VY, boatBubbleLift)
			}
		}
	}
	if firstThrow == 0 {
		t.Fatalf("the boat on the column was never thrown, peak vy %v", peak)
	}
	plain := bubblePool(t, 4, false, false)
	c := NewBoat(2, 0.5, 4-boatBuoyancyDiv*BoatHeight, 0.5, 0)
	for range 120 {
		c.Step(plain, false)
		if c.Thrown != BoatThrowNone || c.BubbleTime != 0 || c.VY > 0.5 {
			t.Fatalf("the boat on plain water took a column effect: throw %v, timer %d, vy %v", c.Thrown, c.BubbleTime, c.VY)
		}
	}
	if firstThrow != boatBubbleTicks {
		t.Errorf("first throw on tick %d, want %d", firstThrow, boatBubbleTicks)
	}
}

func TestADraggedBoatShakesItsPassengersLoose(t *testing.T) {
	t.Parallel()
	drag := bubblePool(t, 4, true, true)
	b := NewBoat(1, 0.5, 4-boatBuoyancyDiv*BoatHeight, 0.5, 0)
	thrown := 0
	for i := 1; i <= 80 && thrown == 0; i++ {
		b.Step(drag, false)
		if i <= boatBubbleTicks && b.BubbleTime != boatBubbleTicks-i {
			t.Errorf("tick %d: bubble timer = %d, want %d", i, b.BubbleTime, boatBubbleTicks-i)
		}
		if b.EjectPassengers {
			if b.Thrown != BoatThrowSink || b.VY > -0.6 {
				t.Errorf("ejection with throw %v and vy %v, want a downward column throw", b.Thrown, b.VY)
			}
			thrown = i
		}
	}
	if thrown != boatBubbleTicks {
		t.Errorf("riders shaken loose on tick %d, want %d", thrown, boatBubbleTicks)
	}
	lift := bubblePool(t, 4, true, false)
	c := NewBoat(2, 0.5, 4-boatBuoyancyDiv*BoatHeight, 0.5, 0)
	for range 80 {
		c.Step(lift, false)
		if c.EjectPassengers {
			t.Fatal("a lifting column shook the riders loose")
		}
	}
}

func TestBodiesAtAColumnsSurfaceSendParticlesAndInsideOnesDoNot(t *testing.T) {
	t.Parallel()
	deep := bubblePool(t, 12, true, false)
	count := func(events []ItemEvent) int {
		n := 0
		for _, ev := range events {
			if ev.Kind == ItemBubbleSurface {
				n++
				if ev.X < -1 || ev.X > 1 || ev.Y != 11 || ev.Z < -1 || ev.Z > 1 {
					t.Errorf("surface cell (%d %d %d) is not the top of the column", ev.X, ev.Y, ev.Z)
				}
			}
		}
		return n
	}
	top := &Item{X: 0.5, Y: 11.5, Z: 0.5}
	if count(top.Step(deep, nil)) == 0 {
		t.Error("an item at the top of the column sent no surface event")
	}
	mid := &Item{X: 0.5, Y: 5.5, Z: 0.5}
	if count(mid.Step(deep, nil)) != 0 {
		t.Error("an item inside the column sent a surface event")
	}
	surfaceMobs := func(m *Mob) int {
		n := 0
		for _, ev := range m.Tick(deep, nil) {
			if ev.Kind == MobBubbleSurface {
				n++
			}
		}
		return n
	}
	if surfaceMobs(aimless(NewMob(TypeCow, 1, 0.5, 11, 0.5, 1))) == 0 {
		t.Error("a mob at the top of the column sent no surface event")
	}
	if surfaceMobs(aimless(NewMob(TypeCow, 2, 0.5, 4, 0.5, 1))) != 0 {
		t.Error("a mob inside the column sent a surface event")
	}
}

// bodyRise runs the same body in a plain pool and in a lifting column and
// reports how far each ended up from where it began.
func bodyRise(t *testing.T, y0 float64, step func(w World) float64) (plain, lift float64) {
	t.Helper()
	return step(bubblePool(t, 40, false, false)) - y0, step(bubblePool(t, 40, true, false)) - y0
}
