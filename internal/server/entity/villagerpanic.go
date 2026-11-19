package entity

// The hostile sensor's radii. vanilla: VillagerHostilesSensor
func villagerHostileRange(typ MobType) float64 {
	switch typ {
	case TypeDrowned, TypeHusk, TypeVex, TypeZombie, TypeZombieVillager:
		return 8
	case TypeVindicator, TypeZoglin:
		return 10
	case TypeEvoker, TypeRavager, TypeIllusioner:
		return 12
	case TypePillager:
		return 15
	default:
		return 0
	}
}

// villagerMaxHostileRange is the largest villagerHostileRange (the pillager's).
const villagerMaxHostileRange = 15

// villagerGridMinHerd is the herd size above which the grid query beats a full
// scan (measured with BenchmarkMobsTickVillage).
const villagerGridMinHerd = 64

// villagerDanger holds unsaved sensor memories; an attacker survives the
// damage-source expiry until it dies, leaves the dimension or is far away.
// vanilla: HurtBySensor, VillagerHostilesSensor
type villagerDanger struct {
	scan     int
	hostile  *Mob
	hurt     bool
	attacker int32
}

const (
	villagerDangerScan = 20 // vanilla: Sensor.DEFAULT_SCAN_RATE
	villagerSafeDistSq = 36 // vanilla: VillagerCalmDown
)

// villagerDangerStep selects PANIC before the schedule and wakes sleepers.
// vanilla: VillagerPanicTrigger, VillagerCalmDown
func (m *Mob) villagerDangerStep(t *tickView) {
	d := &m.vb.danger
	if d.scan--; d.scan <= 0 {
		d.scan = villagerDangerScan
		d.hostile = nil
		best := m.followRange() * m.followRange()
		scan := t.others
		if m.push != nil && len(t.others) > villagerGridMinHerd {
			// A superset of the mobs within villagerMaxHostileRange, in snapshot
			// order. The scratch is valid until the next query, so nothing in the
			// loop may call candidates.
			scan = m.push.candidates(m, villagerMaxHostileRange)
		}
		for _, o := range scan {
			r := villagerHostileRange(o.Type)
			if r == 0 || !o.Alive() || o.gone {
				continue
			}
			dist := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z)
			visibility := m.visibilityRange(&Player{Invisible: o.hasEffect(mobEffectInvisibility), Worn: o.Worn})
			if dist <= r*r && dist <= best && dist <= visibility*visibility && m.sees(t, o) {
				d.hostile, best = o, dist
			}
		}
		d.hurt = m.lastDamageSource() != 0
		if d.hurt && m.blowAttacker != 0 {
			d.attacker = m.blowAttacker
		}
		if _, ok := m.villagerAttacker(t); !ok {
			d.attacker = 0
		}
	}
	if d.hostile != nil && (!d.hostile.Alive() || d.hostile.gone) {
		d.hostile = nil
	}
	if d.hurt || d.hostile != nil {
		m.setVillagerActivity(t, actPanic)
		return
	}
	if m.vb.activity == actPanic {
		if a, ok := m.villagerAttacker(t); ok && Dist2(m.X, m.Y, m.Z, a.X, a.Y, a.Z) <= villagerSafeDistSq {
			return
		}
		d.attacker = 0
		m.vb.activity = actIdle
		m.vb.lastSchedule = m.GameTime - 21
		m.updateActivity()
	}
}

// villagerAttacker resolves the remembered entity in this tick's dimension.
func (m *Mob) villagerAttacker(t *tickView) (Player, bool) {
	id := m.vb.danger.attacker
	if id != 0 {
		if p := t.playerByID(id); p != nil && !p.Dead {
			return *p, true
		}
		if o := t.mobByID(id); o != nil && o.Alive() && !o.gone {
			return o.asTarget(), true
		}
	}
	return Player{}, false
}

// villagerPanicGoal holds the legs throughout PANIC, so normal activities
// cannot start when a flee has no reachable destination.
// vanilla: VillagerGoalPackages.getPanicPackage, SetWalkTargetAwayFrom
type villagerPanicGoal struct{ m *Mob }

func (g *villagerPanicGoal) Priority() int                   { return priorityPanic }
func (g *villagerPanicGoal) Flags() GoalFlags                { return GoalMove }
func (g *villagerPanicGoal) RequiresEveryTick() bool         { return true }
func (g *villagerPanicGoal) CanStart(*tickView) bool         { return g.m.vb.activity == actPanic }
func (g *villagerPanicGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *villagerPanicGoal) Start(*tickView)                 {}
func (g *villagerPanicGoal) Stop(*tickView)                  { g.m.stopSeeking() }
func (g *villagerPanicGoal) Tick(t *tickView) {
	m := g.m
	if m.vb.activity != actPanic || len(m.navOf().nav.path) != 0 {
		return // interruptCurrentWalk=false
	}
	flee := func(x, y, z float64) bool {
		if Dist2(m.X, m.Y, m.Z, x, y, z) >= villagerSafeDistSq {
			return false
		}
		for range 10 {
			if p, ok := landRandomPosAway(t.w, m, x, z, 16, 7); ok {
				m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, villagerPanicPace, 0)
				return true
			}
		}
		return false
	}
	if h := m.vb.danger.hostile; h != nil && flee(h.X, h.Y, h.Z) {
		return
	}
	if a, ok := m.villagerAttacker(t); ok && flee(a.X, a.Y, a.Z) {
		return
	}
	if p, ok := m.villageBoundPos(t, 2, 2); ok {
		m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, villagerPanicPace, 0)
	}
}
