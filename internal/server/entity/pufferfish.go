package entity

import (
	"github.com/golem-mc/golem/registry"
)

// The pufferfish: a fish that puffs up near anything scary and stings what it
// touches while puffed.
// vanilla: Pufferfish

const TypePufferfish = registry.EntityTypePufferfish

func init() {
	// vanilla: EntityTypes.PUFFERFISH; the 0.7 speed is Mob.createMobAttributes'.
	kinds[TypePufferfish] = &kind{
		name: "Pufferfish", width: 0.7, height: 0.7, eyeHeight: 0.455,
		maxHealth: 3, speed: 0.7, water: true,
		sounds: MobSounds{
			Hurt:  registry.SoundEntityPufferFishHurt,
			Death: registry.SoundEntityPufferFishDeath,
		},
		ambientInterval: 120,
	}
	mobTracking[TypePufferfish] = tracking{rangeChunks: 4, updateInterval: 3}
}

// Puff states, Pufferfish.STATE_SMALL, STATE_MID and STATE_FULL.
const (
	PuffSmall = 0
	PuffMid   = 1
	PuffFull  = 2
)

// The puff timers, in ticks (Pufferfish.tick), and the boxes the puff goal
// and the sting scan.
const (
	puffFullAfter   = 40  // inflateCounter past this takes state 1 to 2
	puffDeflateFull = 60  // deflateTimer past this takes state 2 to 1
	puffDeflateMid  = 100 // deflateTimer past this takes state 1 to 0
	puffScanGrow    = 2.0 // PufferfishPuffGoal.canUse
	puffStingGrow   = 0.3 // Pufferfish.aiStep
)

// puffScale is Pufferfish.getScale, by puff state.
var puffScale = [3]float64{0.5, 0.7, 1.0}

// pufferState is the puff a pufferfish keeps: the state and the goal's two
// counters.
type pufferState struct {
	state                   int
	inflateCounter, deflate int
}

// PuffState is the pufferfish's puff state, 0 for any other mob.
func (m *Mob) PuffState() int { return m.puffer.state }

// SetPuffState sets the saved puff state, clamped like Pufferfish.readAdditionalSaveData.
func (m *Mob) SetPuffState(s int) { m.SetMetadataPufferState(max(0, min(s, PuffFull))) }

// scaryTo is Pufferfish.SCARY_MOB for a mob: alive and of a type outside
// #not_scary_for_pufferfish.
func scaryTo(o *Mob) bool {
	return o.Alive() && !registry.EntityType(o.TypeID()).InTag(registry.EntityTypeTagIDNotScaryForPufferfish)
}

// maxShulkerShell is how far an open shulker's shell reaches past its cell:
// the largest scale (3) at a full peek.
// vanilla: Shulker.sanitizeScale, Shulker.getPhysicalPeek
const maxShulkerShell = 3

// scaryCandidates is the herd a reach grown by grow around the fish can meet.
func (m *Mob) scaryCandidates(t *tickView, grow float64) []*Mob {
	near := grow
	if m.push != nil && m.push.shellWide > 0 {
		near += maxShulkerShell
	}
	return m.herdNear(t, near)
}

// scaryIn reports whether o is a scary mob other than the fish whose box
// crosses reach and whose section the walk of window visits.
// vanilla: EntityGetter.getEntitiesOfClass, EntitySection.getEntities
func (m *Mob) scaryIn(window SectionWindow, reach AABB, o *Mob) bool {
	return o != m && window.holds(o) && o.mobBox().Crosses(reach) && scaryTo(o)
}

// NewSectionWindowOf is the sections an entity query for box reaches.
func NewSectionWindowOf(box AABB) SectionWindow {
	b := box.Bounds()
	return NewSectionWindow(b[0], b[3], b[1], b[4], b[2], b[5])
}

// scaryMobNear reports whether a scary mob is in the fish's box grown by grow.
func (m *Mob) scaryMobNear(t *tickView, grow float64) bool {
	reach := m.mobBox().Inflate(grow)
	window := NewSectionWindowOf(reach)
	for _, o := range m.scaryCandidates(t, grow) {
		if m.scaryIn(window, reach, o) {
			return true
		}
	}
	return false
}

// scaryPlayerNear reports whether a player that is neither creative nor a
// spectator is in the fish's box grown by grow. Invisibility and line of
// sight do not matter.
func (m *Mob) scaryPlayerNear(t *tickView, grow float64) bool {
	box := m.mobBox()
	for i := range t.players {
		p := &t.players[i]
		if p.Creative || p.Spectator {
			continue
		}
		pw, ph, _ := p.box()
		if p.X+pw/2 > box.minX-grow && p.X-pw/2 < box.maxX+grow &&
			p.Z+pw/2 > box.minZ-grow && p.Z-pw/2 < box.maxZ+grow &&
			p.Y+ph > box.minY-grow && p.Y < box.maxY+grow {
			return true
		}
	}
	return false
}

// setPuff changes the state with the blow sound a change makes.
func (m *Mob) setPuff(state int, sound registry.Sound, events []MobEvent) []MobEvent {
	m.SetMetadataPufferState(state)
	return append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound,
		Volume: m.SoundVolume(), Pitch: m.VoicePitch()})
}

// pufferStep is the pufferfish's tick and aiStep: the puff counters, then
// the stings.
// vanilla: Pufferfish.tick, Pufferfish.aiStep
func (m *Mob) pufferStep(t *tickView, events []MobEvent) []MobEvent {
	p := &m.puffer
	if p.inflateCounter > 0 {
		if p.state == PuffSmall {
			events = m.setPuff(PuffMid, registry.SoundEntityPufferFishBlowUp, events)
		} else if p.inflateCounter > puffFullAfter && p.state == PuffMid {
			events = m.setPuff(PuffFull, registry.SoundEntityPufferFishBlowUp, events)
		}
		p.inflateCounter++
	} else if p.state != PuffSmall {
		if p.deflate > puffDeflateFull && p.state == PuffFull {
			events = m.setPuff(PuffMid, registry.SoundEntityPufferFishBlowOut, events)
		} else if p.deflate > puffDeflateMid && p.state == PuffMid {
			events = m.setPuff(PuffSmall, registry.SoundEntityPufferFishBlowOut, events)
		}
		p.deflate++
	}
	return m.pufferStings(t, events)
}

// pufferStings is the sting on every scary mob in reach, which runs without
// AI; a player's touch is the server's (applyPufferTouches).
// vanilla: Pufferfish.aiStep
func (m *Mob) pufferStings(t *tickView, events []MobEvent) []MobEvent {
	p := &m.puffer
	if p.state == PuffSmall {
		return events
	}
	damage := float64(1 + p.state)
	reach := m.mobBox().Inflate(puffStingGrow)
	window := NewSectionWindowOf(reach)
	for _, o := range m.scaryCandidates(t, puffStingGrow) {
		if m.scaryIn(window, reach, o) {
			events = append(events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: damage,
				Player: o.asTarget(), Touch: true, Puff: p.state})
		}
	}
	return events
}

// pufferGoal is PufferfishPuffGoal: running while anything scary is within
// two blocks; a run restarts the inflation.
type pufferGoal struct{ m *Mob }

func (g *pufferGoal) Priority() int    { return 1 }
func (g *pufferGoal) Flags() GoalFlags { return 0 }

func (g *pufferGoal) CanStart(t *tickView) bool {
	return g.m.scaryPlayerNear(t, puffScanGrow) || g.m.scaryMobNear(t, puffScanGrow)
}

func (g *pufferGoal) Start(*tickView) {
	g.m.puffer.inflateCounter, g.m.puffer.deflate = 1, 0
}

func (g *pufferGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *pufferGoal) Tick(*tickView)                  {}
func (g *pufferGoal) Stop(*tickView)                  { g.m.puffer.inflateCounter = 0 }
func (g *pufferGoal) RequiresEveryTick() bool         { return false }

// pufferfishGoals is Pufferfish.registerGoals over AbstractFish's: panic,
// the puff goal, the player flee and the swim, with no school.
func pufferfishGoals(m *Mob) []Goal {
	return []Goal{
		&fishPanicGoal{m: m},
		&pufferGoal{m: m},
		&fishAvoidGoal{m: m},
		&fishSwimGoal{m: m},
	}
}
