package entity

import (
	"math"
	"sync"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Water-bound navigation: the path search through water and the node walk
// fish steer by.
// vanilla: WaterBoundPathNavigation, SwimNodeEvaluator, PathFinder

const (
	// swimHeuristicWeight is PathFinder.FUDGING, the weight on the remaining
	// distance.
	swimHeuristicWeight = 1.5
	// swimVerticalReach is WaterBoundPathNavigation.getMaxVerticalDistanceToWaypoint.
	swimVerticalReach = 0.5
	// swimVisitedPerRange is the nodes opened per block of follow range.
	swimVisitedPerRange = 16
	// swimDenseMaxRange matches pathScratch.prepare's 33^3 dense box cap.
	swimDenseMaxRange = 16
)

// swimNode is one cell of a search, PathFinder's Node.
type swimNode struct {
	cell     Waypoint
	epoch    uint32
	g        float64
	walked   float64
	from     *swimNode
	inOpen   bool
	closed   bool
	accepted bool // the cell's water test has run
	valid    bool // the footprint from the cell is all water
}

// swimEntry is a node queued at its score then; a requeue at a better score
// leaves the old entry to be skipped. Equal scores pop in queue order.
type swimEntry struct {
	node  *swimNode
	f     float64
	order int
}

type swimOpen []swimEntry

func (h swimOpen) less(i, j int) bool {
	return h[i].f < h[j].f || h[i].f == h[j].f && h[i].order < h[j].order
}

func (h *swimOpen) push(e swimEntry) {
	*h = append(*h, e)
	s := *h
	i := len(s) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if !s.less(i, parent) {
			break
		}
		s[i], s[parent] = s[parent], s[i]
		i = parent
	}
}

func (h *swimOpen) pop() swimEntry {
	s := *h
	top, n := s[0], len(s)-1
	s[0] = s[n]
	s[n] = swimEntry{}
	s = s[:n]
	for i := 0; ; {
		child := 2*i + 1
		if child >= n {
			break
		}
		if child+1 < n && s.less(child+1, child) {
			child++
		}
		if !s.less(child, i) {
			break
		}
		s[i], s[child] = s[child], s[i]
		i = child
	}
	*h = s
	return top
}

// swimScratch is the working memory of one water-bound search, reset between
// searches. Searches within swimDenseMaxRange use an epoch-stamped dense grid;
// larger ranges fall back to a map.
type swimScratch struct {
	cells  []swimNode
	nodes  map[Waypoint]*swimNode
	origin Waypoint
	side   int32
	epoch  uint32
	open   swimOpen
}

var swimScratchPool = sync.Pool{New: func() any { return new(swimScratch) }}

// prepare sizes the dense grid for a box of radius maxRange around start, or
// leaves side 0 to fall back to the map when maxRange exceeds swimDenseMaxRange.
func (s *swimScratch) prepare(start Waypoint, maxRange int32) {
	s.side = 0
	s.open = s.open[:0]
	if maxRange < 0 || maxRange > swimDenseMaxRange {
		if s.nodes == nil {
			s.nodes = make(map[Waypoint]*swimNode)
		}
		return
	}
	s.side = 2*maxRange + 1
	s.origin = Waypoint{start.X - maxRange, start.Y - maxRange, start.Z - maxRange}
	n := int(s.side * s.side * s.side)
	if len(s.cells) < n {
		s.cells = make([]swimNode, n)
	}
	s.epoch++
	if s.epoch == 0 {
		clear(s.cells)
		s.epoch = 1
	}
}

func (s *swimScratch) nodeAt(c Waypoint) *swimNode {
	if s.side != 0 {
		x, y, z := uint32(c.X-s.origin.X), uint32(c.Y-s.origin.Y), uint32(c.Z-s.origin.Z)
		side := uint32(s.side)
		if max(x, y, z) < side {
			n := &s.cells[(x*side+y)*side+z]
			if n.epoch != s.epoch {
				*n = swimNode{cell: c, epoch: s.epoch}
			}
			return n
		}
		if s.nodes == nil {
			s.nodes = make(map[Waypoint]*swimNode)
		}
	}
	n, ok := s.nodes[c]
	if !ok {
		n = &swimNode{cell: c}
		s.nodes[c] = n
	}
	return n
}

// accepted is SwimNodeEvaluator.findAcceptedNode: a node only for water.
func (s *swimScratch) accepted(w World, ew, eh int32, c Waypoint, breach bool) *swimNode {
	n := s.nodeAt(c)
	if !n.accepted {
		n.accepted = true
		n.valid = swimCellOpen(w, ew, eh, c.X, c.Y, c.Z)
		if breach && rules.IsAir(w.BlockAt(c.X, c.Y, c.Z)) && waterPathfindable(w.BlockAt(c.X, c.Y-1, c.Z)) {
			// vanilla: SwimNodeEvaluator.getPathTypeOfMob, BREACH
			n.valid = true
		}
	}
	if !n.valid {
		return nil
	}
	return n
}

func (s *swimScratch) reset() {
	clear(s.nodes)
	clear(s.open)
	s.open = s.open[:0]
}

func (s *swimScratch) fits() bool {
	return len(s.nodes) <= scratchKeep
}

func (s *swimScratch) release() {
	keep := s.fits()
	s.reset()
	if keep {
		swimScratchPool.Put(s)
	}
}

func (p *MobPathScratch) findSwimPath(w World, ew, eh int32, start, target Waypoint, reach int32, maxLength float64, maxVisited int, breach bool) []Waypoint {
	if p == nil {
		s := swimScratchPool.Get().(*swimScratch)
		path := findSwimPathBreaching(s, w, ew, eh, start, target, reach, maxLength, maxVisited, breach)
		s.release()
		return path
	}
	if p.swim == nil {
		p.swim = new(swimScratch)
	}
	s := p.swim
	path := findSwimPathBreaching(s, w, ew, eh, start, target, reach, maxLength, maxVisited, breach)
	if !s.fits() {
		p.swim = &swimScratch{cells: s.cells, epoch: s.epoch}
		return path
	}
	s.reset()
	return path
}

// swimHorizontal is Direction.Plane.HORIZONTAL as (x, z), each followed
// clockwise by the next.
var swimHorizontal = [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

// swimOrthogonal are the six axis steps in the order of Direction.values().
var swimOrthogonal = [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// waterUnpathfindableBlocks marks the waterloggable blocks whose class
// overrides isPathfindable to refuse WATER. A trapdoor, slab, shelf and
// copper golem statue keep "fluid is water".
// vanilla: StairBlock, FenceBlock, CrossCollisionBlock, WallBlock, ChestBlock,
// EnderChestBlock, LanternBlock, ChainBlock, RodBlock, SeaPickleBlock,
// ConduitBlock, DecoratedPotBlock, CampfireBlock, HeavyCoreBlock,
// SpeleothemBlock, SculkSensorBlock, DriedGhastBlock, WallHangingSignBlock
var waterUnpathfindableBlocks = registry.BlocksOf(func(b registry.Block) bool {
	switch b {
	case registry.BlockEnderChest, registry.BlockEndRod, registry.BlockSeaPickle,
		registry.BlockConduit, registry.BlockDecoratedPot, registry.BlockCampfire,
		registry.BlockSoulCampfire, registry.BlockHeavyCore,
		registry.BlockPointedDripstone, registry.BlockSulfurSpike,
		registry.BlockSculkSensor, registry.BlockCalibratedSculkSensor,
		registry.BlockDriedGhast:
		return true
	}
	return b.Type() == registry.BlockTypeWallHangingSign ||
		b.InGroup(registry.GroupStairs|registry.GroupFence|registry.GroupWall|
			registry.GroupBars|registry.GroupChest|registry.GroupLantern|
			registry.GroupChain|registry.GroupLightningRod)
})

// waterPathfindable is BlockBehaviour.isPathfindable(WATER): the block holds
// water and its class does not refuse it.
func waterPathfindable(state uint16) bool {
	if !rules.IsWater(state) {
		return false
	}
	return int(state) >= registry.StateCount || !waterUnpathfindableBlocks[registry.BlockOf(state)]
}

// swimCellOpen is SwimNodeEvaluator.getPathTypeOfMob: every footprint cell
// from the corner is water, and the far corner's block is pathfindable
// through water.
func swimCellOpen(w World, ew, eh, x, y, z int32) bool {
	for dx := range ew {
		for dy := range eh {
			for dz := range ew {
				if !rules.IsWater(w.BlockAt(x+dx, y+dy, z+dz)) {
					return false
				}
			}
		}
	}
	// vanilla checks only the last cell of the loop
	return waterPathfindable(w.BlockAt(x+ew-1, y+eh-1, z+ew-1))
}

// findSwimPath is PathFinder.findPath over SwimNodeEvaluator: A* through
// water, six ways plus the four horizontal diagonals, ending at the node
// nearest the target when none in reach is found. The start is the path's
// first node. maxLength bounds both distance walked and reach from the
// start; maxVisited bounds nodes opened.
// vanilla: PathFinder.findPath
func findSwimPath(w World, ew, eh int32, start, target Waypoint, reach int32, maxLength float64, maxVisited int) []Waypoint {
	return (*MobPathScratch)(nil).findSwimPath(w, ew, eh, start, target, reach, maxLength, maxVisited, false)
}

func findSwimPathBreaching(s *swimScratch, w World, ew, eh int32, start, target Waypoint, reach int32, maxLength float64, maxVisited int, breach bool) []Waypoint {
	maxRange := int32(-1)
	if maxLength <= 0 {
		maxRange = 0
	} else if maxLength <= swimDenseMaxRange {
		maxRange = int32(math.Ceil(maxLength))
	}
	s.prepare(start, maxRange)
	var counter int
	from := s.nodeAt(start)
	best, bestH := from, distanceTo(start, target)
	from.inOpen = true
	s.open.push(swimEntry{node: from, f: bestH})
	for count := 1; len(s.open) > 0 && count < maxVisited; count++ {
		cur := s.open.pop().node
		if cur.closed {
			continue
		}
		cur.inOpen = false
		cur.closed = true
		if manhattan(cur.cell, target) <= reach {
			break
		}
		if distanceTo(cur.cell, start) >= maxLength {
			continue
		}
		var around [6]*swimNode
		var found [10]*swimNode
		n := 0
		for i, d := range swimOrthogonal {
			node := s.accepted(w, ew, eh, Waypoint{cur.cell.X + d[0], cur.cell.Y + d[1], cur.cell.Z + d[2]}, breach)
			around[i] = node
			if node != nil && !node.closed {
				found[n] = node
				n++
			}
		}
		// A diagonal needs both of the cells it cuts between to be water.
		for i, d := range swimHorizontal {
			e := swimHorizontal[(i+1)%4]
			if !swimSideOpen(around, d) || !swimSideOpen(around, e) {
				continue
			}
			node := s.accepted(w, ew, eh, Waypoint{cur.cell.X + d[0] + e[0], cur.cell.Y, cur.cell.Z + d[1] + e[1]}, breach)
			if node != nil && !node.closed {
				found[n] = node
				n++
			}
		}
		for _, node := range found[:n] {
			step := distanceTo(cur.cell, node.cell)
			walked := cur.walked + step
			g := cur.g + step
			if breach && rules.IsAir(w.BlockAt(node.cell.X, node.cell.Y, node.cell.Z)) {
				g += 4 + 8 // vanilla: PathType.BREACH + SwimNodeEvaluator.findAcceptedNode
			}
			if walked >= maxLength || node.inOpen && !(g < node.g) {
				continue
			}
			node.from = cur
			node.g = g
			node.walked = walked
			h := distanceTo(node.cell, target)
			if h < bestH {
				best, bestH = node, h
			}
			counter++
			node.inOpen = true
			s.open.push(swimEntry{node: node, f: g + h*swimHeuristicWeight, order: counter})
		}
	}
	depth := 0
	for n := best; n != nil; n = n.from {
		depth++
	}
	path := make([]Waypoint, depth)
	for n := best; n != nil; n = n.from {
		depth--
		path[depth] = n.cell
	}
	return path
}

// swimSideOpen reports whether the accepted node on a horizontal side exists.
func swimSideOpen(around [6]*swimNode, d [2]int32) bool {
	for i, o := range swimOrthogonal {
		if o[0] == d[0] && o[2] == d[1] && o[1] == 0 {
			return around[i] != nil
		}
	}
	return false
}

// footprintCells is NodeEvaluator.prepare's entityWidth and entityHeight in
// whole cells.
func (m *Mob) footprintCells() (ew, eh int32) {
	return int32(math.Floor(m.width() + 1)), int32(math.Floor(m.height() + 1))
}

// swimStart is SwimNodeEvaluator.getStart: the box's low corner cell, half a
// block above the feet.
func (m *Mob) swimStart() Waypoint {
	return Waypoint{
		int32(math.Floor(m.X - m.width()/2)),
		int32(math.Floor(m.Y + 0.5)),
		int32(math.Floor(m.Z - m.width()/2)),
	}
}

// createSwimPath is PathNavigation.createPath for a water-bound body: nil
// out of liquid or below the world, else a path (possibly to the nearest
// node only). end is the target block later asks are matched against.
// vanilla: PathNavigation.createPath, WaterBoundPathNavigation.canUpdatePath
func (m *Mob) createSwimPath(w World, x, y, z float64, reach int32) (path []Waypoint, end Waypoint, ok bool) {
	o := m.navOf()
	if o.Type != TypeDolphin && !o.InWater() && !o.InLava() || o.Y < float64(w.MinY()) {
		return nil, Waypoint{}, false
	}
	end = cellAt(x, y, z)
	ew, eh := o.footprintCells()
	rng := o.followRange()
	return m.tickPaths.findSwimPath(w, ew, eh, o.swimStart(), end, reach, rng, int(math.Floor(rng*swimVisitedPerRange)), o.Type == TypeDolphin), end, true
}

// SwimTo is swimTo for a caller outside the package.
func (m *Mob) SwimTo(w World, x, y, z, speedFactor float64) bool {
	return m.swimTo(w, x, y, z, speedFactor)
}

// swimTo is PathNavigation.moveTo(x, y, z, speed) with reach 1: a path to
// the same block stands, otherwise a new one replaces it. It reports whether
// a path remains.
// vanilla: PathNavigation.moveTo, PathNavigation.createPath
func (m *Mob) swimTo(w World, x, y, z, speedFactor float64) bool {
	o := m.navOf()
	if len(o.nav.path) > 0 && o.nav.end == cellAt(x, y, z) {
		o.nav.speedFactor = speedFactor
		o.nav.lastStuckCheck = o.nav.navTick
		o.nav.stuckX, o.nav.stuckY, o.nav.stuckZ = o.X, o.Y, o.Z
		return true
	}
	path, end, ok := o.createSwimPath(w, x, y, z, 1)
	if !ok {
		o.nav.path = nil
		return false
	}
	o.moveTo(path, end, speedFactor)
	o.nav.swim = true
	return len(path) > 0
}

// swimNavTick is PathNavigation.tick for a water-bound body: pass the
// reached node, run the stuck watch, and aim the move control at the next.
// vanilla: PathNavigation.tick, PathNavigation.followThePath
func (m *Mob) swimNavTick(w World) { m.swimNavTickAt(w, m.fish.speed) }

// swimNavTickAt is swimNavTick with the speed the stuck watch measures
// progress against.
func (m *Mob) swimNavTickAt(w World, speed float64) {
	n := &m.nav
	n.navTick++
	if len(n.path) == 0 {
		return
	}
	node := n.path[0]
	waterBound := !m.usesAmphibiousNavigation()
	verticalReach := swimVerticalReach
	if !waterBound {
		verticalReach = groundVerticalReach
	}
	reached := m.atWaypoint(m.X-(float64(node.X)+0.5), m.Z-(float64(node.Z)+0.5)) &&
		math.Abs(m.Y-float64(node.Y)) < verticalReach
	if reached || m.cutsPathCorner(w, waterBound) {
		n.advance()
	}
	if len(n.path) == 0 {
		return
	}
	m.stuckStepAt(speed)
	if len(n.path) == 0 {
		return
	}
	n.flyX, n.flyZ = waypointPoint(n.path[0], m.width())
	n.flyY = float64(n.path[0].Y)
}

// swimMovesDirectly is canMoveDirectly of the water-bound and amphibious
// navigations: a collider clip, fluids ignored, from the body to the current
// node lifted by half its height.
// vanilla: WaterBoundPathNavigation.canMoveDirectly, AmphibiousPathNavigation.canMoveDirectly, PathNavigation.isClearForMovementBetween
func (m *Mob) swimMovesDirectly(w World, py float64) bool {
	tx, tz := waypointPoint(m.nav.path[0], m.width())
	ty := float64(m.nav.path[0].Y) + m.height()*0.5
	// Deliberately stricter than vanilla's single ray: the rays of the body's
	// two sides must be clear too, or the box wedges on a wall corner the
	// centre line grazes.
	dx, dz := tx-m.X, tz-m.Z
	l := math.Hypot(dx, dz)
	var sx, sz float64
	if l > 0 {
		sx, sz = -dz/l*m.width()/2, dx/l*m.width()/2
	}
	for _, k := range [...]float64{0, 1, -1} {
		if clipArrowFlight(w, m.X+sx*k, py, m.Z+sz*k, dx, ty-py, dz).ok {
			return false
		}
	}
	return true
}

// swimStrollGoal is RandomSwimmingGoal with the kind's interval and pace 1.
type swimStrollGoal struct {
	m        *Mob
	interval int
}

func (g *swimStrollGoal) Priority() int    { return 4 }
func (g *swimStrollGoal) Flags() GoalFlags { return GoalMove }
func (g *swimStrollGoal) CanStart(t *tickView) bool {
	m := g.m
	if !m.InWater() || len(m.nav.path) > 0 || m.Rand.Intn(g.interval) != 0 {
		return false
	}
	for range 10 {
		x, y, z := m.X+float64(m.Rand.Intn(21)-10), m.Y+float64(m.Rand.Intn(15)-7), m.Z+float64(m.Rand.Intn(21)-10)
		if m.withinHome(cellAt(x, y, z)) && m.swimTo(t.w, x, y, z, 1) {
			return true
		}
	}
	return false
}
func (g *swimStrollGoal) Start(*tickView)               {}
func (g *swimStrollGoal) ShouldContinue(*tickView) bool { return len(g.m.nav.path) > 0 }
func (g *swimStrollGoal) Tick(*tickView)                {}
func (g *swimStrollGoal) Stop(*tickView)                {}
func (g *swimStrollGoal) RequiresEveryTick() bool       { return true }
