package entity

import (
	"math"
)

// The bat: an ambient flier that hangs from a solid ceiling, wakes when a
// player comes within four blocks and flutters toward random targets
// otherwise. It runs no goals; its tick is batMoveStep.
// vanilla: Bat

// batWakeRange is BAT_RESTING_TARGETING's range, in blocks.
const batWakeRange = 4.0

// The flight's numbers: the pull toward the target velocity is the float
// 0.1f, the climb rate the float 0.7f, and Bat.tick damps the vertical
// velocity by 0.6.
var (
	batPull  = float64(float32(0.1))
	batClimb = float64(float32(0.7))
)

const batVerticalDamping = 0.6

// batState is the bat's flight target.
// vanilla: Bat.targetPosition
type batState struct {
	target    Waypoint
	hasTarget bool
}

// batMoveStep is the bat's whole tick: customServerAiStep, then travel and
// Bat.tick's hanging or vertical damping. A bat resting before and after
// does not travel; one settling this tick travels first, then hangs.
// vanilla: Bat.customServerAiStep, Bat.tick
func (m *Mob) batMoveStep(t *tickView) []MobEvent {
	var events []MobEvent
	above := m.cell()
	above.Y++
	wasResting := m.Resting
	if m.Resting {
		if conductorAt(t.w, above.X, above.Y, above.Z) {
			if m.Rand.Intn(200) == 0 {
				m.HeadYaw = float32(m.Rand.Intn(360))
			}
			if m.nearestPlayerWithin(t, batWakeRange, true) != nil {
				events = m.batWake(events)
			}
		} else {
			events = m.batWake(events)
		}
	} else {
		m.batFly(t, above)
	}
	if wasResting && m.Resting {
		m.batHang()
		return events
	}
	events = m.step(t.w, events)
	if m.Resting {
		m.batHang()
		return events
	}
	m.VY *= batVerticalDamping
	return events
}

// batHang pins the box's top against the block above.
// vanilla: Bat.tick
func (m *Mob) batHang() {
	m.VX, m.VY, m.VZ = 0, 0, 0
	m.Y = math.Floor(m.Y) + 1 - m.height()
}

// batWake takes the bat off its ceiling and tells the world, unless it is
// silent.
func (m *Mob) batWake(events []MobEvent) []MobEvent {
	m.SetMetadataResting(false)
	if !m.Silent {
		events = append(events, MobEvent{Mob: m, Kind: MobBatTookOff})
	}
	return events
}

// batFly picks the flight target and steers toward it, and settles the bat
// under a ceiling one tick in a hundred.
// vanilla: Bat.customServerAiStep
func (m *Mob) batFly(t *tickView, above Waypoint) {
	b := &m.bat
	if b.hasTarget && (!t.w.IsAirAt(b.target.X, b.target.Y, b.target.Z) || b.target.Y <= t.w.MinY()) {
		b.hasTarget = false
	}
	if !b.hasTarget || m.Rand.Intn(30) == 0 || m.nearBatTarget() {
		x := int32(math.Floor(m.X + float64(m.Rand.Intn(7)) - float64(m.Rand.Intn(7))))
		y := int32(math.Floor(m.Y + float64(m.Rand.Intn(6)) - 2.0))
		z := int32(math.Floor(m.Z + float64(m.Rand.Intn(7)) - float64(m.Rand.Intn(7))))
		b.target, b.hasTarget = Waypoint{X: x, Y: y, Z: z}, true
	}
	dx := float64(b.target.X) + 0.5 - m.X
	dy := float64(b.target.Y) + 0.1 - m.Y
	dz := float64(b.target.Z) + 0.5 - m.Z
	m.VX += (signum(dx)*0.5 - m.VX) * batPull
	m.VY += (signum(dy)*batClimb - m.VY) * batPull
	m.VZ += (signum(dz)*0.5 - m.VZ) * batPull
	yawDiff := wrapDegrees(math.Atan2(m.VZ, m.VX)*57.2957763671875 - 90 - float64(m.Yaw))
	m.Yaw += float32(yawDiff)
	yaw := float64(m.Yaw) * math.Pi / 180
	m.walk.dirX, m.walk.dirZ = -math.Sin(yaw), math.Cos(yaw)
	// No forward push: Bat.customServerAiStep sets zza 0.5, but the MoveControl
	// that runs right after it resets zza to 0 with no MOVE_TO.
	if m.Rand.Intn(100) == 0 && conductorAt(t.w, above.X, above.Y, above.Z) {
		m.SetMetadataResting(true)
	}
}

// nearBatTarget is BlockPos.closerToCenterThan(position, 2.0) for the target.
func (m *Mob) nearBatTarget() bool {
	b := m.bat.target
	return Dist2(float64(b.X)+0.5, float64(b.Y)+0.5, float64(b.Z)+0.5, m.X, m.Y, m.Z) < 4
}
