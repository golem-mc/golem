package entity

import "math"

// A flyer's flight path: PathFinder over FlyNodeEvaluator. A node is a cell
// the body may hover in, a step goes to any of the 26 neighbours whose
// component cells are accepted too, and the path is followed one waypoint a
// tick by FlyingPathNavigation (flyFollowPath).
// vanilla: FlyingPathNavigation, FlyNodeEvaluator, PathFinder.findPath

const (
	// flyPathWeight is the heuristic's factor, which makes the search greedy.
	// vanilla: PathFinder.FUDGING
	flyPathWeight = 1.5
	// flyNodeAbove is PathNavigation.getMaxVerticalDistanceToWaypoint.
	flyNodeAbove = 1.0
)

// flyMaluses is a flyer's PathType prices as FlyNodeEvaluator meets them;
// a negative price refuses the cell. pathLength is the path's walked length
// bound, and with 16 nodes a block of it the search's budget.
// vanilla: Mob.getPathfindingMalus, PathNavigation.getMaxPathLength
type flyMaluses struct {
	fire, fireNeighbour, water, waterBorder, cocoa, fence float64
	damaging, damagingNeighbour                           float64
	pathLength                                            int
}

// PathType's defaults: FIRE 16, FIRE_IN_NEIGHBOR 8, WATER 8, WATER_BORDER 8,
// COCOA 0, FENCE -1, DAMAGING -1, DAMAGING_IN_NEIGHBOR 8.
var (
	// vanilla: Animal.<init>, Bee.<init>, Bee.createNavigation's setRequiredPathLength(48)
	beeFlight = flyMaluses{fire: -1, fireNeighbour: 16, water: -1, waterBorder: 16, cocoa: -1, fence: -1,
		damaging: -1, damagingNeighbour: 8, pathLength: 48}
	// vanilla: Parrot.<init>; no required length, so its follow range 16
	parrotFlight = flyMaluses{fire: -1, fireNeighbour: -1, water: 8, waterBorder: 8, cocoa: -1, fence: -1,
		damaging: -1, damagingNeighbour: 8, pathLength: 16}
	// vanilla: Allay.createNavigation's setRequiredPathLength(48)
	allayFlight = flyMaluses{fire: 16, fireNeighbour: 8, water: 8, waterBorder: 8, cocoa: 0, fence: -1,
		damaging: -1, damagingNeighbour: 8, pathLength: 48}
)

// flyStep is one neighbour of a node and the cells that must be accepted
// before it is: every smaller move along its axes.
type flyStep struct {
	d    [3]int32
	need [][3]int32
}

// flySteps are the neighbours in FlyNodeEvaluator.getNeighbors' order: the
// four sides, up, down, then the diagonals by how many axes they cross.
var flySteps = buildFlySteps()

func buildFlySteps() []flyStep {
	order := [][3]int32{
		{0, 0, 1}, {-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 1, 0}, {0, -1, 0},
		{0, 1, 1}, {-1, 1, 0}, {1, 1, 0}, {0, 1, -1},
		{0, -1, 1}, {-1, -1, 0}, {1, -1, 0}, {0, -1, -1},
		{1, 0, -1}, {1, 0, 1}, {-1, 0, -1}, {-1, 0, 1},
		{1, 1, -1}, {1, 1, 1}, {-1, 1, -1}, {-1, 1, 1},
		{1, -1, -1}, {1, -1, 1}, {-1, -1, -1}, {-1, -1, 1},
	}
	steps := make([]flyStep, len(order))
	for i, d := range order {
		steps[i].d = d
		for mask := 1; mask < 8; mask++ {
			var part [3]int32
			for axis := range 3 {
				if mask>>axis&1 == 1 {
					part[axis] = d[axis]
				}
			}
			if part != d && part != [3]int32{} {
				steps[i].need = append(steps[i].need, part)
			}
		}
	}
	return steps
}

// flyCellMalus is FlyNodeEvaluator.findAcceptedNode: ok is false for a
// cell that is no node, else the cell's extra cost. The cell's own type is
// priced first; an open cell takes its type from the block under it (1 over
// a block); then the first damaging, fire or lava, water or cautious block
// among the 26 around it but the cells over and under sets the price instead.
// vanilla: FlyNodeEvaluator.findAcceptedNode, getPathType, WalkNodeEvaluator.checkNeighbourBlocks
func flyCellMalus(w World, f *flyMaluses, x, y, z int32) (malus float64, ok bool) {
	state := w.BlockAt(x, y, z)
	switch blockPathType(state) {
	case pathWater:
		return priced(f.water)
	case pathBlockedType, pathLeaves, pathTrapdoor, pathLava:
		return 0, false
	case pathFence:
		return priced(f.fence)
	case pathFire:
		return priced(f.fire)
	case pathDamaging:
		return priced(f.damaging)
	case pathCocoa:
		return priced(f.cocoa)
	case pathDamageCautious:
		return 0, true
	}
	if len(blockBoxes(state)) != 0 {
		return 0, false
	}
	below := w.BlockAt(x, y-1, z)
	switch blockPathType(below) {
	case pathFire, pathLava:
		return priced(f.fire)
	case pathDamaging:
		return priced(f.damaging)
	case pathCocoa:
		return priced(f.cocoa)
	case pathFence:
		return priced(f.fence)
	}
	if len(blockBoxes(below)) != 0 {
		malus++
	}
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(-1); dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				// The neighbour's type replaces WALKABLE, so its 1 is dropped.
				switch blockPathType(w.BlockAt(x+dx, y+dy, z+dz)) {
				case pathDamaging:
					return priced(f.damagingNeighbour)
				case pathFire, pathLava:
					return priced(f.fireNeighbour)
				case pathWater:
					return priced(f.waterBorder)
				case pathDamageCautious:
					return 0, true
				}
			}
		}
	}
	return malus, true
}

// priced is a PathType price as a node's malus: a negative one refuses it.
func priced(p float64) (float64, bool) {
	return max(p, 0), p >= 0
}

// flyNode is one cell the search has met.
type flyNode struct {
	cell         Waypoint
	g, h, walked float64
	from         *flyNode
	malus        float64
	evaluated    bool
	accepted     bool
	open, closed bool
	// price is the heap key the node was last pushed with.
	price float64
}

// flySearch is one PathFinder.findPath run.
type flySearch struct {
	w     World
	f     *flyMaluses
	to    Waypoint
	nodes map[Waypoint]*flyNode
	open  frontier
	count int
	// best is the node nearest the target by straight distance.
	best     *flyNode
	bestAway float64
}

func (s *flySearch) node(c Waypoint) *flyNode {
	n, seen := s.nodes[c]
	if !seen {
		n = &flyNode{cell: c}
		s.nodes[c] = n
	}
	if !n.evaluated {
		n.evaluated = true
		n.malus, n.accepted = flyCellMalus(s.w, s.f, c.X, c.Y, c.Z)
	}
	return n
}

// accepted is the node at an offset from a cell, or nil when it is none.
func (s *flySearch) accepted(c Waypoint, d [3]int32) *flyNode {
	n := s.node(Waypoint{c.X + d[0], c.Y + d[1], c.Z + d[2]})
	if !n.accepted {
		return nil
	}
	return n
}

// consider scores a node's distance to the target for the best-node record.
// vanilla: Target.updateBest
func (s *flySearch) consider(n *flyNode, away float64) {
	if away < s.bestAway {
		s.best, s.bestAway = n, away
	}
}

func (s *flySearch) push(n *flyNode) {
	s.count++
	n.open = true
	n.price = n.g + n.h
	s.open.push(openCell{cell: n.cell, price: n.price, order: s.count})
}

// pop is the open node of least price, nil when none is left.
func (s *flySearch) pop() *flyNode {
	for len(s.open) > 0 {
		top := s.open.pop()
		if n := s.nodes[top.cell]; !n.closed && n.price == top.price {
			return n
		}
	}
	return nil
}

// flyPathFind plans a flight from one cell toward another and returns the
// waypoints after the start and whether a node within reach manhattan of the
// target was met. The path ends at the node nearest the target, so an
// unreachable place is still approached. The start is taken as it is; vanilla
// tries cells around a start it rejects.
// vanilla: PathFinder.findPath
func flyPathFind(w World, f *flyMaluses, from, to Waypoint, reach int32, budget int) ([]Waypoint, bool) {
	length := float64(f.pathLength)
	s := &flySearch{w: w, f: f, to: to, nodes: map[Waypoint]*flyNode{}, bestAway: math.Inf(1)}
	start := &flyNode{cell: from, evaluated: true, accepted: true}
	s.nodes[from] = start
	start.h = distanceTo(from, to)
	s.consider(start, start.h)
	s.push(start)
	reached := false
	for visited := 1; visited < budget; visited++ {
		cur := s.pop()
		if cur == nil {
			break
		}
		cur.closed = true
		if manhattan(cur.cell, to) <= reach {
			reached = true
			break
		}
		if distanceTo(cur.cell, from) >= length {
			continue
		}
	steps:
		for _, st := range flySteps {
			n := s.accepted(cur.cell, st.d)
			if n == nil || n.closed {
				continue
			}
			for _, d := range st.need {
				if s.accepted(cur.cell, d) == nil {
					continue steps
				}
			}
			dist := distanceTo(cur.cell, n.cell)
			walked := cur.walked + dist
			g := cur.g + dist + n.malus
			if walked >= length || n.open && g >= n.g {
				continue
			}
			n.from, n.g, n.walked = cur, g, walked
			away := distanceTo(n.cell, to)
			s.consider(n, away)
			n.h = away * flyPathWeight
			s.push(n)
		}
	}
	var path []Waypoint
	for n := s.best; n != nil && n != start; n = n.from {
		path = append(path, n.cell)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, reached
}

// seekToFlyPath is PathNavigation.moveTo for a flyer that plans paths: it
// plans a flight to the block the point falls in and holds it, unless one to
// that block already stands. nodeFactor scales the search's node budget. It
// reports whether a flight is under way and whether it ends within reach of
// the target.
// vanilla: PathNavigation.createPath, PathNavigation.moveTo, Path.canReach
func (m *Mob) seekToFlyPath(w World, x, y, z, speedFactor float64, reach int32, nodeFactor float64) (held, reached bool) {
	o := m.navOf()
	o.nav.speedFactor = speedFactor
	end := cellAt(x, y, z)
	if len(o.nav.path) > 0 && o.nav.end == end {
		return true, o.nav.flyReached
	}
	// vanilla: FlyNodeEvaluator.getStart rounds the height
	feet := cellAt(o.X, o.Y+0.5, o.Z)
	f := m.kind().flyPath
	if o.nav.waterFree {
		free := *f
		free.water = 0
		f = &free
	}
	path, reached := flyPathFind(w, f, feet, end, reach, int(float64(f.pathLength*16)*nodeFactor))
	o.nav.flyReached = reached
	o.moveTo(path, end, speedFactor)
	return len(path) > 0, reached
}

// flightOn reports whether a flight is under way: a path to follow or an
// order standing.
// vanilla: PathNavigation.isInProgress
func (m *Mob) flightOn() bool {
	o := m.navOf()
	return o.nav.flyActive || len(o.nav.path) > 0
}

// flyFollowPath is FlyingPathNavigation.tick: the next waypoint is passed
// when the body is within its reach, and the move control is ordered to the
// one after, at the waypoint's foot. A bee pollinating hovers by its own
// order instead.
// vanilla: FlyingPathNavigation.tick, PathNavigation.followThePath
func (m *Mob) flyFollowPath() {
	o := m.navOf()
	if len(o.nav.path) == 0 || m.bee.mem.pollinating {
		return
	}
	o.nav.navTick++
	node := o.nav.path[0]
	reach := m.width() / 2
	if m.width() <= 0.75 {
		reach = 0.75 - m.width()/2
	}
	if math.Abs(o.X-(float64(node.X)+0.5)) < reach && math.Abs(o.Z-(float64(node.Z)+0.5)) < reach &&
		math.Abs(o.Y-float64(node.Y)) < flyNodeAbove {
		o.nav.advance()
	}
	if len(o.nav.path) > 0 {
		o.stuckStep()
	}
	if len(o.nav.path) == 0 {
		o.nav.flyActive = false
		return
	}
	next := o.nav.path[0]
	o.nav.flyActive = true
	o.nav.flyTimeout = flyOrderMin
	o.nav.flyX, o.nav.flyY, o.nav.flyZ = float64(next.X)+0.5, float64(next.Y), float64(next.Z)+0.5
}
