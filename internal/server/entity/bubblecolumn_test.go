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

func TestAnItemInADraggingColumnSinks(t *testing.T) {
	t.Parallel()
	plain, drag := bubblePool(t, 12, false, false), bubblePool(t, 12, true, true)
	a := &Item{X: 0.5, Y: 8.5, Z: 0.5}
	b := &Item{X: 0.5, Y: 8.5, Z: 0.5}
	a.Step(plain, nil)
	b.Step(drag, nil)
	if got, want := b.VY-a.VY, -0.03*airDragFloat; !closeEnough(got, want) {
		t.Errorf("extra drag in one tick = %v, want %v", got, want)
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

func TestAMobInADraggingColumnSinksAtTheCap(t *testing.T) {
	t.Parallel()
	drag := bubblePool(t, 30, true, true)
	m := aimless(NewMob(TypeCow, 1, 0.5, 25, 0.5, 1))
	for range 60 {
		m.Tick(drag, nil)
	}
	if m.VY >= -0.1 || m.VY < -0.31 {
		t.Errorf("sinking speed = %v, want a drag-capped sink near -0.3", m.VY)
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

// vanilla: AbstractBoat.floatBoat, tickBubbleColumn
func TestASubmergedBoatInAColumnDoesNotStartASurfaceCountdown(t *testing.T) {
	t.Parallel()
	for _, drag := range []bool{false, true} {
		w := bubblePool(t, 100, true, drag)
		b := NewBoat(1, 0.5, 50, 0.5, 0)
		for tick := 1; tick <= boatBubbleTicks; tick++ {
			b.Step(w, false)
			if b.BubbleTime != 0 || b.Thrown != BoatThrowNone {
				t.Fatalf("drag=%v tick %d: submerged boat at y=%v has timer %d and throw %v", drag, tick, b.Y, b.BubbleTime, b.Thrown)
			}
		}
		if drag && b.Y >= 50 || !drag && b.Y <= 50 {
			t.Errorf("drag=%v: submerged boat moved from y=50 to %v", drag, b.Y)
		}
	}
}

func TestABoatInsideAColumnTakesItsPushTwiceATick(t *testing.T) {
	t.Parallel()
	b := NewBoat(1, 0.5, 3, 0.5, 0)
	w := newTestWorld()
	w.set(0, 3, 0, bubbleColumnState(t, true))
	w.set(0, 4, 0, bubbleColumnState(t, true))
	b.bubbleColumn(w, false)
	if want := -0.06; !closeEnough(b.VY, want) {
		t.Errorf("vy after one tick's pushes = %v, want %v", b.VY, want)
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

func TestAnOrbInALiftingColumnRises(t *testing.T) {
	t.Parallel()
	plain, lift := bodyRise(t, 5, func(w World) float64 {
		o := NewOrb(1, 0.5, 5, 0.5, 0, 0, 0, 1, 1)
		for range 10 {
			o.Step(w, nil, nil)
		}
		return o.Y
	})
	if lift < plain+0.3 {
		t.Errorf("orb rose %v in the column, %v in water", lift, plain)
	}
}

func TestAFallingBlockInALiftingColumnRisesAndForgetsItsFall(t *testing.T) {
	t.Parallel()
	state, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("no sand state")
	}
	var fall float64
	plain, lift := bodyRise(t, 20, func(w World) float64 {
		f := NewFallingBlock(1, 0.5, 20, 0.5, 0, 0, 0, state, 1)
		for range 10 {
			f.Step(w)
		}
		fall = f.FallDistance
		return f.Y
	})
	if lift < plain+0.3 {
		t.Errorf("block rose %v in the column, %v in water", lift, plain)
	}
	if fall != 0 {
		t.Errorf("fall distance in the column = %v, want 0", fall)
	}
}

func TestPrimedTNTInALiftingColumnRises(t *testing.T) {
	t.Parallel()
	plain, lift := bodyRise(t, 20, func(w World) float64 {
		tnt := NewTNT(1, 0.5, 20, 0.5, 0, 0, 0, 80, 0, 0)
		for range 10 {
			tnt.Step(w)
		}
		return tnt.Y
	})
	if lift < plain+0.3 {
		t.Errorf("TNT rose %v in the column, %v in water", lift, plain)
	}
}

func TestAMinecartInALiftingColumnRises(t *testing.T) {
	t.Parallel()
	plain, lift := bodyRise(t, 20, func(w World) float64 {
		m := NewMinecart(1, 0.5, 20, 0.5)
		for range 10 {
			m.Step(w, false)
		}
		return m.Y
	})
	if lift < plain+0.3 {
		t.Errorf("minecart rose %v in the column, %v in water", lift, plain)
	}
}

func TestAnArmorStandInALiftingColumnRises(t *testing.T) {
	t.Parallel()
	plain, lift := bodyRise(t, 20, func(w World) float64 {
		s := NewArmorStand(1, 0.5, 20, 0.5)
		for range 10 {
			s.Step(w)
		}
		return s.Y
	})
	if lift < plain+0.3 {
		t.Errorf("armor stand rose %v in the column, %v in water", lift, plain)
	}
}

func TestAFallingBlockDroppingIntoAColumnForgetsItsFall(t *testing.T) {
	t.Parallel()
	state, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("no sand state")
	}
	f := NewFallingBlock(1, 0.5, 43, 0.5, 0, 0, 0, state, 1)
	w := bubblePool(t, 40, true, true)
	var peak float64
	for range 20 {
		f.Step(w)
		peak = max(peak, f.FallDistance)
	}
	if peak == 0 {
		t.Fatal("the block built no fall above the column")
	}
	if f.Y >= 40 || f.FallDistance != 0 {
		t.Errorf("block at %v with fall %v, want inside the column with no fall", f.Y, f.FallDistance)
	}
}

func TestAnOrbInADraggingColumnSinksFasterThanInWater(t *testing.T) {
	t.Parallel()
	fall := func(w World) float64 {
		o := NewOrb(1, 0.5, 30, 0.5, 0, 0, 0, 1, 1)
		for range 10 {
			o.Step(w, nil, nil)
		}
		return o.Y
	}
	if plain, drag := fall(bubblePool(t, 40, false, false)), fall(bubblePool(t, 40, true, true)); drag > plain-0.1 {
		t.Errorf("orb at %v in the dragging column, %v in water", drag, plain)
	}
}

func TestATNTAtTheSurfaceCellIsLiftedByTheSurfaceSpeed(t *testing.T) {
	t.Parallel()
	speed := func(column bool) float64 {
		// Cell 9 is the top of the column and the box stands in it alone.
		tnt := NewTNT(1, 0.5, 9.3, 0.5, 0, 0, 0, 80, 0, 0)
		tnt.Step(bubblePool(t, 10, column, false))
		return tnt.VY
	}
	// The surface adds 0.1, scaled by the tick's 0.98 drag; the inside's 0.06
	// would add 0.0588.
	if got := speed(true) - speed(false); got < 0.09 || got > 0.105 {
		t.Errorf("extra speed at the surface = %v, want about 0.098", got)
	}
}

func TestANoGravityArmorStandIgnoresAColumn(t *testing.T) {
	t.Parallel()
	s := NewArmorStand(1, 0.5, 20, 0.5)
	s.NoGravity = true
	for range 10 {
		s.Step(bubblePool(t, 40, true, false))
	}
	if s.Y != 20 || s.VY != 0 {
		t.Errorf("no-gravity stand at %v with speed %v", s.Y, s.VY)
	}
}

func TestAnOffTrackMinecartIsPushedTwiceATick(t *testing.T) {
	t.Parallel()
	vy := func(w World) float64 {
		m := NewMinecart(1, 0.5, 20, 0.5)
		m.Step(w, false)
		return m.VY
	}
	plain, lift := vy(bubblePool(t, 40, false, false)), vy(bubblePool(t, 40, true, false))
	if lift-plain < 0.2 {
		t.Errorf("one tick's extra speed = %v, want two pushes", lift-plain)
	}
}
