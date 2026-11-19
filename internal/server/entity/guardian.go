package entity

import (
	"github.com/golem-mc/golem/registry"

	"math"

	"github.com/golem-mc/golem/internal/server/rules"
)

// The guardian and elder guardian: swimmers on their own move control that
// charge a beam at a visible player and prick attackers while their spikes
// are out. Natural spawns come from monuments.
// vanilla: Guardian, ElderGuardian, Guardian.GuardianAttackGoal

const (
	TypeGuardian      = registry.EntityTypeGuardian
	TypeElderGuardian = registry.EntityTypeElderGuardian
)

// The kinds are registered here, not in mob.go's table, so the guardians stay
// in one file.
func init() {
	// vanilla: EntityTypes.GUARDIAN and Guardian.createAttributes. The
	// follow range stays Mob.createMobAttributes' 16.
	kinds[TypeGuardian] = &kind{
		maxHeadXRot: 180,
		name:        "Guardian", width: 0.85, height: 0.85, eyeHeight: 0.425,
		maxHealth: 30, speed: 0.5, hostile: true, attackDamage: 6,
		sounds: MobSounds{
			Ambient: registry.SoundEntityGuardianAmbient,
			Hurt:    registry.SoundEntityGuardianHurt,
			Death:   registry.SoundEntityGuardianDeath,
		},
		ambientInterval: 160, // Guardian.getAmbientSoundInterval
	}
	// vanilla: EntityTypes.ELDER_GUARDIAN and ElderGuardian.createAttributes
	kinds[TypeElderGuardian] = &kind{
		family: TypeGuardian,
		name:   "Elder Guardian", width: 1.9975, height: 1.9975, eyeHeight: 0.99875,
		maxHealth: 80, speed: float64(float32(0.3)), hostile: true, attackDamage: 8,
		sounds: MobSounds{
			Ambient: registry.SoundEntityElderGuardianAmbient,
			Hurt:    registry.SoundEntityElderGuardianHurt,
			Death:   registry.SoundEntityElderGuardianDeath,
		},
		ambientInterval: 160,
	}
	// vanilla: EntityTypes.GUARDIAN clientTrackingRange(8), ELDER_GUARDIAN (10)
	mobTracking[TypeGuardian] = tracking{rangeChunks: 8, updateInterval: 3}
	mobTracking[TypeElderGuardian] = tracking{rangeChunks: 10, updateInterval: 3}
}

// The guardian's numbers.
const (
	guardianAttackTime     = 80  // Guardian.getAttackDuration
	elderAttackTime        = 60  // ElderGuardian.getAttackDuration
	guardianWindUp         = -10 // GuardianAttackGoal.start's attackTime
	guardianMinTargetDist2 = 9.0 // GuardianAttackSelector, canContinueToUse
	guardianThornsDamage   = 2.0 // Guardian.hurtServer
	guardianStrollEvery    = 80  // RandomStrollGoal(this, 1.0, 80)
	elderStrollEvery       = 400 // ElderGuardian's setInterval(400)
	guardianTurn           = 90  // LookControl.setLookAt(target, 90, 90)
	guardianSwimSpread     = 10  // DefaultRandomPos.getPos(mob, 10, 7)
	guardianSwimSpreadY    = 7
	guardianWaterValue     = 10.0 // Guardian.getWalkTargetValue
	guardianHomeSpread     = 16   // MoveTowardsRestrictionGoal's getPosTowards reach
	guardianLookRange      = 12.0 // the second LookAtPlayerGoal
	guardianLookRangeY     = 3.0  // LookAtPlayerGoal's bounding box inflate in y
	guardianLookChance     = 0.01

	elderCurseEvery = 1200 // ElderGuardian.EFFECT_INTERVAL
	elderHomeRadius = 16   // ElderGuardian.customServerAiStep
)

// The elder's curse, from ElderGuardian's EFFECT_ constants.
const (
	ElderCurseRadius   = 50.0
	ElderCurseDuration = 6000
	ElderCurseAmp      = 2
	ElderCurseDisplay  = 1200
)

// guardianState is what a guardian keeps beyond the common mob.
type guardianState struct {
	// moving is DATA_ID_MOVING: the spikes are drawn in while it swims to a
	// point.
	moving bool
	// target is DATA_ID_ATTACK_TARGET: the entity ID of the body the beam
	// is charging at, 0 for none.
	target int32
	speed  float64 // the move control's eased pace
	// trigger is RandomStrollGoal.forceTrigger.
	trigger bool
}

// IsGuardian reports whether the mob is a guardian or an elder guardian.
func (m *Mob) IsGuardian() bool { return m.Type == TypeGuardian || m.Type == TypeElderGuardian }

// GuardianMoving is the guardian's DATA_ID_MOVING.
func (m *Mob) GuardianMoving() bool { return m.guardian.moving }

// GuardianTarget is the entity ID in the guardian's DATA_ID_ATTACK_TARGET,
// 0 when no beam is charging.
func (m *Mob) GuardianTarget() int32 { return m.guardian.target }

// ThornsOut reports whether a blow struck now is pricked back: the spikes
// are out while the guardian is not swimming to a point.
// vanilla: Guardian.hurtServer
func (m *Mob) ThornsOut() bool { return m.IsGuardian() && !m.guardian.moving }

// GuardianThornsDamage is the damage the spikes deal back.
func GuardianThornsDamage() float64 { return guardianThornsDamage }

func (m *Mob) attackDuration() int {
	if m.Type == TypeElderGuardian {
		return elderAttackTime
	}
	return guardianAttackTime
}

// noteGuardianHurt is Guardian.hurtServer's trigger of the stroll.
func (m *Mob) noteGuardianHurt() {
	if m.IsGuardian() {
		m.guardian.trigger = true
	}
}

// guardianGoals is Guardian.registerGoals.
func guardianGoals(m *Mob) []Goal {
	interval := guardianStrollEvery
	if m.Type == TypeElderGuardian {
		interval = elderStrollEvery
	}
	return []Goal{
		&guardianTargetGoal{nearestTargetGoal{m: m, priority: 1, accept: guardianOkTarget(m)}},
		&guardianAttackGoal{m: m},
		&guardianHomeGoal{m: m},
		&guardianStrollGoal{m: m, interval: interval},
		&lookAtPlayer{m: m, priority: 8},
		&lookAtGuardian{m: m},
		&randomLookAround{m: m, priority: 9},
	}
}

// guardianOkTarget is GuardianAttackSelector: farther than three blocks.
func guardianOkTarget(m *Mob) func(*tickView, *Player) bool {
	return func(_ *tickView, p *Player) bool {
		return Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) > guardianMinTargetDist2
	}
}

// guardianTargetGoal is the guardian's NearestAttackableTargetGoal over every
// living entity its selector takes: players, squids and axolotls.
type guardianTargetGoal struct{ nearestTargetGoal }

func (g *guardianTargetGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.TargetID != 0 || m.Rand.Intn(reducedTickDelay(nearestScanInterval)) != 0 {
		return false
	}
	var id int32
	best := math.Inf(1)
	if p := m.nearestHuntable(t, g.accept, true); p != nil {
		id, best = p.EntityID, Dist2(p.X, p.Y, p.Z, m.X, m.Y+m.eyeHeight(), m.Z)
	}
	follow := m.followRange()
	for _, o := range t.others {
		if !o.is(TypeSquid) && o.Type != TypeAxolotl || !o.Alive() || o.Invulnerable || o.PlayingDead() ||
			Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) <= guardianMinTargetDist2 ||
			Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) > follow*follow ||
			!rayClear(t.w, m.X, m.Y+m.eyeHeight(), m.Z, o.X, o.Y+o.eyeHeight(), o.Z) {
			continue
		}
		if rank := Dist2(o.X, o.Y, o.Z, m.X, m.Y+m.eyeHeight(), m.Z); rank < best {
			id, best = o.ID, rank
		}
	}
	if id == 0 {
		return false
	}
	m.SetMetadataTargetID(id)
	return true
}

// ShouldContinue is TargetGoal.canContinueToUse for a mob target; a
// player is held as nearestTargetGoal holds one.
func (g *guardianTargetGoal) ShouldContinue(t *tickView) bool {
	o := t.mobByID(g.m.TargetID)
	if o == nil {
		return g.nearestTargetGoal.ShouldContinue(t)
	}
	follow := g.m.followRange()
	if !o.Alive() || o.Invulnerable || o.PlayingDead() || Dist2(g.m.X, g.m.Y, g.m.Z, o.X, o.Y, o.Z) > follow*follow {
		return false
	}
	if rayClear(t.w, g.m.X, g.m.Y+g.m.eyeHeight(), g.m.Z, o.X, o.Y+o.eyeHeight(), o.Z) {
		g.unseen = 0
		return true
	}
	g.unseen++
	return g.unseen <= reducedTickDelay(unseenMemoryTicks)
}

// guardianAttackGoal is Guardian.GuardianAttackGoal.
type guardianAttackGoal struct {
	m          *Mob
	attackTime int
}

func (g *guardianAttackGoal) Priority() int    { return 4 }
func (g *guardianAttackGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *guardianAttackGoal) CanStart(t *tickView) bool { return g.m.targetIn(t) != nil }

// ShouldContinue holds an elder's charge at any range and a guardian's until
// the target comes within three blocks.
func (g *guardianAttackGoal) ShouldContinue(t *tickView) bool {
	p := g.m.targetIn(t)
	if p == nil {
		return false
	}
	return g.m.Type == TypeElderGuardian || Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) > guardianMinTargetDist2
}

func (g *guardianAttackGoal) Start(t *tickView) {
	g.attackTime = guardianWindUp
	g.m.stopSeeking()
	if p := g.m.targetIn(t); p != nil {
		g.m.lookAtTarget(p)
	}
}

func (g *guardianAttackGoal) Stop(*tickView) {
	g.m.SetMetadataGuardianTarget(0)
	g.m.SetMetadataTargetID(0)
	g.m.guardian.trigger = true
}

// RequiresEveryTick is the goal's own override: the charge counts every tick.
func (g *guardianAttackGoal) RequiresEveryTick() bool { return true }

func (g *guardianAttackGoal) Tick(t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	m.stopSeeking()
	m.lookAtTarget(p)
	if !m.seesTarget(t.w, p) {
		m.SetMetadataTargetID(0)
		return
	}
	g.attackTime++
	if g.attackTime == 0 {
		m.SetMetadataGuardianTarget(p.EntityID)
		if !m.Silent {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobGuardianCharged})
		}
	} else if g.attackTime >= m.attackDuration() {
		magic := 1.0
		if t.w.Difficulty() == rules.DifficultyHard {
			magic += 2
		}
		if m.Type == TypeElderGuardian {
			magic += 2
		}
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobGuardianBeam, Damage: m.attackDamage(), Magic: magic, Player: *p})
		m.SetMetadataTargetID(0)
	}
}

// lookAtTarget turns the head square on the target's eyes, at the 90 degrees
// a tick the guardian's look control allows.
func (m *Mob) lookAtTarget(p *Player) {
	dx, dz := p.X-m.X, p.Z-m.Z
	if dx != 0 || dz != 0 {
		m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(dx, dz)), guardianTurn)
	}
	m.Pitch = turnToward(m.Pitch,
		float32(pitchToward(dx, p.Y+p.eyeHeight()-(m.Y+m.eyeHeight()), dz)), guardianTurn)
}

// guardianStrollGoal is RandomStrollGoal(this, 1.0, interval) over
// water-bound navigation, with the forced start a hurt or finished attack
// asks for.
type guardianStrollGoal struct {
	m        *Mob
	interval int
	target   Waypoint
	ok       bool
}

func (g *guardianStrollGoal) Priority() int    { return 7 }
func (g *guardianStrollGoal) Flags() GoalFlags { return GoalMove }

func (g *guardianStrollGoal) CanStart(t *tickView) bool {
	m := g.m
	if !m.guardian.trigger {
		if m.NoActionTicks >= strollIdleLimit || m.Rand.Intn(reducedTickDelay(g.interval)) != 0 {
			return false
		}
	}
	var found bool
	g.target, found = guardianStrollPos(t.w, m)
	if !found {
		return false
	}
	g.ok = swimmable(t.w, g.target)
	m.guardian.trigger = false
	return true
}

func (g *guardianStrollGoal) Start(t *tickView) {
	if g.ok {
		g.m.seekWater(t.w, g.target, 1.0)
	}
}

func (g *guardianStrollGoal) ShouldContinue(*tickView) bool { return g.m.fishOrdered() }
func (g *guardianStrollGoal) Tick(*tickView)                {}
func (g *guardianStrollGoal) Stop(*tickView)                { g.m.stopSeeking() }
func (g *guardianStrollGoal) RequiresEveryTick() bool       { return false }

// guardianStrollPos is DefaultRandomPos.getPos(mob, 10, 7): of ten offsets
// that are not solid, fire, lava or cactus (and inside the home, if set),
// the best by walk target value.
func guardianStrollPos(w World, m *Mob) (Waypoint, bool) {
	restrict := m.homeRestricted(guardianSwimSpread)
	return randomPosTries(w, m, func() (Waypoint, bool) {
		dx, dy, dz := randomOffset(m, guardianSwimSpread, guardianSwimSpreadY)
		c := m.offsetTowardHome(dx, dy, dz, guardianSwimSpread)
		return c, guardianCellOK(w, m, c, restrict)
	}, func(c Waypoint) (Waypoint, bool) { return c, true })
}

// guardianCellOK is DefaultRandomPos.generateRandomPosTowardDirection's
// test: inside the build height, the home and no solid block, fire, lava or
// cactus.
func guardianCellOK(w World, m *Mob, c Waypoint, restrict bool) bool {
	if c.Y < w.MinY() || c.Y >= w.MaxY() || (restrict && !m.withinHome(c)) || !notSolidRender(w, c) {
		return false
	}
	state := w.BlockAt(c.X, c.Y, c.Z)
	return !rules.IsFire(state) && !rules.IsLava(state) && !rules.IsCactus(state) && !rules.IsHotFloor(state)
}

// guardianHomeGoal is MoveTowardsRestrictionGoal(this, 1.0): outside the
// home, swim to a cell on the way back to it.
type guardianHomeGoal struct {
	m      *Mob
	target Waypoint
	ok     bool
}

func (g *guardianHomeGoal) Priority() int    { return 5 }
func (g *guardianHomeGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *guardianHomeGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.withinHome(m.cell()) {
		return false
	}
	// vanilla: DefaultRandomPos.getPosTowards(mob, 16, 7, home, pi/2)
	restrict := m.homeRestricted(guardianHomeSpread)
	dirX, dirZ := float64(m.home.center.X)+0.5-m.X, float64(m.home.center.Z)+0.5-m.Z
	var found bool
	g.target, found = randomPosTries(t.w, m, func() (Waypoint, bool) {
		angle := math.Atan2(dirZ, dirX) - math.Pi/2 + float64(2*m.Rand.Float32()-1)*math.Pi/2
		dist := math.Sqrt(m.Rand.Float64()) * guardianHomeSpread * math.Sqrt2
		xt, zt := -dist*math.Sin(angle), dist*math.Cos(angle)
		if math.Abs(xt) > guardianHomeSpread || math.Abs(zt) > guardianHomeSpread {
			return Waypoint{}, false
		}
		dy := m.Rand.Intn(2*guardianSwimSpreadY+1) - guardianSwimSpreadY
		c := m.offsetTowardHome(int(math.Floor(xt)), dy, int(math.Floor(zt)), guardianHomeSpread)
		return c, guardianCellOK(t.w, m, c, restrict)
	}, func(c Waypoint) (Waypoint, bool) { return c, true })
	if found {
		g.ok = swimmable(t.w, g.target)
	}
	return found
}

func (g *guardianHomeGoal) Start(t *tickView) {
	if g.ok {
		g.m.seekWater(t.w, g.target, 1.0)
	}
}

func (g *guardianHomeGoal) ShouldContinue(*tickView) bool { return g.m.fishOrdered() }
func (g *guardianHomeGoal) Tick(*tickView)                {}
func (g *guardianHomeGoal) Stop(*tickView)                { g.m.stopSeeking() }
func (g *guardianHomeGoal) RequiresEveryTick() bool       { return false }

// lookAtGuardian is LookAtPlayerGoal(this, Guardian.class, 12.0f, 0.01f): a
// glance at the nearest other guardian within twelve blocks.
type lookAtGuardian struct {
	m       *Mob
	remains int
	target  int32
}

func (g *lookAtGuardian) Priority() int    { return 8 }
func (g *lookAtGuardian) Flags() GoalFlags { return GoalLook }

func (g *lookAtGuardian) CanStart(t *tickView) bool {
	m := g.m
	if m.Rand.Float64() >= guardianLookChance {
		return false
	}
	var nearest *Mob
	best := guardianLookRange * guardianLookRange
	for _, o := range t.others {
		if o == m || !o.IsGuardian() || !o.Alive() || math.Abs(o.Y-m.Y) > m.height()+guardianLookRangeY {
			continue
		}
		if d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z); d <= best {
			best, nearest = d, o
		}
	}
	if nearest == nil {
		return false
	}
	g.target = nearest.ID
	return true
}

func (g *lookAtGuardian) Start(*tickView) {
	g.remains = reducedTickDelay(lookForTicks + g.m.Rand.Intn(lookForVaries))
}

func (g *lookAtGuardian) other(t *tickView) *Mob {
	o := t.mobByID(g.target)
	if o == nil || !o.Alive() || Dist2(g.m.X, g.m.Y, g.m.Z, o.X, o.Y, o.Z) > guardianLookRange*guardianLookRange {
		return nil
	}
	return o
}

func (g *lookAtGuardian) ShouldContinue(t *tickView) bool {
	return g.remains > 0 && g.other(t) != nil
}

func (g *lookAtGuardian) Tick(t *tickView) {
	g.remains--
	if o := g.other(t); o != nil {
		m := g.m
		dx, dz := o.X-m.X, o.Z-m.Z
		m.HeadYaw = float32(yawToward(dx, dz))
		m.Pitch = float32(pitchToward(dx, o.Y+o.eyeHeight()-(m.Y+m.eyeHeight()), dz))
	}
}

func (g *lookAtGuardian) Stop(*tickView)          {}
func (g *lookAtGuardian) RequiresEveryTick() bool { return false }

// guardianWalkTargetValue is Guardian.getWalkTargetValue: water is worth ten
// and the light's cost, any other cell Monster's.
func guardianWalkTargetValue(w World, c Waypoint) float64 {
	cost := monsterWalkTargetValue(w, c.X, c.Y, c.Z)
	if swimmable(w, c) {
		return guardianWaterValue - cost
	}
	return cost
}

// guardianMoveStep is the guardian's aiStep, move control and travel: breath
// refills in water and it flops on land, GuardianMoveControl steers,
// Guardian.travelInWater moves the body, and the elder curses and homes.
// vanilla: Guardian.aiStep, Guardian.GuardianMoveControl.tick,
// Guardian.travelInWater, ElderGuardian.customServerAiStep
func (m *Mob) guardianMoveStep(t *tickView) []MobEvent {
	var events []MobEvent
	g := &m.guardian
	if m.Type == TypeElderGuardian && (m.Age+int(m.ID))%elderCurseEvery == 0 {
		events = append(events, MobEvent{Mob: m, Kind: MobElderCurse, X: m.X, Y: m.Y, Z: m.Z})
	}
	if m.Type == TypeElderGuardian && !m.home.set {
		// Every tick, so a home a lead cleared is set again from where it is.
		c := m.cell()
		m.RestrictTo(c.X, c.Y, c.Z, elderHomeRadius)
	}
	if !m.InWater() && m.OnGround {
		// vanilla: Guardian.aiStep
		m.VX += float64(m.Rand.Float32()*2-1) * 0.4
		m.VY += 0.5
		m.VZ += float64(m.Rand.Float32()*2-1) * 0.4
		m.Yaw = m.Rand.Float32() * 360
		m.OnGround = false
	}
	if g.target != 0 {
		m.Yaw = m.HeadYaw
	}

	n := &m.navOf().nav
	m.swimNavTick(t.w)
	if len(n.path) == 0 {
		g.speed = 0
		m.SetMetadataGuardianMoving(false)
	} else {
		dx, dy, dz := n.flyX-m.X, n.flyY-m.Y, n.flyZ-m.Z
		length := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if length > 0 {
			yd := dy / length
			if dx != 0 || dz != 0 {
				m.Yaw = turnToward(m.Yaw, float32(yawToward(dx, dz)), 90)
				m.HeadYaw = turnToward(m.HeadYaw, m.Yaw, 10)
			}
			target := n.speedFactor * m.kind().speed
			g.speed += (target - g.speed) * 0.125
			clock := float64(m.Age + int(m.ID))
			push := math.Sin(clock*0.5) * 0.05
			yPush := math.Sin(clock*0.75) * 0.05
			sin, cos := math.Sincos(float64(m.Yaw) * math.Pi / 180)
			m.VX += push * cos
			m.VY += yPush*(sin+cos)*0.25 + g.speed*yd*0.1
			m.VZ += push * sin
			m.SetMetadataGuardianMoving(true)
		}
	}

	if !m.InWater() {
		return m.step(t.w, events)
	}
	// moveRelative(0.1, (0, 0, speed)): Mob.setSpeed also sets the forward
	// input.
	forward := g.speed * 0.1
	sin, cos := math.Sincos(float64(m.Yaw) * math.Pi / 180)
	m.VX -= forward * sin
	m.VZ += forward * cos
	m.move(t.w, m.VX, m.VY, m.VZ)
	m.VX, m.VY, m.VZ = m.VX*0.9, m.VY*0.9, m.VZ*0.9
	if !g.moving && m.TargetID == 0 {
		m.VY -= 0.005
	}
	return events
}
