package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
)

// FeedDolphin applies Dolphin.mobInteract after the caller checks #fishes.
func (m *Mob) FeedDolphin() {
	if m.IsBaby() && !m.AgeLocked {
		m.forceAgeUp()
	} else {
		m.SetMetadataDolphinGotFish(true)
	}
}

// DolphinNeedsTreasure requests a structure lookup before the mob lock.
func (m *Mob) DolphinNeedsTreasure() bool {
	return m.Type == TypeDolphin && m.Alive() && !m.NoAI && m.dolphin.gotFish && m.AirSupply >= 100 && !m.dolphin.treasureReady
}

// SetDolphinTreasure supplies the result of the tick's structure lookup.
func (m *Mob) SetDolphinTreasure(x, z int32, found bool) {
	m.dolphin.treasure = Waypoint{X: x, Z: z}
	m.dolphin.treasureReady, m.dolphin.treasureFound = true, found
}

// vanilla: Dolphin.DolphinSwimToTreasureGoal
type dolphinTreasureGoal struct {
	m     *Mob
	stuck bool
}

func (g *dolphinTreasureGoal) Priority() int           { return 1 }
func (g *dolphinTreasureGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *dolphinTreasureGoal) Uninterruptible() bool   { return true }
func (g *dolphinTreasureGoal) RequiresEveryTick() bool { return false }
func (g *dolphinTreasureGoal) CanStart(*tickView) bool {
	return g.m.dolphin.gotFish && g.m.AirSupply >= 100 && g.m.dolphin.treasureReady
}
func (g *dolphinTreasureGoal) arrived() bool {
	m := g.m
	dx, dz := float64(m.dolphin.treasure.X)+0.5-m.X, float64(m.dolphin.treasure.Z)+0.5-m.Z
	// vanilla measures to the centre of a block at the dolphin's current Y.
	dy := math.Floor(m.Y) + 0.5 - m.Y
	return dx*dx+dy*dy+dz*dz < 16
}
func (g *dolphinTreasureGoal) ShouldContinue(*tickView) bool {
	return g.m.dolphin.treasureFound && !g.arrived() && !g.stuck && g.m.AirSupply >= 100
}
func (g *dolphinTreasureGoal) Start(t *tickView) {
	g.m.stopSeeking()
	g.stuck = !g.m.dolphin.treasureFound
	if !g.stuck {
		t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobDolphinTreasure})
	}
}
func (g *dolphinTreasureGoal) Stop(*tickView) {
	m := g.m
	if !m.dolphin.treasureFound || g.arrived() || g.stuck {
		m.SetMetadataDolphinGotFish(false)
	}
	m.dolphin.treasureReady = false
}
func (g *dolphinTreasureGoal) Tick(t *tickView) {
	m := g.m
	if !m.dolphin.treasureFound {
		return
	}
	end := m.nav.end
	dx, dy, dz := float64(end.X)+0.5-m.X, float64(end.Y)+0.5-m.Y, float64(end.Z)+0.5-m.Z
	if len(m.nav.path) > 0 && dx*dx+dy*dy+dz*dz >= 144 {
		return
	}
	target := [2]float64{float64(m.dolphin.treasure.X) + 0.5, float64(m.dolphin.treasure.Z) + 0.5}
	pick := func(xz, y int, angle float64) (Waypoint, bool) {
		return posTowardsWhere(t.w, m, xz, y, target, angle, func(p Waypoint) bool {
			return guardianCellOK(t.w, m, p, m.homeRestricted(xz)) && !hasMalus(t.w, m, p)
		})
	}
	next, ok := pick(16, 1, float64(float32(math.Pi/8)))
	if !ok {
		next, ok = pick(8, 4, float64(float32(math.Pi/2)))
	}
	if ok && !rules.IsWater(t.w.BlockAt(next.X, next.Y, next.Z)) {
		next, ok = pick(8, 5, float64(float32(math.Pi/2)))
	}
	if !ok {
		g.stuck = true
		return
	}
	x, y, z := float64(next.X)+0.5, float64(next.Y), float64(next.Z)+0.5
	m.lookAt(x, y, z, m.maxHeadYRot()+20, m.maxHeadXRot())
	m.swimTo(t.w, x, y, z, 1.3)
	if m.Rand.Intn(reducedTickDelay(80)) == 0 {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobDolphinTreasure})
	}
}

// vanilla: Dolphin.DolphinSwimWithPlayerGoal
type dolphinSwimWithPlayerGoal struct {
	m      *Mob
	player int32
}

func (g *dolphinSwimWithPlayerGoal) Priority() int           { return 2 }
func (g *dolphinSwimWithPlayerGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *dolphinSwimWithPlayerGoal) RequiresEveryTick() bool { return false }
func (g *dolphinSwimWithPlayerGoal) CanStart(t *tickView) bool {
	nearest := g.m.nearestPlayerWithin(t, 10, false)
	if nearest == nil || !nearest.Swimming || g.m.TargetID == nearest.EntityID {
		return false
	}
	g.player = nearest.EntityID
	return true
}
func (g *dolphinSwimWithPlayerGoal) ShouldContinue(t *tickView) bool {
	p := t.playerByID(g.player)
	return p != nil && p.Swimming && g.m.distanceToPlayer(p) < 16
}
func (g *dolphinSwimWithPlayerGoal) grant(t *tickView) {
	if p := t.playerByID(g.player); p != nil {
		t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobDolphinGrace, Player: *p})
	}
}
func (g *dolphinSwimWithPlayerGoal) Start(t *tickView) { g.grant(t) }
func (g *dolphinSwimWithPlayerGoal) Stop(*tickView)    { g.player = 0; g.m.stopSeeking() }
func (g *dolphinSwimWithPlayerGoal) Tick(t *tickView) {
	p := t.playerByID(g.player)
	if p == nil {
		return
	}
	m := g.m
	eye := p.eyeHeight()
	if p.Width == 0 && p.Swimming {
		eye = rules.PlayerEyeHeight(protocol.PoseSwimming)
	}
	m.lookAt(p.X, p.Y+eye, p.Z, m.maxHeadYRot()+20, m.maxHeadXRot())
	if m.distanceToPlayer(p) < 2.5 {
		m.stopSeeking()
	} else {
		m.swimTo(t.w, p.X, p.Y, p.Z, 4)
	}
	if p.Swimming && m.Rand.Intn(6) == 0 {
		g.grant(t)
	}
}
