package entity

import "math"

// vanilla: FollowPlayerRiddenEntityGoal
type dolphinRideGoal struct {
	m      *Mob
	boat   bool
	player int32
	recalc int
	ahead  bool
}

func (g *dolphinRideGoal) Priority() int           { return 8 }
func (g *dolphinRideGoal) Flags() GoalFlags        { return 0 }
func (g *dolphinRideGoal) RequiresEveryTick() bool { return false }
func dolphinRiderMoving(p *Player) bool {
	return p.KnownVX*p.KnownVX+p.KnownVZ*p.KnownVZ > float64(float32(1e-5))*float64(float32(1e-5))
}
func (g *dolphinRideGoal) nearby(p *Player) bool {
	box := g.m.mobBox()
	b := p.RideBox
	return ((g.boat && p.BoatDriver) || (!g.boat && p.NautilusDriver)) && b[0] < box.maxX+5 && b[3] > box.minX-5 && b[1] < box.maxY+5 && b[4] > box.minY-5 && b[2] < box.maxZ+5 && b[5] > box.minZ-5
}
func (g *dolphinRideGoal) CanStart(t *tickView) bool {
	if p := t.playerByID(g.player); p != nil && dolphinRiderMoving(p) {
		return true
	}
	for i := range t.players {
		p := &t.players[i]
		if g.nearby(p) && dolphinRiderMoving(p) {
			return true
		}
	}
	return false
}
func (g *dolphinRideGoal) ShouldContinue(t *tickView) bool {
	p := t.playerByID(g.player)
	return p != nil && p.Passenger && dolphinRiderMoving(p)
}
func (g *dolphinRideGoal) Start(t *tickView) {
	for i := range t.players {
		p := &t.players[i]
		if g.nearby(p) {
			g.player = p.EntityID
			break
		}
	}
	g.recalc = 0
	g.ahead = false
}
func (g *dolphinRideGoal) Stop(*tickView) { g.player = 0 }
func (g *dolphinRideGoal) Tick(t *tickView) {
	m, p := g.m, t.playerByID(g.player)
	if p == nil {
		return
	}
	speed := float64(float32(0.015))
	if g.ahead {
		speed = float64(float32(0.01))
	}
	m.smoothSwimTravelInput(speed, m.dolphin.up)
	m.move(t.w, m.VX, m.VY, m.VZ)
	g.recalc--
	if g.recalc > 0 {
		return
	}
	g.recalc = reducedTickDelay(10)
	dx, dz := dolphinDirection(p.Yaw)
	step := int32(-1)
	if g.ahead {
		step = 10
	}
	m.swimTo(t.w, math.Floor(p.X)+float64(dx*step), math.Floor(p.Y)-1, math.Floor(p.Z)+float64(dz*step), 1)
	if !g.ahead && m.distanceToPlayer(p) < 4 {
		g.ahead = true
		g.recalc = 0
	} else if g.ahead && m.distanceToPlayer(p) > 12 {
		g.ahead = false
		g.recalc = 0
	}
}
