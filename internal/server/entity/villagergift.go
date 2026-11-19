package entity

import (
	"math"
	"strings"

	"github.com/golem-mc/golem/internal/server/rules"
)

// Gift timings, in ticks, and the throwing reach in blocks.
// vanilla: GiveGiftToHero
const (
	giftFirstDelay  = 600
	giftDelaySpread = 6001 // the next gift comes 600 + [0, 6000] ticks later
	giftTimeout     = 100
	giftTurnTicks   = 20 // the gift waits for the head to finish turning
	giftReach       = 5
	giftWalkPace    = 0.5
	// villagerPlayerSense is the range of the nearest-visible-player memory.
	villagerPlayerSense = 16
)

// giftProfessions are the professions that have a gift table of their own;
// every other one gives the unemployed gift.
// vanilla: GiveGiftToHero.GIFTS
var giftProfessions = map[string]bool{
	"armorer": true, "butcher": true, "cartographer": true, "cleric": true, "farmer": true,
	"fisherman": true, "fletcher": true, "leatherworker": true, "librarian": true, "mason": true,
	"shepherd": true, "toolsmith": true, "weaponsmith": true,
}

// giftTable is the loot table a villager throws a hero from.
// vanilla: GiveGiftToHero.getLootTableToThrow
func (m *Mob) giftTable() string {
	const dir = "minecraft:gameplay/hero_of_the_village/"
	if m.IsBaby() {
		return dir + "baby_gift"
	}
	if p := strings.TrimPrefix(m.Villager.Profession, "minecraft:"); giftProfessions[p] {
		return dir + p + "_gift"
	}
	return dir + "unemployed_gift"
}

// giveGiftGoal walks to the nearest visible Hero of the Village and, once
// within five blocks and the head has turned, throws them one roll of the
// profession's gift table. timeLeft is the wait before the next gift; it only
// counts down while a hero is in view and the goal is allowed to run.
// vanilla: GiveGiftToHero
// One counter is shared across work, meet and idle; vanilla keeps one per package.
type giveGiftGoal struct {
	m        *Mob
	timeLeft int
	started  int64
	given    bool
	left     int
	recalc   int
}

func (g *giveGiftGoal) Priority() int           { return prioritySleep }
func (g *giveGiftGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *giveGiftGoal) RequiresEveryTick() bool { return true }

// hero is the nearest player the villager sees, when it is a hero.
func (g *giveGiftGoal) hero(t *tickView) *Player {
	hasHero := false
	for _, p := range t.players {
		hasHero = hasHero || p.Hero
	}
	if !hasHero {
		return nil
	}
	if p := g.m.nearestPlayerWithin(t, villagerPlayerSense, true); p != nil && p.Hero {
		return p
	}
	return nil
}

func (g *giveGiftGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.Sleeping || (m.vb.activity != actWork && m.vb.activity != actMeet && m.vb.activity != actIdle) || g.hero(t) == nil {
		return false
	}
	if g.timeLeft > 0 {
		g.timeLeft--
		return false
	}
	return true
}

func (g *giveGiftGoal) Start(t *tickView) {
	g.given, g.started, g.left, g.recalc = false, g.m.GameTime, giftTimeout, 0
	g.m.lookAtTarget(g.hero(t))
}

func (g *giveGiftGoal) ShouldContinue(t *tickView) bool {
	return g.left > 0 && !g.given && g.hero(t) != nil
}

func (g *giveGiftGoal) Tick(t *tickView) {
	m, p := g.m, g.hero(t)
	if p == nil {
		return
	}
	g.left--
	m.lookAtTarget(p)
	if dx, dy, dz := math.Floor(p.X)-math.Floor(m.X), math.Floor(p.Y)-math.Floor(m.Y), math.Floor(p.Z)-math.Floor(m.Z); dx*dx+dy*dy+dz*dz < giftReach*giftReach {
		if m.GameTime-g.started > giftTurnTicks {
			for _, stack := range rules.RollLoot(m.giftTable(), m.giftContext(t.w), m.Rand) {
				m.throwAt(t, stack, p.X, p.Y, p.Z)
			}
			g.given = true
		}
		return
	}
	if g.recalc--; g.recalc <= 0 {
		g.recalc = reducedTickDelay(followReplan)
		m.seekTo(t.w, p.X, p.Y, p.Z, giftWalkPace, giftReach)
	}
}

func (g *giveGiftGoal) Stop(*tickView) {
	g.timeLeft = giftDelayFor(g.m)
	g.m.stopSeeking()
}

// giftDelayFor is the wait in selector passes, which run every other tick.
func giftDelayFor(m *Mob) int {
	return reducedTickDelay(giftFirstDelay + m.Rand.Intn(giftDelaySpread))
}
