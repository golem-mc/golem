package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// yieldJobSite hands the potential job site to a nearby villager whose
// profession holds that kind of site, when this adult has no profession and
// that villager has no site of its own, or already holds this one.
// vanilla: YieldJobSite
func (m *Mob) yieldJobSite(t *tickView) {
	vb := &m.vb
	p := vb.potentialJobSite
	if vb.jobSite != nil || !m.memHere(p) || m.IsBaby() || m.Villager.Profession != "minecraft:none" {
		return
	}
	kind := rules.POIKindOf(t.w.BlockAt(p.X, p.Y, p.Z))
	if kind == rules.POINone {
		return
	}
	for _, o := range t.villagerMobs() {
		if o == m || !o.Alive() || o.vb.potentialJobSite != nil ||
			Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) > 256 || !heldJobSite(o.Villager.Profession, kind) {
			continue
		}
		if j := o.vb.jobSite; j != nil {
			if !o.memHere(j) || j.at() != p.at() {
				continue
			}
		} else if !o.pathReaches(t.w, p.at(), 1) {
			continue
		}
		vb.potentialJobSite = nil
		m.stopSeeking()
		if o.vb.jobSite == nil {
			o.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, villagerWalkPace, 1)
			o.vb.potentialJobSite = &GlobalPos{Dim: p.Dim, X: p.X, Y: p.Y, Z: p.Z}
		}
		return
	}
}

// Display timings of the trades a villager shows.
// vanilla: ShowTradesToPlayer, SetLookAndInteract
const (
	showMaxLook   = 900
	showStartLook = 40
	showCycle     = 40
	showMinTime   = 400
	showMaxTime   = 1600
	showReachSq   = 17
	showSenseSq   = 16
)

// showTradesGoal faces a player within four blocks and holds up, one at a
// time, what its offers sell for the item the player is holding.
// vanilla: SetLookAndInteract, ShowTradesToPlayer
type showTradesGoal struct {
	m        *Mob
	player   int32
	held     protocol.ItemStack
	haveHeld bool
	display  []protocol.ItemStack
	cycle    int
	index    int
	lookTime int
	left     int
}

func (g *showTradesGoal) Priority() int           { return priorityPoiStrol }
func (g *showTradesGoal) Flags() GoalFlags        { return GoalLook }
func (g *showTradesGoal) RequiresEveryTick() bool { return true }

func (g *showTradesGoal) target(t *tickView) *Player {
	for i := range t.players {
		if p := &t.players[i]; p.EntityID == g.player {
			return p
		}
	}
	return nil
}

func (g *showTradesGoal) reaches(p *Player) bool {
	m := g.m
	return p != nil && !p.Dead && !m.IsBaby() && Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) <= showReachSq
}

func (g *showTradesGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.Sleeping || m.vb.activity == actRest || m.vb.activity == actPanic {
		return false
	}
	p := m.nearestPlayerWithin(t, 4, true)
	if p == nil || Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) > showSenseSq {
		return false
	}
	g.player = p.EntityID
	return g.reaches(p)
}

func (g *showTradesGoal) Start(t *tickView) {
	g.cycle, g.index, g.lookTime = 0, 0, showStartLook
	g.left = showMinTime + g.m.Rand.Intn(showMaxTime-showMinTime+1)
	g.haveHeld, g.display = false, g.display[:0]
	g.m.lookAtTarget(g.target(t))
}

func (g *showTradesGoal) ShouldContinue(t *tickView) bool {
	return g.left > 0 && g.lookTime > 0 && g.reaches(g.target(t))
}

func (g *showTradesGoal) Tick(t *tickView) {
	m, p := g.m, g.target(t)
	if !g.reaches(p) {
		return
	}
	g.left--
	m.lookAtTarget(p)
	if !g.haveHeld || g.held.ItemID != p.HeldItem.ItemID {
		g.held, g.haveHeld = p.HeldItem, true
		g.display = g.display[:0]
		if g.held.Count > 0 {
			g.display = m.offersFor(g.held.ItemID, g.display)
			// A deferred draw is ready on the next tick, even with the same held item.
			if m.Offers == nil {
				g.haveHeld = false
			}
			if len(g.display) > 0 {
				g.lookTime = showMaxLook
				g.show(g.display[0])
			}
		}
	}
	if len(g.display) == 0 {
		g.clear()
		g.lookTime = min(g.lookTime, showStartLook)
	} else if g.cycle++; len(g.display) >= 2 && g.cycle >= showCycle {
		g.index = (g.index + 1) % len(g.display)
		g.cycle = 0
		g.show(g.display[g.index])
	}
	g.lookTime--
}

func (g *showTradesGoal) Stop(*tickView) { g.clear(); g.haveHeld = false }

func (g *showTradesGoal) show(s protocol.ItemStack) {
	g.m.SetEquipment(protocol.EquipMainHand, s)
	g.m.DropChances[0] = 0
}

func (g *showTradesGoal) clear() {
	g.m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
	g.m.DropChances[0] = DefaultDropChances()[0]
}

// offersFor appends what each offer in stock that costs the item sells.
// vanilla: ShowTradesToPlayer.updateDisplayItems
func (m *Mob) offersFor(item int32, out []protocol.ItemStack) []protocol.ItemStack {
	if m.Offers == nil && m.DrawTrades != nil {
		m.DrawTrades(m)
	}
	name, _ := registry.ItemName(item)
	recipes, _ := nbt.Get[nbt.List](m.Offers, "Recipes")
	for _, it := range recipes.Items {
		r, ok := nbt.CompoundView(it)
		if !ok {
			continue
		}
		uses, _ := nbt.IntField(r, "uses")
		maxUses, ok := nbt.IntField(r, "maxUses")
		if !ok {
			maxUses = defaultOfferUses
		}
		a, _ := nbt.Get[nbt.Compound](r, "buy")
		b, _ := nbt.Get[nbt.Compound](r, "buyB")
		sellTag, _ := nbt.Get[nbt.Compound](r, "sell")
		sell, okS := registry.StoredStack(sellTag)
		if uses >= maxUses || !okS || a["id"] != nbt.String(name) && b["id"] != nbt.String(name) {
			continue
		}
		out = append(out, sell)
	}
	return out
}

// jumpOnBedGoal has a child walk to its nearest reachable bed and jump on it
// three to six times.
// vanilla: JumpOnBed, NearestBedSensor
type jumpOnBedGoal struct {
	m                     *Mob
	bed                   Waypoint
	walkLeft, jumps, wait int
}

func (g *jumpOnBedGoal) Priority() int           { return priorityPoiStrol }
func (g *jumpOnBedGoal) Flags() GoalFlags        { return GoalMove | GoalJump }
func (g *jumpOnBedGoal) RequiresEveryTick() bool { return true }

func (g *jumpOnBedGoal) onOrOver(t *tickView) bool {
	c := g.m.cell()
	return g.m.Village.IsBed(c.X, c.Y, c.Z) || g.m.Village.IsBed(c.X, c.Y-1, c.Z)
}

// CanStart takes the idle package's one-in-eight share for the choice.
func (g *jumpOnBedGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.Village == nil || !m.IsBaby() || m.vb.activity != actIdle || m.Sleeping || m.Rand.Intn(8) != 0 {
		return false
	}
	if m.vb.nearestBed == nil {
		return false
	}
	g.bed = *m.vb.nearestBed
	return true
}

func (g *jumpOnBedGoal) Start(t *tickView) {
	g.walkLeft, g.jumps, g.wait = 100, 3+g.m.Rand.Intn(4), 0
	g.m.seekTo(t.w, float64(g.bed.X)+0.5, float64(g.bed.Y), float64(g.bed.Z)+0.5, villagerWalkPace, 0)
}

func (g *jumpOnBedGoal) ShouldContinue(t *tickView) bool {
	on := g.onOrOver(t)
	return g.m.IsBaby() && g.m.Village.IsBed(g.bed.X, g.bed.Y, g.bed.Z) && (on || g.walkLeft > 0) && (!on || g.jumps > 0)
}

func (g *jumpOnBedGoal) Tick(t *tickView) {
	m := g.m
	if !g.onOrOver(t) {
		g.walkLeft--
		return
	}
	if g.wait > 0 {
		g.wait--
		return
	}
	if c := m.cell(); m.Village.IsBed(c.X, c.Y, c.Z) {
		m.jump = true
		g.jumps--
		g.wait = 5
	}
}

func (g *jumpOnBedGoal) Stop(*tickView) { g.m.stopSeeking() }

// Closest-home search timings.
// vanilla: SetClosestHomeAsWalkTarget
const (
	homeSearchRate  = 20
	homeSearchBatch = 5
	homeSearchCache = 40
	homeSearchRange = 48
	homeSearchOkSq  = 4
)

// homeSearchGoal walks a homeless villager at rest to the closest bed it can
// reach, tried five beds at a time, until it is there or the walk ends.
// vanilla: SetClosestHomeAsWalkTarget (getRestPackage)
type homeSearchGoal struct {
	m      *Mob
	search bedSearch
	target Waypoint
}

func (g *homeSearchGoal) Priority() int           { return priorityPoiStrol }
func (g *homeSearchGoal) Flags() GoalFlags        { return GoalMove }
func (g *homeSearchGoal) RequiresEveryTick() bool { return false }

func (g *homeSearchGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.Village == nil || m.vb.activity != actRest || m.vb.home != nil || m.Sleeping {
		return false
	}
	p, ok := g.search.find(m, t.w, true)
	if ok {
		g.target = p
	}
	return ok
}

// bedSearch batches path checks for both nearest-bed sensing and the rest walk.
// vanilla: NearestBedSensor, SetClosestHomeAsWalkTarget
type bedSearch struct {
	next   int64
	cached map[Waypoint]int64
}

func (g *bedSearch) find(m *Mob, w World, skipNear bool) (Waypoint, bool) {
	if m.GameTime < g.next {
		return Waypoint{}, false
	}
	c := m.cell()
	beds := m.Village.POIs(isHome, c.X, c.Y, c.Z, homeSearchRange)
	g.next = m.GameTime + homeSearchRate
	if len(beds) == 0 || skipNear && sqDist3(float64(beds[0].X), float64(beds[0].Y), float64(beds[0].Z), float64(c.X), float64(c.Y), float64(c.Z)) <= homeSearchOkSq {
		return Waypoint{}, false
	}
	if g.cached == nil {
		g.cached = map[Waypoint]int64{}
	}
	tried := 0
	var picks []Waypoint
	for _, p := range beds {
		if _, seen := g.cached[p]; seen {
			continue
		}
		if tried++; tried > homeSearchBatch {
			break
		}
		g.cached[p] = g.next + homeSearchCache
		picks = append(picks, p)
	}
	for _, p := range picks {
		if m.pathReaches(w, p, 1) {
			return p, true
		}
	}
	if tried < homeSearchBatch {
		for p, until := range g.cached {
			if until < g.next {
				delete(g.cached, p)
			}
		}
	}
	return Waypoint{}, false
}

func (g *homeSearchGoal) Start(t *tickView) {
	p := g.target
	g.m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, villagerWalkPace, 1)
}

func (g *homeSearchGoal) ShouldContinue(*tickView) bool {
	return g.m.vb.activity == actRest && g.m.vb.home == nil && len(g.m.navOf().nav.path) > 0
}

func (g *homeSearchGoal) Tick(*tickView) {}
func (g *homeSearchGoal) Stop(*tickView) { g.m.stopSeeking() }
