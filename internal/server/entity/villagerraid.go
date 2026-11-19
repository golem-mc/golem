package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// villagerRaid holds unsaved bell and hiding memories. The raid itself is
// snapshotted in raidWalk.view; no raid lock is taken during a mob step.
type villagerRaid struct {
	heardBell *int64
	hiding    *Waypoint
	hidden    int
}

// HearBell remembers a bell within hearing distance, including for NoAI mobs.
// vanilla: BellBlockEntity.updateEntities
func (m *Mob) HearBell(x, y, z int32, gameTime int64) {
	if m.Type == TypeVillager && m.Alive() && !m.gone && m.nearCenter(Waypoint{x, y, z}, 32) {
		m.vb.raid.heardBell = &gameTime
	}
}

// setVillagerActivity drops the old activity's walk and interactions immediately,
// including on ticks between goal selections.
func (m *Mob) setVillagerActivity(t *tickView, act villagerActivity) {
	if m.vb.activity == act {
		return
	}
	for _, g := range append([]Goal(nil), m.ai.running...) {
		if g.Flags()&(GoalMove|GoalLook) != 0 && g.Priority() > priorityFloat {
			g.Stop(t)
			m.ai.retire(g)
		}
	}
	m.stopSeeking()
	m.vb.look = villagerLookTarget{}
	m.vb.activity = act
	if act != actRest {
		m.WakeSleeper()
	}
}

// villagerRaidStep overrides the schedule on bells and periodically on raids.
// vanilla: ReactToBell, SetRaidStatus, ResetRaidStatus, SetHiddenState
func (m *Mob) villagerRaidStep(t *tickView) {
	r, v := &m.vb.raid, m.raidWalk.view
	if r.heardBell != nil && !v.Has {
		m.setVillagerActivity(t, actHide)
	}
	if (v.Has || m.vb.activity == actPreRaid || m.vb.activity == actRaid) && m.Rand.Intn(20) == 0 {
		if v.Has && !v.Loss {
			act := actRaid
			if v.Preparing {
				act = actPreRaid
			}
			m.setVillagerActivity(t, act)
		} else if m.vb.activity == actPreRaid || m.vb.activity == actRaid {
			m.resumeVillagerSchedule(t)
		}
	}
	if m.vb.activity == actHide && r.hiding != nil && r.heardBell != nil {
		if r.hidden > 15*20 || *r.heardBell+300 <= m.GameTime {
			r.heardBell, r.hiding, r.hidden = nil, nil, 0
			m.resumeVillagerSchedule(t)
		} else if Dist2(float64(r.hiding.X), float64(r.hiding.Y), float64(r.hiding.Z),
			float64(m.cell().X), float64(m.cell().Y), float64(m.cell().Z)) < 9 {
			r.hidden++
		}
	}
}

func (m *Mob) resumeVillagerSchedule(t *tickView) {
	m.setVillagerActivity(t, actIdle)
	m.vb.lastSchedule = m.GameTime - 21
	m.updateActivity()
}

// villagerRaidGoal holds the legs while gathering, hiding or celebrating.
// vanilla: VillagerGoalPackages.getPreRaidPackage/getRaidPackage/getHidePackage
type villagerRaidGoal struct {
	m              *Mob
	walk           poiWalkGoal
	celebrateUntil int64
}

func (g *villagerRaidGoal) Priority() int                   { return priorityPanic }
func (g *villagerRaidGoal) Flags() GoalFlags                { return GoalMove }
func (g *villagerRaidGoal) RequiresEveryTick() bool         { return true }
func (g *villagerRaidGoal) CanStart(*tickView) bool         { return g.m.vb.activity >= actPreRaid }
func (g *villagerRaidGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *villagerRaidGoal) Start(*tickView) {
	g.walk = poiWalkGoal{m: g.m, slot: slotMeeting, act: actPreRaid, pace: 0.75, close: 2, far: 150, giveUp: 200}
}
func (g *villagerRaidGoal) Stop(*tickView) { g.m.stopSeeking(); g.celebrateUntil = 0 }
func (g *villagerRaidGoal) Tick(t *tickView) {
	m, v := g.m, g.m.raidWalk.view
	switch m.vb.activity {
	case actPreRaid:
		// vanilla: RingBell.BELL_RING_CHANCE
		if m.Rand.Float32() > 0.95 && m.memHere(m.vb.meetingPoint) {
			p := m.vb.meetingPoint.at()
			if Dist2(float64(p.X), float64(p.Y), float64(p.Z), float64(m.cell().X), float64(m.cell().Y), float64(m.cell().Z)) < 9 &&
				rules.POIKindOf(t.w.BlockAt(p.X, p.Y, p.Z)) == rules.POIMeeting {
				t.events = append(t.events, MobEvent{Mob: m, Kind: MobRangBell, X: float64(p.X), Y: float64(p.Y), Z: float64(p.Z)})
			}
		}
		if len(m.navOf().nav.path) != 0 {
			g.walk.Tick(t)
			return
		}
		for _, c := range weightedChoices(m, 6, 2) {
			if c.kind == 0 {
				if g.walk.CanStart(t) {
					g.walk.Start(t)
					return
				}
			} else if p, ok := m.villageBoundPos(t, 10, 7); ok {
				m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, 0.75, 0)
				return
			}
		}
	case actHide:
		g.hide(t, 32, 0.625, 2)
	case actRaid:
		if v.Active && !v.Over {
			g.hide(t, 24, 0.7, 1)
		}
		if v.Over && !v.Loss {
			g.celebrate(t)
		}
	}
}

// hide uses any bed POI, regardless of its owner or occupancy; it does not
// reserve a home or sleep in it. vanilla: LocateHidingPlace
func (g *villagerRaidGoal) hide(t *tickView, radius int, pace, close float64) {
	m := g.m
	if m.Village == nil || len(m.navOf().nav.path) != 0 {
		return
	}
	c := m.cell()
	pois := m.Village.POIs(isHome, c.X, c.Y, c.Z, int(close)+1)
	var p Waypoint
	if len(pois) != 0 && m.nearCenter(pois[0], close) {
		p = pois[0]
	} else {
		pois = m.Village.POIs(isHome, c.X, c.Y, c.Z, radius)
		if len(pois) != 0 {
			p = pois[m.Rand.Intn(len(pois))]
		} else if m.memHere(m.vb.home) {
			p = m.vb.home.at()
		} else {
			return
		}
	}
	m.vb.raid.hiding = &p
	m.vb.look = villagerLookTarget{}
	// vanilla: MoveToTargetSink.reachedTarget checks the block Manhattan distance
	// before planning; adjacent hiders must not rebuild an empty path each tick.
	if !m.nearCenter(p, close) && manhattan3(p, c) > int32(close) {
		m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, pace, int32(close))
	}
}

// raidSkySpot also requires the column's motion-blocking surface below the
// villager's feet. vanilla: MoveToSkySeeingSpot.hasNoBlocksAbove
func (m *Mob) raidSkySpot(w World, p Waypoint) bool {
	return canSeeSkyAt(w, p.X, p.Y, p.Z) && float64(w.MotionBlockingHeight(p.X, p.Z)+1) <= m.Y
}

// celebrate runs for 600 ticks and launches a single-colour burst rocket.
// vanilla: CelebrateVillagersSurvivedRaid, MoveToSkySeeingSpot
func (g *villagerRaidGoal) celebrate(t *tickView) {
	m := g.m
	if g.celebrateUntil == 0 && m.raidSkySpot(t.w, m.cell()) {
		g.celebrateUntil = m.GameTime + 600
	}
	if g.celebrateUntil != 0 {
		if m.GameTime > g.celebrateUntil {
			m.resumeVillagerSchedule(t)
			return
		}
		if m.Rand.Intn(100) == 0 {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityVillagerCelebrate, Volume: 1, Pitch: m.VoicePitch()})
		}
		if m.Rand.Intn(200) == 0 && m.raidSkySpot(t.w, m.cell()) {
			colour := registry.FireworkDyeColour(m.Rand.Intn(len(registry.DyeNames)))
			stack := protocol.ItemStack{ItemID: int32(registry.ItemFireworkRocket), Count: 1}
			stack.SetRaw(protocol.FireworksRawComponent(int32(m.Rand.Intn(3)), []protocol.FireworkExplosion{{Shape: 4, Colors: []int32{colour}}}))
			x, y, z := m.EyePosition()
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobCelebrated, Stack: stack, X: x, Y: y, Z: z})
		}
	}
	if len(m.navOf().nav.path) != 0 {
		return
	}
	for _, c := range weightedChoices(m, 5, 2) {
		if c.kind == 0 && !m.seesSkyAbove(t.w) {
			pos := m.cell()
			for range 10 {
				p := Waypoint{pos.X + int32(m.Rand.Intn(20)) - 10, pos.Y + int32(m.Rand.Intn(6)) - 3, pos.Z + int32(m.Rand.Intn(20)) - 10}
				if m.raidSkySpot(t.w, p) {
					m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, 0.5, 0)
					return
				}
			}
		} else if c.kind == 1 {
			if p, ok := m.villageBoundPos(t, 10, 7); ok {
				m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, 0.55, 0)
				return
			}
		}
	}
}

// VillagerActivity is the villager's current schedule or emergency activity.
func (m *Mob) VillagerActivity() string {
	return [...]string{"idle", "work", "meet", "rest", "panic", "play", "pre_raid", "raid", "hide"}[m.vb.activity]
}
