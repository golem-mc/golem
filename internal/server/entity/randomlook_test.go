package entity

import (
	"math"
	"testing"
)

func TestAnActiveStrollBlocksRandomLookAroundAtTheSamePriority(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	m := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	stroll := &randomStroll{m: m, target: Waypoint{X: 3, Y: 64, Z: 0}, blocked: func() bool { return true }}
	look := &randomLookAround{m: m, priority: stroll.Priority()}
	view := &tickView{w: w}
	stroll.Start(view)
	if !stroll.ShouldContinue(view) {
		t.Fatal("stroll has no active path")
	}
	sel := goalSelector{running: []Goal{stroll}}
	sel.wire(stroll, look)
	// Keep the path active without advancing navigation, giving the random
	// look repeated opportunities to contend for the stroll's MOVE control.
	for range 1000 {
		if sel.tick(view, 0) || sel.isRunning(look) {
			t.Fatal("random look started while a same-priority stroll held MOVE")
		}
	}
	stroll.Stop(view)
	for range 1000 {
		if sel.tick(view, 0) {
			if !sel.isRunning(look) || sel.isRunning(stroll) {
				t.Fatal("random look did not acquire the released controls")
			}
			return
		}
	}
	t.Fatal("random look never started after the stroll ended")
}

func TestRandomLookAroundCoversFullCircleAtEyeLevel(t *testing.T) {
	var widest float64
	for seed := int64(1); seed <= 500; seed++ {
		cow := NewMob(TypeCow, 1, 0.5, 66, 0.5, seed)
		g := &randomLookAround{m: cow}
		g.Start(nil)
		d := math.Abs(math.Mod(yawToward(g.relX, g.relZ)-float64(cow.HeadYaw)+540, 360) - 180)
		widest = math.Max(widest, d)
	}
	if widest <= 90 {
		t.Errorf("widest yaw turn = %v, want more than 90", widest)
	}
}

func TestRandomLookAroundTurnSpeed(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0.5, 66, 0.5, 1)
	rad := 120 * math.Pi / 180
	g := &randomLookAround{m: cow, relX: -math.Sin(rad), relZ: math.Cos(rad), remains: 5}
	cow.HeadYaw, cow.Pitch = 0, 30
	g.Tick(nil)
	cow.lookControlTick(true)
	if cow.HeadYaw != 10 || cow.Pitch != 0 {
		t.Errorf("after one tick yaw=%v pitch=%v, want 10 and 0", cow.HeadYaw, cow.Pitch)
	}
}
