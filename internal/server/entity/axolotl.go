package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TypeAxolotl is minecraft:axolotl.
const TypeAxolotl = registry.EntityTypeAxolotl

// Axolotl variants, in Axolotl.Variant's legacy and stream order.
const (
	AxolotlLucy = iota
	AxolotlWild
	AxolotlGold
	AxolotlCyan
	AxolotlBlue
)

func init() {
	// vanilla: EntityTypes.AXOLOTL, Axolotl.createAttributes, BABY_DIMENSIONS
	kinds[TypeAxolotl] = &kind{
		fireAverse:  true,
		maxHeadXRot: 1, maxHeadYRot: 1,
		name: "Axolotl", width: 0.75, height: float64(float32(0.42)), eyeHeight: float64(float32(0.2751)),
		babyWidth: 0.375, babyHeight: float64(float32(0.21)), babyEye: 0.09375,
		maxHealth: 14, speed: 1, attackDamage: 2, stepHeight: 1,
		temptTag: &registry.ItemTagIDAxolotlFood, temptPace: 0.5, followPace: 0.6, strollPace: 0.5,
		ambientInterval: 120,
		sounds:          MobSounds{Ambient: registry.SoundEntityAxolotlIdleAir, Hurt: registry.SoundEntityAxolotlHurt, Death: registry.SoundEntityAxolotlDeath},
	}
	mobTracking[TypeAxolotl] = tracking{rangeChunks: 10, updateInterval: 3}
}

type axolotlState struct {
	variant            int
	playDead           int
	playingDead        bool
	huntingCooldown    int64
	temptationCooldown int
	attackCooldown     int
	speed, up          float64
	senseIn            int
	attackable, adult  *Mob
	tempting           int32
	lookID             int32
}

// AxolotlVariant is the synced variant, defaulting unknown legacy IDs to lucy.
func (m *Mob) AxolotlVariant() int { return m.axolotl.variant }

// SetAxolotlVariant applies Axolotl.Variant.byId's zero fallback.
func (m *Mob) SetAxolotlVariant(v int) {
	if v < AxolotlLucy || v > AxolotlBlue {
		v = AxolotlLucy
	}
	m.SetMetadataAxolotlVariant(v)
}

// PlayingDead is Axolotl.DATA_PLAYING_DEAD.
func (m *Mob) PlayingDead() bool { return m.Type == TypeAxolotl && m.axolotl.playingDead }

// Rehydrate restores the air supplied by a splash water bottle.
// vanilla: Axolotl.rehydrate
func (m *Mob) Rehydrate() {
	if m.Type == TypeAxolotl {
		m.SetMetadataAirSupply(min(axolotlMaxAir, m.AirSupply+1800))
	}
}

func (m *Mob) finalizeAxolotl(group *SpawnGroup) {
	// vanilla: Axolotl.finalizeSpawn, AxolotlGroupData
	if group == nil {
		m.SetMetadataAxolotlVariant(m.Rand.Intn(4))
		return
	}
	if !group.axolotlSet {
		group.axolotlVariants = [2]int{m.Rand.Intn(4), m.Rand.Intn(4)}
		group.axolotlSet = true
	}
	m.SetMetadataAxolotlVariant(group.axolotlVariants[m.Rand.Intn(2)])
	if group.axolotlCount >= 2 {
		m.SetMetadataBabyAge(-24000)
	}
	group.axolotlCount++
}

func (m *Mob) breedAxolotl(baby, partner *Mob) {
	// vanilla: Axolotl.getBreedOffspring
	baby.SetMetadataAxolotlVariant(m.axolotl.variant)
	if m.Rand.Intn(1200) == 0 {
		baby.SetMetadataAxolotlVariant(AxolotlBlue)
	} else if m.Rand.Intn(2) != 0 {
		baby.SetMetadataAxolotlVariant(partner.axolotl.variant)
	}
	baby.Persistence = true
}

func (m *Mob) noteAxolotlHurt(damage float64, entityBehind bool) {
	// vanilla: Axolotl.hurtServer; this roll precedes LivingEntity's hurt window.
	if m.Type == TypeAxolotl && !m.NoAI && m.Rand.Intn(3) == 0 &&
		(float64(m.Rand.Intn(3)) < damage || m.Health/m.MaxHealth < 0.5) &&
		damage < m.Health && m.InWater() && entityBehind && !m.PlayingDead() {
		m.axolotl.playDead = 200
	}
}

const axolotlMaxAir = 6000 // vanilla: Axolotl.getMaxAirSupply

func (m *Mob) axolotlAirStep(w World, events []MobEvent) []MobEvent {
	// vanilla: Axolotl.baseTick, handleAirSupply
	if m.NoAI {
		return events
	}
	if m.InWater() || m.inRain(w) {
		m.SetMetadataAirSupply(axolotlMaxAir)
		return events
	}
	if !m.Alive() {
		return events
	}
	m.SetMetadataAirSupply(m.AirSupply - 1)
	if m.AirSupply <= rules.AirDrownFloor {
		m.SetMetadataAirSupply(0)
		if landed, soft := m.plainHurt(2, registry.DamageTypeDryOut, false); landed {
			m.noteLastDamage(registry.DamageTypeDryOut)
			events = append(events, MobEvent{Mob: m, Kind: MobBurned, DamageType: registry.DamageTypeDryOut, Softened: soft, NoBubble: true})
		}
	}
	return events
}

func (m *Mob) axolotlBrainStep() {
	a := &m.axolotl
	if a.attackCooldown > 0 {
		a.attackCooldown--
	}
	if a.temptationCooldown > 0 {
		a.temptationCooldown--
	}
	if a.huntingCooldown > 0 {
		a.huntingCooldown--
	}
	// vanilla: ValidatePlayDead, PlayDead.start, Axolotl.customServerAiStep
	if a.playDead > 0 {
		a.playDead--
		if !a.playingDead && m.InWater() && m.retaliate != 0 && m.lastDamageSource() != 0 {
			m.stopSeeking()
			m.AddEffect(effect.New(mobEffectRegeneration, 200, 0, false, true, true))
		}
	}
	if m.axolotlBreeding() {
		a.playDead = 0
	}
	m.SetMetadataAxolotlPlayingDead(a.playDead > 0)
}

// axolotlBreeding is BehaviorUtils.isBreeding: an actual BREED_TARGET, not love alone.
func (m *Mob) axolotlBreeding() bool {
	for _, g := range m.ai.running {
		if g, ok := g.(*gatedGoal); ok {
			if b, ok := g.Goal.(*breedGoal); ok && b.partner != nil {
				return true
			}
		}
	}
	return false
}

func axolotlGoals(m *Mob) []Goal {
	idle := func() bool { return !m.PlayingDead() && m.TargetID == 0 }
	return []Goal{
		&axolotlPlayDeadGoal{m: m},
		&axolotlHuntGoal{m: m},
		&gatedGoal{Goal: &breedGoal{m: m, priority: 2}, enabled: idle},
		&gatedGoal{Goal: &runOneGoal{m: m, priority: 3, choices: []Goal{
			&temptGoal{m: m, cooldownTicks: &m.axolotl.temptationCooldown, sensedPlayer: &m.axolotl.tempting},
			&followParentGoal{m: m, ranged: &followRange{min: 5, max: 16, scan: 16}, sensedParent: &m.axolotl.adult, oneShot: true},
		}}, enabled: idle},
		&gatedGoal{Goal: &swimmingIdleGoal{m: m, amphibious: true, lookID: &m.axolotl.lookID}, enabled: idle},
		&gatedGoal{Goal: &timedPlayerLookGoal{m: m, priority: 7, reach: 6, lookID: &m.axolotl.lookID}, enabled: idle},
	}
}

// axolotlSense runs independently of the active activity, at Sensor's 20-tick cadence.
func (m *Mob) axolotlSense(t *tickView) {
	a := &m.axolotl
	if a.senseIn > 0 {
		a.senseIn--
		return
	}
	a.senseIn = 19
	a.attackable, a.adult, a.tempting = nil, nil, 0
	senseDist := m.followRange() * m.followRange()
	attackDist, adultDist := 64.0, senseDist
	for _, o := range t.others {
		if o == m || !o.Alive() {
			continue
		}
		d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z)
		if d > max(64, senseDist) || !rayClear(t.w, m.X, m.Y+m.eyeHeight(), m.Z, o.X, o.Y+o.eyeHeight(), o.Z) {
			continue
		}
		if o.Type == m.Type && !o.IsBaby() && d <= adultDist {
			a.adult, adultDist = o, d
		}
		if d > attackDist || !o.InWater() || o.Invulnerable || o.PlayingDead() || m.Team != "" && m.Team == o.Team {
			continue
		}
		if o.Type.InTag(registry.EntityTypeTagIDAxolotlAlwaysHostiles) || a.huntingCooldown == 0 && o.Type.InTag(registry.EntityTypeTagIDAxolotlHuntTargets) {
			a.attackable, attackDist = o, d
		}
	}
	if p := m.nearestTemptingPlayerWithin(t, m.temptRangeOr(temptRange)); p != nil {
		a.tempting = p.EntityID
	}
}

type axolotlPlayDeadGoal struct{ m *Mob }

func (g *axolotlPlayDeadGoal) Priority() int                 { return 0 }
func (g *axolotlPlayDeadGoal) Flags() GoalFlags              { return GoalMove | GoalLook | GoalJump }
func (g *axolotlPlayDeadGoal) CanStart(*tickView) bool       { return g.m.PlayingDead() }
func (g *axolotlPlayDeadGoal) ShouldContinue(*tickView) bool { return g.m.PlayingDead() }
func (g *axolotlPlayDeadGoal) Start(*tickView)               { g.m.stopSeeking() }
func (g *axolotlPlayDeadGoal) Tick(*tickView)                {}
func (g *axolotlPlayDeadGoal) Stop(*tickView)                {}
func (g *axolotlPlayDeadGoal) RequiresEveryTick() bool       { return true }

type swimmingIdleGoal struct {
	amphibious bool
	lookID     *int32
	m          *Mob
	waterRetry int
}

func (g *swimmingIdleGoal) Priority() int    { return 4 }
func (g *swimmingIdleGoal) Flags() GoalFlags { return GoalMove }
func (g *swimmingIdleGoal) CanStart(t *tickView) bool {
	m := g.m
	if len(m.nav.path) > 0 {
		return false
	}
	// vanilla: TryFindWater precedes GateBehavior's ORDERED / TRY_ALL.
	if g.amphibious && !rules.IsWater(t.w.BlockAt(m.cell().X, m.cell().Y, m.cell().Z)) {
		if m.Age < g.waterRetry {
			g.waterRetry = m.Age + 22
		} else {
			g.waterRetry = m.Age + 40
			if p, ok := axolotlWater(t.w, m); ok {
				return m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y)+0.5, float64(p.Z)+0.5, 0.15, 0)
			}
		}
	}
	if m.InWater() {
		if p, ok := axolotlSwimPos(t.w, m); ok {
			if m.seekTo(t.w, p[0], p[1], p[2], 0.5, 0) {
				return true
			}
		}
	} else if g.amphibious {
		if p, ok := landRandomPos(t.w, m, 10, 7); ok {
			if m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, 0.15, 0) {
				return true
			}
		}
	}
	for _, p := range t.players {
		if p.EntityID != *g.lookID || m.InWater() != rules.IsWater(t.w.BlockAt(floorI(p.X), floorI(p.Y), floorI(p.Z))) {
			continue
		}
		pace := 0.15
		if m.InWater() {
			pace = 0.5
		}
		return m.seekTo(t.w, p.X, p.Y, p.Z, pace, 3)
	}
	return false
}
func (g *swimmingIdleGoal) Start(*tickView)               {}
func (g *swimmingIdleGoal) ShouldContinue(*tickView) bool { return false }
func (g *swimmingIdleGoal) Tick(*tickView)                {}

// A one-shot writes WALK_TARGET; ending it must not cancel MoveToTargetSink.
func (g *swimmingIdleGoal) Stop(*tickView)          {}
func (g *swimmingIdleGoal) RequiresEveryTick() bool { return true }

// axolotlWater picks the first surface water in withinManhattan order, then a submerged fallback.
func axolotlWater(w World, m *Mob) (Waypoint, bool) {
	at := m.cell()
	var surface, fallback Waypoint
	surfaceDist, fallbackDist := int32(100), int32(100)
	for x := int32(-6); x <= 6; x++ {
		for y := int32(-6); y <= 6; y++ {
			for z := int32(6); z >= -6; z-- {
				if x == 0 && z == 0 {
					continue
				}
				p := Waypoint{at.X + x, at.Y + y, at.Z + z}
				if !rules.IsWaterBlock(w.BlockAt(p.X, p.Y, p.Z)) {
					continue
				}
				d := max(x, -x) + max(y, -y) + max(z, -z)
				if rules.IsAir(w.BlockAt(p.X, p.Y+1, p.Z)) {
					if d < surfaceDist {
						surface, surfaceDist = p, d
					}
				} else if d < fallbackDist && Dist2(m.X, m.Y, m.Z, float64(p.X)+0.5, float64(p.Y)+0.5, float64(p.Z)+0.5) >= 2.25 {
					fallback, fallbackDist = p, d
				}
			}
		}
	}
	if surfaceDist < 100 {
		return surface, true
	}
	return fallback, fallbackDist < 100
}

// axolotlSwimPos is RandomStroll.getTargetSwimPos's increasing-distance probe.
func axolotlSwimPos(w World, m *Mob) ([3]float64, bool) {
	var last [3]float64
	found := false
	for _, spread := range [][2]int{{1, 1}, {3, 3}, {5, 5}, {6, 5}, {7, 7}, {10, 7}} {
		var pos [3]float64
		if !found {
			for range 11 {
				p, ok := randomNavigablePos(w, m, spread[0], spread[1], true)
				if !ok {
					return last, found
				}
				pos = [3]float64{float64(p.X) + 0.5, float64(p.Y), float64(p.Z) + 0.5}
				if rules.IsWater(w.BlockAt(p.X, p.Y, p.Z)) {
					break
				}
			}
		} else {
			dx, dy, dz := last[0]-m.X, last[1]-m.Y, last[2]-m.Z
			length := math.Sqrt(dx*dx + dy*dy + dz*dz)
			if length < 1e-5 {
				return last, true
			}
			pos = [3]float64{m.X + dx/length*float64(spread[0]), m.Y + dy/length*float64(spread[1]), m.Z + dz/length*float64(spread[0])}
		}
		cell := Waypoint{floorI(pos[0]), floorI(pos[1]), floorI(pos[2])}
		if !rules.IsWater(w.BlockAt(cell.X, cell.Y, cell.Z)) || m.homeRestricted(spread[0]) && !m.withinHome(cell) {
			return last, found
		}
		last, found = pos, true
	}
	return last, found
}

// timedPlayerLookGoal is SetEntityLookTargetSometimes plus LookAtTargetSink.
type timedPlayerLookGoal struct {
	lookID *int32
	lookAtPlayer
	delay int
}

func (g *timedPlayerLookGoal) CanStart(t *tickView) bool {
	p := g.m.nearestPlayerWithin(t, 6, true)
	if p == nil {
		return false
	}
	if g.delay == 0 {
		g.delay = 30 + g.m.Rand.Intn(31)
	}
	g.delay--
	if g.delay > 0 {
		return false
	}
	g.target = p.EntityID
	return true
}
func (g *timedPlayerLookGoal) Start(*tickView) { *g.lookID = g.target }
func (g *timedPlayerLookGoal) player(t *tickView) *Player {
	for i := range t.players {
		p := &t.players[i]
		if p.EntityID == g.target && g.m.noticesPlayer(t, p, g.m.followRange(), true) {
			return p
		}
	}
	return nil
}
func (g *timedPlayerLookGoal) ShouldContinue(t *tickView) bool { return g.player(t) != nil }
func (g *timedPlayerLookGoal) Tick(t *tickView) {
	if p := g.player(t); p != nil {
		m := g.m
		m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(p.X-m.X, p.Z-m.Z)), 90)
		m.Pitch = turnToward(m.Pitch, float32(pitchToward(p.X-m.X, p.Y+p.eyeHeight()-(m.Y+m.eyeHeight()), p.Z-m.Z)), 45)
	}
}
func (g *timedPlayerLookGoal) Stop(*tickView) { *g.lookID = 0 }

// temptRangeOr is the tempt_range attribute, or fallback with no attribute
// map installed.
func (m *Mob) temptRangeOr(fallback float64) float64 {
	if m.attrs != nil {
		return m.attrs.Value(registry.AttributeTemptRange)
	}
	return fallback
}

func (m *Mob) axolotlMoveControl(w World) {
	m.axolotl.speed, m.axolotl.up = 0, 0
	if m.PlayingDead() {
		m.walk.speed = 0
		return
	}
	m.axolotl.speed, m.axolotl.up = m.smoothSwimMoveControl(w, 0.1, 0.5)
}

// smoothSwimMoveControl returns the acceleration and vertical input of a swimming move control.
// vanilla: SmoothSwimmingMoveControl.tick
func (m *Mob) smoothSwimMoveControl(w World, waterModifier, landModifier float64) (speed, up float64) {
	m.swimNavTickAt(w, m.pace()*m.nav.speedFactor)
	if len(m.nav.path) == 0 {
		m.walk.speed = 0
		return 0, 0
	}
	dx, dy, dz := m.nav.flyX-m.X, m.nav.flyY-m.Y, m.nav.flyZ-m.Z
	if dx*dx+dy*dy+dz*dz < 2.500000277905201e-7 {
		m.walk.speed = 0
		return 0, 0
	}
	targetYaw := float32(yawToward(dx, dz))
	m.Yaw = turnToward(m.Yaw, targetYaw, 10)
	m.HeadYaw = m.Yaw
	pace := m.pace() * m.nav.speedFactor
	if m.InWater() {
		pitch := math.Max(-85, math.Min(85, pitchToward(dx, dy, dz)))
		m.Pitch = turnToward(m.Pitch, float32(pitch), 5)
		speed = pace * waterModifier
		up = -math.Sin(float64(m.Pitch)*math.Pi/180) * pace
		pace *= math.Cos(float64(m.Pitch) * math.Pi / 180)
	} else {
		left := math.Abs(math.Mod(float64(m.Yaw-targetYaw)+540, 360) - 180)
		m.walk.speed = pace * landModifier * (1 - math.Max(0, math.Min(1, (left-10)/50)))
	}
	sin, cos := math.Sincos(float64(m.Yaw) * math.Pi / 180)
	m.walk.dirX, m.walk.dirZ = -sin, cos
	if m.InWater() {
		m.walk.speed = pace
	}
	return speed, up
}

func (m *Mob) axolotlTravelInWater(w World) {
	speed := m.axolotl.speed
	if m.NoAI || m.PlayingDead() || speed <= 0 {
		speed = 0
	}
	m.smoothSwimTravel(w, speed, m.axolotl.up)
}

// smoothSwimTravel applies swimming input and water drag.
// vanilla: LivingEntity.travelInWater, LivingEntity.aiStep input damping
func (m *Mob) smoothSwimTravel(w World, speed, up float64) {
	m.smoothSwimTravelInput(speed, up)
	m.move(w, m.VX, m.VY, m.VZ)
	m.VX, m.VY, m.VZ = m.VX*0.9, m.VY*0.9, m.VZ*0.9
}

func (m *Mob) smoothSwimTravelInput(speed, up float64) {
	forward, up := m.walk.speed*0.98, up*0.98
	scale := speed / math.Max(1, math.Hypot(forward, up))
	m.VX += m.walk.dirX * forward * scale
	m.VY += up * scale
	m.VZ += m.walk.dirZ * forward * scale
}

func (m *Mob) saveAxolotlBucket(tag nbt.CompoundTag) {
	tag.Set("Age", nbt.Int(m.BabyAge))
	tag.Set("AgeLocked", boolTag(m.AgeLocked))
	if m.axolotl.huntingCooldown > 0 {
		tag.Set("HuntingCooldown", nbt.Long(m.axolotl.huntingCooldown))
	}
}
func (m *Mob) loadAxolotlBucket(tag nbt.Compound) {
	age, _ := nbt.IntField(tag, "Age")
	m.SetMetadataBabyAge(int(age))
	m.SetMetadataAgeLocked(nbt.BoolField(tag, "AgeLocked"))
	m.axolotl.huntingCooldown, _ = nbt.LongValue(tag["HuntingCooldown"])
}

// axolotlHuntGoal is AxolotlAi's FIGHT activity and attackables sensor.
type axolotlHuntGoal struct {
	m           *Mob
	target      *Mob
	recalc      int
	unreachable int
}

func (g *axolotlHuntGoal) Priority() int    { return 1 }
func (g *axolotlHuntGoal) Flags() GoalFlags { return GoalMove | GoalLook }
func (g *axolotlHuntGoal) CanStart(t *tickView) bool {
	g.target = g.m.axolotl.attackable
	return !g.m.axolotlBreeding() && !g.m.PlayingDead() && g.target != nil && g.target.Alive() && !g.target.Invulnerable && !g.target.PlayingDead() && (g.m.Team == "" || g.m.Team != g.target.Team) && slices.Contains(t.others, g.target)
}
func (g *axolotlHuntGoal) Start(*tickView) {
	g.m.SetMetadataTargetID(g.target.ID)
	g.recalc = 0
	g.unreachable = 0
}
func (g *axolotlHuntGoal) ShouldContinue(t *tickView) bool {
	o := g.target
	return !g.m.axolotlBreeding() && !g.m.PlayingDead() && o != nil && o.Alive() && !o.Invulnerable && !o.PlayingDead() &&
		slices.Contains(t.others, o) && (g.m.Team == "" || g.m.Team != o.Team) && g.unreachable <= 200
}
func (g *axolotlHuntGoal) Tick(t *tickView) {
	m, o := g.m, g.target
	if o == nil || !o.Alive() || m.PlayingDead() {
		return
	}
	p := o.asTarget()
	m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(o.X-m.X, o.Z-m.Z)), 90)
	if g.unreachable > 0 {
		g.unreachable++
	}
	if m.inMeleeReach(&p) && rayClear(t.w, m.X, m.Y+m.eyeHeight(), m.Z, o.X, o.Y+o.eyeHeight(), o.Z) {
		m.stopSeeking()
		if m.axolotl.attackCooldown == 0 {
			m.axolotl.attackCooldown = 20
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: m.attackDamage(), Player: p},
				MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityAxolotlAttack, Volume: 1, Pitch: 1})
		}
	} else if g.recalc <= 0 {
		pace := 0.15
		if m.InWater() {
			pace = 0.6
		}
		if m.seekTo(t.w, o.X, o.Y, o.Z, pace, navReach) {
			g.unreachable = 0
		} else if g.unreachable == 0 {
			g.unreachable = 1
		}
		g.recalc = 10
	}
	g.recalc--
}
func (g *axolotlHuntGoal) Stop(t *tickView) {
	// vanilla: Axolotl.onStopAttacking; kill credit alone is insufficient:
	// the final fresh damage source must name this player.
	if o := g.target; t != nil && o != nil && !o.Alive() && o.lastDamageSource() != 0 {
		for _, p := range t.players {
			box := g.m.mobBox().Inflate(20)
			pw, ph, _ := p.box()
			playerBox := NewAABB(p.X-pw/2, p.Y, p.Z-pw/2, p.X+pw/2, p.Y+ph, p.Z+pw/2)
			if p.EntityID == o.blowAttacker && box.Crosses(playerBox) {
				t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobAxolotlSupported, Player: p})
				break
			}
		}
	}
	// vanilla: AxolotlAi.updateActivity, leaving FIGHT for any activity.
	g.m.axolotl.huntingCooldown = 2400
	g.m.SetMetadataTargetID(0)
	g.m.stopSeeking()
	g.target = nil
}
func (g *axolotlHuntGoal) RequiresEveryTick() bool { return true }
