package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// TypeBreeze is the trial chamber's wind-shooting monster.
const TypeBreeze = registry.EntityTypeBreeze

func init() {
	// vanilla: EntityTypes.BREEZE, Breeze.createAttributes
	kinds[TypeBreeze] = &kind{
		headRotSpeed: 25, maxHeadYRot: 30,
		name: "Breeze", width: 0.6, height: 1.77, eyeHeight: 1.3452,
		maxHealth: 30, speed: float64(float32(0.63)), followRange: 24,
		attackDamage: 3, hostile: true,
		sounds: MobSounds{Hurt: registry.SoundEntityBreezeHurt, Death: registry.SoundEntityBreezeDeath},
	}
	mobTracking[TypeBreeze] = tracking{rangeChunks: 10, updateInterval: 3}
}

// breezeState stores Brain's expiring memories and the move sink's timeout.
type breezeState struct {
	shoot, charge, recover, cooldown int
	slide, pause, unseen             int
	pose                             int32
	destination                      Waypoint

	jumpCooldown  int
	jumpInhaling  int
	jumpTimeout   int
	jumpTarget    Waypoint
	hasJumpTarget bool
	leavingWater  bool
	noFriction    bool
}

// BreezePose is the synchronized breeze pose.
func (m *Mob) BreezePose() int32 { return m.breeze.pose }

// breezeStep runs the FIGHT priorities, with the move sink and idle stroll.
// vanilla: BreezeAi, Shoot, Slide
func (m *Mob) breezeStep(t *tickView) {
	if m.Dead || m.NoAI {
		return
	}
	b := &m.breeze
	for _, memory := range []*int{&b.shoot, &b.cooldown, &b.pause, &b.jumpCooldown} {
		if *memory > 0 {
			*memory--
		}
	}
	p := m.breezeTarget(t)
	if b.pose == protocol.PoseShooting {
		if p == nil || b.shoot == 0 {
			m.breezeStopShooting()
		} else {
			m.lookAtTarget(p)
			if b.charge > 0 {
				b.charge--
				if b.charge == 0 {
					m.breezeFire(t, p)
					b.recover = 4
				}
			} else if b.recover > 0 {
				b.recover--
				if b.recover == 0 {
					m.breezeStopShooting()
				}
			}
			return
		}
	}
	if b.pose == protocol.PoseInhaling || b.pose == protocol.PoseLongJumping {
		m.breezeLongJumpTick(t)
		return
	}
	// Shoot.checkExtraStartConditions runs before LongJump in the same pass and
	// forgets the shoot memory once the target is out of range.
	if p != nil && b.shoot > 0 && b.cooldown == 0 && b.slide == 0 && b.pose == protocol.PoseStanding &&
		len(m.nav.path) == 0 && Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) >= 256 {
		b.shoot = 0
	}
	if p != nil && b.shoot == 0 && b.jumpCooldown == 0 && b.pose == protocol.PoseStanding &&
		len(m.nav.path) == 0 && m.breezeCanLongJump(t, p) {
		m.breezeStartLongJump(t)
		return
	}
	if p != nil && b.shoot == 0 && b.pose == protocol.PoseStanding && len(m.nav.path) == 0 &&
		m.breezeStuck() {
		b.shoot = 60
		return
	}
	if b.slide > 0 {
		b.slide--
		if b.slide == 0 || len(m.nav.path) == 0 || m.breezeAtDestination() {
			m.stopSeeking()
			b.slide = 0
			m.SetMetadataBreezePose(protocol.PoseStanding)
			if p != nil {
				b.shoot = 60
			} else {
				b.pause = 20 + m.Rand.Intn(81)
			}
		}
		return
	}
	if p != nil {
		m.lookAtTarget(p)
		if b.shoot > 0 && b.cooldown == 0 && Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) < 256 {
			m.SetMetadataBreezePose(protocol.PoseShooting)
			b.charge = 15
			m.breezeSound(t, registry.SoundEntityBreezeInhale, 1)
			return
		}
		if b.shoot == 0 && b.jumpCooldown == 0 && m.OnGround && !m.InWater() && b.pose == protocol.PoseStanding && len(m.nav.path) == 0 {
			m.breezeStartSlide(t, m.breezeSlidePos(t, p))
		}
		return
	}
	if b.pause == 0 && m.OnGround && !m.InWater() {
		if spot, ok := strollPos(t.w, m); ok {
			m.breezeStartSlide(t, spot)
		}
		b.pause = 20 + m.Rand.Intn(81)
	}
}

// breezeLongJumpTick is LongJump's inhale, launch and landing phases.
// vanilla: LongJump.start, LongJump.tick
func (m *Mob) breezeLongJumpTick(t *tickView) {
	b := &m.breeze
	if b.jumpTimeout > 0 {
		b.jumpTimeout--
	}
	if b.jumpTimeout == 0 {
		m.breezeStopLongJump()
		return
	}
	if !m.InWater() && b.leavingWater {
		b.leavingWater = false
	}
	if b.jumpInhaling > 0 {
		b.jumpInhaling--
		m.goatLookAt(float64(b.jumpTarget.X)+0.5, float64(b.jumpTarget.Y)+0.5, float64(b.jumpTarget.Z)+0.5)
		if b.jumpInhaling > 0 {
			return
		}
	}
	if b.pose == protocol.PoseInhaling {
		target := [3]float64{float64(b.jumpTarget.X) + 0.5, float64(b.jumpTarget.Y), float64(b.jumpTarget.Z) + 0.5}
		v, ok := m.breezeJumpVector(t.w, target)
		if !ok {
			m.breezeStopLongJump()
			return
		}
		if m.InWater() {
			b.leavingWater = true
		}
		m.SetMetadataBreezePose(protocol.PoseLongJumping)
		b.noFriction = true
		m.Yaw = m.HeadYaw
		m.VX, m.VY, m.VZ = v[0], v[1], v[2]
		m.breezeSound(t, registry.SoundEntityBreezeJump, 1)
		return
	}
	if m.OnGround || m.InWater() && !b.leavingWater {
		m.breezeSound(t, registry.SoundEntityBreezeLand, 1)
		m.SetMetadataBreezePose(protocol.PoseStanding)
		b.noFriction = false
		// HURT_BY lives as long as getLastDamageSource, 40 ticks.
		b.jumpCooldown = 10
		if m.lastDamageSource() != 0 {
			b.jumpCooldown = 2
		}
		b.shoot = 100
		b.hasJumpTarget, b.leavingWater = false, false
	}
}

func (m *Mob) breezeStopLongJump() {
	b := &m.breeze
	m.SetMetadataBreezePose(protocol.PoseStanding)
	b.jumpInhaling, b.jumpTimeout, b.noFriction = 0, 0, false
	b.hasJumpTarget, b.leavingWater = false, false
}

// breezeCanLongJump is LongJump.canRun. A jump already chosen overrides the
// target-distance tests, because the behaviour keeps its target while inhaling.
// vanilla: LongJump.canRun
func (m *Mob) breezeCanLongJump(t *tickView, p *Player) bool {
	if !m.OnGround && !m.InWater() {
		return false
	}
	if m.InWater() && m.waterFluid.height > m.fluidJumpThreshold() || m.InLava() {
		return false
	}
	b := &m.breeze
	if b.hasJumpTarget {
		return true
	}
	distanceSquared := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
	follow := m.followRange()
	if distanceSquared > follow*follow {
		m.SetMetadataTargetID(0)
		b.unseen = 0
		return false
	}
	if math.Sqrt(distanceSquared)-4 <= 0 {
		return false
	}
	cell := m.cell()
	if registry.BlockOf(t.w.BlockAt(cell.X, cell.Y, cell.Z)) == registry.BlockHoneyBlock {
		return false
	}
	for dy := int32(1); dy <= 4; dy++ {
		state := t.w.BlockAt(cell.X, cell.Y+dy, cell.Z)
		if registry.BlockOf(state) != registry.BlockAir && blocklogic.FluidOf(state).Kind != blocklogic.FluidWater {
			return false
		}
	}
	x, z := m.breezePointBehind(t, p)
	target, ok := m.breezeSnapToSurface(t.w, x, p.Y, z)
	if !ok {
		return false
	}
	if spawnBlockDangerous(TypeBreeze, t.w.BlockAt(target.X, target.Y-1, target.Z)) {
		return false
	}
	if !m.breezeHasLineOfSight(t.w, target.X, target.Y, target.Z) &&
		!m.breezeHasLineOfSight(t.w, target.X, target.Y+4, target.Z) {
		return false
	}
	b.jumpTarget = target
	b.hasJumpTarget = true
	return true
}

func (m *Mob) breezeStartLongJump(t *tickView) {
	b := &m.breeze
	if b.jumpInhaling == 0 {
		b.jumpInhaling = 10
	}
	b.jumpTimeout = 200
	m.SetMetadataBreezePose(protocol.PoseInhaling)
	m.breezeSound(t, registry.SoundEntityBreezeCharge, 1)
	m.goatLookAt(float64(b.jumpTarget.X)+0.5, float64(b.jumpTarget.Y)+0.5, float64(b.jumpTarget.Z)+0.5)
}

// breezePointBehind is BreezeUtil.randomPointBehindTarget.
// vanilla: BreezeUtil.randomPointBehindTarget
func (m *Mob) breezePointBehind(t *tickView, p *Player) (float64, float64) {
	angle := float32(p.Yaw) + 180 + float32(m.Rand.NormFloat64())*45
	radius := float64(float32(4) + m.Rand.Float32()*4)
	x, _, z := LookVector(angle, 0)
	return p.X + x*radius, p.Z + z*radius
}

// breezeSnapToSurface clips first ten blocks down, then ten up, and lands on
// the cell above the first collision.
// vanilla: LongJump.snapToSurface
func (m *Mob) breezeSnapToSurface(w World, x, y, z float64) (Waypoint, bool) {
	if c := clipArrowFlight(w, x, y, z, 0, -10, 0); c.ok {
		return Waypoint{X: c.bx, Y: c.by + 1, Z: c.bz}, true
	}
	if c := clipArrowFlight(w, x, y, z, 0, 10, 0); c.ok {
		return Waypoint{X: c.bx, Y: c.by + 1, Z: c.bz}, true
	}
	return Waypoint{}, false
}

// breezeHasLineOfSight tests a cell's centre within the shared fifty-block
// floor and follow range.
// vanilla: BreezeUtil.hasLineOfSight
func (m *Mob) breezeHasLineOfSight(w World, x, y, z int32) bool {
	tx, ty, tz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	dx, dy, dz := tx-m.X, ty-m.Y, tz-m.Z
	if math.Sqrt(dx*dx+dy*dy+dz*dz) > math.Max(50, m.followRange()) {
		return false
	}
	return !clipArrowFlight(w, m.X, m.Y, m.Z, dx, dy, dz).ok
}

// breezeJumpVector tries the allowed angles in a random order, with Jump
// Boost's rise added to the launch's vertical component.
// vanilla: LongJump.calculateOptimalJumpVector
func (m *Mob) breezeJumpVector(w World, target [3]float64) ([3]float64, bool) {
	angles := []int{40, 55, 60, 75, 80}
	m.Rand.Shuffle(len(angles), func(a, b int) { angles[a], angles[b] = angles[b], angles[a] })
	maxV := float64(float32(0.058333334) * float32(m.followRange()))
	for _, angle := range angles {
		v, ok := jumpVectorForAngle(w, m, target, maxV, angle, false)
		if !ok {
			continue
		}
		boost := rules.JumpBoostPower(m.effects.All())
		if boost != 0 {
			length := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
			v[1] += v[1] / length * boost
		}
		return v, true
	}
	return [3]float64{}, false
}

// breezeStuck is ShootWhenStuck's rider, water and levitation check.
// vanilla: ShootWhenStuck.checkExtraStartConditions
func (m *Mob) breezeStuck() bool {
	_, levitating := rules.LevitationLift(m.effects.All())
	return m.RidesVehicle || m.Vehicle != nil || m.InWater() || levitating
}

func (m *Mob) breezeStopShooting() {
	b := &m.breeze
	m.SetMetadataBreezePose(protocol.PoseStanding)
	b.shoot, b.charge, b.recover, b.cooldown = 0, 0, 0, 10
}

// breezeCandidate is the player or iron golem with the ID, nil when it is
// not one the breeze may attack.
// vanilla: Breeze.canAttack
func (m *Mob) breezeCandidate(t *tickView, id int32) *Player {
	if id == 0 {
		return nil
	}
	if p := t.playerByID(id); p != nil {
		if !p.Huntable || p.Creative || p.Spectator || p.Dead || m.alliedTo(p) {
			return nil
		}
		return p
	}
	if o := t.mobByID(id); o != nil && o.Type == TypeIronGolem && o.Alive() && !o.Invulnerable && !m.alliedToMob(o) {
		p := o.asTarget()
		return &p
	}
	return nil
}

// breezeNotices is whether the breeze sees the candidate within its follow
// range, a golem by its eyes.
// vanilla: Sensor.isEntityAttackable
func (m *Mob) breezeNotices(t *tickView, p *Player) bool {
	if t.playerByID(p.EntityID) != nil {
		return m.noticesPlayer(t, p, m.followRange(), true)
	}
	o := t.mobByID(p.EntityID)
	return o != nil && m.distanceToPlayer(p) <= m.followRange() && m.seesPoint(t.w, o.X, o.Y+o.eyeHeight(), o.Z)
}

// breezeTarget retains a seen target for 100 ticks before reacquiring.
// vanilla: BreezeAttackEntitySensor, Sensor.wasEntityAttackableLastNTicks
func (m *Mob) breezeTarget(t *tickView) *Player {
	b := &m.breeze
	if p := m.breezeCandidate(t, m.TargetID); p != nil {
		if m.breezeNotices(t, p) {
			b.unseen = 0
		} else {
			b.unseen++
		}
		if b.unseen < 100 {
			return p
		}
	}
	if b.unseen >= 100 && m.retaliate == m.TargetID {
		m.retaliate = 0
	}
	m.SetMetadataTargetID(0)
	b.unseen = 0
	var target *Player
	best := math.Inf(1)
	consider := func(p *Player) {
		if d := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z); d < best && m.breezeNotices(t, p) {
			best, target = d, p
		}
	}
	for i := range t.players {
		if p := m.breezeCandidate(t, t.players[i].EntityID); p != nil {
			consider(p)
		}
	}
	for _, o := range t.others {
		if o.Type == TypeIronGolem {
			if p := m.breezeCandidate(t, o.ID); p != nil {
				consider(p)
			}
		}
	}
	if target == nil {
		target = m.breezeCandidate(t, m.retaliate)
	}
	if target != nil {
		m.SetMetadataTargetID(target.EntityID)
	}
	return target
}

func (m *Mob) breezeSound(t *tickView, sound registry.Sound, volume float32) {
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: volume, Pitch: 1})
}

// vanilla: Shoot.tick, Breeze.getFiringYPosition, Entity.getY
func (m *Mob) breezeFire(t *tickView, p *Player) {
	y := m.Y + m.height()/2 + 0.3
	fraction := 0.3
	if p.Passenger {
		fraction = 0.8
	}
	_, height, _ := p.box()
	vx, vy, vz := ShootVelocity(p.X-m.X, p.Y+height*fraction-y, p.Z-m.Z,
		float64(float32(0.7)), 5-4*float64(t.w.Difficulty()), m.Rand)
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobShotBreezeCharge, X: m.X, Y: y, Z: m.Z, VX: vx, VY: vy, VZ: vz})
	m.breezeSound(t, registry.SoundEntityBreezeShoot, 1.5)
}

func (m *Mob) breezeAtDestination() bool {
	p := m.breeze.destination
	return math.Abs(m.X-(float64(p.X)+0.5))+math.Abs(m.Y-float64(p.Y))+math.Abs(m.Z-(float64(p.Z)+0.5)) <= 1
}

func (m *Mob) breezeStartSlide(t *tickView, spot Waypoint) {
	if !m.seekTo(t.w, float64(spot.X)+0.5, float64(spot.Y), float64(spot.Z)+0.5, float64(float32(0.6)), navReach) {
		return
	}
	b := &m.breeze
	b.destination, b.slide = spot, 20+m.Rand.Intn(21)
	m.SetMetadataBreezePose(protocol.PoseSliding)
	m.breezeSound(t, registry.SoundEntityBreezeSlide, 1)
}

// vanilla: Slide.start, BreezeUtil.randomPointBehindTarget
func (m *Mob) breezeSlidePos(t *tickView, p *Player) Waypoint {
	center := m.cell()
	dx, dz := p.X-(float64(center.X)+0.5), p.Z-(float64(center.Z)+0.5)
	if dx*dx+dz*dz < 16 && math.Abs(p.Y-(float64(center.Y)+0.5)) < 10 {
		if spot, ok := awayRandomPos(t.w, m, p.X, p.Z, 5, 5); ok {
			x, y, z := float64(spot.X)+0.5, float64(spot.Y), float64(spot.Z)+0.5
			dist := Dist2(m.X, m.Y, m.Z, x, y, z)
			if dist <= math.Pow(math.Max(50, m.followRange()), 2) && !clipArrowFlight(t.w, m.X, m.Y, m.Z, x-m.X, y-m.Y, z-m.Z).ok && Dist2(p.X, p.Y, p.Z, x, y, z) > Dist2(p.X, p.Y, p.Z, m.X, m.Y, m.Z) {
				return spot
			}
		}
	}
	if m.Rand.Intn(2) == 0 {
		angle := float32(p.Yaw) + 180 + float32(m.Rand.NormFloat64())*45
		r := float64(float32(4) + m.Rand.Float32()*4)
		x, _, z := LookVector(angle, 0)
		return cellAt(p.X+x*r, p.Y, p.Z+z*r)
	}
	direction := vec3{p.X - m.X, p.Y - m.Y, p.Z - m.Z}
	distance := math.Sqrt(Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)) - (8 - 4*m.Rand.Float64())
	direction = direction.normalize()
	return cellAt(m.X+direction.x*distance, m.Y+direction.y*distance, m.Z+direction.z*distance)
}
