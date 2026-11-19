package entity

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

const (
	// spearUseApproach and spearUseInRange are SpearUseGoal's approach distance
	// and the target-in-range radius.
	// vanilla: Zombie.addBehaviourGoals SpearUseGoal(this, 1.0, 1.0, 10.0f, 2.0f)
	spearUseApproach, spearUseInRange = 10.0, 2.0
	// spearUseMountExtra is added to the reposition and cooldown distances of a rider.
	spearUseMountExtra = 2.0
	// spearUseDone is SpearUseGoal.MAX_FLEEING_TIME in ticks.
	spearUseDone = 100
	// priorityZombieSpear is SpearUseGoal's slot in Zombie.addBehaviourGoals.
	priorityZombieSpear = 2
)

// spearUseGoal is the zombie family's charge with a kinetic weapon: approach
// to ten blocks, charge with the item in use, reposition when close, and flee
// when the use ends. A rider moves at its root vehicle's chargeSpeedModifier.
// vanilla: SpearUseGoal
type spearUseGoal struct {
	m *Mob
	spearState
	// engage is the ticks of the use left, -1 before it starts; fleeing counts
	// from 1 once the use ends, 0 before.
	engage, fleeing int
	started, done   bool
}

func (g *spearUseGoal) Priority() int           { return priorityZombieSpear }
func (g *spearUseGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *spearUseGoal) RequiresEveryTick() bool { return true }

func (g *spearUseGoal) CanStart(t *tickView) bool {
	return g.able(t) && !g.m.UsingItem
}

func (g *spearUseGoal) able(t *tickView) bool {
	return g.m.holdsKinetic() && g.m.targetIn(t) != nil
}

func (g *spearUseGoal) ShouldContinue(t *tickView) bool {
	return g.started && !g.done && g.able(t)
}

func (g *spearUseGoal) Start(*tickView) {
	g.spearState = spearState{}
	g.engage, g.fleeing, g.started, g.done = -1, 0, true, false
	g.m.SetMetadataAggressive(true)
}

func (g *spearUseGoal) Stop(*tickView) {
	m := g.m
	m.stopSeeking()
	m.SetMetadataAggressive(false)
	g.started = false
	m.SetMetadataUsingItem(false)
}

func (g *spearUseGoal) Tick(t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if !g.started || p == nil {
		return
	}
	dist2 := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
	speed := m.chargeSpeedModifier()
	mountExtra := 0.0
	if m.Vehicle != nil {
		mountExtra = spearUseMountExtra
	}
	// SpearUseGoal requests both Mob.lookAt and LookControl.setLookAt.
	dx, dz := p.X-m.X, p.Z-m.Z
	m.Yaw = turnToward(m.Yaw, float32(yawToward(dx, dz)), 30)
	m.Pitch = turnToward(m.Pitch, float32(pitchToward(dx, p.Y+p.eyeHeight()-(m.Y+m.eyeHeight()), dz)), 30)
	m.lookAt(p.X, p.Y+p.eyeHeight(), p.Z, 30, 30)
	k := registry.StackKineticWeapon(m.MainHand)
	if g.engage < 0 {
		if dist2 > spearUseApproach*spearUseApproach {
			m.seekTo(t.w, p.X, p.Y, p.Z, speed, 0)
			return
		}
		g.engage = int(k.ComputeDamageUseDuration())
		m.SetMetadataUsingItem(true)
		g.used = 0
	}
	if g.engage > 0 {
		g.engage--
		if g.engage == 0 {
			m.SetMetadataUsingItem(false)
			d := math.Sqrt(dist2)
			g.away, g.hasAway = posAwayBetween(t.w, m, max(0, 9+mountExtra-d), max(1, 11+mountExtra-d), spearVerticalRange, p.X, p.Z)
			g.fleeing = 1
		}
	}
	if g.fleeing > 0 {
		g.fleeing++
		if g.fleeing > spearUseDone {
			g.done = true
			return
		}
	}
	switch {
	case g.hasAway:
		m.seekTo(t.w, float64(g.away.X)+0.5, float64(g.away.Y), float64(g.away.Z)+0.5, speed, 0)
		if m.navDone() {
			if g.fleeing > 0 {
				g.done = true
				return
			}
			g.hasAway = false
		}
	default:
		m.seekTo(t.w, p.X, p.Y, p.Z, speed, 0)
		if dist2 < spearUseInRange*spearUseInRange || m.navDone() {
			d := math.Sqrt(dist2)
			g.away, g.hasAway = posAwayBetween(t.w, m, 6+mountExtra-d, 7+mountExtra-d, spearVerticalRange, p.X, p.Z)
		}
	}
	if m.UsingItem {
		g.used++
		m.stab(t, &g.spearState, k, g.used)
	}
}

// NavSpeedFactor is the speed factor of the navigation this mob controls.
func (m *Mob) NavSpeedFactor() float64 { return m.navOf().nav.speedFactor }
