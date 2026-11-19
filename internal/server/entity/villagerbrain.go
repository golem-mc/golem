package entity

import (
	"iter"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// GlobalPos is a block position in a dimension. vanilla: GlobalPos
type GlobalPos struct {
	Dim     string
	X, Y, Z int32
}

func (g *GlobalPos) at() Waypoint { return Waypoint{g.X, g.Y, g.Z} }

// VillageView is the village points of interest of the mob's dimension.
type VillageView interface {
	Dimension() string
	// POIs lists village POIs of the kinds within radius blocks, closest first.
	POIs(match func(rules.POIKind) bool, x, y, z int32, radius int) []Waypoint
	// Claimed reports whether a villager holds the POI at the block.
	Claimed(x, y, z int32) bool
	// SectionsToVillage is the section distance to the nearest occupied village POI, or
	// limit+1 when none is within limit sections.
	SectionsToVillage(x, y, z int32, limit int) int
	// IsBed reports whether the block is a bed half.
	IsBed(x, y, z int32) bool
	// BedStandUp is BedBlock.findStandUpPosition for a bed head.
	BedStandUp(x, y, z int32, yaw float32) (sx, sy, sz float64)
}

// OfferUpdater copies saved recipes while updating their numeric state.
type OfferUpdater interface {
	UpdateOfferState(offers nbt.Compound, update func(uses, maxUses, demand int32) (int32, int32)) nbt.Compound
}

// villagerActivity is the schedule's activity or a danger override.
// vanilla: Activity
type villagerActivity uint8

const (
	actIdle villagerActivity = iota
	actWork
	actMeet
	actRest
	actPanic
	actPlay
	actPreRaid
	actRaid
	actHide
)

// BedOp is a bed's occupied property a villager changed, which the server
// applies after the tick.
type BedOp struct {
	X, Y, Z  int32
	Occupied bool
}

// villagerMemory is what a villager remembers: the Brain memories this code
// models, the activity and the restock counters. A GlobalPos whose Dim is not
// the view's dimension is kept and saved but never acted on or counted.
type villagerMemory struct {
	assignProfessionWhenSpawned                   bool
	home, jobSite, potentialJobSite, meetingPoint *GlobalPos
	lastSlept, lastWoken, lastWorkedAtPoi         int64 // 0 = absent
	// golemUntil is the game time the golem-seen memory expires, 0 = absent;
	// golemScan counts passes to the next sensor scan. Neither is saved.
	golemUntil int64
	golemScan  int
	kids       villagerKids
	look       villagerLookTarget
	raid       villagerRaid
	danger     villagerDanger
	bedSensor  bedSearch
	nearestBed *Waypoint
	farmland   []Waypoint // SecondaryPoiSensor; unsaved, in the current dimension
	farmScan   int
	farmDim    string
	// brainRest is the loaded Brain.memories this code does not model, written
	// back as read.
	brainRest *memoryMap

	activity     villagerActivity
	lastSchedule int64

	lastRestock   int64 // LastRestock
	restocksToday int32 // RestocksToday
	// offersChanged is set when a restock or demand catch-up rewrote the
	// offers; unsaved.
	offersChanged bool
	// lastRestockCheckDay is not saved.
	lastRestockCheckDay int64

	// sleepPos is the bed head the villager lies in while Sleeping.
	sleepPos Waypoint
	// restoredSleep marks a sleeper loaded from sleeping_pos until sleepInBedGoal adopts it.
	restoredSleep bool
	bedOps        []BedOp
	// acquire is each AcquirePoi's state: potential job site, home, meeting point.
	acquire [3]poiAcquire
	// childHome is the bed giveBirth took for a child not yet spawned.
	childHome *GlobalPos
	// doors is the villager's InteractWithDoor goal, shared with sleepInBedGoal
	// so lying down closes the doors it remembers; nil until the goals are wired.
	doors *interactWithDoor
}

// SetJobSite sets the job site memory.
func (m *Mob) SetJobSite(dim string, x, y, z int32) { m.vb.jobSite = &GlobalPos{dim, x, y, z} }

// TakeBedOps returns and clears the bed changes the villager queued.
func (m *Mob) TakeBedOps() []BedOp {
	o := m.vb.bedOps
	m.vb.bedOps = nil
	return o
}

// SleepingPos is the bed head a sleeping villager lies in.
func (m *Mob) SleepingPos() (x, y, z int32, ok bool) {
	p := m.vb.sleepPos
	return p.X, p.Y, p.Z, m.Sleeping && m.Type == TypeVillager
}

// BedHeadSleeper reports whether the villager lies in the bed head at x, y, z.
func (m *Mob) BedHeadSleeper(x, y, z int32) bool {
	sx, sy, sz, ok := m.SleepingPos()
	return ok && sx == x && sy == y && sz == z
}

// The villager schedule: the activity from each time of day on.
// vanilla: Timelines VILLAGER_SCHEDULE. It is not data-driven; a data pack's
// timeline is not read.
type scheduleKey struct {
	at  int64
	act villagerActivity
}

var (
	adultSchedule = [...]scheduleKey{{10, actIdle}, {2000, actWork}, {9000, actMeet}, {11000, actIdle}, {12000, actRest}}
	babySchedule  = [...]scheduleKey{{10, actIdle}, {3000, actPlay}, {6000, actIdle}, {10000, actPlay}, {12000, actRest}}
)

// villagerSchedule is the activity the schedule gives at a time of day; the
// last keyframe's value wraps around to before the first.
func villagerSchedule(dayTime int64, baby bool) villagerActivity {
	keys := adultSchedule[:]
	if baby {
		keys = babySchedule[:]
	}
	t := dayTime % 24000
	act := keys[len(keys)-1].act
	for _, k := range keys {
		if t < k.at {
			break
		}
		act = k.act
	}
	return act
}

// updateActivity re-reads the schedule every 20 ticks; an activity whose
// memory is missing is idling.
// vanilla: Brain.updateActivityFromSchedule, Brain.setActiveActivityIfPossible
func (m *Mob) updateActivity() {
	vb := &m.vb
	if vb.activity == actPanic || vb.activity >= actPreRaid || m.GameTime-vb.lastSchedule <= 20 {
		return
	}
	vb.lastSchedule = m.GameTime
	act := villagerSchedule(m.DayTime, m.IsBaby())
	if act == actWork && !m.memHere(vb.jobSite) || act == actMeet && !m.memHere(vb.meetingPoint) {
		act = actIdle
	}
	vb.activity = act
}

// memHere reports whether the memory is a position in the mob's dimension.
func (m *Mob) memHere(g *GlobalPos) bool {
	return g != nil && m.Village != nil && g.Dim == m.Village.Dimension()
}

// nearCenter is BlockPos.closerToCenterThan on the mob's position.
func (m *Mob) nearCenter(p Waypoint, dist float64) bool {
	dx, dy, dz := float64(p.X)+0.5-m.X, float64(p.Y)+0.5-m.Y, float64(p.Z)+0.5-m.Z
	return dx*dx+dy*dy+dz*dz < dist*dist
}

func manhattan3(a, b Waypoint) int32 {
	return max(a.X-b.X, b.X-a.X) + max(a.Y-b.Y, b.Y-a.Y) + max(a.Z-b.Z, b.Z-a.Z)
}

// poiTickets is how many villagers a point of interest holds.
// vanilla: PoiTypes.bootstrap
func poiTickets(k rules.POIKind) int {
	if k == rules.POIMeeting {
		return 32
	}
	return 1
}

// owns reports whether any of the villager's POI memories is at the position.
func (v *villagerMemory) owns(dim string, p Waypoint) bool {
	for owned := range v.claims(dim) {
		if owned == p {
			return true
		}
	}
	return false
}

func (v *villagerMemory) claims(dim string) iter.Seq[Waypoint] {
	return func(yield func(Waypoint) bool) {
		for _, g := range [...]*GlobalPos{v.home, v.jobSite, v.potentialJobSite, v.meetingPoint, v.childHome} {
			if g != nil && g.Dim == dim && !yield(g.at()) {
				return
			}
		}
	}
}

// VillageClaims yields the live villager's POI ticket reservations in dim.
func (m *Mob) VillageClaims(dim string) iter.Seq[Waypoint] {
	return func(yield func(Waypoint) bool) {
		if m.Type == TypeVillager && !m.Dead {
			m.vb.claims(dim)(yield)
		}
	}
}

// poiTaken counts the villagers owning the position: the mob and the herd's.
// A POI owned by a villager in an unloaded chunk counts as free;
// competitorScan settles the clash when that villager loads again.
func (m *Mob) poiTaken(t *tickView, p Waypoint) int {
	dim := m.Village.Dimension()
	n := 0
	if m.vb.owns(dim, p) {
		n++
	}
	for _, o := range t.others {
		if o.Type == TypeVillager && !o.Dead && o.vb.owns(dim, p) {
			n++
		}
	}
	return n
}

// acquirableJobSite and heldJobSite are the job kinds a profession takes a
// site of and keeps one of.
// vanilla: VillagerProfession.acquirableJobSite, heldJobSite
func acquirableJobSite(profession string, k rules.POIKind) bool {
	switch profession {
	case "minecraft:none":
		return rules.JobSiteProfession(k) != ""
	case "minecraft:nitwit":
		return false
	}
	return rules.ProfessionJobSite(profession) == k
}

func heldJobSite(profession string, k rules.POIKind) bool {
	switch profession {
	case "minecraft:none", "minecraft:nitwit":
		return false
	}
	return rules.ProfessionJobSite(profession) == k
}

func isHome(k rules.POIKind) bool    { return k == rules.POIHome }
func isMeeting(k rules.POIKind) bool { return k == rules.POIMeeting }

// poiRetry is AcquirePoi.JitteredLinearRetry: a candidate that had no path
// is tried again after a delay growing by 40-80 ticks, up to 400.
type poiRetry struct{ prev, next, delay int64 }

func (r *poiRetry) mark(m *Mob, now int64) {
	r.prev = now
	r.delay = min(r.delay+int64(m.Rand.Intn(40))+40, 400)
	r.next = now + r.delay
}

// poiAcquire is one AcquirePoi's schedule and retry markers.
type poiAcquire struct {
	next  int64
	retry map[Waypoint]*poiRetry
}

// AcquirePoi's scan radius, candidates tried, base rate and the path reach
// of a bell. vanilla: AcquirePoi, PoiTypes.bootstrap validRange
const (
	acquireScan  = 48
	acquireBatch = 5
	acquireRate  = 20
	bellReach    = 6
)

// acquirePoi stores the closest reachable POI of the kind in dst.
// vanilla: AcquirePoi.create
func (m *Mob) acquirePoi(t *tickView, a *poiAcquire, match func(rules.POIKind) bool, dst **GlobalPos,
	adultOnly bool, valid func(Waypoint) bool, reach int32, happy bool) {
	now := m.GameTime
	if adultOnly && m.IsBaby() {
		return
	}
	if a.next == 0 {
		a.next = now + int64(m.Rand.Intn(acquireRate))
		return
	}
	if now < a.next {
		return
	}
	a.next = now + acquireRate + int64(m.Rand.Intn(acquireRate))
	for p, r := range a.retry {
		if now-r.prev >= 400 {
			delete(a.retry, p)
		}
	}
	c := m.cell()
	var picks []Waypoint
	for _, p := range m.Village.POIs(match, c.X, c.Y, c.Z, acquireScan) {
		if len(picks) == acquireBatch {
			break
		}
		kind := rules.POIKindOf(t.w.BlockAt(p.X, p.Y, p.Z))
		if !match(kind) {
			continue
		}
		if m.poiTaken(t, p) >= poiTickets(kind) {
			continue
		}
		if r := a.retry[p]; r != nil {
			if now < r.next {
				continue
			}
			r.mark(m, now)
		}
		picks = append(picks, p)
	}
	picks = slices.DeleteFunc(picks, func(p Waypoint) bool { return !valid(p) })
	for _, p := range picks {
		if m.pathReaches(t.w, p, reach) {
			*dst = &GlobalPos{Dim: m.Village.Dimension(), X: p.X, Y: p.Y, Z: p.Z}
			if happy {
				t.events = append(t.events, MobEvent{Mob: m, Kind: MobStatus, Status: protocol.EntityStatusVillagerHappy})
			}
			a.retry = nil
			return
		}
	}
	for _, p := range picks {
		if a.retry == nil {
			a.retry = map[Waypoint]*poiRetry{}
		}
		if a.retry[p] == nil {
			r := &poiRetry{}
			r.mark(m, now)
			a.retry[p] = r
		}
	}
}

// pathReaches reports whether a path from the mob gets within reach of the
// block.
func (m *Mob) pathReaches(w World, p Waypoint, reach int32) bool {
	foot := m.pathFootprint(w)
	_, reached := findPath(w, foot, m.cell(), p, int32(m.maxPathLength()), reach)
	return reached
}

// villagerBeds is the tag of beds a villager sleeps on.
var villagerBeds = registry.BlockTagOf("minecraft:villagers_can_sleep_on_bed")

// validBed is the home's AcquirePoi test: a bed nobody lies in.
// vanilla: VillagerGoalPackages.validateBedPoi
func validBed(w World, p Waypoint) bool {
	s := w.BlockAt(p.X, p.Y, p.Z)
	return registry.BlockOf(s).InTag(villagerBeds) && !registry.StateBool(s, registry.PropOccupied)
}

// villagerBrainStep is the core package of the villager's brain, which runs
// in every activity: the schedule, waking, the validation and acquisition of
// POIs, the competitor scan, and gaining and losing a profession.
// vanilla: VillagerGoalPackages.getCorePackage
func (m *Mob) villagerBrainStep(t *tickView) {
	// vanilla: Villager.customServerAiStep clears this after the first brain tick.
	defer func() { m.vb.assignProfessionWhenSpawned = false }()
	m.villagerDangerStep(t)
	m.villagerRaidStep(t)
	m.villagerKidsStep(t)
	m.farmlandStep(t)
	// vanilla: Villager.customServerAiStep rolls before looking up the raid.
	if m.Rand.Intn(100) == 0 && m.raidWalk.view.Has && m.raidWalk.view.Active && !m.raidWalk.view.Over {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobStatus, Status: protocol.EntityStatusVillagerSplash})
	}
	if m.Village == nil {
		return
	}
	vb := &m.vb
	m.golemStep(t)
	if m.IsBaby() && m.GameTime >= vb.bedSensor.next {
		vb.nearestBed = nil
		if p, ok := vb.bedSensor.find(m, t.w, false); ok {
			vb.nearestBed = &p
		}
	}
	m.updateActivity()
	m.wakeStep(t.w)
	m.validatePoi(t, &vb.jobSite, func(k rules.POIKind) bool { return heldJobSite(m.Villager.Profession, k) })
	m.validatePoi(t, &vb.potentialJobSite, func(k rules.POIKind) bool { return acquirableJobSite(m.Villager.Profession, k) })
	if vb.activity == actRest {
		m.validatePoi(t, &vb.home, isHome)
	}
	if vb.activity == actMeet {
		m.validatePoi(t, &vb.meetingPoint, isMeeting)
	}
	m.yieldJobSite(t)
	m.competitorScan(t)
	if vb.jobSite == nil && vb.potentialJobSite == nil && m.Villager.Profession != "minecraft:nitwit" {
		m.acquirePoi(t, &vb.acquire[0], func(k rules.POIKind) bool { return acquirableJobSite(m.Villager.Profession, k) },
			&vb.potentialJobSite, true, func(Waypoint) bool { return true }, 1, false)
	}
	if vb.home == nil {
		m.acquirePoi(t, &vb.acquire[1], isHome, &vb.home, false, func(p Waypoint) bool { return validBed(t.w, p) }, 1, true)
	}
	if vb.meetingPoint == nil {
		m.acquirePoi(t, &vb.acquire[2], isMeeting, &vb.meetingPoint, true, func(Waypoint) bool { return true }, bellReach, true)
	}
	m.assignProfession(t)
	m.resetProfession()
}

// validatePoi erases a memory within 16 blocks whose block is no longer a POI
// of a fitting kind, or a bed someone else lies in, judged on the live block.
// vanilla: ValidateNearbyPoi
func (m *Mob) validatePoi(t *tickView, slot **GlobalPos, fits func(rules.POIKind) bool) {
	g := *slot
	if !m.memHere(g) || !m.nearCenter(g.at(), 16) {
		return
	}
	s := t.w.BlockAt(g.X, g.Y, g.Z)
	if !fits(rules.POIKindOf(s)) {
		*slot = nil
		return
	}
	// vanilla: ValidateNearbyPoi.bedIsOccupied
	if registry.BlockOf(s).InTag(villagerBeds) && registry.StateBool(s, registry.PropOccupied) && !m.Sleeping {
		*slot = nil
	}
}

// competitorScan makes the villager with less experience among those holding
// one job site let it go.
// vanilla: PoiCompetitorScan
func (m *Mob) competitorScan(t *tickView) {
	g := m.vb.jobSite
	if !m.memHere(g) {
		return
	}
	kind := rules.POIKindOf(t.w.BlockAt(g.X, g.Y, g.Z))
	if kind == rules.POINone {
		return
	}
	winner := m
	for _, o := range t.others {
		if o.Type != TypeVillager || o == m || o.Dead || o.vb.jobSite == nil || *o.vb.jobSite != *g ||
			!heldJobSite(o.Villager.Profession, kind) || !nearBox(m, o, 16) {
			continue
		}
		loser, w := winner, o
		if winner.VillagerXp > o.VillagerXp {
			loser, w = o, winner
		}
		loser.vb.jobSite = nil
		winner = w
	}
}

func nearBox(a, b *Mob, r float64) bool {
	return math.Abs(a.X-b.X) <= r && math.Abs(a.Y-b.Y) <= r && math.Abs(a.Z-b.Z) <= r
}

// assignProfession turns the potential job site into the job site once the
// villager stands by it, and a jobless villager into the site's profession.
// vanilla: AssignProfessionFromJobSite
func (m *Mob) assignProfession(t *tickView) {
	vb := &m.vb
	g := vb.potentialJobSite
	if !m.memHere(g) || (!m.nearCenter(g.at(), 2) && !vb.assignProfessionWhenSpawned) {
		return
	}
	vb.potentialJobSite = nil
	vb.jobSite = g
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobStatus, Status: protocol.EntityStatusVillagerHappy})
	if m.Villager.Profession != "minecraft:none" {
		return
	}
	if p := rules.JobSiteProfession(rules.POIKindOf(t.w.BlockAt(g.X, g.Y, g.Z))); p != "" {
		m.setProfession(p)
	}
}

// resetProfession takes the profession from a villager with no job site that
// never traded.
// vanilla: ResetProfession
func (m *Mob) resetProfession() {
	p := m.Villager.Profession
	if m.vb.jobSite == nil && p != "minecraft:none" && p != "minecraft:nitwit" && m.VillagerXp == 0 && m.Villager.Level <= 1 {
		m.setProfession("minecraft:none")
	}
}

// setProfession changes the profession; the offers went with the old one.
// vanilla: Villager.setVillagerData
func (m *Mob) setProfession(p string) {
	if m.Villager.Profession != p {
		if m.Career != nil && !m.Career(m, p) {
			return
		}
		m.Offers = nil
	}
	m.SetMetadataVillagerProfession(p)
}

// startSleeping lies the villager in the bed head and queues the bed's
// occupancy.
// vanilla: LivingEntity.startSleeping
func (m *Mob) startSleeping(bed Waypoint) {
	m.SetMetadataSleeping(true)
	m.vb.sleepPos = bed
	m.InvalidateMetadataVbSleepPos()
	m.X, m.Y, m.Z = float64(bed.X)+0.5, float64(bed.Y)+0.6875, float64(bed.Z)+0.5
	m.VX, m.VY, m.VZ = 0, 0, 0
	m.stopSeeking()
	m.vb.lastSlept = m.GameTime
	m.vb.bedOps = append(m.vb.bedOps, BedOp{X: bed.X, Y: bed.Y, Z: bed.Z, Occupied: true})
}

// WakeSleeper gets the villager out of its bed: it frees the bed and, if the
// bed still stands, steps to the side of it.
// vanilla: LivingEntity.stopSleeping
func (m *Mob) WakeSleeper() {
	if !m.Sleeping || m.Type != TypeVillager {
		return
	}
	m.SetMetadataSleeping(false)
	m.vb.restoredSleep = false
	bed := m.vb.sleepPos
	m.vb.lastWoken = m.GameTime
	if m.Village == nil || !m.Village.IsBed(bed.X, bed.Y, bed.Z) {
		return
	}
	m.vb.bedOps = append(m.vb.bedOps, BedOp{X: bed.X, Y: bed.Y, Z: bed.Z})
	m.X, m.Y, m.Z = m.Village.BedStandUp(bed.X, bed.Y, bed.Z, m.Yaw)
	m.Pitch = 0
}

// wakeStep wakes a villager out of REST or out of a bed that is gone.
// vanilla: WakeUp, LivingEntity.tick
func (m *Mob) wakeStep(w World) {
	if !m.Sleeping {
		return
	}
	bed := m.vb.sleepPos
	if m.vb.activity != actRest || !registry.BlockOf(w.BlockAt(bed.X, bed.Y, bed.Z)).InGroup(registry.GroupBed) {
		m.WakeSleeper()
	}
}

// memSlot picks one of the villager's position memories.
type memSlot uint8

const (
	slotHome memSlot = iota
	slotJob
	slotPotential
	slotMeeting
)

func (v *villagerMemory) slot(s memSlot) **GlobalPos {
	switch s {
	case slotHome:
		return &v.home
	case slotJob:
		return &v.jobSite
	case slotPotential:
		return &v.potentialJobSite
	}
	return &v.meetingPoint
}

// sleepingBox is the width and height of a sleeping body's box.
// vanilla: LivingEntity.SLEEPING_DIMENSIONS
const sleepingBox = 0.2

// The activity-gated goals' priorities, between the panic and the stroll.
const (
	priorityPoiWalk  = 2
	prioritySleep    = 3
	priorityWork     = 4
	priorityPoiStrol = 5
)

// poiWalkGoal walks to a position memory until within closeEnough blocks, in
// legs when it is farther than far; a target unreachable for too long is
// forgotten.
// vanilla: SetWalkTargetFromBlockMemory
type poiWalkGoal struct {
	m          *Mob
	slot       memSlot
	act        villagerActivity
	pace       float64
	close, far int32
	giveUp     int64
	// cantReach is the game tick the walk stopped short of progress, 0 for none.
	cantReach int64
	// retryAt is the game tick a walk with no path may be tried again.
	retryAt   int64
	target    Waypoint
	startDist int32
}

func (g *poiWalkGoal) Priority() int           { return priorityPoiWalk }
func (g *poiWalkGoal) Flags() GoalFlags        { return GoalMove }
func (g *poiWalkGoal) RequiresEveryTick() bool { return false }

func (g *poiWalkGoal) CanStart(t *tickView) bool {
	m := g.m
	slot := m.vb.slot(g.slot)
	p := *slot
	if m.vb.activity != g.act || !m.memHere(p) || m.GameTime < g.retryAt {
		return false
	}
	d := manhattan3(p.at(), m.cell())
	if d <= g.close {
		g.cantReach = 0
		return false
	}
	if g.cantReach != 0 && m.GameTime-g.cantReach > g.giveUp {
		*slot, g.cantReach = nil, m.GameTime
		return false
	}
	switch {
	case d > g.far:
		for range 1000 { // vanilla: SetWalkTargetFromBlockMemory.MAX_TRIES
			spot, ok := posTowardsWalkable(t.w, m, 15, 7, [2]float64{float64(p.X) + 0.5, float64(p.Z) + 0.5}, math.Pi/2)
			if ok && manhattan3(spot, m.cell()) <= g.far {
				g.target = spot
				return true
			}
		}
		*slot, g.cantReach = nil, m.GameTime
		return false
	case d > g.close:
		g.target = p.at()
		return true
	}
	return false
}

func (g *poiWalkGoal) Start(t *tickView) {
	m := g.m
	g.startDist = manhattan3(g.target, m.cell())
	if !m.seekTo(t.w, float64(g.target.X)+0.5, float64(g.target.Y), float64(g.target.Z)+0.5, g.pace, navReach) {
		g.retryAt = m.GameTime + 20
		if g.cantReach == 0 {
			g.cantReach = m.GameTime
		}
	}
}

func (g *poiWalkGoal) ShouldContinue(*tickView) bool { return len(g.m.navOf().nav.path) > 0 }

func (g *poiWalkGoal) Tick(*tickView) {
	m := g.m
	if manhattan3(g.target, m.cell()) <= g.close {
		m.stopSeeking()
	}
}

func (g *poiWalkGoal) Stop(*tickView) {
	m := g.m
	m.stopSeeking()
	if d := manhattan3(g.target, m.cell()); d <= g.close || d < g.startDist {
		g.cantReach = 0
	} else if g.cantReach == 0 {
		g.cantReach = m.GameTime
	}
}

// potentialJobGoal walks to the potential job site for up to 1200 ticks and
// forgets it after.
// vanilla: GoToPotentialJobSite
type potentialJobGoal struct {
	m       *Mob
	started int64
}

func (g *potentialJobGoal) Priority() int           { return priorityPoiWalk }
func (g *potentialJobGoal) Flags() GoalFlags        { return GoalMove }
func (g *potentialJobGoal) RequiresEveryTick() bool { return false }

func (g *potentialJobGoal) CanStart(*tickView) bool {
	m := g.m
	return (m.vb.activity == actIdle || m.vb.activity == actWork) && m.memHere(m.vb.potentialJobSite)
}

func (g *potentialJobGoal) Start(*tickView) { g.started = g.m.GameTime }

func (g *potentialJobGoal) ShouldContinue(*tickView) bool {
	return g.m.memHere(g.m.vb.potentialJobSite) && g.m.GameTime-g.started < 1200
}

func (g *potentialJobGoal) Tick(t *tickView) {
	m := g.m
	if p := m.vb.potentialJobSite; p != nil && !m.nearCenter(p.at(), 1) {
		m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, villagerWalkPace, navReach)
	}
}

func (g *potentialJobGoal) Stop(*tickView) {
	g.m.vb.potentialJobSite = nil
	g.m.stopSeeking()
}

// workAtPoiGoal stands at the job site for 60 ticks, restocking at its start.
// The restock waits while a window is open, since closing it would write its
// offers back over the new ones.
// vanilla: WorkAtPoi
type workAtPoiGoal struct {
	m         *Mob
	lastCheck int64
	left      int
}

func (g *workAtPoiGoal) Priority() int           { return priorityWork }
func (g *workAtPoiGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *workAtPoiGoal) RequiresEveryTick() bool { return false }

func (g *workAtPoiGoal) CanStart(*tickView) bool {
	m := g.m
	if m.vb.activity != actWork || !m.memHere(m.vb.jobSite) || m.GameTime-g.lastCheck < 300 || m.Rand.Intn(2) != 0 {
		return false
	}
	g.lastCheck = m.GameTime
	return m.nearCenter(m.vb.jobSite.at(), 1.73)
}

func (g *workAtPoiGoal) Start(t *tickView) {
	m := g.m
	g.left = 60
	m.vb.lastWorkedAtPoi = m.GameTime
	if _, profession, ok := m.Villager.IDs(); ok {
		if sound := villagerWorkSounds[profession]; sound != registry.SoundNone {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound,
				Volume: m.SoundVolume(), Pitch: m.VoicePitch()})
		}
	}
	if m.Villager.Profession == "minecraft:farmer" {
		m.farmEvent(t, FarmCompost, m.vb.jobSite.at())
	}
	// Restocks even with a window open, which is then sent the new offers.
	// vanilla: WorkAtPoi.start
	if m.shouldRestock() {
		m.restock()
	}
	if m.vb.offersChanged {
		m.vb.offersChanged = false
		if m.Trading {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobOffersChanged})
		}
	}
}

func (g *workAtPoiGoal) ShouldContinue(*tickView) bool {
	m := g.m
	return g.left > 0 && m.memHere(m.vb.jobSite) && m.nearCenter(m.vb.jobSite.at(), 1.73)
}

func (g *workAtPoiGoal) Tick(*tickView) { g.left-- }
func (g *workAtPoiGoal) Stop(*tickView) {}

func cutPrefix(id string) (string, bool) {
	const p = "minecraft:"
	if len(id) > len(p) && id[:len(p)] == p {
		return id[len(p):], true
	}
	return id, false
}

// poiStrollGoal is StrollAroundPoi (a land position near the mob, while
// within maxDist of the POI) or, with toPoi, StrollToPoi (the POI itself).
// reach is the walk target's close-enough distance in blocks.
// vanilla: StrollAroundPoi, StrollToPoi
type poiStrollGoal struct {
	m        *Mob
	poi      func(t *tickView) (Waypoint, bool)
	pace     float64
	reach    int32
	maxDist  float64
	cooldown int64
	toPoi    bool
	nextOk   int64
	target   Waypoint
}

// villagerPoi is the villager's POI in slot while its activity is act.
func villagerPoi(m *Mob, slot memSlot, act villagerActivity) func(*tickView) (Waypoint, bool) {
	return func(*tickView) (Waypoint, bool) {
		p := *m.vb.slot(slot)
		if m.vb.activity != act || !m.memHere(p) {
			return Waypoint{}, false
		}
		return p.at(), true
	}
}

func (g *poiStrollGoal) Priority() int           { return priorityPoiStrol }
func (g *poiStrollGoal) Flags() GoalFlags        { return GoalMove }
func (g *poiStrollGoal) RequiresEveryTick() bool { return false }

func (g *poiStrollGoal) CanStart(t *tickView) bool {
	m := g.m
	p, ok := g.poi(t)
	if !ok || !m.nearCenter(p, g.maxDist) || m.GameTime <= g.nextOk {
		return false
	}
	g.nextOk = m.GameTime + g.cooldown
	if g.toPoi {
		g.target = p
		return true
	}
	g.target, ok = landRandomPos(t.w, m, 8, 6)
	return ok
}

func (g *poiStrollGoal) Start(t *tickView) {
	g.m.seekTo(t.w, float64(g.target.X)+0.5, float64(g.target.Y), float64(g.target.Z)+0.5, g.pace, g.reach)
}

func (g *poiStrollGoal) ShouldContinue(*tickView) bool { return len(g.m.navOf().nav.path) > 0 }
func (g *poiStrollGoal) Tick(*tickView)                {}
func (g *poiStrollGoal) Stop(*tickView)                { g.m.stopSeeking() }

// sleepInBedGoal lies the villager in its home bed once it stands by it.
// vanilla: SleepInBed
type sleepInBedGoal struct {
	m      *Mob
	nextOk int64
}

func (g *sleepInBedGoal) Priority() int           { return prioritySleep }
func (g *sleepInBedGoal) Flags() GoalFlags        { return GoalMove | GoalLook | GoalJump }
func (g *sleepInBedGoal) RequiresEveryTick() bool { return false }

func (g *sleepInBedGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.vb.restoredSleep {
		return m.Sleeping && m.vb.activity == actRest
	}
	h := m.vb.home
	if m.vb.activity != actRest || m.Sleeping || m.RidesVehicle || !m.memHere(h) || m.GameTime <= g.nextOk {
		return false
	}
	if since := m.GameTime - m.vb.lastWoken; m.vb.lastWoken != 0 && since > 0 && since < 100 {
		return false
	}
	return m.nearCenter(h.at(), 2) && validBed(t.w, h.at())
}

func (g *sleepInBedGoal) Start(t *tickView) {
	if g.m.vb.restoredSleep {
		g.m.vb.restoredSleep = false
		return
	}
	// vanilla: SleepInBed.start (InteractWithDoor.closeDoorsThatIHaveOpenedOrPassedThrough)
	if d := g.m.vb.doors; d != nil {
		d.closeBehind(t, nil, nil)
	}
	g.m.startSleeping(g.m.vb.home.at())
}

func (g *sleepInBedGoal) ShouldContinue(*tickView) bool {
	m := g.m
	h := m.vb.home
	return m.Sleeping && m.vb.activity == actRest && m.memHere(h) && m.Y > float64(h.Y)+0.4 && m.nearCenter(h.at(), 1.14)
}

func (g *sleepInBedGoal) Tick(*tickView) {}

func (g *sleepInBedGoal) Stop(*tickView) {
	if g.m.Sleeping {
		g.m.WakeSleeper()
		g.nextOk = g.m.GameTime + 40
	}
}

// The restock timings. vanilla: Villager.shouldRestock, allowedToRestock
const (
	restockHalfDay   = 12000
	restockGap       = 2400
	restocksPerDay   = 2
	ticksPerDay      = 24000
	defaultOfferUses = 4
)

// needsRestock reports whether any recipe was used.
// vanilla: MerchantOffer.needsRestock
func (m *Mob) needsRestock() bool {
	recipes, _ := nbt.Get[nbt.List](m.Offers, "Recipes")
	for _, it := range recipes.Items {
		if r, ok := nbt.CompoundView(it); ok {
			if uses, _ := nbt.IntValue(r["uses"]); uses > 0 {
				return true
			}
		}
	}
	return false
}

// restock moves each recipe's demand by its use and refills it.
// vanilla: Villager.restock, MerchantOffer.updateDemand
func (m *Mob) restock() {
	if m.OfferUpdater != nil {
		m.Offers = m.OfferUpdater.UpdateOfferState(m.Offers, func(uses, maxUses, demand int32) (int32, int32) {
			return 0, demand + uses - (maxUses - uses)
		})
	}
	m.vb.lastRestock = m.GameTime
	m.vb.restocksToday++
	m.vb.offersChanged = true
}

// catchUpDemand applies the demand updates of the restocks a day missed,
// resetting the uses first.
// vanilla: Villager.catchUpDemand
func (m *Mob) catchUpDemand() {
	missed := restocksPerDay - m.vb.restocksToday
	if missed <= 0 {
		return
	}
	if m.OfferUpdater != nil {
		m.Offers = m.OfferUpdater.UpdateOfferState(m.Offers, func(_, maxUses, demand int32) (int32, int32) {
			return 0, demand - missed*maxUses
		})
	}
	m.vb.offersChanged = true
}

// shouldRestock is Villager.shouldRestock: a new day (half a day since the
// last restock, or the day counter turned) resets the day's restocks, and a
// villager restocks at most twice a day, 2400 ticks apart.
func (m *Mob) shouldRestock() bool {
	vb := &m.vb
	day := m.DayTime / ticksPerDay
	turned := vb.lastRestockCheckDay > 0 && day > vb.lastRestockCheckDay
	vb.lastRestockCheckDay = day
	if m.GameTime > vb.lastRestock+restockHalfDay || turned {
		vb.lastRestock = m.GameTime
		m.catchUpDemand()
		vb.restocksToday = 0
	}
	allowed := vb.restocksToday == 0 || vb.restocksToday < restocksPerDay && m.GameTime > vb.lastRestock+restockGap
	return allowed && m.needsRestock()
}

// The Brain keys of the memories modelled here.
// vanilla: MemoryModuleType, Brain.Packed.CODEC
const (
	memHomeKey      = "minecraft:home"
	memJobKey       = "minecraft:job_site"
	memPotentialKey = "minecraft:potential_job_site"
	memMeetingKey   = "minecraft:meeting_point"
	memSleptKey     = "minecraft:last_slept"
	memWokenKey     = "minecraft:last_woken"
	memWorkedKey    = "minecraft:last_worked_at_poi"
)

// saveBrain writes the villager's Brain: its memories, and those it kept
// unread, as ExpirableValue {value}.
func (m *Mob) saveBrain(out nbt.CompoundTag) {
	vb := &m.vb
	mems := m.cloneMemoryMap(vb.brainRest)
	for _, entry := range []struct {
		key string
		pos *GlobalPos
	}{
		{memHomeKey, vb.home}, {memJobKey, vb.jobSite},
		{memPotentialKey, vb.potentialJobSite}, {memMeetingKey, vb.meetingPoint},
	} {
		putBrainGlobalPos(mems, entry.key, entry.pos)
	}
	for _, entry := range []struct {
		key   string
		value int64
	}{
		{memSleptKey, vb.lastSlept}, {memWokenKey, vb.lastWoken}, {memWorkedKey, vb.lastWorkedAtPoi},
	} {
		if entry.value != 0 {
			mems.Set(entry.key, memoryTag(nbt.Long(entry.value)))
		}
	}
	out.Set("Brain", brainTag(mems))
}

// loadBrain reads the villager's Brain and restock counters.
func (m *Mob) loadBrain(c nbt.Compound) {
	vb := &m.vb
	vb.lastRestock = 0
	if v, ok := nbt.LongValue(c["LastRestock"]); ok {
		vb.lastRestock = v
	}
	vb.restocksToday, _ = nbt.IntField(c, "RestocksToday")
	m.loadGossip(c)
	brain, _ := nbt.Get[nbt.Compound](c, "Brain")
	vb.brainRest = m.loadMemoryMap(brain["memories"])
	for _, k := range slices.Clone(vb.brainRest.keys) {
		tag := vb.brainRest.entries[k]
		value := func() (nbt.Tag, bool) {
			e, ok := nbt.CompoundView(tag)
			if !ok {
				return nil, false
			}
			v, ok := e["value"]
			return v, ok
		}
		pos := func() *GlobalPos {
			v, _ := value()
			c, ok := nbt.CompoundView(v)
			if !ok {
				return nil
			}
			dim, ok := nbt.Get[nbt.String](c, "dimension")
			p, ok2 := nbt.Get[nbt.IntArray](c, "pos")
			if !ok || !ok2 || len(p) != 3 {
				return nil
			}
			return &GlobalPos{Dim: string(dim), X: p[0], Y: p[1], Z: p[2]}
		}
		long := func() int64 { v, _ := value(); n, _ := nbt.LongValue(v); return n }
		switch k {
		case memHomeKey:
			vb.home = pos()
		case memJobKey:
			vb.jobSite = pos()
		case memPotentialKey:
			vb.potentialJobSite = pos()
		case memMeetingKey:
			vb.meetingPoint = pos()
		case memSleptKey:
			vb.lastSlept = long()
		case memWokenKey:
			vb.lastWoken = long()
		case memWorkedKey:
			vb.lastWorkedAtPoi = long()
		default:
			continue
		}
		vb.brainRest.Delete(k)
	}
}

// loadSleepingPos restores a sleeper in its bed without moving it or queuing a
// BedOp: the saved Pos is the bed and the block is already occupied.
// vanilla: LivingEntity.readAdditionalSaveData
func (m *Mob) loadSleepingPos(compound nbt.Compound) {
	if pos, ok := nbt.Get[nbt.IntArray](compound, "sleeping_pos"); ok && len(pos) == 3 {
		m.SetMetadataSleeping(true)
		m.vb.sleepPos = Waypoint{pos[0], pos[1], pos[2]}
		m.InvalidateMetadataVbSleepPos()
		m.vb.restoredSleep = true
	}
}

// Indexed by VillagerData.IDs profession, including the two professions without work.
var villagerWorkSounds = [...]registry.Sound{
	registry.SoundNone,
	registry.SoundEntityVillagerWorkArmorer,
	registry.SoundEntityVillagerWorkButcher,
	registry.SoundEntityVillagerWorkCartographer,
	registry.SoundEntityVillagerWorkCleric,
	registry.SoundEntityVillagerWorkFarmer,
	registry.SoundEntityVillagerWorkFisherman,
	registry.SoundEntityVillagerWorkFletcher,
	registry.SoundEntityVillagerWorkLeatherworker,
	registry.SoundEntityVillagerWorkLibrarian,
	registry.SoundEntityVillagerWorkMason,
	registry.SoundNone,
	registry.SoundEntityVillagerWorkShepherd,
	registry.SoundEntityVillagerWorkToolsmith,
	registry.SoundEntityVillagerWorkWeaponsmith,
}
