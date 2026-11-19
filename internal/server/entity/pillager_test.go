package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func TestPillagerSpawnPiercingRoll(t *testing.T) {
	piercing, _ := registry.EnchantmentID("minecraft:piercing")
	for _, tc := range []struct {
		name string
		seed int64
		want int32
	}{
		// After the generic roll's Float32, these equipment seeds draw 0
		// and a nonzero value respectively from Intn(300).
		{"hit", 15, 1},
		{"miss", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypePillager, 1, 0.5, 64, 0.5, 3)
			m.lootSeed = tc.seed ^ equipmentSalt
			// Zero special multiplier disables the generic enchantment,
			// isolating the difficulty-independent pillager roll.
			m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
			if m.MainHand.ItemID != int32(registry.ItemCrossbow) || m.MainHand.Count != 1 {
				t.Fatalf("main hand = %+v, want one crossbow", m.MainHand)
			}
			if got := m.MainHand.Enchantments[piercing]; got != tc.want {
				t.Errorf("Piercing level = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestSpreadAimTurnsSidewaysByTheAngle: a multishot arrow leaves the aim by
// the volley's angle, level with the aim and across it.
// vanilla: CrossbowItem.getProjectileShotVector
func TestSpreadAimTurnsSidewaysByTheAngle(t *testing.T) {
	x, y, z := spreadAim(0, 0, 1, 10)
	if got := math.Hypot(x, z); math.Abs(y) > 1e-9 || math.Abs(math.Atan2(math.Abs(x), z)*180/math.Pi-10) > 1e-6 || math.Abs(got-1) > 1e-9 {
		t.Errorf("spread of 10 degrees about +Z = (%v,%v,%v), want a level vector 10 degrees to the side", x, y, z)
	}
	if x, y, z = spreadAim(0, 0, 1, 0); x != 0 || y != 0 || z != 1 {
		t.Errorf("no spread = (%v,%v,%v), want the aim", x, y, z)
	}
}

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
		ox, oy, oz, ax, ay, az := arrowAim(m, p, crossbowAimLift)
		_, _, _ = ox, oy, oz
		n := math.Sqrt(ax*ax + ay*ay + az*az)
		bound := 0.0172275 * tc.inaccuracy
		var worst float64
		for range 300 {
			view := &tickView{w: w}
			m.loadCrossbow()
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

// TestAPillagerIgnoresARaiderButNotAZombie: HurtByTargetGoal(Raider.class)
// spares every raider, the witch and the vindicator among them.
// vanilla: Pillager.registerGoals
func TestAPillagerIgnoresARaiderButNotAZombie(t *testing.T) {
	for _, tc := range []struct {
		kind MobType
		want bool
	}{{TypeWitch, false}, {TypeVindicator, false}, {TypeZombie, true}} {
		w := newTestWorld()
		floor64(w, t)
		p := NewMob(TypePillager, 1, 0.5, 64, 0.5, 3)
		o := NewMob(tc.kind, 2, 1.5, 64, 0.5, 4)
		p.retaliate = o.ID
		view := &tickView{w: w, others: []*Mob{p, o}}
		var goal *hurtByGoal
		for _, g := range pillagerGoals(p) {
			if h, ok := g.(*hurtByGoal); ok {
				goal = h
			}
		}
		if goal == nil {
			t.Fatal("pillagerGoals has no hurt-by goal")
		}
		if got := goal.CanStart(view); got != tc.want {
			t.Errorf("%s: retaliation = %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// TestACrossbowAttackStopKeepsTheAttackWait: losing the target mid-wait
// leaves the state and the wait running; only a draw in progress is undone.
// vanilla: RangedCrossbowAttackGoal.stop
func TestACrossbowAttackStopKeepsTheAttackWait(t *testing.T) {
	m := NewMob(TypePillager, 1, 0.5, 64, 0.5, 3)
	g := &crossbowAttackGoal{m: m, state: crossbowChargedWaiting, attackWait: 12}
	m.loadCrossbow()
	g.Stop(&tickView{w: newTestWorld()})
	if g.state != crossbowChargedWaiting || g.attackWait != 12 || !m.CrossbowLoaded() {
		t.Errorf("after Stop: state %d, wait %d, loaded %v; want the wait kept", g.state, g.attackWait, m.CrossbowLoaded())
	}
	m.UsingItem, m.ChargingCrossbow = true, true
	g.Stop(&tickView{w: newTestWorld()})
	if m.UsingItem || m.ChargingCrossbow || m.CrossbowLoaded() {
		t.Errorf("a draw in progress survived Stop: using %v charging %v loaded %v", m.UsingItem, m.ChargingCrossbow, m.CrossbowLoaded())
	}
}

func TestFreshIllagersRollPatrolLeader(t *testing.T) {
	count := func(typ MobType, structure bool) (leaders, patrolling int) {
		for seed := int64(1); seed <= 2000; seed++ {
			m := NewMob(typ, 1, 0.5, 64, 0.5, seed)
			if structure {
				m.FinalizeStructureSpawn(m.SpawnDice(), registry.BiomePlains, false, rules.LocalDifficulty{})
			} else {
				m.FinalizeSpawn(m.SpawnDice(), registry.BiomePlains, nil, rules.LocalDifficulty{})
			}
			if m.PatrolLeader {
				leaders++
			}
			if m.Patrolling {
				patrolling++
			}
		}
		return
	}
	if n, p := count(TypePillager, false); n < 80 || n > 160 || p != 0 {
		t.Errorf("pillager leaders = %d, patrolling = %d, want 80..160 and 0", n, p)
	}
	for _, typ := range []MobType{TypeWitch, TypeRavager} {
		if n, _ := count(typ, false); n != 0 {
			t.Errorf("type %v leaders = %d, want 0", typ, n)
		}
	}
	if n, _ := count(TypePillager, true); n != 0 {
		t.Errorf("structure pillager leaders = %d, want 0", n)
	}
}
