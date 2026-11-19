package entity

import "math"

// lookControl is LookControl's target (the point looked at, the request's
// turn limits, the ticks it still holds) and BodyRotationControl's state.
// None of it is saved.
type lookControl struct {
	x, y, z  float64
	yawSpeed float32
	pitchMax float32
	cooldown int

	// bodyYaw is vanilla's yBodyRot; it is false in bodyYawSet until the
	// first read seeds it from Yaw. headStable and lastStableHead are
	// BodyRotationControl's counters.
	bodyYaw        float32
	bodyYawSet     bool
	headStable     int
	lastStableHead float32
}

// vanilla: Mob.getHeadRotSpeed, getMaxHeadXRot, getMaxHeadYRot defaults.
const (
	defaultHeadRotSpeed = 10
	defaultMaxHeadXRot  = 40
	defaultMaxHeadYRot  = 75
)

// headRotSpeed is Mob.getHeadRotSpeed in degrees a tick.
func (m *Mob) headRotSpeed() float32 {
	if v := m.kind().headRotSpeed; v != 0 {
		return float32(v)
	}
	return defaultHeadRotSpeed
}

// maxHeadXRot is Mob.getMaxHeadXRot: the most the head tilts.
func (m *Mob) maxHeadXRot() float32 {
	if m.isCube() {
		return 0 // vanilla: AbstractCubeMob.getMaxHeadXRot
	}
	if m.Type == TypeWolf && m.Tamable.SittingPose {
		return 20 // vanilla: Wolf.getMaxHeadXRot
	}
	if v := m.kind().maxHeadXRot; v != 0 {
		return float32(v)
	}
	return defaultMaxHeadXRot
}

// maxHeadYRot is Mob.getMaxHeadYRot: the most the head turns from the body.
func (m *Mob) maxHeadYRot() float32 {
	if m.Type == TypeArmadillo { // vanilla: Armadillo.getMaxHeadYRot
		if m.ArmadilloScared() {
			return 0
		}
		return 32
	}
	if v := m.kind().maxHeadYRot; v != 0 {
		return float32(v)
	}
	return defaultMaxHeadYRot
}

// lookAt is LookControl.setLookAt.
func (m *Mob) lookAt(x, y, z float64, yawSpeed, pitchMax float32) {
	m.look.x, m.look.y, m.look.z = x, y, z
	m.look.yawSpeed, m.look.pitchMax = yawSpeed, pitchMax
	m.look.cooldown = 2
}

// lookAtDefault is LookControl.setLookAt(x, y, z) with the mob's own limits.
func (m *Mob) lookAtDefault(x, y, z float64) {
	m.lookAt(x, y, z, m.headRotSpeed(), m.maxHeadXRot())
}

// resetsXRot is LookControl.resetXRotOnTick: false for a fox in a
// head-holding state and a frog with a tongue target.
// vanilla: Fox.FoxLookControl, Frog.FrogLookControl
func (m *Mob) resetsXRot() bool {
	switch m.Type {
	case TypeFox:
		return !m.Fox.Pouncing && !m.Fox.Crouching && !m.Fox.Interested && !m.Fox.Faceplanted
	case TypeFrog:
		return m.FrogTongueTarget() == 0
	}
	return true
}

// rotateIfNecessary is Mth.rotateIfNecessary.
func rotateIfNecessary(base, target, maxDiff float32) float32 {
	diff := float32(wrapDegrees(float64(target - base)))
	if diff > maxDiff {
		diff = maxDiff
	} else if diff < -maxDiff {
		diff = -maxDiff
	}
	return target - diff
}

func (m *Mob) seedBodyYaw() {
	if !m.look.bodyYawSet {
		m.look.bodyYaw, m.look.bodyYawSet = m.Yaw, true
	}
}

// lookControlTick is LookControl.tick; lookHeld is true while a running
// goal holds the LOOK flag, which skips the idle return to the body.
// vanilla: LookControl.tick, Mob.tickHeadTurn
func (m *Mob) lookControlTick(lookHeld bool) {
	if c := m.smoothSwimLookCenter(); c != 0 {
		m.smoothSwimLookTick(lookHeld, c)
		return
	}
	m.seedBodyYaw()
	l := &m.look
	if l.cooldown > 0 {
		l.cooldown--
		if m.resetsXRot() {
			m.Pitch = 0
		}
		dx, dz := l.x-m.X, l.z-m.Z
		if math.Abs(dz) > 1e-5 || math.Abs(dx) > 1e-5 {
			m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(dx, dz)), float64(l.yawSpeed))
		}
		dy := l.y - (m.Y + m.eyeHeight())
		if math.Abs(dy) > 1e-5 || math.Abs(dx) > 1e-5 || math.Abs(dz) > 1e-5 {
			m.Pitch = turnToward(m.Pitch, float32(pitchToward(dx, dy, dz)), float64(l.pitchMax))
		}
	} else if !lookHeld {
		if m.resetsXRot() {
			m.Pitch = 0
		}
		m.HeadYaw = turnToward(m.HeadYaw, l.bodyYaw, 10)
	}
	if len(m.navOf().nav.path) > 0 {
		m.HeadYaw = rotateIfNecessary(m.HeadYaw, l.bodyYaw, m.maxHeadYRot())
	}
}

// smoothSwimLookCenter is SmoothSwimmingLookControl's maxYRotFromCenter, or
// 0 for a mob on the plain LookControl.
// vanilla: Dolphin, Axolotl, Tadpole, AbstractNautilus constructors
func (m *Mob) smoothSwimLookCenter() float32 {
	switch {
	case m.Type == TypeAxolotl:
		return 20
	case m.Type == TypeDolphin, m.Type == TypeTadpole, IsNautilus(m.Type):
		return 10
	}
	return 0
}

// smoothSwimLookTick is SmoothSwimmingLookControl.tick: the look target is
// offset by 20 yaw and 10 pitch, the pitch is never reset but eased to 0 at
// 5 a tick once the path is done, and a head more than center degrees off
// the body drags the body 4 a tick.
// vanilla: SmoothSwimmingLookControl.tick, Axolotl.AxolotlLookControl
func (m *Mob) smoothSwimLookTick(lookHeld bool, center float32) {
	if m.PlayingDead() {
		return
	}
	m.seedBodyYaw()
	l := &m.look
	if l.cooldown > 0 {
		l.cooldown--
		dx, dz := l.x-m.X, l.z-m.Z
		if math.Abs(dz) > 1e-5 || math.Abs(dx) > 1e-5 {
			m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(dx, dz))+20, float64(l.yawSpeed))
		}
		dy := l.y - (m.Y + m.eyeHeight())
		if math.Abs(dy) > 1e-5 || math.Abs(dx) > 1e-5 || math.Abs(dz) > 1e-5 {
			m.Pitch = turnToward(m.Pitch, float32(pitchToward(dx, dy, dz))+10, float64(l.pitchMax))
		}
	} else if !lookHeld {
		if m.navDone() {
			m.Pitch = turnToward(m.Pitch, 0, 5)
		}
		// yMaxRotSpeed is the last request's, 0 before the first.
		m.HeadYaw = turnToward(m.HeadYaw, l.bodyYaw, float64(l.yawSpeed))
	}
	if d := float32(wrapDegrees(float64(m.HeadYaw - l.bodyYaw))); d < -center {
		l.bodyYaw -= 4
	} else if d > center {
		l.bodyYaw += 4
	}
}

// bodyRotationStep is BodyRotationControl.clientTick, after a move from
// (fromX, fromZ).
func (m *Mob) bodyRotationStep(fromX, fromZ float64) {
	m.seedBodyYaw()
	l := &m.look
	dx, dz := m.X-fromX, m.Z-fromZ
	maxY := m.maxHeadYRot()
	if dx*dx+dz*dz > 2.500000277905201e-7 {
		l.bodyYaw = m.Yaw
		m.HeadYaw = rotateIfNecessary(m.HeadYaw, l.bodyYaw, maxY)
		l.lastStableHead = m.HeadYaw
		l.headStable = 0
		return
	}
	if len(m.Passengers) > 0 {
		return
	}
	if math.Abs(float64(m.HeadYaw-l.lastStableHead)) > 15 {
		l.headStable = 0
		l.lastStableHead = m.HeadYaw
		l.bodyYaw = rotateIfNecessary(l.bodyYaw, m.HeadYaw, maxY)
		return
	}
	l.headStable++
	if l.headStable > 10 {
		frac := float32(l.headStable-10) / 10
		if frac > 1 {
			frac = 1
		}
		l.bodyYaw = rotateIfNecessary(l.bodyYaw, m.HeadYaw, maxY*(1-frac))
	}
}
