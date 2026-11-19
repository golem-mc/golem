package entity

import (
	"testing"
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
