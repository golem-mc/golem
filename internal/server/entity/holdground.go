package entity

// holdGroundRadius2 is the squared radius inside which a held patrol turns
// aggressive; holdGroundShout is the reach of its call to nearby raiders.
// vanilla: Raider.HoldGroundAttackGoal
const (
	holdGroundRadius2 = 10.0 * 10.0
	holdGroundShout   = 8.0
)

// holdGroundGoal keeps a patrolling illager still, facing a target beyond ten
// blocks, until the target closes in or the goal ends; then it and the raiders
// around it turn on the target.
// vanilla: Raider.HoldGroundAttackGoal
type holdGroundGoal struct {
	m        *Mob
	priority int
}

func (g *holdGroundGoal) Priority() int           { return g.priority }
func (g *holdGroundGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *holdGroundGoal) RequiresEveryTick() bool { return true }

func (g *holdGroundGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.InRaid || !m.Patrolling || m.TargetID == 0 || m.Aggressive {
		return false
	}
	if id := m.LastAttackerID(); id != 0 {
		for i := range t.players {
			if t.players[i].EntityID == id {
				return false
			}
		}
	}
	return true
}

func (g *holdGroundGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

// shout calls fn on every other raider within the shout reach.
// vanilla: Raider.HoldGroundAttackGoal.shoutTargeting
func (g *holdGroundGoal) shout(t *tickView, fn func(*Mob)) {
	m := g.m
	for _, o := range t.others {
		if o == m || !o.Alive() || !isRaiderType(o.Type) ||
			Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) > holdGroundShout*holdGroundShout ||
			!boxesOverlap(m.X, m.Y-holdGroundShout, m.Z, m.width()+2*holdGroundShout, m.height()+2*holdGroundShout,
				o.X, o.Y, o.Z, o.width(), o.height()) {
			continue
		}
		fn(o)
	}
}

func (g *holdGroundGoal) Start(t *tickView) {
	g.m.stopSeeking()
	id := g.m.TargetID
	g.shout(t, func(o *Mob) { o.SetMetadataTargetID(id) })
}

func (g *holdGroundGoal) Stop(t *tickView) {
	id := g.m.TargetID
	if id == 0 {
		return
	}
	g.shout(t, func(o *Mob) {
		o.SetMetadataTargetID(id)
		o.SetMetadataAggressive(true)
	})
	g.m.SetMetadataAggressive(true)
}

func (g *holdGroundGoal) Tick(t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	if Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) > holdGroundRadius2 {
		dx, dz := p.X-m.X, p.Z-m.Z
		m.HeadYaw = float32(yawToward(dx, dz))
		m.Pitch = float32(pitchToward(dx, p.Y+p.eyeHeight()-(m.Y+m.eyeHeight()), dz))
		if m.Rand.Intn(50) == 0 && !m.Silent {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobMoaned})
		}
	} else {
		m.SetMetadataAggressive(true)
	}
}
