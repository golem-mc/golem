package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

const (
	// spearApproachDist is the distance from the target the approach ends at.
	// vanilla: PiglinAi.initFightActivity SpearApproach(1.0, 10.0)
	spearApproachDist = 10.0
	// spearInRange is the distance at which a charge turns to reposition.
	// vanilla: PiglinAi.initFightActivity SpearAttack(1.0, 1.0, 2.0)
	spearInRange = 2.0
	// spearRepositionMin and spearRepositionMax are the distances from the
	// target a charge repositions to, spearRetreatMin and spearRetreatMax the
	// ones a retreat runs to.
	// vanilla: SpearAttack.MIN_REPOSITION_DISTANCE, SpearRetreat.MIN_COOLDOWN_DISTANCE
	spearRepositionMin, spearRepositionMax = 6.0, 7.0
	spearRetreatMin, spearRetreatMax       = 9.0, 11.0
	// spearVerticalRange is the vertical spread of both positions.
	spearVerticalRange = 7
	// spearMaxFleeing is SpearRetreat.MAX_FLEEING_TIME in ticks.
	spearMaxFleeing = 100
	// spearMobFactor scales a kinetic condition's speeds for a mob.
	// vanilla: KineticWeapon.damageEntities
	spearMobFactor = 0.2
)

// SpearStab rides a MobAttackedPlayer that is a kinetic weapon's contact: what
// the hit did to the body, Damage being the blow's damage.
// vanilla: LivingEntity.stabAttack
type SpearStab struct {
	DealsDamage, Knockback, Dismount bool
}

type spearStatus uint8

const (
	spearNone spearStatus = iota
	spearApproach
	spearCharging
	spearRetreat
)

// spearState is SPEAR_STATUS and the memories behind it.
type spearState struct {
	status spearStatus
	// used counts the ticks of the charge and engage the ticks left of it.
	used, engage int
	// away is SPEAR_CHARGE_POSITION or SPEAR_FLEEING_POSITION.
	away    Waypoint
	hasAway bool
	fleeing int
	// stabbed is the age each body was last hit at during this charge.
	// vanilla: LivingEntity.recentKineticEnemies
	stabbed map[int32]int
}

// holdsKinetic reports a main hand that carries a kinetic weapon.
func (m *Mob) holdsKinetic() bool {
	return !m.MainHand.Empty() && registry.StackKineticWeapon(m.MainHand) != nil
}

// holdsSpear reports a piglin's main hand that carries a kinetic weapon.
func (m *Mob) holdsSpear() bool {
	// vanilla: Piglin.canUseNonMeleeWeapon; a brute melees with it
	return m.Type == TypePiglin && m.holdsKinetic()
}

// chargeSpeedModifier is the speed factor of the root vehicle's Mob.chargeSpeedModifier.
func (m *Mob) chargeSpeedModifier() float64 {
	switch m.RootVehicle().Type {
	case TypeCamelHusk:
		return 4
	case TypeZombieHorse:
		return float64(float32(1.4))
	}
	return 1
}

// spearGoal is the fight activity of a piglin holding a spear: approach to ten
// blocks, charge with the item in use, reposition when close, and retreat to
// about ten blocks after the charge ends.
// vanilla: SpearApproach, SpearAttack, SpearRetreat
type spearGoal struct {
	m        *Mob
	priority int
}

func (g *spearGoal) Priority() int           { return g.priority }
func (g *spearGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *spearGoal) RequiresEveryTick() bool { return true }

func (g *spearGoal) CanStart(t *tickView) bool {
	return g.m.holdsSpear() && g.m.targetIn(t) != nil
}

func (g *spearGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *spearGoal) Start(*tickView) {}

func (g *spearGoal) Stop(*tickView) {
	m := g.m
	m.barter.spearState = spearState{}
	m.SetMetadataUsingItem(false)
	m.stopSeeking()
}

func (g *spearGoal) Tick(t *tickView) {
	m := g.m
	s := &m.barter.spearState
	p := m.targetIn(t)
	if p == nil {
		return
	}
	m.lookAtPoint(p.X, p.Y+p.eyeHeight(), p.Z)
	dist2 := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
	m.SetMetadataAggressive(true)
	switch s.status {
	case spearNone, spearApproach:
		if s.status == spearNone || dist2 > spearApproachDist*spearApproachDist {
			s.status = spearApproach
			if dist2 > spearApproachDist*spearApproachDist {
				m.seekTo(t.w, p.X, p.Y, p.Z, m.chargeSpeedModifier(), 0)
				break
			}
		}
		m.stopSeeking()
		s.status = spearCharging
		g.charge(t, p, dist2)
	case spearCharging:
		g.charge(t, p, dist2)
	case spearRetreat:
		g.retreat(t, p, dist2)
	}
}

// charge is SpearAttack: start using the spear, walk at the target and
// reposition when within two blocks or the walk is done, until the use ends.
func (g *spearGoal) charge(t *tickView, p *Player, dist2 float64) {
	m := g.m
	s := &m.barter.spearState
	k := registry.StackKineticWeapon(m.MainHand)
	if !m.UsingItem {
		m.SetMetadataUsingItem(true)
		m.SetMetadataUseOffHand(false)
		s.engage, s.used, s.hasAway, s.stabbed = int(k.ComputeDamageUseDuration()), 0, false, nil
	}
	s.engage--
	s.used++
	if s.hasAway {
		m.seekTo(t.w, float64(s.away.X)+0.5, float64(s.away.Y), float64(s.away.Z)+0.5, m.chargeSpeedModifier(), 0)
		if m.navDone() {
			s.hasAway = false
		}
	} else {
		m.seekTo(t.w, p.X, p.Y, p.Z, m.chargeSpeedModifier(), 0)
		if dist2 < spearInRange*spearInRange || m.navDone() {
			d := math.Sqrt(dist2)
			s.away, s.hasAway = posAwayBetween(t.w, m, spearRepositionMin-d, spearRepositionMax-d, spearVerticalRange, p.X, p.Z)
		}
	}
	m.stab(t, s, k, s.used)
	if s.engage <= 0 {
		m.SetMetadataUsingItem(false)
		m.stopSeeking()
		s.hasAway, s.status = false, spearRetreat
	}
}

// retreat is SpearRetreat: run to a point about ten blocks from the target for
// at most 100 ticks, then start over.
func (g *spearGoal) retreat(t *tickView, p *Player, dist2 float64) {
	m := g.m
	s := &m.barter.spearState
	if !s.hasAway {
		d := math.Sqrt(dist2)
		s.away, s.hasAway = posAwayBetween(t.w, m, max(0, spearRetreatMin-d), max(1, spearRetreatMax-d), spearVerticalRange, p.X, p.Z)
		s.fleeing = 0
		if !s.hasAway {
			// vanilla: SpearRetreat.checkExtraStartConditions fails; the status
			// stays RETREAT and the next tick tries again
			return
		}
	}
	s.fleeing++
	m.seekTo(t.w, float64(s.away.X)+0.5, float64(s.away.Y), float64(s.away.Z)+0.5, m.chargeSpeedModifier(), 0)
	if s.fleeing >= spearMaxFleeing || m.navDone() {
		m.stopSeeking()
		s.hasAway, s.status = false, spearNone
	}
}

// KineticContact is the decision half of KineticWeapon.damageEntities for one
// body: what its conditions allow at the attacker's speed along the look and
// the body's, in blocks per second, and the damage base plus the relative
// speed's share. ok is false when nothing applies. factor scales the minimums
// (spearMobFactor for a mob, 1 for a player).
func KineticContact(k *registry.KineticWeapon, used int, factor, speed, targetSpeed, base float64) (stab *SpearStab, damage float64, ok bool) {
	rel := max(0, speed-targetSpeed)
	stab = &SpearStab{
		Dismount:    k.Dismount.Test(int32(used), speed, rel, factor),
		Knockback:   k.Knockback.Test(int32(used), speed, rel, factor),
		DealsDamage: k.Damage.Test(int32(used), speed, rel, factor),
	}
	return stab, base + float64(int(rel*float64(k.DamageMultiplier))), stab.Dismount || stab.Knockback || stab.DealsDamage
}

// stab is KineticWeapon.damageEntities: every body along the look in reach
// that was not hit for the contact cooldown takes what the weapon's conditions
// allow at the speeds of this tick, along the reach of AttackRangeWith the
// main hand: a spear that removes its attack_range reaches the mob's
// entity_interaction_range.
// vanilla: KineticWeapon.damageEntities, LivingEntity.getAttackRangeWith
func (m *Mob) stab(t *tickView, s *spearState, k *registry.KineticWeapon, used int) {
	if used < int(k.Delay) {
		return
	}
	used -= int(k.Delay)
	lx, ly, lz := LookVector(m.HeadYaw, m.Pitch)
	// vanilla: KineticWeapon.getMotion, Entity.getKnownSpeed
	v := m.RootVehicle().motion.speed
	ax, ay, az := LookVector(m.Yaw, m.Pitch)
	speed := (ax*v[0] + ay*v[1] + az*v[2]) * 20
	reach := rules.AttackRangeWith(m.MainHand, m.attrOr(registry.AttributeEntityInteractionRange,
		registry.AttributeEntityInteractionRange.Attribute().Default))
	ex, ey, ez := m.X, m.Y+m.eyeHeight(), m.Z
	ray := PiercingRay{
		EyeX: ex, EyeY: ey, EyeZ: ez, LookX: lx, LookY: ly, LookZ: lz,
		MinReach: float64(reach.MinReach * reach.MobFactor), MaxReach: float64(reach.MaxReach * reach.MobFactor),
		Margin: float64(reach.HitboxMargin), Forward: max(0, lx*m.VX+ly*m.VY+lz*m.VZ),
	}
	type body struct {
		id         int32
		vx, vy, vz float64
		box        AABB
	}
	var bodies []body
	var boxes []AABB
	// vanilla: ProjectileUtil.getHitEntitiesAlong searches the ray's box inflated
	// by 1; a body further from the eye than the ray can reach is left out.
	near := ray.MaxReach + ray.Forward + ray.Margin + 1
	within := func(b AABB) bool {
		dx := max(b.minX-ex, 0, ex-b.maxX)
		dy := max(b.minY-ey, 0, ey-b.maxY)
		dz := max(b.minZ-ez, 0, ez-b.maxZ)
		return dx*dx+dy*dy+dz*dz <= near*near
	}
	for i := range t.players {
		p := &t.players[i]
		if p.Dead || p.Spectator || !p.Huntable {
			continue
		}
		pw, ph, _ := p.box()
		box := NewAABB(p.X-pw/2, p.Y, p.Z-pw/2, p.X+pw/2, p.Y+ph, p.Z+pw/2)
		if within(box) {
			bodies = append(bodies, body{p.EntityID, p.KnownVX, p.KnownVY, p.KnownVZ, box})
		}
	}
	for _, o := range t.others {
		// vanilla: PiercingWeapon.canHitEntity
		if !o.Alive() || o.Invulnerable || o.RootVehicle() == m.RootVehicle() {
			continue
		}
		if box := NewAABB(o.BlastBox()); within(box) {
			v := o.KnownSpeed()
			bodies = append(bodies, body{o.ID, v[0], v[1], v[2], box})
		}
	}
	for _, b := range bodies {
		boxes = append(boxes, b.box)
	}
	for _, i := range PiercingHits(t.w, ray, boxes) {
		b := bodies[i]
		if at, ok := s.stabbed[b.id]; ok && m.Age-at < int(k.ContactCooldown) {
			continue
		}
		if s.stabbed == nil {
			s.stabbed = map[int32]int{}
		}
		s.stabbed[b.id] = m.Age
		// vanilla: KineticWeapon.damageEntities, the mob's base attack damage
		stab, damage, ok := KineticContact(k, used, spearMobFactor, speed, (ax*b.vx+ay*b.vy+az*b.vz)*20, m.kind().attackDamage)
		if !ok {
			continue
		}
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: damage,
			Player: Player{EntityID: b.id}, Stab: stab})
	}
}
