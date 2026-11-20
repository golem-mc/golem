package entity

import "math"

const (
	// llamaCaravanSpeed is the goal's speed factor, 2.1f.
	llamaCaravanSpeed = float64(float32(2.1))
	// llamaCaravanLimit is how far down a caravan the leashed lead is looked
	// for. vanilla: LlamaFollowCaravanGoal.CARAVAN_LIMIT
	llamaCaravanLimit = 8
	// llamaCaravanScanXZ and llamaCaravanScanY inflate the llama's box to
	// find a caravan to join.
	llamaCaravanScanXZ = 9.0
	llamaCaravanScanY  = 4.0
	// llamaCaravanJoinGap is the squared distance under which a llama does
	// not join; llamaCaravanFarSq is the one past which it hurries.
	llamaCaravanJoinGap = 4.0
	llamaCaravanFarSq   = 676.0
	// llamaCaravanHurry multiplies the speed each time the head is far off,
	// until the speed passes llamaCaravanMaxSpeed.
	llamaCaravanHurry    = 1.2
	llamaCaravanMaxSpeed = 3.0
	// llamaCaravanGap is the distance kept to the llama ahead.
	llamaCaravanGap = 2.0
)

// inCaravan reports whether the llama follows another.
// vanilla: Llama.inCaravan
func (m *Mob) inCaravan() bool { return m.Horse.head != nil }

func (m *Mob) leashed() bool { return m.LeashHolderID != 0 }

// leaveCaravan is Llama.leaveCaravan.
func (m *Mob) leaveCaravan() {
	if h := m.Horse.head; h != nil {
		h.Horse.tail = nil
	}
	m.Horse.head = nil
}

// joinCaravan is Llama.joinCaravan: m follows head.
func (m *Mob) joinCaravan(head *Mob) {
	m.Horse.head = head
	head.Horse.tail = m
}

// caravanGoal is LlamaFollowCaravanGoal: a llama falls in behind the nearest
// caravan whose lead is on a leash, and trails it at two blocks.
type caravanGoal struct {
	m             *Mob
	speed         float64
	distCheckTime int
}

func (g *caravanGoal) Priority() int    { return priorityBreed }
func (g *caravanGoal) Flags() GoalFlags { return GoalMove }

func (g *caravanGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.leashed() || m.inCaravan() {
		return false
	}
	// The nearest unfollowed llama already in a caravan wins; failing that,
	// the nearest leashed one. Ties go to the later in herd order: the
	// whole herd is scanned in that order, the grid's gather in none.
	var inCaravan, leashed *Mob
	bestC, bestL := math.MaxFloat64, math.MaxFloat64
	scan, ordered := t.others, true
	if m.push != nil && len(t.others) > villagerGridMinHerd {
		scan, ordered = m.push.gather(m, llamaCaravanScanXZ+1e-6), false
	}
	later := func(d, best float64, o, cur *Mob) bool {
		return d < best || d == best && (ordered || o.pushIdx > cur.pushIdx)
	}
	// Only a llama in a caravan or on a leash can be picked, so that cheap
	// test goes before the box.
	scanW, scanH := m.width()+2*llamaCaravanScanXZ, m.height()+2*llamaCaravanScanY
	for _, o := range scan {
		if o == m || !o.inCaravan() && !o.leashed() || o.Horse.tail != nil || !IsLlama(o.Type) || !o.Alive() {
			continue
		}
		if !boxesOverlap(m.X, m.Y-llamaCaravanScanY, m.Z, scanW, scanH, o.X, o.Y, o.Z, o.width(), o.height()) {
			continue
		}
		d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z)
		if o.inCaravan() && later(d, bestC, o, inCaravan) {
			bestC, inCaravan = d, o
		}
		if o.leashed() && later(d, bestL, o, leashed) {
			bestL, leashed = d, o
		}
	}
	closest, best := inCaravan, bestC
	if closest == nil {
		closest, best = leashed, bestL
	}
	if closest == nil || best < llamaCaravanJoinGap {
		return false
	}
	if !closest.leashed() && !firstIsLeashed(closest, 1) {
		return false
	}
	m.joinCaravan(closest)
	return true
}

func (g *caravanGoal) ShouldContinue(*tickView) bool {
	m := g.m
	if !m.inCaravan() || !m.Horse.head.Alive() || !firstIsLeashed(m, 0) {
		return false
	}
	h := m.Horse.head
	if Dist2(m.X, m.Y, m.Z, h.X, h.Y, h.Z) > llamaCaravanFarSq {
		if g.speed <= llamaCaravanMaxSpeed {
			g.speed *= llamaCaravanHurry
			g.distCheckTime = reducedTickDelay(40)
			return true
		}
		if g.distCheckTime == 0 {
			return false
		}
	}
	if g.distCheckTime > 0 {
		g.distCheckTime--
	}
	return true
}

func (g *caravanGoal) Start(*tickView) {}

func (g *caravanGoal) Stop(*tickView) {
	g.m.leaveCaravan()
	g.speed = llamaCaravanSpeed
}

func (g *caravanGoal) RequiresEveryTick() bool { return false }

func (g *caravanGoal) Tick(t *tickView) {
	m := g.m
	if !m.inCaravan() || m.Horse.knotHeld {
		return
	}
	h := m.Horse.head
	dx, dy, dz := h.X-m.X, h.Y-m.Y, h.Z-m.Z
	dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if dist == 0 {
		return
	}
	k := math.Max(dist-llamaCaravanGap, 0) / dist
	m.seekTo(t.w, m.X+dx*k, m.Y+dy*k, m.Z+dz*k, g.speed, navReach)
}

// firstIsLeashed reports whether the lead of the caravan m trails is on a
// leash, within llamaCaravanLimit llamas.
func firstIsLeashed(m *Mob, counter int) bool {
	if counter > llamaCaravanLimit || !m.inCaravan() {
		return false
	}
	if m.Horse.head.leashed() {
		return true
	}
	return firstIsLeashed(m.Horse.head, counter+1)
}
