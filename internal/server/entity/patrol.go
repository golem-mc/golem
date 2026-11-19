package entity

import "math"

// findPatrolTarget chooses a distant block, keeping the patrol active.
// vanilla: PatrollingMonster.findPatrolTarget
func (m *Mob) findPatrolTarget() {
	at := m.cell()
	at.X += int32(m.Rand.Intn(1000) - 500)
	at.Z += int32(m.Rand.Intn(1000) - 500)
	m.PatrolTarget, m.Patrolling = &at, true
}

// patrolGoal walks ten blocks at a time along a curved long-distance route.
// vanilla: PatrollingMonster.LongDistancePatrolGoal
type patrolGoal struct {
	m             *Mob
	cooldownUntil int
}

func (g *patrolGoal) Priority() int           { return 4 }
func (g *patrolGoal) Flags() GoalFlags        { return GoalMove }
func (g *patrolGoal) RequiresEveryTick() bool { return false }
func (g *patrolGoal) Start(*tickView)         {}
func (g *patrolGoal) Stop(*tickView)          {}
func (g *patrolGoal) CanStart(*tickView) bool {
	m := g.m
	return m.Patrolling && m.TargetID == 0 && len(m.Passengers) == 0 && m.PatrolTarget != nil && m.Age >= g.cooldownUntil
}
func (g *patrolGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *patrolGoal) Tick(t *tickView) {
	m := g.m
	if !m.navDone() {
		return
	}
	pack := t.others
	var companions []*Mob
	if m.push != nil {
		pack = m.push.candidates(m, 16)
		companions = pack[:0]
	}
	for _, o := range pack {
		if o == m || !o.Alive() || !isRaiderType(o.Type) || o.raidWalk.view.Active {
			continue
		}
		// Bounding boxes intersect the caller's box inflated by sixteen.
		if math.Abs(o.X-m.X) < 16+(m.width()+o.width())/2 &&
			math.Abs(o.Z-m.Z) < 16+(m.width()+o.width())/2 &&
			o.Y+o.height() > m.Y-16 && o.Y < m.Y+m.height()+16 {
			companions = append(companions, o)
		}
	}
	if len(companions) == 0 {
		m.Patrolling = false
		return
	}
	at := m.PatrolTarget
	if m.PatrolLeader && Dist2(m.X, m.Y, m.Z, float64(at.X)+0.5, float64(at.Y)+0.5, float64(at.Z)+0.5) < 100 {
		m.findPatrolTarget()
		return
	}
	tx, ty, tz := float64(at.X)+0.5, float64(at.Y), float64(at.Z)+0.5
	dx, dy, dz := m.X-tx, m.Y-ty, m.Z-tz
	// vanilla: Vec3.yRot consumes the literal 90.0f as radians.
	sin, cos := math.Sin(90), math.Cos(90)
	tx += (dx*cos + dz*sin) * 0.4
	ty += dy * 0.4
	tz += (dz*cos - dx*sin) * 0.4
	dx, dy, dz = tx-m.X, ty-m.Y, tz-m.Z
	length := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if length >= 1e-4 {
		dx, dz = dx/length*10, dz/length*10
	} else {
		dx, dz = 0, 0
	}
	pos := Waypoint{X: int32(math.Floor(m.X + dx)), Z: int32(math.Floor(m.Z + dz))}
	pos.Y = t.w.NoLeavesHeight(pos.X, pos.Z) + 1
	pace := 0.7
	if m.PatrolLeader {
		pace = 0.595
	}
	if !m.seekTo(t.w, float64(pos.X), float64(pos.Y), float64(pos.Z), pace, navReach) {
		random := m.cell()
		random.X += int32(m.Rand.Intn(16) - 8)
		random.Z += int32(m.Rand.Intn(16) - 8)
		random.Y = t.w.NoLeavesHeight(random.X, random.Z) + 1
		m.seekTo(t.w, float64(random.X), float64(random.Y), float64(random.Z), 0.7, navReach)
		g.cooldownUntil = m.Age + 200
	} else if m.PatrolLeader {
		for _, o := range companions {
			target := pos
			o.PatrolTarget, o.Patrolling = &target, true
		}
	}
}
