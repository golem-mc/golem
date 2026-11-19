package entity

// PlanMoveTo plans a path to a point as a goal would and holds it until the
// walk ends, the goals standing aside meanwhile. It reports whether a path
// was found; a mob without AI does not walk.
// vanilla: PathNavigation.moveTo
func (m *Mob) PlanMoveTo(w World, x, y, z, speedFactor float64) bool {
	if m.NoAI || m.Dead {
		return false
	}
	held := m.seekTo(w, x, y, z, speedFactor, navReach)
	m.moveOrder = held && !m.navDone()
	return held
}

// StopMoving drops the path and gives the walk back to the goals.
// vanilla: PathNavigation.stop
func (m *Mob) StopMoving() {
	m.moveOrder = false
	m.stopSeeking()
}

// LookAtPoint turns the head toward a point for the next ticks.
// vanilla: LookControl.setLookAt
func (m *Mob) LookAtPoint(x, y, z float64) { m.lookAtDefault(x, y, z) }

// goalsStandAside reports a plugin walk in progress; it ends with the path.
func (m *Mob) goalsStandAside() bool {
	if m.moveOrder && m.navDone() {
		m.moveOrder = false
	}
	return m.moveOrder
}
