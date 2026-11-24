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

// caravanGridMinList is the candidate count above which the grid's
// neighbourhood query replaces scanning the whole list.
const caravanGridMinList = 64

// sectionCoord is the entity section a coordinate is filed in.
func sectionCoord(v float64) int { return int(math.Floor(v)) >> 4 }

// sectionRange is the entity sections a horizontal query span reaches, padded by 2.
// vanilla: EntitySectionStorage.forEachAccessibleNonEmptySection
func sectionRange(lo, hi float64) (int, int) {
	return int(math.Floor(lo-2)) >> 4, int(math.Floor(hi+2)) >> 4
}

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
	if m.push != nil {
		if len(m.push.caravan) == 0 {
			return false
		}
		// pushIdx is herd order, so ties resolve as in a full scan.
		scan, ordered = m.push.caravan, false
		if len(scan) > caravanGridMinList {
			// A long list is cut to the neighbourhood by the grid.
			scan = m.push.gather(m, llamaCaravanScanXZ+1e-6)
		}
	}
	later := func(d, best float64, o, cur *Mob) bool {
		return d < best || d == best && (ordered || o.pushIdx > cur.pushIdx)
	}
	// Only a llama in a caravan or on a leash can be picked, so that cheap
	// test goes before the box.
	width, height := m.width(), m.height()
	loX, hiX := sectionRange(m.X-width/2-llamaCaravanScanXZ, m.X+width/2+llamaCaravanScanXZ)
	// Entities are filed by their feet, so Y reaches 4 below and 0 above.
	loY, hiY := sectionCoord(m.Y-llamaCaravanScanY-4), sectionCoord(m.Y+height+llamaCaravanScanY)
	loZ, hiZ := sectionRange(m.Z-width/2-llamaCaravanScanXZ, m.Z+width/2+llamaCaravanScanXZ)
	scanW, scanH := width+2*llamaCaravanScanXZ, height+2*llamaCaravanScanY
	for _, o := range scan {
		if o == m || !o.inCaravan() && !o.leashed() || o.Horse.tail != nil || !IsLlama(o.Type) || !o.Alive() {
			continue
		}
		// Vanilla only searches the entity sections the window touches, and
		// files an entity by its position, not its box.
		if sx, sy, sz := sectionCoord(o.X), sectionCoord(o.Y), sectionCoord(o.Z); sx < loX || sx > hiX || sy < loY || sy > hiY || sz < loZ || sz > hiZ {
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
	if m.push != nil {
		m.push.caravan = append(m.push.caravan, m)
	}
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
	// A coordinate moveTo drops its path if createPath cannot run mid-fall.
	// vanilla: PathNavigation.createPath, PathNavigation.moveTo
	if !m.canUpdatePath() {
		m.stopSeeking()
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
