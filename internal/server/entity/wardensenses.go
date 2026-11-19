package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// vanilla: WardenEntitySensor, NearestLivingEntitySensor; WardenAi
func (m *Mob) wardenSensesTick(t *tickView) {
	s := &m.warden
	for _, hurt := range s.hurt {
		m.wardenIncreaseAnger(t, hurt.id, 100, false)
		if _, p, ok := m.wardenSuspect(t, hurt.id); ok && m.TargetID == 0 &&
			(hurt.direct || Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) < 25) {
			m.wardenSetAttackTarget(hurt.id)
		}
	}
	s.hurt = s.hurt[:0]
	if s.touchCooldown > 0 {
		s.touchCooldown--
	}
	if s.sniffCooldown > 0 {
		s.sniffCooldown--
	}
	if s.disturbanceTicks > 0 {
		s.disturbanceTicks--
	}
	s.sensorDelay--
	if s.sensorDelay > 0 {
		return
	}
	s.sensorDelay, s.nearest = 20, 0
	best, player := math.Inf(1), false
	hw0, r := m.width()/2, m.followRange()
	inBox := func(x, y, z, width, height float64) bool {
		hw := hw0 + width/2
		return math.Abs(x-m.X) < hw+r && math.Abs(z-m.Z) < hw+r && y+height > m.Y-r && y < m.Y+m.height()+r
	}
	pick := func(s wardenSuspect, p Player) {
		d := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
		if (!player || s.player) && (s.player != player || d < best) {
			best, player = d, s.player
			m.warden.nearest = s.id
		}
	}
	for _, p := range t.players {
		pw, ph, _ := p.box()
		if !inBox(p.X, p.Y, p.Z, pw, ph) {
			continue
		}
		if s, ok := m.wardenCheck(t, p, p.width(), nil); ok {
			pick(s, p)
		}
	}
	for _, o := range t.others {
		if o.Type == TypeWarden {
			continue
		}
		if !inBox(o.X, o.Y, o.Z, o.width(), o.height()) {
			continue
		}
		if s, p, ok := m.wardenCheckMob(t, o); ok {
			pick(s, p)
		}
	}
}

// vanilla: Warden.doPush
func (m *Mob) wardenTouch(t *tickView, touched Player, events *[]MobEvent) {
	s := &m.warden
	if m.NoAI || m.wardenUnderground() || s.touchCooldown > 0 {
		return
	}
	s.touchCooldown = 20
	t.events = *events
	m.wardenIncreaseAnger(t, touched.EntityID, 35, true)
	m.wardenDisturbance(t, &touched)
	*events = t.events
}

// vanilla: WardenAi.setDisturbanceLocation
func (m *Mob) wardenDisturbance(t *tickView, p *Player) {
	at := Waypoint{int32(math.Floor(p.X)), int32(math.Floor(p.Y)), int32(math.Floor(p.Z))}
	m.wardenDisturbanceAt(t, at)
}

func (m *Mob) wardenDisturbanceAt(t *tickView, at Waypoint) {
	s := &m.warden
	if m.TargetID != 0 || m.wardenActiveAnger() >= 80 {
		return
	}
	if !t.w.InBorder(at.X, at.Z) {
		return
	}
	if s.digCooldown > 0 {
		s.digCooldown = wardenDigCooldown
	}
	s.sniffCooldown, s.disturbanceTicks, s.disturbance = 100, 100, at
	m.stopSeeking()
}

// vanilla: TryToSniff, Sniffing (84 ticks, Behavior.timedOut uses > endTimestamp)
func (m *Mob) wardenSniffStep(t *tickView) bool {
	s := &m.warden
	if m.TargetID != 0 {
		return false
	}
	if s.sniffing {
		// An investigation takes the activity, but the running sniff keeps
		// ticking until its timeout and the warden may walk meanwhile.
		// vanilla: Brain.tickRunningBehaviors, WardenAi.updateActivity
		if s.sniffTicks >= 84 {
			m.wardenStopSniffing(t)
			return s.disturbanceTicks == 0
		}
		s.sniffTicks++
		return s.disturbanceTicks == 0
	}
	if s.disturbanceTicks == 0 && s.sniffCooldown == 0 && s.nearest != 0 {
		s.sniffing = true
		s.sniffTicks = 0
		m.SetMetadataWardenPose(protocol.PoseSniffing)
		s.sniffCooldown = int64(100 + m.Rand.Intn(101))
		m.stopSeeking()
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityWardenSniff, Volume: 5, Pitch: 1})
		return true
	}
	return false
}

func (m *Mob) wardenStopSniffing(t *tickView) {
	s := &m.warden
	if !s.sniffing {
		return
	}
	s.sniffing = false
	m.SetMetadataWardenPose(protocol.PoseStanding)
	if _, p, ok := m.wardenSuspect(t, s.nearest); ok {
		if math.Hypot(p.X-m.X, p.Z-m.Z) < 6 && math.Abs(p.Y-m.Y) < 20 {
			m.wardenIncreaseAnger(t, p.EntityID, 35, true)
		}
		if s.disturbanceTicks == 0 {
			m.wardenDisturbance(t, &p)
		}
	}
}

type wardenInvestigateGoal struct {
	m    *Mob
	look Waypoint
}

func (g *wardenInvestigateGoal) Priority() int           { return 2 }
func (g *wardenInvestigateGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *wardenInvestigateGoal) RequiresEveryTick() bool { return true }
func (g *wardenInvestigateGoal) CanStart(*tickView) bool {
	return g.m.warden.disturbanceTicks > 0 && g.m.TargetID == 0
}
func (g *wardenInvestigateGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *wardenInvestigateGoal) Start(*tickView)                 { g.m.stopSeeking() }
func (g *wardenInvestigateGoal) Stop(*tickView)                  { g.m.stopSeeking() }

// vanilla: GoToTargetLocation
func (g *wardenInvestigateGoal) Tick(t *tickView) {
	m, at := g.m, g.m.warden.disturbance
	if m.navDone() {
		cell := m.cell()
		if Dist2(float64(cell.X), float64(cell.Y), float64(cell.Z), float64(at.X), float64(at.Y), float64(at.Z)) < 4 {
			return
		}
		g.look = Waypoint{at.X + int32(m.Rand.Intn(3)-1), at.Y, at.Z + int32(m.Rand.Intn(3)-1)}
		m.seekTo(t.w, float64(g.look.X), float64(g.look.Y), float64(g.look.Z), 0.7, 2)
	}
	m.lookAtPoint(float64(g.look.X)+0.5, float64(g.look.Y)+0.5, float64(g.look.Z)+0.5)
}
