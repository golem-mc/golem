package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world"
)

// A piglin celebrates a kill: it walks to where its target fell and, after a
// hoglin, one time in ten dances there. Left out: the celebration's looks at
// nearby piglins and its random strolls.
// vanilla: StartCelebratingIfTargetDead, PiglinAi.initCelebrateActivity

const (
	// piglinCelebrateTicks is the expiry of CELEBRATE_LOCATION and DANCING.
	// vanilla: PiglinAi.CELEBRATION_TIME
	piglinCelebrateTicks = 300
	// piglinDanceChance is the chance a killed hoglin starts a dance.
	// vanilla: PiglinAi.PROBABILITY_OF_CELEBRATION_DANCE
	piglinDanceChance = 0.1
	// piglinDanceSpeed is the walking speed factor while dancing.
	// vanilla: PiglinAi.SPEED_MULTIPLIER_WHEN_DANCING
	piglinDanceSpeed = 0.6
	// priorityCelebrate ranks the celebration above idling.
	priorityCelebrate = hostileStroll - 1
)

// celebrateState is the CELEBRATE_LOCATION and DANCING memories.
type celebrateState struct {
	// celebrate and dance count down the memories' expiry, in ticks.
	celebrate, dance int
	// x, y, z is the block the target fell on.
	x, y, z int32
}

// Dancing is Piglin.isDancing.
func (m *Mob) Dancing() bool { return m.isPiglin() && m.barter.dance > 0 }

// endCelebration erases both memories.
// vanilla: PiglinAi.wasHurtBy
func (m *Mob) endCelebration() {
	m.barter.celebrate = 0
	m.SetMetadataBarterDance(0)
}

// celebrateStep runs the memories' countdown and starts a celebration when
// the target is found dead. A dead player stays the target unless the
// forgive_dead_players rule is set.
// vanilla: StartCelebratingIfTargetDead
func (m *Mob) celebrateStep(t *tickView) {
	c := &m.barter.celebrateState
	if c.celebrate > 0 {
		c.celebrate--
	}
	if c.dance > 0 {
		m.SetMetadataBarterDance(c.dance - 1)
	}
	if c.celebrate == 0 {
		m.SetMetadataBarterDance(0)
	}
	if m.Type != TypePiglin || m.TargetID == 0 || c.celebrate > 0 {
		return
	}
	if p := t.playerByID(m.TargetID); p != nil {
		if p.Dead {
			m.celebrateAt(p.X, p.Y, p.Z, false)
			if t.w.BoolRule(world.RuleForgiveDeadPlayers) {
				m.SetMetadataTargetID(0)
			}
		}
		return
	}
	if o := t.mobByID(m.TargetID); o != nil && !o.Alive() {
		m.celebrateAt(o.X, o.Y, o.Z, o.Type == TypeHoglin)
		m.SetMetadataTargetID(0)
	}
}

// celebrateAt sets the celebration at the block holding (x, y, z).
func (m *Mob) celebrateAt(x, y, z float64, hoglin bool) {
	c := &m.barter.celebrateState
	// Piglins celebrating in the same game tick share the dance decision.
	// vanilla: PiglinAi.wantsToDance
	if hoglin && newLegacyRandom(m.GameTime).nextFloat() < piglinDanceChance {
		m.SetMetadataBarterDance(piglinCelebrateTicks)
	}
	c.celebrate = piglinCelebrateTicks
	c.x, c.y, c.z = int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
}

// celebrating reports the celebrate activity, which ranks under admiring, the
// fight and the retreat.
// vanilla: PiglinAi.updateActivity
func (m *Mob) celebrating() bool {
	b := &m.barter
	return b.celebrate > 0 && b.admiring == 0 && b.retreat == 0 && m.TargetID == 0
}

// celebrateGoal walks the piglin to the kill's block, or within four blocks of
// it while dancing.
// vanilla: GoToTargetLocation in Activity.CELEBRATE
type celebrateGoal struct{ m *Mob }

func (g *celebrateGoal) Priority() int                 { return priorityCelebrate }
func (g *celebrateGoal) Flags() GoalFlags              { return GoalMove }
func (g *celebrateGoal) CanStart(*tickView) bool       { return g.m.celebrating() }
func (g *celebrateGoal) ShouldContinue(*tickView) bool { return g.m.celebrating() }
func (g *celebrateGoal) Start(*tickView)               {}
func (g *celebrateGoal) Stop(*tickView)                { g.m.stopSeeking() }
func (g *celebrateGoal) RequiresEveryTick() bool       { return true }

func (g *celebrateGoal) Tick(t *tickView) {
	m := g.m
	c := &m.barter.celebrateState
	reach, speed := 2.0, 1.0
	if m.barter.dance > 0 {
		reach, speed = 4, piglinDanceSpeed
	}
	x, y, z := float64(c.x)+0.5, float64(c.y), float64(c.z)+0.5
	if sqDist3(m.X, m.Y, m.Z, x, y, z) > reach*reach {
		m.seekTo(t.w, x, y, z, speed, 0)
	}
}
