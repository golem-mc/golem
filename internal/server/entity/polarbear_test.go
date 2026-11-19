package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// polarSwingAt runs one polarSwing with the bear's cooldown at ticks and the
// player dist blocks along x, outside the reach of a blow.
func polarSwingAt(ticks int, dist float64) (*Mob, *meleeAttackGoal, []MobEvent) {
	m := NewMob(TypePolarBear, 1, 0, 64, 0, 1)
	g := &meleeAttackGoal{m: m, polar: true, ticksUntilAttack: ticks}
	tv := &tickView{}
	g.polarSwing(tv, &Player{X: dist, Y: 64, Z: 0})
	return m, g, tv.events
}

// TestAPolarBearRearsUpOnlyForTheLastTenTicksBeforeABlow checks
// PolarBearMeleeAttackGoal.checkAndPerformAttack: within 3.6 blocks of the
// target it stands once the cooldown is 10 or less, and growls once.
func TestAPolarBearRearsUpOnlyForTheLastTenTicksBeforeABlow(t *testing.T) {
	if m, _, ev := polarSwingAt(11, 3); m.Standing || len(ev) != 0 {
		t.Errorf("11 ticks out: standing %v, events %v; want neither", m.Standing, ev)
	}
	m, _, ev := polarSwingAt(10, 3)
	if !m.Standing || len(ev) != 1 || ev[0].Kind != MobWarned {
		t.Errorf("10 ticks out: standing %v, events %v; want standing and one growl", m.Standing, ev)
	}
	if m.polar.warn != polarWarnInterval {
		t.Errorf("warning counter = %d, want %d", m.polar.warn, polarWarnInterval)
	}
}

func TestAPolarBearInTheWarningGapDoesNotGrowlAgain(t *testing.T) {
	m := NewMob(TypePolarBear, 1, 0, 64, 0, 1)
	m.polar.warn = 12
	g := &meleeAttackGoal{m: m, polar: true, ticksUntilAttack: 5}
	tv := &tickView{}
	g.polarSwing(tv, &Player{X: 3, Y: 64})
	if !m.Standing || len(tv.events) != 0 {
		t.Errorf("standing %v, events %v; want standing and silence", m.Standing, tv.events)
	}
}

// A target beyond 3.6 blocks, or one the bear has no blow ready for, sends it
// back down with the cooldown restarted.
func TestAPolarBearFarFromItsTargetDropsToAllFoursAndResetsTheBlow(t *testing.T) {
	m, g, _ := polarSwingAt(4, 6)
	m.Standing = true
	g.polarSwing(&tickView{}, &Player{X: 6, Y: 64})
	if m.Standing || g.ticksUntilAttack != attackInterval {
		t.Errorf("standing %v, cooldown %d; want down and %d", m.Standing, g.ticksUntilAttack, attackInterval)
	}
	// Ready to strike but out of reach: it drops down, restarts the cooldown
	// and, at 20 ticks, stays down.
	m, g, ev := polarSwingAt(0, 3)
	if m.Standing || g.ticksUntilAttack != attackInterval || len(ev) != 0 {
		t.Errorf("standing %v, cooldown %d, events %v; want down, %d, none", m.Standing, g.ticksUntilAttack, ev, attackInterval)
	}
}

func TestAPolarBearKindNumbers(t *testing.T) {
	m := NewMob(TypePolarBear, 1, 0, 64, 0, 1)
	k := m.kind()
	if k.maxHealth != 30 || k.attackDamage != 6 || k.speed != 0.25 || k.followRange != 20 {
		t.Errorf("health %v, attack %v, speed %v, follow %v; want 30, 6, 0.25, 20", k.maxHealth, k.attackDamage, k.speed, k.followRange)
	}
	m.SetBaby(true)
	if m.width() != 0.7 || m.height() != 0.7 {
		t.Errorf("cub box %v by %v, want 0.7 by 0.7", m.width(), m.height())
	}
	if m.Sounds().Ambient.Name() != "minecraft:entity.polar_bear.ambient_baby" {
		t.Errorf("cub ambient sound = %q", m.Sounds().Ambient.Name())
	}
}

// A natural pack of polar bears is a bear and then cubs: every member after
// the first is a baby.
// vanilla: PolarBear.finalizeSpawn's AgeableMobGroupData(1.0f)
func TestAPolarBearPackIsAdultThenCubs(t *testing.T) {
	g := NewSpawnGroup(GoDice{rand.New(rand.NewSource(5))})
	var babies []bool
	for i := range 3 {
		m := NewMob(TypePolarBear, int32(i+1), 0, 64, 0, int64(i))
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:frozen_ocean"), g, rules.LocalDifficulty{})
		babies = append(babies, m.IsBaby())
	}
	if babies[0] || !babies[1] || !babies[2] {
		t.Errorf("babies = %v, want [false true true]", babies)
	}
}

// A struck cub alerts the grown bears around it and not the other cubs.
// vanilla: PolarBear.PolarBearHurtByTargetGoal.alertOther
func TestAStruckCubAlertsOnlyGrownBears(t *testing.T) {
	cub := NewMob(TypePolarBear, 1, 0, 64, 0, 1)
	cub.SetBaby(true)
	sibling := NewMob(TypePolarBear, 2, 2, 64, 0, 2)
	sibling.SetBaby(true)
	adult := NewMob(TypePolarBear, 3, 4, 64, 0, 3)
	g := &hurtByGoal{m: cub, cub: true}
	g.alertNearby(&tickView{others: []*Mob{cub, sibling, adult}}, 99)
	if adult.TargetID != 99 || sibling.TargetID != 0 {
		t.Errorf("adult targets %d, sibling %d; want 99 and 0", adult.TargetID, sibling.TargetID)
	}
}

// TestAPolarBearSpawnsOnIceInTheFrozenOceanAndGrassElsewhere checks
// PolarBear.checkPolarBearSpawnRules: ice in the frozen oceans, the animals'
// grass block in every other biome.
func TestAPolarBearSpawnsOnIceInTheFrozenOceanAndGrassElsewhere(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, c := range []struct {
		biome, ground string
		want          bool
	}{
		{"minecraft:frozen_ocean", "minecraft:ice", true},
		{"minecraft:deep_frozen_ocean", "minecraft:ice", true},
		{"minecraft:frozen_ocean", "minecraft:water", false},
		{"minecraft:snowy_plains", "minecraft:grass_block", true},
		{"minecraft:snowy_plains", "minecraft:ice", false},
		{"minecraft:frozen_ocean", "minecraft:packed_ice", false},
		{"minecraft:frozen_ocean", "minecraft:blue_ice", false},
		{"minecraft:frozen_ocean", "minecraft:frosted_ice", false},
	} {
		w := newSpawnTestWorld()
		w.sky = 15
		w.biomeName = c.biome
		floor(w.testWorld, -4, -4, 4, 4, 63, defaultState(t, c.ground))
		if got := validSpawnPositionAt(w, GoDice{rng}, TypePolarBear, 0.5, 64, 0.5, 0, CategoryCreature); got != c.want {
			t.Errorf("polar bear on %s in %s = %v, want %v", c.ground, c.biome, got, c.want)
		}
	}
}

// TestACubDoesNotRollTheAttackPlayersScan checks that
// PolarBearAttackPlayersGoal.canUse returns for a baby before the interval
// roll, so the cub's random stream stays where it was.
func TestACubDoesNotRollTheAttackPlayersScan(t *testing.T) {
	for _, baby := range []bool{true, false} {
		m := NewMob(TypePolarBear, 1, 0, 64, 0, 1)
		m.SetBaby(baby)
		ref := NewMob(TypePolarBear, 1, 0, 64, 0, 1)
		for _, g := range polarBearGoals(m) {
			if n, ok := g.(*nearestTargetGoal); ok && n.priority == 2 {
				n.CanStart(&tickView{})
			}
		}
		rolled := m.Rand.Int63() != ref.Rand.Int63()
		if rolled == baby {
			t.Errorf("baby=%v: scan rolled=%v", baby, rolled)
		}
	}
}

// TestThePolarBearResetsAngerAtPriorityFive checks the order of the bear's
// target selector: anger reset sits after the three target goals and the fox
// goal. vanilla: PolarBear.registerGoals
func TestThePolarBearResetsAngerAtPriorityFive(t *testing.T) {
	for _, g := range polarBearGoals(NewMob(TypePolarBear, 1, 0, 64, 0, 1)) {
		if r, ok := g.(*resetAngerGoal); ok && r.Priority() != 5 {
			t.Errorf("reset anger priority = %d, want 5", r.Priority())
		}
	}
}

// TestACubNearOnlyCountsWithinEightBlocksSideways checks the inflate(8, 4, 8)
// box of PolarBearAttackPlayersGoal.canUse.
func TestACubNearOnlyCountsWithinEightBlocksSideways(t *testing.T) {
	bear := NewMob(TypePolarBear, 1, 0, 64, 0, 1)
	for _, c := range []struct {
		dx, dy float64
		want   bool
	}{{7, 0, true}, {12, 0, false}, {0, 3, true}, {0, 8, false}} {
		cub := NewMob(TypePolarBear, 2, c.dx, 64+c.dy, 0, 2)
		cub.SetBaby(true)
		if got := bear.cubNear(&tickView{others: []*Mob{cub}}); got != c.want {
			t.Errorf("cub at (%v, %v) = %v, want %v", c.dx, c.dy, got, c.want)
		}
	}
}

// A cub flees every damage cause; a grown bear only the environmental ones.
// vanilla: PolarBear.PolarBearPanicGoal
func TestACubPanicsAtABlowButAGrownBearDoesNot(t *testing.T) {
	w := newTestWorld()
	floor(w, -6, -6, 6, 6, 63, defaultState(t, "minecraft:stone"))
	for _, c := range []struct {
		baby   bool
		damage registry.DamageType
		want   bool
	}{
		{true, rules.DamageTypePlayerAttack, true},
		{false, rules.DamageTypePlayerAttack, false},
		{false, rules.DamageTypeLava, true},
		{true, rules.DamageTypeLava, true},
	} {
		bear := NewMob(TypePolarBear, 1, 0.5, 64, 0.5, 1)
		bear.OnGround = true
		bear.SetBaby(c.baby)
		var panic *panicGoal
		for _, g := range polarBearGoals(bear) {
			if pg, ok := g.(*panicGoal); ok {
				panic = pg
			}
		}
		bear.noteLastDamage(c.damage)
		if got := panic.CanStart(&tickView{w: w}); got != c.want {
			t.Errorf("baby %v hurt by %s: panics %v, want %v", c.baby, c.damage, got, c.want)
		}
	}
}
