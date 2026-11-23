package entity

import "math"

// The raider's pursuit of a dropped captain's banner.
// vanilla: Raider.ObtainRaidLeaderBannerGoal

// The goal's figures.
// vanilla: Raider.ObtainRaidLeaderBannerGoal
const (
	bannerPace  = 1.15
	bannerReach = 1.414 // Entity.closerThan, between the feet positions
	// bannerRetry is how long a banner that could not be reached is left alone.
	bannerRetry = 600
	// bannerRise is the vertical reach of the search box, which the follow
	// range gives sideways.
	bannerRise = 8.0
	// bannerHalf is the half width of an item entity's box.
	bannerHalf = 0.125
)

// BannerItem is a dropped ominous banner as the raiders' search sees it.
type BannerItem struct {
	ID      int32
	X, Y, Z float64
}

// bannerSeek is what a raider keeps of its search for a banner.
type bannerSeek struct {
	// wanted is set from outside each tick: the raid runs, the mob can lead,
	// its wave has no living captain and it wears no banner.
	wanted bool
	items  []BannerItem
	clock  int64
	// unreachable maps an item to the clock it is looked at again from.
	unreachable map[int32]int64
	pursued     BannerItem
	chasing     bool
	// grab is the banner the goal came within reach of, for the server to
	// claim; 0 for none.
	grab int32
}

// SetBannerSearch tells a raider, once a tick, whether it may take a captain's
// banner and the banners lying about. The clock counts these calls.
func (m *Mob) SetBannerSearch(wanted bool, items []BannerItem) {
	m.banner.wanted, m.banner.items = wanted, items
	m.banner.clock++
}

// BannerGrabbing reports whether the raider reached a banner it has not taken.
func (m *Mob) BannerGrabbing() bool { return m.banner.grab != 0 }

// CaptainBannerPickupEnabled reports whether ordinary loot pickup may seek a captain's banner.
func (m *Mob) CaptainBannerPickupEnabled() bool {
	return m.banner.wanted && m.LootPickupEnabled()
}

// CanPickUpCaptainBanner reports whether ordinary loot pickup can claim a
// captain's banner here, independently of the navigation goal.
// vanilla: Raider.pickUpItem, Mob.aiStep
func (m *Mob) CanPickUpCaptainBanner(loot LootItem) bool {
	return m.CaptainBannerPickupEnabled() && lootWithinReach(lootPickupBox(m), &loot)
}

// BannerGrab returns the banner the raider reached since the last call.
func (m *Mob) BannerGrab() (id int32, ok bool) {
	id, m.banner.grab = m.banner.grab, 0
	return id, id != 0
}

// obtainBannerGoal walks to a reachable banner in the follow range and picks
// it up on arrival.
// vanilla: Raider.ObtainRaidLeaderBannerGoal
type obtainBannerGoal struct {
	m    *Mob
	path []Waypoint
	end  Waypoint
}

func newObtainBannerGoal(m *Mob) *obtainBannerGoal { return &obtainBannerGoal{m: m} }

func (g *obtainBannerGoal) Priority() int           { return 1 }
func (g *obtainBannerGoal) Flags() GoalFlags        { return GoalMove }
func (g *obtainBannerGoal) RequiresEveryTick() bool { return false }

// inRange reports whether the banner's box meets the mob's box grown by the
// follow range sideways and eight blocks up and down.
func (g *obtainBannerGoal) inRange(it BannerItem) bool {
	box, reach := g.m.mobBox(), g.m.followRange()
	return box.minX-reach < it.X+bannerHalf && box.maxX+reach > it.X-bannerHalf &&
		box.minY-bannerRise < it.Y+bannerHalf && box.maxY+bannerRise > it.Y-bannerHalf &&
		box.minZ-reach < it.Z+bannerHalf && box.maxZ+reach > it.Z-bannerHalf
}

func (g *obtainBannerGoal) CanStart(t *tickView) bool {
	b := &g.m.banner
	if !b.wanted {
		return false
	}
	skipped := map[int32]int64{}
	for _, it := range b.items {
		if !g.inRange(it) {
			continue
		}
		if until, ok := b.unreachable[it.ID]; ok && b.clock < until {
			skipped[it.ID] = until
			continue
		}
		if path, end, reached := g.m.createReachingPath(t.w, it.X, it.Y, it.Z); reached {
			g.path, g.end = path, end
			b.pursued, b.chasing = it, true
			return true
		}
		skipped[it.ID] = b.clock + bannerRetry
	}
	b.unreachable = skipped
	return false
}

func (g *obtainBannerGoal) Start(*tickView) { g.m.moveTo(g.path, g.end, bannerPace) }

// ShouldContinue holds while the banner still lies there and the walk is not
// over.
func (g *obtainBannerGoal) ShouldContinue(*tickView) bool {
	b := &g.m.banner
	return b.chasing && b.wanted && len(g.m.navOf().nav.path) > 0 && slicesHasBanner(b.items, b.pursued.ID)
}

func (g *obtainBannerGoal) Stop(*tickView) {
	g.m.banner.chasing = false
	g.path = nil
}

func (g *obtainBannerGoal) Tick(*tickView) {
	b := &g.m.banner
	if b.chasing && Dist2(g.m.X, g.m.Y, g.m.Z, b.pursued.X, b.pursued.Y, b.pursued.Z) < bannerReach*bannerReach {
		b.grab = b.pursued.ID
	}
}

func slicesHasBanner(items []BannerItem, id int32) bool {
	for _, it := range items {
		if it.ID == id {
			return true
		}
	}
	return false
}

// createReachingPath is createPath that also says whether the path gets
// there.
// vanilla: PathNavigation.createPath, Path.canReach
func (m *Mob) createReachingPath(w World, x, y, z float64) (path []Waypoint, end Waypoint, reached bool) {
	o := m.navOf()
	if !o.canUpdatePath() || o.Y < float64(w.MinY()) {
		return nil, Waypoint{}, false
	}
	foot := o.pathFootprint(w)
	target, ok := walkableNear(w, foot, x, y, z)
	if !ok {
		return nil, Waypoint{}, false
	}
	path, reached = findPath(w, foot, o.cell(), target, int32(math.Ceil(o.maxPathLength())), navReach)
	return o.trimForSun(w, raiseCauldronNodes(w, path)), cellAt(x, y, z), reached
}
