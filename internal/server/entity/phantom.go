package entity

import (
	"math"
	"sort"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The phantom: a flier that circles an anchor above its target, then swoops
// to bite. Its goals and move control run here instead of the walk physics
// and goal selector, as the squid's do.
// vanilla: Phantom

// The phantom's numbers, vanilla Phantom's.
const (
	phantomStartSpeed  = 0.1
	phantomMaxSpeed    = 1.8
	phantomSlowSpeed   = 0.2
	phantomTurnStep    = 4.0
	phantomTargetRange = 64.0
	phantomScanEvery   = 60
	phantomFirstScan   = 20
	phantomFirstSweep  = 10
	phantomAnchorLift  = 5
	phantomWaterDrag   = 0.8
	phantomLavaDrag    = 0.5
	phantomAirDrag     = 0.91
	phantomHitInflate  = 0.2
)

type phantomPhase uint8

const (
	phantomCircle phantomPhase = iota
	phantomSwoop
)

// phantomState is the phantom's mind: the move target, its anchor and its
// goals' state. Goal delays count goal ticks (every other mob tick), the
// halving adjustedTickDelay applies to goals that are not every-tick.
type phantomState struct {
	// size is ID_SIZE, 0..64: the box scales by 1 + 0.15*size.
	size    int
	started bool
	phase   phantomPhase
	speed   float64

	moveX, moveY, moveZ float64

	hasAnchor bool
	anchor    Waypoint

	// Which goals run, and the counters they keep.
	scanning, strategy, sweeping, circling bool
	nextScan                               int
	nextSweep                              int
	// catSearch is the Age after which the sweep looks for cats again;
	// scaredOfCat is what the last look found.
	catSearch                          int
	scaredOfCat                        bool
	angle, distance, height, clockwise float64
}

// goalDelay is Goal.adjustedTickDelay for a goal ticked every other tick.
func goalDelay(ticks int) int { return (ticks + 1) / 2 }

// AnchorPos is the block the phantom circles, and whether it has one.
// vanilla: Phantom.anchorPoint
func (m *Mob) AnchorPos() (x, y, z int32, ok bool) {
	a := m.phantom.anchor
	return a.X, a.Y, a.Z, m.phantom.hasAnchor
}

// PhantomSize is the phantom's ID_SIZE.
// vanilla: Phantom.getPhantomSize
func (m *Mob) PhantomSize() int { return m.phantom.size }

// SetPhantomSize sets the size, clamped to 0..64; a change moves the
// attack_damage base with it.
// vanilla: Phantom.setPhantomSize, Phantom.onSyncedDataUpdated
func (m *Mob) SetPhantomSize(n int) {
	n = min(max(n, 0), 64)
	if n == m.phantom.size {
		return
	}
	m.SetMetadataPhantomSize(n)
	m.SyncPhantomSize()
}

// SyncPhantomSize writes 6 + size as the attack_damage base.
// vanilla: Phantom.updatePhantomSizeInfo
func (m *Mob) SyncPhantomSize() {
	// A resize creates a persistent attribute instance even before a command
	// reads the map, so returning to size zero survives a save and reload.
	if m.attrs == nil {
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
	}
	m.attrs.SetBase(registry.AttributeAttackDamage, 6+float64(m.phantom.size))
}

// phantomScale is the box factor of the size.
// vanilla: Phantom.getDefaultDimensions
func (m *Mob) phantomScale() float64 {
	return float64(1.0 + float32(0.15)*float32(m.phantom.size))
}

// SetAnchorPos sets the block the phantom circles.
func (m *Mob) SetAnchorPos(x, y, z int32) {
	m.phantom.hasAnchor = true
	m.phantom.anchor = Waypoint{X: x, Y: y, Z: z}
}

// finalizePhantom anchors a freshly spawned phantom five blocks above where
// it stands.
// vanilla: Phantom.finalizeSpawn
func (m *Mob) finalizePhantom() {
	c := m.cell()
	m.SetAnchorPos(c.X, c.Y+phantomAnchorLift, c.Z)
}

// phantomTarget is the player the phantom hunts, if still known.
func (m *Mob) phantomTarget(t *tickView) *Player {
	if m.TargetID == 0 {
		return nil
	}
	for i := range t.players {
		if p := &t.players[i]; p.EntityID == m.TargetID {
			return p
		}
	}
	return nil
}

// phantomCanAttack is TargetingConditions.DEFAULT against a player: one a
// monster may hunt, in sight.
func (m *Mob) phantomCanAttack(t *tickView, p *Player) bool {
	return p.Huntable && !m.alliedTo(p) && m.seesTarget(t.w, p)
}

// phantomMoveStep is the phantom's whole tick: goals on selector ticks, then
// the move control, then flight travel.
// vanilla: Mob.serverAiStep, Phantom.PhantomMoveControl, LivingEntity.travelFlying
func (m *Mob) phantomMoveStep(t *tickView) []MobEvent {
	ph := &m.phantom
	if !ph.started {
		ph.started = true
		ph.speed = phantomStartSpeed
		ph.nextScan = goalDelay(phantomFirstScan)
		ph.clockwise = 1
		ph.moveX, ph.moveY, ph.moveZ = m.X, m.Y, m.Z
	}
	var events []MobEvent
	if (m.Age+int(m.ID))%2 == 0 || m.Age <= 1 {
		events = m.phantomGoals(t, events)
	}
	m.phantomControl()
	m.phantomTravel(t)
	return events
}

// phantomGoals is one full tick of the target and goal selectors: stop goals
// that cannot continue, start those that can, tick every running one.
// vanilla: GoalSelector.tick
func (m *Mob) phantomGoals(t *tickView, events []MobEvent) []MobEvent {
	ph := &m.phantom

	// PhantomAttackPlayerTargetGoal.
	if ph.scanning {
		p := m.phantomTarget(t)
		if p == nil || !m.phantomCanAttack(t, p) {
			ph.scanning = false
		}
	}
	if !ph.scanning {
		if ph.nextScan > 0 {
			ph.nextScan--
		} else {
			ph.nextScan = goalDelay(phantomScanEvery)
			if p := m.phantomScan(t); p != nil {
				m.SetMetadataTargetID(p.EntityID)
				ph.scanning = true
			}
		}
	}

	target := m.phantomTarget(t)
	attackable := target != nil && m.phantomCanAttack(t, target)

	// Stops.
	if ph.strategy && !attackable {
		ph.strategy = false
		m.phantomStrategyStop(t.w)
	}
	if ph.sweeping && !m.phantomSweepContinues(t, target) {
		ph.sweeping = false
		m.SetMetadataTargetID(0)
		ph.phase = phantomCircle
		target, attackable = nil, false
	}
	if ph.circling && m.TargetID != 0 && ph.phase != phantomCircle {
		ph.circling = false
	}

	// Starts, by priority: strategy 1, sweep 2, circle 3.
	if !ph.strategy && attackable {
		ph.strategy = true
		ph.nextSweep = goalDelay(phantomFirstSweep)
		ph.phase = phantomCircle
		m.phantomAnchorAboveTarget(t.w, target)
	}
	if !ph.sweeping && m.TargetID != 0 && ph.phase == phantomSwoop {
		ph.sweeping = true
		ph.circling = false
	}
	if !ph.circling && !ph.sweeping && (m.TargetID == 0 || ph.phase == phantomCircle) {
		ph.circling = true
		ph.distance = 5 + m.Rand.Float64()*10
		ph.height = -4 + m.Rand.Float64()*9
		ph.clockwise = -1
		if m.Rand.Intn(2) == 0 {
			ph.clockwise = 1
		}
		m.phantomSelectNext()
	}

	// Ticks.
	if ph.strategy && ph.phase == phantomCircle {
		ph.nextSweep--
		if ph.nextSweep <= 0 {
			ph.phase = phantomSwoop
			if p := m.phantomTarget(t); p != nil {
				m.phantomAnchorAboveTarget(t.w, p)
			}
			ph.nextSweep = goalDelay((8 + m.Rand.Intn(4)) * 20)
			events = append(events, MobEvent{Mob: m, Kind: MobPhantomSwooped,
				Pitch: 0.95 + m.Rand.Float32()*0.1})
		}
	}
	if ph.sweeping {
		events = m.phantomSweepTick(t, events)
	}
	if ph.circling {
		m.phantomCircleTick(t.w)
	}
	return events
}

// phantomScan is the target search: huntable players in a box 16 out and 64
// up and down within the visibility-adjusted 64-block range, highest first,
// the first enemy in sight.
func (m *Mob) phantomScan(t *tickView) *Player {
	var found []*Player
	for i := range t.players {
		p := &t.players[i]
		if !p.Huntable || m.alliedTo(p) {
			continue
		}
		pw, ph, _ := p.box()
		if math.Abs(p.X-m.X) > 16+m.width()/2+pw/2 ||
			math.Abs(p.Y-m.Y) > 64+ph ||
			math.Abs(p.Z-m.Z) > 16+m.width()/2+pw/2 {
			continue
		}
		reach := max(phantomTargetRange*visibilityPercent(m, p), minVisibilityDistance)
		if Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) > reach*reach {
			continue
		}
		if !m.seesTarget(t.w, p) {
			continue
		}
		found = append(found, p)
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Y > found[j].Y })
	for _, p := range found {
		if m.phantomCanAttack(t, p) {
			return p
		}
	}
	return nil
}

// phantomSweepContinues is PhantomSweepAttackGoal.canContinueToUse: a live
// non-creative, non-spectator target, mid-swoop, and no live cat within 16
// blocks (looked for once every 20 ticks, each cat found hissing).
func (m *Mob) phantomSweepContinues(t *tickView, target *Player) bool {
	ph := &m.phantom
	if target == nil || !target.Huntable || ph.phase != phantomSwoop {
		return false
	}
	if m.Age > ph.catSearch {
		ph.catSearch = m.Age + 20
		ph.scaredOfCat = false
		for _, o := range t.others {
			if o.Type == TypeCat && o.Alive() && m.boxNear(o, 16) {
				ph.scaredOfCat = true
				// vanilla: Cat.hiss
				t.events = append(t.events, MobEvent{Mob: o, Kind: MobSounded, SoundID: o.catSoundSet().hiss, Volume: o.SoundVolume(), Pitch: o.VoicePitch()})
			}
		}
	}
	return !ph.scaredOfCat
}

// boxNear is whether o's box meets this body's box inflated by r on every
// axis.
func (m *Mob) boxNear(o *Mob, r float64) bool {
	return boxesOverlap(
		m.X, m.Y-r, m.Z, m.width()+2*r, m.height()+2*r,
		o.X, o.Y, o.Z, o.width(), o.height())
}

// phantomStrategyStop moves the anchor to ten to twenty-nine blocks over the
// first free cell above the column's MOTION_BLOCKING height.
// vanilla: PhantomAttackStrategyGoal.stop
func (m *Mob) phantomStrategyStop(w World) {
	ph := &m.phantom
	if !ph.hasAnchor {
		return
	}
	ph.anchor.Y = w.MotionBlockingHeight(ph.anchor.X, ph.anchor.Z) + 1 + int32(10+m.Rand.Intn(20))
}

// phantomAnchorAboveTarget puts the anchor 20 to 39 blocks over the target,
// never under the sea.
// vanilla: PhantomAttackStrategyGoal.setAnchorAboveTarget
func (m *Mob) phantomAnchorAboveTarget(w World, p *Player) {
	ph := &m.phantom
	if !ph.hasAnchor {
		return
	}
	ph.anchor = Waypoint{
		X: int32(math.Floor(p.X)),
		Y: int32(math.Floor(p.Y)) + int32(20+m.Rand.Intn(20)),
		Z: int32(math.Floor(p.Z)),
	}
	if sea := w.SeaLevel(); ph.anchor.Y < sea {
		ph.anchor.Y = sea + 1
	}
}

// phantomSweepTick steers at the target's middle and bites it on contact.
// vanilla: PhantomSweepAttackGoal.tick
func (m *Mob) phantomSweepTick(t *tickView, events []MobEvent) []MobEvent {
	ph := &m.phantom
	p := m.phantomTarget(t)
	if p == nil {
		return events
	}
	pw, pbh, _ := p.box()
	ph.moveX, ph.moveY, ph.moveZ = p.X, p.Y+pbh*0.5, p.Z
	hw, h := m.width()/2+phantomHitInflate, m.height()+2*phantomHitInflate
	hit := m.X+hw > p.X-pw/2 && m.X-hw < p.X+pw/2 &&
		m.Y-phantomHitInflate < p.Y+pbh && m.Y-phantomHitInflate+h > p.Y &&
		m.Z+hw > p.Z-pw/2 && m.Z-hw < p.Z+pw/2
	switch {
	case hit:
		events = append(events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: m.attackDamage(), Player: *p})
		ph.phase = phantomCircle
		if !m.Silent {
			events = append(events, MobEvent{Mob: m, Kind: MobPhantomBit})
		}
	case m.horizontalCollision || m.InvulnTicks > hurtCooldown:
		ph.phase = phantomCircle
	}
	return events
}

// phantomSelectNext picks the next point of the circle, 15 degrees further
// round.
// vanilla: PhantomCircleAroundAnchorGoal.selectNext
func (m *Mob) phantomSelectNext() {
	ph := &m.phantom
	if !ph.hasAnchor {
		c := m.cell()
		ph.anchor, ph.hasAnchor = c, true
	}
	ph.angle += ph.clockwise * 15 * math.Pi / 180
	ph.moveX = float64(ph.anchor.X) + ph.distance*math.Cos(ph.angle)
	ph.moveY = float64(ph.anchor.Y) - 4 + ph.height
	ph.moveZ = float64(ph.anchor.Z) + ph.distance*math.Sin(ph.angle)
}

// phantomCircleTick is PhantomCircleAroundAnchorGoal.tick.
func (m *Mob) phantomCircleTick(w World) {
	ph := &m.phantom
	if m.Rand.Intn(goalDelay(350)) == 0 {
		ph.height = -4 + m.Rand.Float64()*9
	}
	if m.Rand.Intn(goalDelay(250)) == 0 {
		ph.distance++
		if ph.distance > 15 {
			ph.distance = 5
			ph.clockwise = -ph.clockwise
		}
	}
	if m.Rand.Intn(goalDelay(450)) == 0 {
		ph.angle = m.Rand.Float64() * 2 * math.Pi
		m.phantomSelectNext()
	}
	if Dist2(ph.moveX, ph.moveY, ph.moveZ, m.X, m.Y, m.Z) < 4 {
		m.phantomSelectNext()
	}
	c := m.cell()
	if ph.moveY < m.Y && !phantomAir(w, c.X, c.Y-1, c.Z) {
		ph.height = math.Max(1, ph.height)
		m.phantomSelectNext()
	}
	if ph.moveY > m.Y && !phantomAir(w, c.X, c.Y+1, c.Z) {
		ph.height = math.Min(-1, ph.height)
		m.phantomSelectNext()
	}
}

// phantomAir is Level.isEmptyBlock: the cell holds air.
func phantomAir(w World, x, y, z int32) bool {
	return world.IsAirState(w.BlockAt(x, y, z))
}

// phantomControl is PhantomMoveControl.tick: turn up to four degrees a tick
// toward the move target and ease velocity toward the speed.
func (m *Mob) phantomControl() {
	ph := &m.phantom
	if m.horizontalCollision {
		m.Yaw += 180
		ph.speed = phantomStartSpeed
	}
	tdx, tdy, tdz := ph.moveX-m.X, ph.moveY-m.Y, ph.moveZ-m.Z
	sd := math.Hypot(tdx, tdz)
	if math.Abs(sd) <= 1.0e-5 {
		return
	}
	scale := 1 - math.Abs(tdy*0.7)/sd
	tdx *= scale
	tdz *= scale
	sd = math.Hypot(tdx, tdz)
	sd2 := math.Sqrt(tdx*tdx + tdz*tdz + tdy*tdy)
	prev := m.Yaw
	angle := math.Atan2(tdz, tdx)
	a := wrapDegrees(float64(m.Yaw) + 90)
	b := wrapDegrees(angle * 180 / math.Pi)
	m.Yaw = turnToward(float32(a), float32(b), phantomTurnStep) - 90
	m.HeadYaw = m.Yaw
	if math.Abs(wrapDegrees(float64(m.Yaw-prev))) < 3 {
		ph.speed = approachFloat(ph.speed, phantomMaxSpeed, 0.005*(phantomMaxSpeed/ph.speed))
	} else {
		ph.speed = approachFloat(ph.speed, phantomSlowSpeed, 0.025)
	}
	xRot := -(math.Atan2(-tdy, sd) * 180 / math.Pi)
	m.Pitch = float32(xRot)
	moveAngle := (float64(m.Yaw) + 90) * math.Pi / 180
	txd := ph.speed * math.Cos(moveAngle) * math.Abs(tdx/sd2)
	tzd := ph.speed * math.Sin(moveAngle) * math.Abs(tdz/sd2)
	tyd := ph.speed * math.Sin(xRot*math.Pi/180) * math.Abs(tdy/sd2)
	m.VX += (txd - m.VX) * 0.2
	m.VY += (tyd - m.VY) * 0.2
	m.VZ += (tzd - m.VZ) * 0.2
}

// phantomTravel moves the body on its velocity and takes the air's, the
// water's or the lava's drag off it; there is no gravity.
// vanilla: LivingEntity.travelFlying
func (m *Mob) phantomTravel(t *tickView) {
	m.move(t.w, m.VX, m.VY, m.VZ)
	drag := phantomAirDrag
	if m.InWater() {
		drag = phantomWaterDrag
	} else if m.InLava() {
		drag = phantomLavaDrag
	}
	m.VX, m.VY, m.VZ = m.VX*drag, m.VY*drag, m.VZ*drag
}

// wrapDegrees is Mth.wrapDegrees: an angle into [-180, 180).
func wrapDegrees(d float64) float64 {
	d = math.Mod(d, 360)
	if d >= 180 {
		d -= 360
	}
	if d < -180 {
		d += 360
	}
	return d
}

// approachFloat is Mth.approach: value moved toward target by at most inc.
func approachFloat(value, target, inc float64) float64 {
	if value < target {
		return math.Min(value+inc, target)
	}
	return math.Max(value-inc, target)
}
