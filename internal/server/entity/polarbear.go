package entity

import "github.com/golem-mc/golem/registry"

// TypePolarBear is the polar bear, a neutral animal that guards its cub.
const TypePolarBear = registry.EntityTypePolarBear

// polarState is the polar bear's own state.
type polarState struct {
	// warn counts the ticks until the warning growl may sound again.
	warn int
}

// The polar bear's numbers.
// vanilla: PolarBear
const (
	polarAttackPace   = 1.25 // PolarBearMeleeAttackGoal's speed modifier
	polarWarnInterval = 40   // PolarBear.playWarningSound
	polarStandWindow  = 10   // PolarBearMeleeAttackGoal.checkAndPerformAttack
	polarStandMargin  = 3.0  // target width plus this, squared, is the standing range
	polarCubReachXZ   = 8.0  // PolarBearAttackPlayersGoal's inflate(8, 4, 8)
	polarCubReachY    = 4.0
	polarScanInterval = 20 // PolarBearAttackPlayersGoal's randomInterval
	polarFollowShare  = 0.5
)

// polarBearGoals is PolarBear.registerGoals: melee at 1.25, panic (any cause
// for a cub, environmental only when grown), cub following, stroll, two
// glances; the cub-hurt grudge, the hunt of players near a cub, and the
// anger and fox hunts.
func polarBearGoals(m *Mob) []Goal {
	return []Goal{
		newFloatGoal(m),
		&meleeAttackGoal{m: m, priority: 1, followUnseen: true, pace: polarAttackPace, polar: true},
		&panicGoal{m: m, causes: panicEnvironmentalCauses, babyCauses: panicCauses},
		&hurtByGoal{m: m, priority: 1, cub: true},
		&nearestTargetGoal{m: m, priority: 2, interval: polarScanInterval, followShare: polarFollowShare,
			// vanilla: PolarBearAttackPlayersGoal.canUse returns for a baby before the roll
			refuse: m.IsBaby,
			accept: func(t *tickView, p *Player) bool {
				return m.distanceToPlayer(p) <= max(m.followRange()*polarFollowShare*visibilityPercent(m, p), minVisibilityDistance)
			},
			gate: m.cubNear},
		&nearestTargetGoal{m: m, priority: 3, accept: func(t *tickView, p *Player) bool {
			return m.angryAt(t.w, p)
		}},
		// NearestAttackableTargetGoal.findTarget bypasses TargetGoal.canAttack,
		// so its mustReach constructor argument does not gate the fox hunt.
		&mobTargetGoal{m: m, priority: 4, victim: TypeFox, interval: 10,
			gate: func() bool { return !m.IsBaby() }},
		&followParentGoal{m: m, priority: 4},
		&resetAngerGoal{m: m, priority: 5},
		&randomStroll{m: m, priority: 5},
		&lookAtPlayer{m: m, priority: 6, reach: 6},
		&randomLookAround{m: m, priority: 7},
	}
}

// cubNear is the second half of PolarBearAttackPlayersGoal.canUse: a baby
// polar bear within eight blocks sideways and four up and down.
func (m *Mob) cubNear(t *tickView) bool {
	for _, o := range t.others {
		if o.Type != TypePolarBear || !o.IsBaby() || !o.Alive() {
			continue
		}
		if boxesOverlap(m.X, m.Y-polarCubReachY, m.Z, m.width()+2*polarCubReachXZ, m.height()+2*polarCubReachY,
			o.X, o.Y, o.Z, o.width(), o.height()) {
			return true
		}
	}
	return false
}

// polarSwing is PolarBearMeleeAttackGoal.checkAndPerformAttack: strike when
// ready, else rear up and growl near the target once the next blow is ten
// ticks away.
// vanilla: PolarBearMeleeAttackGoal.checkAndPerformAttack
func (g *meleeAttackGoal) polarSwing(t *tickView, p *Player) {
	m := g.m
	pw, _, _ := p.box()
	reach := pw + polarStandMargin
	switch {
	case g.ticksUntilAttack == 0 && m.inMeleeReach(p) && m.seesTarget(t.w, p):
		g.ticksUntilAttack = attackInterval
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: m.attackDamage(), Player: *p})
		m.SetMetadataStanding(false)
	case Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) < reach*reach:
		if g.ticksUntilAttack == 0 {
			m.SetMetadataStanding(false)
			g.ticksUntilAttack = attackInterval
		}
		if g.ticksUntilAttack <= polarStandWindow {
			m.SetMetadataStanding(true)
			if m.polar.warn <= 0 {
				t.events = append(t.events, MobEvent{Mob: m, Kind: MobWarned})
				m.polar.warn = polarWarnInterval
			}
		}
	default:
		g.ticksUntilAttack = attackInterval
		m.SetMetadataStanding(false)
	}
}
