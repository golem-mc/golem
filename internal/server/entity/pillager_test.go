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
