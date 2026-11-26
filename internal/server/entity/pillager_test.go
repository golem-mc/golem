package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// TestACrossbowShotScattersByTheDifficultyInaccuracy: each axis of the shot
// strays from the aim by at most 0.0172275 x (14 - 4 x difficulty), and
// reaches into the upper half of that bound over many shots.
// vanilla: CrossbowAttackMob.performCrossbowAttack, Projectile.getMovementToShoot
func TestACrossbowShotScattersByTheDifficultyInaccuracy(t *testing.T) {
	for _, tc := range []struct {
		difficulty uint8
		inaccuracy float64
	}{{rules.DifficultyEasy, 10}, {rules.DifficultyHard, 2}} {
		w := newTestWorld()
		w.diffUp, w.diff = true, tc.difficulty
		m := NewMob(TypePillager, 1, 0.5, 64, 0.5, 3)
		m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1}
		p := &Player{X: 8.5, Y: 64, Z: 0.5}
		g := &crossbowAttackGoal{m: m}
		ox, oy, oz, ax, ay, az := arrowAim(m, p, crossbowAimLift, ArrowSpawnBelow)
		_, _, _ = ox, oy, oz
		n := math.Sqrt(ax*ax + ay*ay + az*az)
		bound := 0.0172275 * tc.inaccuracy
		var worst float64
		for range 300 {
			view := &tickView{w: w}
			g.loadCrossbow(view, m.MainHand)
			g.shoot(view, p)
			for _, ev := range view.events {
				if ev.Kind != MobShotArrow {
					continue
				}
				for _, d := range []float64{ev.VX/crossbowMobPower - ax/n, ev.VY/crossbowMobPower - ay/n, ev.VZ/crossbowMobPower - az/n} {
					worst = math.Max(worst, math.Abs(d))
				}
			}
		}
		if worst > bound*1.0001 || worst < bound*0.5 {
			t.Errorf("difficulty %d: worst stray %.4f, want within (%.4f, %.4f]", tc.difficulty, worst, bound*0.5, bound)
		}
	}
}

// TestAPillagerLoadsItsCrossbowTwentyFiveTicksAfterTheDrawStarts: the goal
// tick that starts the draw is tick 0, and the arrow is loaded on tick 25 with
// no Quick Charge.
// vanilla: RangedCrossbowAttackGoal.tick, CrossbowItem.getChargeDuration
func TestAPillagerLoadsItsCrossbowTwentyFiveTicksAfterTheDrawStarts(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypePillager, 1, 0.5, 64, 0.5, 3)
	m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1}
	g := &crossbowAttackGoal{m: m}
	view := &tickView{w: w}
	p := &Player{X: 4.5, Y: 64, Z: 0.5}
	loaded := -1
	for tick := 0; tick < 60 && loaded < 0; tick++ {
		g.stepCrossbow(view, p, true, true)
		if m.CrossbowLoaded() {
			loaded = tick
		}
	}
	if loaded != 25 {
		t.Errorf("crossbow loaded on tick %d after the draw started, want 25", loaded)
	}
}

// TestAPillagerWithATargetInRangeLoadsOnTheThirtiethGoalTick: the draw waits
// for five ticks of sight, then takes 25, and the shot follows a 20 to 39
// tick wait.
// vanilla: RangedCrossbowAttackGoal.tick
func TestAPillagerWithATargetInRangeLoadsOnTheThirtiethGoalTick(t *testing.T) {
	w := newTestWorld()
	w.diffUp, w.diff = true, rules.DifficultyHard
	m := NewMob(TypePillager, 1, 0.5, 64, 0.5, 3)
	m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1}
	p := Player{EntityID: 7, X: 4.5, Y: 64, Z: 0.5, Huntable: true}
	m.TargetID = p.EntityID
	g := &crossbowAttackGoal{m: m}
	view := &tickView{w: w, players: []Player{p}}
	loaded, shot := 0, 0
	for tick := 1; tick <= 100 && shot == 0; tick++ {
		view.events = view.events[:0]
		g.Tick(view)
		if loaded == 0 && m.CrossbowLoaded() {
			loaded = tick
		}
		for _, ev := range view.events {
			if ev.Kind == MobShotArrow {
				shot = tick
			}
		}
	}
	if loaded != 30 {
		t.Errorf("crossbow loaded on goal tick %d, want 30", loaded)
	}
	if wait := shot - loaded; wait < 20 || wait > 39 {
		t.Errorf("shot %d ticks after the load, want 20 to 39", wait)
	}
}
