package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// ironGolemGoals wires defense, anger, melee, village movement and flower offers.
// vanilla: IronGolem.registerGoals
func ironGolemGoals(m *Mob) []Goal {
	return []Goal{
		&defendVillageGoal{m: m},
		&meleeAttackGoal{m: m, priority: 1, followUnseen: true, modifySwing: m.golemBlow,
			swingStatus: protocol.EntityStatusIronGolemAttack, swingSound: registry.SoundEntityIronGolemAttack},
		&golemApproachGoal{m: m, priority: 2, pace: 0.9},
		&randomStroll{m: m, priority: 2, interval: 10, pace: 0.6, ignoreIdle: true,
			blocked: func() bool { return m.villageDistanceAt(m.cell(), 1) <= 1 }, pick: m.golemReturnPos},
		&hurtByGoal{m: m, priority: 2, accept: m.golemCanAttack},
		&nearestTargetGoal{m: m, priority: 3, recheck: true, accept: func(t *tickView, p *Player) bool {
			return m.golemCanAttack(t, p) && m.angryAt(t.w, p)
		}},
		&mobTargetGoal{m: m, priority: 3, interval: 5, unsighted: true, matches: func(o *Mob) bool {
			return Enemy(o.Type) && o.Type != TypeCreeper && (m.Team == "" || m.Team != o.Team)
		}, area: func(o *Mob) bool {
			r := m.visibilityRange(&Player{Invisible: o.hasEffect(mobEffectInvisibility), Worn: o.Worn})
			return Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) <= r*r
		}},
		&resetAngerGoal{m: m, priority: 4},
		&randomStroll{m: m, priority: 4, interval: 240, pace: 0.6, ignoreIdle: true, pick: m.golemStrollPos},
		&offerFlowerGoal{m: m},
		&lookAtPlayer{m: m, priority: 7, reach: 6},
		&randomLookAround{m: m, priority: 8},
	}
}

// golemCanAttack is IronGolem.canAttack, including LivingEntity's team check.
func (m *Mob) golemCanAttack(t *tickView, p *Player) bool {
	if m.alliedTo(p) {
		return false
	}
	if t.playerByID(p.EntityID) != nil {
		return !m.PlayerCreated
	}
	o := t.mobByID(p.EntityID)
	return o != nil && o.Type != TypeCreeper
}

// golemBlow rolls IronGolem.doHurtTarget's half-base plus a bounded integer.
func (m *Mob) golemBlow(ev *MobEvent) {
	damage := float32(ev.Damage)
	if int(damage) > 0 {
		damage = damage/2 + float32(m.Rand.Intn(int(damage)))
	}
	ev.Damage = float64(damage)
}

// defendVillageGoal is DefendVillageTargetGoal: a visible villager's reputation
// of -100 or worse names a visible player in the golem's inflated search box.
type defendVillageGoal struct {
	m      *Mob
	target int32
}

func (g *defendVillageGoal) Priority() int           { return 1 }
func (g *defendVillageGoal) Flags() GoalFlags        { return GoalTarget }
func (g *defendVillageGoal) RequiresEveryTick() bool { return false }

func (g *defendVillageGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.PlayerCreated {
		return false
	}
	// potentialTarget survives stop and reputation changes.
	// vanilla: DefendVillageTargetGoal.canUse, TargetGoal.stop
	remembered := t.playerByID(g.target)
	canStart := remembered != nil && t.canAttackPlayer(remembered)
	players := make([]*Player, 0, len(t.players))
	for i := range t.players {
		p := &t.players[i]
		if t.canAttackPlayer(p) && !m.alliedTo(p) && g.playerInBox(p) &&
			m.distanceToPlayer(p) <= math.Max(64*visibilityPercent(m, p), minVisibilityDistance) &&
			m.seesTarget(t.w, p) {
			players = append(players, p)
		}
	}
	if len(players) == 0 {
		return canStart
	}
	for _, v := range t.others {
		if v.Type != TypeVillager || !v.Alive() {
			continue
		}
		p := v.asTarget()
		p.Invisible, p.Worn = v.hasEffect(mobEffectInvisibility), v.Worn
		if !p.Huntable || m.alliedTo(&p) ||
			!g.inBox(v.X, v.Y, v.Z, v.width(), v.height()) ||
			m.distanceToPlayer(&p) > math.Max(64*visibilityPercent(m, &p), minVisibilityDistance) {
			continue
		}
		var target int32
		for _, p := range players {
			if v.PlayerReputation(p.UUID) <= -100 {
				target = p.EntityID
			}
		}
		if target != 0 && rayClear(t.w, m.X, m.Y+m.eyeHeight(), m.Z, v.X, v.Y+v.eyeHeight(), v.Z) {
			g.target = target
			canStart = true
		}
	}
	return canStart
}

func (g *defendVillageGoal) inBox(x, y, z, width, height float64) bool {
	m := g.m
	return boxesOverlap(m.X, m.Y-8, m.Z, m.width()+20, m.height()+16, x, y, z, width, height)
}

// playerInBox tests the feet, rather than the body bounds.
// vanilla: ServerEntityGetter.getNearbyPlayers
func (g *defendVillageGoal) playerInBox(p *Player) bool {
	m := g.m
	half := m.width()/2 + 10
	return p.X >= m.X-half && p.X < m.X+half &&
		p.Z >= m.Z-half && p.Z < m.Z+half &&
		p.Y >= m.Y-8 && p.Y < m.Y+m.height()+8
}

func (g *defendVillageGoal) Start(*tickView) { g.m.setTargetBy(g.target, TargetByDefend) }
func (g *defendVillageGoal) Tick(*tickView)  {}
func (g *defendVillageGoal) ShouldContinue(t *tickView) bool {
	p := g.m.targetIn(t)
	return p != nil && g.m.golemCanAttack(t, p) && g.m.inFollowRange(p)
}
func (g *defendVillageGoal) Stop(*tickView) { g.m.SetMetadataTargetID(0) }

// golemApproachGoal is the fallback when melee cannot find a path to the target.
// vanilla: MoveTowardsTargetGoal
type golemApproachGoal struct {
	randomStroll
	targetID int32
}

func (g *golemApproachGoal) CanStart(t *tickView) bool {
	p := g.m.targetIn(t)
	if p == nil || g.m.distanceToPlayer(p) > 32 {
		return false
	}
	var ok bool
	g.target, ok = g.m.golemTowards(t, p.X, p.Z, 16, false)
	g.targetID = p.EntityID
	return ok
}
func (g *golemApproachGoal) ShouldContinue(t *tickView) bool {
	p := t.playerByID(g.targetID)
	if p == nil {
		if m := t.mobByID(g.targetID); m != nil && m.Alive() {
			target := m.asTarget()
			p = &target
		}
	}
	return p != nil && p.Huntable && g.m.distanceToPlayer(p) < 32 && g.randomStroll.ShouldContinue(t)
}
func (g *golemApproachGoal) Stop(*tickView) { g.targetID = 0 }

func (m *Mob) villageDistanceAt(p Waypoint, limit int) int {
	if m.Village == nil {
		return limit + 1
	}
	return m.Village.SectionsToVillage(p.X, p.Y, p.Z, limit)
}

func (m *Mob) golemTowards(t *tickView, x, z float64, spread int, land bool) (Waypoint, bool) {
	// The shared search points away from its argument.
	return directedRandomPos(t.w, m, 2*m.X-x, 2*m.Z-z, spread, 7, land)
}

// vanilla: MoveBackToVillageGoal.getPosition, BehaviorUtils.findSectionClosestToVillage
func (m *Mob) golemReturnPos(t *tickView) (Waypoint, bool) {
	if m.Village == nil {
		return Waypoint{}, false
	}
	from := sectionOf(m.cell())
	target, best := from, m.villageDistanceAt(m.cell(), 6)
	for z := from.Z - 2; z <= from.Z+2; z++ {
		for y := from.Y - 2; y <= from.Y+2; y++ {
			for x := from.X - 2; x <= from.X+2; x++ {
				p := Waypoint{x*16 + 8, y*16 + 8, z*16 + 8}
				if d := m.villageDistanceAt(p, 6); d < best {
					best, target = d, Waypoint{x, y, z}
				}
			}
		}
	}
	if target == from {
		return Waypoint{}, false
	}
	return m.golemTowards(t, float64(target.X*16+8)+0.5, float64(target.Z*16+8)+0.5, 10, false)
}

// vanilla: GolemRandomStrollInVillageGoal.getPosition
func (m *Mob) golemStrollPos(t *tickView) (Waypoint, bool) {
	if m.Rand.Float32() < 0.3 {
		return landRandomPos(t.w, m, 10, 7)
	}
	picks := []func(*tickView) (Waypoint, bool){m.golemVillagerPos, m.golemPOIPos}
	if m.Rand.Float32() >= 0.7 {
		picks[0], picks[1] = picks[1], picks[0]
	}
	for _, pick := range picks {
		if p, ok := pick(t); ok {
			return p, true
		}
	}
	return landRandomPos(t.w, m, 10, 7)
}

func (m *Mob) golemVillagerPos(t *tickView) (Waypoint, bool) {
	var villagers []*Mob
	for _, o := range t.others {
		if o.Type == TypeVillager && o.Alive() && m.NearBox(o, 32) && o.WantsGolem() {
			villagers = append(villagers, o)
		}
	}
	if len(villagers) == 0 {
		return Waypoint{}, false
	}
	v := villagers[m.Rand.Intn(len(villagers))]
	return m.golemTowards(t, v.X, v.Z, 10, true)
}

func (m *Mob) golemPOIPos(t *tickView) (Waypoint, bool) {
	if m.Village == nil {
		return Waypoint{}, false
	}
	from := sectionOf(m.cell())
	var sections []Waypoint
	for z := from.Z - 2; z <= from.Z+2; z++ {
		for y := from.Y - 2; y <= from.Y+2; y++ {
			for x := from.X - 2; x <= from.X+2; x++ {
				p := Waypoint{x*16 + 8, y*16 + 8, z*16 + 8}
				if m.villageDistanceAt(p, 0) == 0 {
					sections = append(sections, p)
				}
			}
		}
	}
	if len(sections) == 0 {
		return Waypoint{}, false
	}
	center := sections[m.Rand.Intn(len(sections))]
	var pois []Waypoint
	for _, o := range t.others {
		for p := range o.VillageClaims(m.Village.Dimension()) {
			if Dist2(float64(p.X), float64(p.Y), float64(p.Z), float64(center.X), float64(center.Y), float64(center.Z)) <= 64 && !slices.Contains(pois, p) {
				pois = append(pois, p)
			}
		}
	}
	if len(pois) == 0 {
		return Waypoint{}, false
	}
	p := pois[m.Rand.Intn(len(pois))]
	return m.golemTowards(t, float64(p.X)+0.5, float64(p.Z)+0.5, 10, true)
}

// ForceGolemFlowerOffer forces the next eligible flower roll for a test.
func (m *Mob) ForceGolemFlowerOffer() { m.forceFlowerOffer = true }

// offerFlowerGoal gives a candidate in sight a poppy. vanilla: OfferFlowerGoal
type offerFlowerGoal struct {
	m      *Mob
	target *Mob
	left   int
}

func (g *offerFlowerGoal) Priority() int           { return 5 }
func (g *offerFlowerGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *offerFlowerGoal) RequiresEveryTick() bool { return false }
func (g *offerFlowerGoal) CanStart(t *tickView) bool {
	m := g.m
	if !t.w.BrightOutside() {
		return false
	}
	forced := m.forceFlowerOffer
	m.forceFlowerOffer = false
	if !forced && m.Rand.Intn(8000) != 0 {
		return false
	}
	best := math.Inf(1)
	g.target = nil
	for _, o := range t.others {
		if !o.inTag(registry.EntityTypeTagIDCandidateForIronGolemGift) || !o.Alive() || !boxesOverlap(m.X, m.Y-2, m.Z, m.width()+12, m.height()+4, o.X, o.Y, o.Z, o.width(), o.height()) {
			continue
		}
		p := o.asTarget()
		p.Invisible, p.Worn = o.hasEffect(mobEffectInvisibility), o.Worn
		reach := math.Max(6*visibilityPercent(m, &p), minVisibilityDistance)
		d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z)
		if d <= reach*reach && d < best && rayClear(t.w, m.X, m.Y+m.eyeHeight(), m.Z, o.X, o.Y+o.eyeHeight(), o.Z) {
			g.target, best = o, d
		}
	}
	return g.target != nil
}
func (g *offerFlowerGoal) Start(t *tickView) {
	g.left = reducedTickDelay(400)
	g.m.OfferFlowerTicks = 400
	t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobStatus, Status: protocol.EntityStatusIronGolemOfferFlower})
}
func (g *offerFlowerGoal) ShouldContinue(*tickView) bool { return g.left > 0 }
func (g *offerFlowerGoal) Tick(*tickView) {
	if g.target != nil {
		g.m.lookAtPoint(g.target.X, g.target.Y+g.target.eyeHeight(), g.target.Z)
	}
	g.left--
}
func (g *offerFlowerGoal) Stop(t *tickView) {
	m, target := g.m, g.target
	m.OfferFlowerTicks = 0
	g.target = nil
	// vanilla: OfferFlowerGoal.stop
	if g.left == 0 && target != nil && target.Alive() && target.inTag(registry.EntityTypeTagIDAcceptsIronGolemGift) &&
		target.Antenna.Empty() &&
		boxesOverlap(m.X, m.Y-2, m.Z, m.width()+12, m.height()+4, target.X, target.Y, target.Z, target.width(), target.height()) {
		target.SetEquipment(protocol.EquipSaddle, protocol.ItemStack{ItemID: int32(registry.ItemPoppy), Count: 1})
		target.SaddleDropChance = guaranteedDropChance
	}
	t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobStatus, Status: protocol.EntityStatusIronGolemStopFlower})
}
