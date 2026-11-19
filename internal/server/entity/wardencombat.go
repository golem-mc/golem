package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Warden.setAttackTarget
func (m *Mob) wardenSetAttackTarget(id int32) {
	s := &m.warden
	m.SetMetadataTargetID(id)
	s.roarTarget = 0
	s.sonicCooldown = 200
	if s.pose == protocol.PoseRoaring || s.pose == protocol.PoseSniffing {
		m.SetMetadataWardenPose(protocol.PoseStanding)
	}
	s.sniffing = false
}

// wardenCombatStep selects the roar/fight activity and runs its timed behaviours.
// vanilla: WardenAi, SetRoarTarget, Roar, SonicBoom
func (m *Mob) wardenCombatStep(t *tickView) bool {
	s := &m.warden
	if m.TargetID != 0 {
		_, _, valid := m.wardenSuspect(t, m.TargetID)
		if !valid || m.wardenActiveAnger() < 80 {
			if !valid {
				present := t.mobByID(m.TargetID) != nil
				for _, p := range t.players {
					present = present || p.EntityID == m.TargetID
				}
				for i := range s.anger {
					suspect := &s.anger[i]
					// Unloaded bodies keep their saved UUID anger.
					if suspect.id == m.TargetID && (present || suspect.mob != nil && !suspect.mob.Alive()) {
						suspect.anger = 0
					}
				}
			}
			m.SetMetadataTargetID(0)
			if s.digCooldown > 0 {
				s.digCooldown = wardenDigCooldown
			}
			m.stopSeeking()
		}
	}
	// A running behaviour finishes even if its target disappears.
	if s.roarTarget != 0 {
		s.roarTicks++
		if !s.roarPlayed && s.roarTicks >= 25 && s.roarSoundDelay == 0 && s.roarSoundCooldown == 0 {
			s.roarPlayed, s.roarSoundCooldown = true, 59
			m.wardenSound(t, registry.SoundEntityWardenRoar, 3)
		}
		if s.roarTicks > 84 {
			id := s.roarTarget
			m.SetMetadataWardenPose(protocol.PoseStanding)
			m.wardenSetAttackTarget(id)
		}
		return true
	}
	if m.TargetID == 0 && s.sonicTicks == 0 {
		for _, suspect := range s.anger {
			if suspect.id == 0 || suspect.anger < 80 {
				continue
			}
			if target, p, ok := m.wardenSuspect(t, suspect.id); ok {
				s.roarTarget = suspect.id
				s.roarTicks = 0
				m.SetMetadataWardenPose(protocol.PoseRoaring)
				s.sniffing = false
				s.roarSoundDelay, s.roarPlayed = 25, false
				m.stopSeeking()
				eye := p.eyeHeight()
				if target.mob != nil {
					eye = target.mob.eyeHeight()
				}
				m.lookAtPoint(p.X, p.Y+eye, p.Z)
				m.wardenIncreaseAnger(t, suspect.id, 20, false)
				return true
			}
		}
	}
	p := m.targetIn(t)
	inRange := func() bool { return p != nil && math.Hypot(p.X-m.X, p.Z-m.Z) < 15 && math.Abs(p.Y-m.Y) < 20 }
	if s.sonicTicks == 0 && s.sonicCooldown == 0 && inRange() {
		s.sonicTicks, s.attackCooldown, s.sonicSoundDelay = 1, 60, 34
		s.sonicFired = false
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobStatus, Status: int32(protocol.EntityStatusWardenSonicBoom)})
		m.wardenSound(t, registry.SoundEntityWardenSonicCharge, 3)
	} else if s.sonicTicks > 0 {
		s.sonicTicks++
		if p != nil {
			m.lookAtPoint(p.X, p.Y, p.Z)
		}
		if !s.sonicFired && s.sonicTicks >= 35 && s.sonicSoundDelay == 0 && s.sonicSoundCooldown == 0 {
			s.sonicFired, s.sonicSoundCooldown = true, 26
			if inRange() {
				// Warden's chest attachment is (0, 1.6, 0); target eyes vary by body.
				eye := p.eyeHeight()
				if o := t.mobByID(p.EntityID); o != nil {
					eye = o.eyeHeight()
				}
				dx, dy, dz := p.X-m.X, p.Y+eye-(m.Y+1.6), p.Z-m.Z
				length := math.Sqrt(dx*dx + dy*dy + dz*dz)
				if length < 1e-4 {
					dx, dy, dz = 0, 0, 0
				} else {
					dx, dy, dz = dx/length, dy/length, dz/length
				}
				t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Player: *p,
					Damage: 10, Sonic: &SonicBoom{DX: dx, DY: dy, DZ: dz, Steps: int(math.Floor(length)) + 7}})
				m.wardenSound(t, registry.SoundEntityWardenSonicBoom, 3)
			}
		}
		if s.sonicTicks > 61 {
			s.sonicTicks, s.sonicCooldown = 0, 40
		}
	}
	if m.TargetID != 0 && s.digCooldown > 0 {
		s.digCooldown = wardenDigCooldown
	}
	return false
}

func (m *Mob) wardenSound(t *tickView, sound registry.Sound, volume float32) {
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: volume, Pitch: 1})
}

// SonicBoom carries the normalized ray from the chest through the target's eyes.
type SonicBoom struct {
	DX, DY, DZ float64
	Steps      int
}

// Push scales the sonic ray by the victim's knockback resistance.
// vanilla: SonicBoom.tick
func (b SonicBoom) Push(resistance float64) (x, y, z float64) {
	return b.DX * 2.5 * (1 - resistance), b.DY * 0.5 * (1 - resistance), b.DZ * 2.5 * (1 - resistance)
}

func (m *Mob) wardenCombatCooldowns() {
	s := &m.warden
	for _, ticks := range []*int64{&s.sonicCooldown, &s.roarSoundDelay, &s.roarSoundCooldown, &s.sonicSoundDelay, &s.sonicSoundCooldown} {
		if *ticks > 0 {
			*ticks--
		}
	}
	if s.attackCooldown > 0 {
		s.attackCooldown--
	}
}
