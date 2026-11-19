package entity

import (
	"math"
	"sync"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The A* a mob paths with, and the navigation that walks the result. A path
// is a chain of Waypoint cells a mob's feet may stand in: open for the body,
// floored, and not deep water. A step moves to one of the eight neighbours,
// up one block or down up to the fall allowance (three unless hunting). The
// search ranges as far as the walk asks (a seek passes the follow range) and
// opens at most sixteen cells per block of that range.

// footprint is the body a path is planned for: its stride in cells and its
// exact box, which the risen-step check reads.
type footprint struct {
	// flatDistance ignores height in edge costs. vanilla: Warden.createNavigation
	flatDistance bool
	// fw and fh are the cells across (and as many deep) and up from the feet.
	fw, fh int32
	// width and height are the body's current box.
	width, height float64
	jumpHeight    float64
	// overFences is WalkNodeEvaluator.canWalkOverFences, separate from step height.
	overFences bool
	// floats is PathNavigation.canFloat: a floating body reads a water cell's
	// floor as the water's half-block top, not the ground below, so it plans
	// its way out of a pond over the surface.
	floats bool
	// maxFall is the deepest drop one step may take, vanilla's
	// Mob.getMaxFallDistance.
	maxFall int32
	// fireMalus, nearFireMalus and lavaMalus are the kind's extra cost for a
	// cell in fire, beside fire or lava, and in lava; negative makes the cell
	// impassable. vanilla: Mob.getPathfindingMalus
	fireMalus, nearFireMalus, lavaMalus float64
	// waterMalus is the extra cost of a water cell, and waterBorderMalus of a
	// floored cell beside water; negative makes the cell impassable.
	// vanilla: PathType WATER and WATER_BORDER
	waterMalus, waterBorderMalus float64
	// railMalus is the price of a rail cell: -1 unless the body stands on or
	// over a rail.
	railMalus float64
	// leavesMalus is the price of entering leaves; negative is impassable.
	leavesMalus float64
	// damageMalus, nearDamageMalus, cautiousMalus and powderSnowMalus are the
	// prices of a cactus or berry bush cell, a cell beside one, a cell by a
	// speleothem or wither rose, and a powder snow cell; negative is
	// impassable.
	damageMalus, nearDamageMalus, cautiousMalus, powderSnowMalus float64
	trapdoorMalus                                                float64
	// onPowderSnowMalus is the price of an open cell over powder snow; the
	// goat, wolf and sniffer's -1 keeps them off it.
	onPowderSnowMalus float64
	// openDoors is canOpenDoors: paths may run through closed wooden doors.
	openDoors bool

	// amphibious marks a body that plans through water cells, including up
	// and down within the water, as well as over ground.
	// vanilla: AmphibiousNodeEvaluator
	amphibious bool

	// lavaWalker marks a body that stands on a lava source's half-block top.
	// vanilla: Strider.canStandOnFluid
	lavaWalker bool

	// leashed marks a body kept near home: a box cell more than 32 blocks
	// from home that is no nearer to it than the body's own cell
	// (mobDistSq, squared) is blocked.
	// vanilla: Creaking.HomeNodeEvaluator.getPathType
	leashed   bool
	home      Waypoint
	mobDistSq int64
}

// homeLeashSq is the squared leash radius of a home-bound path.
const homeLeashSq = 1024

// leashBlocks reports whether the leash blocks a box cell.
func (f *footprint) leashBlocks(x, y, z int32) bool {
	if !f.leashed {
		return false
	}
	d := homeDistSq(f.home, Waypoint{x, y, z})
	return d > homeLeashSq && d >= f.mobDistSq
}

func homeDistSq(a, b Waypoint) int64 {
	dx, dy, dz := int64(a.X-b.X), int64(a.Y-b.Y), int64(a.Z-b.Z)
	return dx*dx + dy*dy + dz*dz
}

// footprintOf is NodeEvaluator.prepare's footprint: floor(width + 1) cells
// across and deep, floor(height + 1) up, anchored at the feet cell and
// reaching toward greater coordinates, as the evaluator's loops sweep.
func footprintOf(width, height float64) footprint {
	return footprint{
		fw:         int32(math.Floor(width + 1)),
		fh:         int32(math.Floor(height + 1)),
		width:      width,
		height:     height,
		jumpHeight: mobJumpHeight,
		// vanilla: LivingEntity.getMaxFallDistance, the idle mob's three
		maxFall: defaultMaxFall,
		// vanilla: PathType FIRE, FIRE_IN_NEIGHBOR and LAVA
		fireMalus: 16, nearFireMalus: 8, lavaMalus: -1,
		// vanilla: PathType.UNPASSABLE_RAIL
		railMalus: -1,
		// vanilla: PathType.LEAVES
		leavesMalus: -1,
		// vanilla: PathType DAMAGING, DAMAGING_IN_NEIGHBOR and POWDER_SNOW
		damageMalus: -1, nearDamageMalus: 8, powderSnowMalus: -1,
		// vanilla: PathType WATER and WATER_BORDER
		waterMalus: 8, waterBorderMalus: 8,
	}
}

// Waypoint is one block of a path, the cell the feet stand in; the point
// walked to is its centre, half a block in.
type Waypoint struct{ X, Y, Z int32 }

// pathStep is one way a path block may follow another: offset, climb or
// drop, and cost. Diagonals cost their hypotenuse, climbs extra, and small
// drops slightly more than level, so a path keeps its level without
// avoiding a downhill shortcut.
type pathStep struct {
	dx, dy, dz int32
	cost       float64
}

// defaultMaxFall is getComfortableFallDistance(0) = floor(0 + 3), the drop
// for a mob with no target.
const defaultMaxFall = 3

// maxPathFall bounds the drops a step list is built for; a hunter's
// health-derived allowance is clamped to it.
const maxPathFall = 64

var (
	pathStepsMu    sync.Mutex
	pathStepsCache = map[int32][]pathStep{}
)

// pathStepsFor returns the steps for a mob that may drop maxFall, in the
// search's fixed order: each of the eight neighbours level, up one, then
// down one to maxFall. The list is shared; callers must not modify it.
func pathStepsFor(maxFall int32, amphibious bool) []pathStep {
	maxFall = min(max(maxFall, 0), maxPathFall)
	key := maxFall
	if amphibious {
		key += maxPathFall + 1
	}
	pathStepsMu.Lock()
	defer pathStepsMu.Unlock()
	if steps, ok := pathStepsCache[key]; ok {
		return steps
	}
	diagonal := math.Sqrt2
	var steps []pathStep
	for _, horizontal := range []struct {
		dx, dz int32
		length float64
	}{
		{1, 0, 1}, {0, 1, 1}, {1, 1, diagonal},
		{-1, 0, 1}, {0, -1, 1}, {-1, -1, diagonal},
		{1, -1, diagonal}, {-1, 1, diagonal},
	} {
		for dy := int32(0); dy >= -maxFall; dy-- {
			steps = append(steps, pathStep{horizontal.dx, dy, horizontal.dz, horizontal.length + 0.1*float64(-dy)})
			if dy == 0 {
				steps = append(steps, pathStep{horizontal.dx, 1, horizontal.dz, horizontal.length + 0.4})
			}
		}
	}
	if amphibious {
		// vanilla: AmphibiousNodeEvaluator.getNeighbors adds the water above
		// and below
		steps = append(steps, pathStep{0, 1, 0, 1}, pathStep{0, -1, 0, 1})
	}
	pathStepsCache[key] = steps
	return steps
}

// footprintClear reports whether the footprint's cells are open with no
// water over the head, and whether any has a floor. Water is judged as a
// foot would: a puddle over ground is waded, but a pond's bottom cell or a
// deeper surface is not walkable, unless the body floats, whose water cell
// is floored by the water's top (getFloorLevel's canFloat branch). As in
// WalkNodeEvaluator.getPathTypeOfMob, a block anywhere in the footprint
// blocks the cell, so a gap narrower than the body is no gap.
func footprintClear(w World, f footprint, x, y, z int32) (clear, floored bool) {
	for dx := int32(0); dx < f.fw; dx++ {
		for dz := int32(0); dz < f.fw; dz++ {
			col, row := x+dx, z+dz
			for dy := int32(0); dy < f.fh; dy++ {
				if f.blocksBody(w.BlockAt(col, y+dy, row)) {
					return false, false
				}
			}
			if f.amphibious && rules.IsWater(w.BlockAt(col, y, row)) {
				// a water node is always open and floored by the water
				floored = true
				continue
			}
			if rules.IsWater(w.BlockAt(col, y, row)) && rules.IsWater(w.BlockAt(col, y+1, row)) {
				return false, false
			}
			if !floored && floorsFeet(w.BlockAt(col, y-1, row)) {
				floored = true
			}
			// A floating body stands on the water's half-block top (floorAt), not on
			// a bed too deep to reach.
			if !floored && f.floats && rules.IsWater(w.BlockAt(col, y, row)) {
				floored = true
			}
			if !floored && f.lavaWalker && lavaSurface(w, col, y, row) {
				floored = true
			}
		}
	}
	return true, floored
}

// pathfindableOpenBlocks marks the blocks whose class keeps BlockBehaviour's
// default isPathfindable although they have collision. A snow layer is
// handled apart because its answer depends on the state. A lily pad and a big
// dripleaf are absent: their path type (TRAPDOOR) blocks the cell regardless.
// vanilla: BlockBehaviour.isPathfindable (LAND)
var pathfindableOpenBlocks = registry.BlocksOf(func(b registry.Block) bool {
	switch b {
	case registry.BlockTurtleEgg, registry.BlockRepeater, registry.BlockComparator,
		registry.BlockDaylightDetector, registry.BlockSculkShrieker,
		registry.BlockAmethystCluster, registry.BlockLargeAmethystBud,
		registry.BlockMediumAmethystBud, registry.BlockSmallAmethystBud,
		registry.BlockMossCarpet, registry.BlockPaleMossCarpet,
		registry.BlockLadder, registry.BlockScaffolding, registry.BlockPitcherCrop,
		registry.BlockHoneyBlock:
		return true
	}
	return b.InTag(registry.TagIDWoolCarpets) || b.InTag(registry.TagIDCandles)
})

// pathfindableOpen reports a block with collision that vanilla still treats as
// pathfindable (an open cell), including a snow layer under five.
// vanilla: BlockBehaviour.isPathfindable, SnowLayerBlock.isPathfindable
func pathfindableOpen(state uint16) bool {
	if int(state) >= registry.StateCount {
		return false
	}
	block := registry.BlockOf(state)
	if block == registry.BlockSnow {
		layers, _ := registry.StateInt(state, registry.PropLayers)
		return layers < 5
	}
	return pathfindableOpenBlocks[block]
}

// floorsFeet reports whether a block under a cell holds a body up. Powder
// snow has no collision here, yet the cell over it is ON_TOP_OF_POWDER_SNOW,
// a node of its own floored at the snow's bottom rather than an open cell to
// fall through. vanilla: WalkNodeEvaluator.findAcceptedNode
func floorsFeet(state uint16) bool {
	return blockExtent(state) != 0 && !pathfindableOpen(state) || rules.IsPowderSnow(state)
}

// walkableAt reports whether the footprint can stand with its feet in the
// cell: clear and floored under the feet cell.
func walkableAt(w World, f footprint, x, y, z int32) bool {
	clear, _ := footprintClear(w, f, x, y, z)
	return clear && (floorsFeet(w.BlockAt(x, y-1, z)) || f.lavaWalker && lavaSurface(w, x, y, z) ||
		f.amphibious && rules.IsWater(w.BlockAt(x, y, z)))
}

// lavaSurface reports a lava source with no lava above, where a lava
// walker's feet rest at half-block height.
// vanilla: LiquidBlock.getCollisionShape, Strider.floatStrider
func lavaSurface(w World, x, y, z int32) bool {
	return rules.IsLavaSource(w.BlockAt(x, y, z)) && !rules.IsLava(w.BlockAt(x, y+1, z))
}

// The path types of a cell, for the malus a kind puts on them.
const (
	pathClear = iota
	pathFire
	pathNearFire
	pathLava
	pathWater
	pathRail
	pathCocoa
	pathPowderSnow
	pathDamageCautious
	pathDamaging
	pathNearDamaging
	pathTrapdoorTop
	pathOnTopOfPowderSnow
	pathWaterBorder
	pathBlockedType
	pathLeaves
	pathFence
	pathTrapdoor
	pathDoorOpen
	pathDoorWoodClosed
	pathDoorIronClosed
	pathStickyHoney
)

// bigMobsCloseToDanger is the malus of a wide body's cell beside a cheaper
// danger. vanilla: PathType.BIG_MOBS_CLOSE_TO_DANGER
const bigMobsCloseToDanger = 4

// burningBlock reports whether the pathfinder reads a block as fire.
// vanilla: NodeEvaluator.isBurningBlock
func burningBlock(state uint16) bool {
	if state == 0 {
		return false
	}
	_, lit := rules.CampfireDamage(state)
	return rules.IsFire(state) || rules.IsHotFloor(state) || rules.IsLavaCauldron(state) || lit
}

// blockPathType is a state's path type, cached independently of the world.
// vanilla: WalkNodeEvaluator.getPathTypeFromState
func blockPathType(state uint16) int {
	if int(state) >= registry.StateCount {
		return pathClear
	}
	return int(blockPathTypes[state])
}

var blockPathTypes = func() *[registry.StateCount]uint8 {
	var types [registry.StateCount]uint8
	for state := range uint16(registry.StateCount) {
		types[state] = uint8(classifyBlockPathType(state))
	}
	return &types
}()

// classifyBlockPathType keeps hazards and land obstructions ahead of water.
// vanilla: WalkNodeEvaluator.getPathTypeFromState
func classifyBlockPathType(state uint16) int {
	if state == 0 {
		return pathClear
	}
	if isTrapdoorPathType(state) {
		return pathTrapdoor
	}
	if rules.IsPowderSnow(state) {
		return pathPowderSnow
	}
	block := registry.BlockOf(state)
	switch block {
	case registry.BlockCactus, registry.BlockSweetBerryBush:
		return pathDamaging
	case registry.BlockHoneyBlock:
		return pathStickyHoney
	case registry.BlockCocoa:
		return pathCocoa
	case registry.BlockPointedDripstone, registry.BlockSulfurSpike, registry.BlockWitherRose:
		return pathDamageCautious
	}
	if rules.IsLava(state) {
		return pathLava
	}
	if burningBlock(state) {
		return pathFire
	}
	switch doorKindOf(state) {
	case doorOpen:
		return pathDoorOpen
	case doorWoodClosed:
		return pathDoorWoodClosed
	case doorIronClosed:
		return pathDoorIronClosed
	}
	switch block {
	case registry.BlockRail, registry.BlockPoweredRail, registry.BlockDetectorRail, registry.BlockActivatorRail:
		return pathRail
	}
	if block.InGroup(registry.GroupLeaves) {
		return pathLeaves
	}
	if block.InGroup(registry.GroupFence|registry.GroupWall) ||
		block.InGroup(registry.GroupFenceGate) && !registry.StateBool(state, registry.PropOpen) {
		return pathFence
	}
	if !landPathfindable(state) {
		return pathBlockedType
	}
	if rules.IsWater(state) {
		return pathWater
	}
	return pathClear
}

var pathTrapdoors = registry.BlocksInGroup(registry.GroupTrapdoor)

// pathBlocked reports whether the pathfinder reads a state as BLOCKED.
// vanilla: WalkNodeEvaluator.getPathTypeFromState
func pathBlocked(state uint16) bool {
	return blockPathType(state) == pathBlockedType
}

// isTrapdoorPathType reports PathType.TRAPDOOR: a trapdoor, lily pad or big
// dripleaf.
// vanilla: WalkNodeEvaluator.getPathTypeFromState
func isTrapdoorPathType(state uint16) bool {
	b := registry.BlockOf(state)
	return pathTrapdoors[b] || b == registry.BlockLilyPad || b == registry.BlockBigDripleaf
}

// positionPathType classifies a cell and the hazards around its floor.
// vanilla: WalkNodeEvaluator.getPathTypeStatic
func positionPathType(w World, x, y, z int32) int {
	if t := blockPathType(w.BlockAt(x, y, z)); t != pathClear {
		return t
	}
	belowState := w.BlockAt(x, y-1, z)
	if pathTrapdoors[registry.BlockOf(belowState)] {
		return pathTrapdoorTop
	}
	below := blockPathType(belowState)
	if below == pathFire || below == pathDamageCautious || below == pathDamaging || below == pathStickyHoney {
		return below
	}
	if below == pathPowderSnow {
		return pathOnTopOfPowderSnow
	}
	if below == pathLava || blockExtent(w.BlockAt(x, y-1, z)) == 0 {
		return pathClear
	}
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(-1); dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				// vanilla: WalkNodeEvaluator.checkNeighbourBlocks takes the
				// first neighbour hit, damaging before fire, water and cautious
				s := w.BlockAt(x+dx, y+dy, z+dz)
				switch blockPathType(s) {
				case pathDamaging:
					return pathNearDamaging
				case pathFire, pathLava:
					return pathNearFire
				case pathWater:
					return pathWaterBorder
				case pathDamageCautious:
					return pathDamageCautious
				}
			}
		}
	}
	return pathClear
}

func (f *footprint) malusOfType(t int) float64 {
	switch t {
	case pathStickyHoney:
		return 8 // vanilla: PathType.STICKY_HONEY
	case pathFire:
		return f.fireMalus
	case pathNearFire:
		return f.nearFireMalus
	case pathLava:
		return f.lavaMalus
	case pathWater:
		return f.waterMalus
	case pathWaterBorder:
		return f.waterBorderMalus
	case pathRail:
		return f.railMalus
	case pathLeaves:
		return f.leavesMalus
	case pathDamageCautious:
		return f.cautiousMalus
	case pathDamaging:
		return f.damageMalus
	case pathNearDamaging:
		return f.nearDamageMalus
	case pathTrapdoorTop:
		return f.trapdoorMalus
	case pathPowderSnow:
		return f.powderSnowMalus
	case pathOnTopOfPowderSnow:
		return f.onPowderSnowMalus
	}
	return 0
}

// Amphibious prices. vanilla: AmphibiousNodeEvaluator.prepare
const (
	amphibiousWalkableMalus    = 6 // PathType.WALKABLE
	amphibiousWaterBorderMalus = 4 // PathType.WATER_BORDER
)

// amphibiousOpenWater says whether the cell is water with no blocked face on
// any of its six sides (PathType.WATER rather than WATER_BORDER).
// vanilla: AmphibiousNodeEvaluator.getPathType
func amphibiousOpenWater(w World, x, y, z int32) bool {
	if state := w.BlockAt(x, y, z); !rules.IsWater(state) || registry.CollisionShapeFull(state) {
		return false
	}
	for _, d := range [6][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if pathBlocked(w.BlockAt(x+d[0], y+d[1], z+d[2])) {
			return false
		}
	}
	return true
}

// cellTypeMalus is the malus of one footprint cell's path type. An
// amphibious body prices open water at 0, water beside a blocked face at
// 4 and floored open ground at 6, except on a lily pad or big dripleaf.
// vanilla: AmphibiousNodeEvaluator.prepare, getPathType
func (f *footprint) cellTypeMalus(w World, x, y, z int32) float64 {
	if f.amphibious {
		if state := w.BlockAt(x, y, z); rules.IsWater(state) && !registry.CollisionShapeFull(state) {
			if amphibiousOpenWater(w, x, y, z) {
				return 0
			}
			return amphibiousWaterBorderMalus
		}
		t := positionPathType(w, x, y, z)
		if t == pathWaterBorder {
			return amphibiousWaterBorderMalus
		}
		// A lily pad or big dripleaf below is TRAPDOOR, so the cell is
		// ON_TOP_OF_TRAPDOOR (frog: OPEN) and costs 0, not WALKABLE's 6.
		if below := w.BlockAt(x, y-1, z); t == pathClear && floorsFeet(below) && !isTrapdoorPathType(below) {
			return amphibiousWalkableMalus
		}
		return f.malusOfType(t)
	}
	return f.malusOfType(positionPathType(w, x, y, z))
}

// cellMalus is the footprint's cost to stand in the cell: any negative
// malus makes it impassable, else the highest counts, and a wide body beside
// a danger cheaper than the worst pays the near-danger malus.
// vanilla: WalkNodeEvaluator.getPathTypeOfMob
func cellMalus(w World, f footprint, x, y, z int32) float64 {
	highest := 0.0
	for dx := int32(0); dx < f.fw; dx++ {
		for dy := int32(0); dy < f.fh; dy++ {
			for dz := int32(0); dz < f.fw; dz++ {
				if f.leashBlocks(x+dx, y+dy, z+dz) {
					return -1
				}
				m := f.cellTypeMalus(w, x+dx, y+dy, z+dz)
				if m < 0 {
					return m
				}
				highest = max(highest, m)
			}
		}
	}
	if f.fw > 1 && f.cellTypeMalus(w, x, y, z) < highest && bigMobsCloseToDanger < highest {
		return bigMobsCloseToDanger
	}
	return highest
}

// traversableAt reports whether a step may land in the cell: clear and
// floored under any footprint cell (getPathTypeOfMob's max-malus read), so a
// wide body can step off an edge while the ledge is still under it.
func traversableAt(w World, f footprint, x, y, z int32) bool {
	clear, floored := footprintClear(w, f, x, y, z)
	return clear && floored
}

// walkableCell classifies the feet cell, independently of the mob's footprint.
// vanilla: MoveControl.isWalkable
func (m *Mob) walkableCell(w World, x, z float64) bool {
	c := cellAt(x, math.Floor(m.Y), z)
	if c.Y <= w.MinY() || positionPathType(w, c.X, c.Y, c.Z) != pathClear {
		return false
	}
	// positionPathType folds OPEN and WALKABLE into pathClear; distinguish
	// them by the support cell. vanilla: WalkNodeEvaluator.getPathTypeStatic
	below := blockPathType(w.BlockAt(c.X, c.Y-1, c.Z))
	return below != pathClear && below != pathWater && below != pathLava
}

// pathOpen reports whether the path passes a column. Diagonals check both
// side columns to avoid cutting a wall's inside corner.
func pathOpen(w World, f footprint, x, y, z int32) bool {
	for dy := range f.fh {
		state := w.BlockAt(x, y+dy, z)
		if blockExtent(state) != 0 && (f.leavesMalus < 0 || blockPathType(state) != pathLeaves) {
			return false
		}
	}
	return true
}

// mobJumpHeight is the floor of WalkNodeEvaluator.getMobJumpHeight's per-mob value.
const mobJumpHeight = 1.125

// vanilla: WalkNodeEvaluator.getPathTypeFromState (FENCE)
var pathFences = registry.BlocksOf(func(b registry.Block) bool {
	return b.InTag(registry.TagIDFences) || b.InTag(registry.TagIDWalls) || b.InGroup(registry.GroupFenceGate)
})

// floorAt is WalkNodeEvaluator.getFloorLevel: a floating body's water cell
// is floored by its half-block top, every other cell by the top of the block
// below.
func floorAt(w World, f footprint, cell Waypoint) float64 {
	if (f.floats || f.amphibious) && rules.IsWater(w.BlockAt(cell.X, cell.Y, cell.Z)) {
		return float64(cell.Y) + 0.5
	}
	if f.lavaWalker && lavaSurface(w, cell.X, cell.Y, cell.Z) {
		return float64(cell.Y) + striderLiquidTop
	}
	return float64(cell.Y-1) + blockExtent(w.BlockAt(cell.X, cell.Y-1, cell.Z))
}

// pathEvaluator remembers geometry only for feet and columns inside the dense
// box; footprint and headroom reads still go through the world.
type pathEvaluator struct {
	World
	footprint
	scratch *pathScratch
	// This evaluator lives for one search; neighbours repeatedly ask for
	// their source's floor before querying a different destination.
	source      Waypoint
	sourceFloor float64
	sourceKnown bool
}

func (e *pathEvaluator) floorFrom(c Waypoint) float64 {
	if e.sourceKnown && e.source == c {
		return e.sourceFloor
	}
	return e.loadSourceFloor(c)
}

func (e *pathEvaluator) loadSourceFloor(c Waypoint) float64 {
	if e.scratch == nil {
		return floorAt(e.World, e.footprint, c)
	}
	n := e.geometry(c)
	v := e.floorCell(c, n)
	if n != nil {
		e.source, e.sourceFloor, e.sourceKnown = c, v, true
	}
	return v
}

func (e *pathEvaluator) geometry(c Waypoint) *pathCell {
	if e.scratch != nil {
		return e.scratch.geometry(c)
	}
	return nil
}

func (e *pathEvaluator) floor(c Waypoint) float64 {
	return e.floorCell(c, e.geometry(c))
}

func (e *pathEvaluator) floorCell(c Waypoint, n *pathCell) float64 {
	if n == nil || n.geometryFlags&floorKnown == 0 {
		return e.loadFloor(c, n)
	}
	return n.floor
}

func (e *pathEvaluator) loadFloor(c Waypoint, n *pathCell) float64 {
	v := floorAt(e.World, e.footprint, c)
	if n != nil {
		n.floor = v
		n.geometryFlags |= floorKnown
	}
	return v
}

func (e *pathEvaluator) traversable(c Waypoint) bool {
	return e.traversableCell(c, e.geometry(c))
}

func (e *pathEvaluator) traversableCell(c Waypoint, n *pathCell) bool {
	if n == nil || n.geometryFlags&traversableKnown == 0 {
		return e.loadTraversable(c, n)
	}
	return n.geometryFlags&traversableValue != 0
}

func (e *pathEvaluator) loadTraversable(c Waypoint, n *pathCell) bool {
	v := traversableAt(e.World, e.footprint, c.X, c.Y, c.Z)
	if n != nil {
		if v {
			n.geometryFlags |= traversableValue
		}
		n.geometryFlags |= traversableKnown
	}
	return v
}

func (e *pathEvaluator) columnOpen(c Waypoint) bool {
	if e.scratch != nil {
		if n := e.scratch.geometry(c); n != nil {
			if n.geometryFlags&columnOpenKnown == 0 {
				if pathOpen(e.World, e.footprint, c.X, c.Y, c.Z) {
					n.geometryFlags |= columnOpenValue
				}
				n.geometryFlags |= columnOpenKnown
			}
			return n.geometryFlags&columnOpenValue != 0
		}
	}
	return pathOpen(e.World, e.footprint, c.X, c.Y, c.Z)
}

// stepTaken says whether the path may take the step: a traversable
// destination, then fair diagonals.
func stepTaken(w World, f footprint, from Waypoint, s pathStep) bool {
	e := pathEvaluator{World: w, footprint: f}
	return e.stepTaken(from, s)
}

func (e *pathEvaluator) stepTaken(from Waypoint, s pathStep) bool {
	w, f := e.World, &e.footprint
	to := Waypoint{from.X + s.dx, from.Y + s.dy, from.Z + s.dz}
	if s.dx == 0 && s.dz == 0 {
		// A vertical step must enter open WATER, not WATER_BORDER.
		// vanilla: AmphibiousNodeEvaluator.isVerticalNeighborValid, getPathType
		return amphibiousOpenWater(w, to.X, to.Y, to.Z) && e.traversable(to)
	}
	fromFloor := e.floorFrom(from)
	var geometry *pathCell
	if e.scratch != nil {
		geometry = e.scratch.geometryInside(to)
	}
	toFloor := e.floorCell(to, geometry)
	// vanilla: WalkNodeEvaluator.findAcceptedNode — a neighbour whose floor
	// is more than the jump height above this one is no node.
	if toFloor-fromFloor > f.jumpHeight {
		return false
	}
	if !e.traversableCell(to, geometry) {
		return false
	}
	if s.dy == 1 && !f.overFences {
		// The cell below a risen node is the source of tryJumpOn; a level
		// node already above a fence does not need permission to jump onto it.
		if fenceAtLevel(w, *f, to.X, to.Y-1, to.Z) {
			return false
		}
	}
	// A deep drop falls down the neighbour's column, so the cells above the
	// landing up to the body's height must be open; shallow drops are exempt.
	if s.dy < -defaultMaxFall && !dropColumnOpen(w, *f, from, to) {
		return false
	}
	if s.dy == 1 && powderSnowAtLevel(w, *f, to.X, from.Y, to.Z) {
		return false
	}
	// WalkNodeEvaluator.tryJumpOn: a step-up's jump leaves the body's column,
	// so headroom is measured over the body from its floor to its risen head on
	// the step's floor. Bodies a block wide or more already sweep their whole
	// footprint in the walkable check, so only narrower ones need this.
	if s.dy == 1 && f.width < 1.0 {
		top := int32(math.Floor(toFloor + f.height - 0.002))
		for y := from.Y + f.fh; y <= top; y++ {
			if blockExtent(w.BlockAt(from.X, y, from.Z)) != 0 {
				return false
			}
		}
	}
	if s.dx != 0 && s.dz != 0 {
		// vanilla: WalkNodeEvaluator.isDiagonalValid refuses a WALKABLE_DOOR
		if f.openDoors && doorInFootprint(w, *f, to.X, to.Y, to.Z) {
			return false
		}
		if !e.columnOpen(Waypoint{from.X + s.dx, from.Y, from.Z}) || !e.columnOpen(Waypoint{from.X, from.Y, from.Z + s.dz}) {
			return false
		}
		if s.dy != 0 &&
			(!e.columnOpen(Waypoint{from.X + s.dx, from.Y + s.dy, from.Z}) || !e.columnOpen(Waypoint{from.X, from.Y + s.dy, from.Z + s.dz})) {
			return false
		}
	}
	return true
}

// fenceAtLevel reads FENCE across the source footprint, including a wide body's leading edge.
func fenceAtLevel(w World, f footprint, x, y, z int32) bool {
	for dx := int32(0); dx < f.fw; dx++ {
		for dz := int32(0); dz < f.fw; dz++ {
			state := w.BlockAt(x+dx, y, z+dz)
			if pathFences[registry.BlockOf(state)] && !registry.StateBool(state, registry.PropOpen) {
				return true
			}
		}
	}
	return false
}

// powderSnowAtLevel reports whether the footprint's cells at the cell make
// the mob's path type POWDER_SNOW, which findAcceptedNode never jumps onto (no
// tryJumpOn), so the snow's top is reached only by walking. A cell the body
// cannot enter makes the type BLOCKED instead, which comes first in PathType
// order, and the jump is tried.
// vanilla: WalkNodeEvaluator.findAcceptedNode, getPathTypeOfMob
func powderSnowAtLevel(w World, f footprint, x, y, z int32) bool {
	snow := false
	for dx := int32(0); dx < f.fw; dx++ {
		for dy := int32(0); dy < f.fh; dy++ {
			for dz := int32(0); dz < f.fw; dz++ {
				state := w.BlockAt(x+dx, y+dy, z+dz)
				if f.blocksBody(state) {
					return false
				}
				snow = snow || rules.IsPowderSnow(state)
			}
		}
	}
	return snow
}

// dropColumnOpen reports whether the cells above a deep drop's landing, up
// to the body's height, are open.
func dropColumnOpen(w World, f footprint, from, to Waypoint) bool {
	for y := to.Y + f.fh; y < from.Y+f.fh; y++ {
		if blockExtent(w.BlockAt(to.X, y, to.Z)) != 0 {
			return false
		}
	}
	return true
}

// openCell is a frontier cell; equal prices pop in queue order, so the
// search is deterministic for a given world.
type openCell struct {
	cell  Waypoint
	price float64
	order int
}

// frontier is the open list as a heap, cheapest first, ties by arrival.
type frontier []openCell

func (f frontier) Less(i, j int) bool {
	return f[i].price < f[j].price || f[i].price == f[j].price && f[i].order < f[j].order
}
func (f frontier) Swap(i, j int) { f[i], f[j] = f[j], f[i] }

// maxPathLength is the path search range. vanilla: PathNavigation.getMaxPathLength
func (m *Mob) maxPathLength() float64 {
	// vanilla: PathNavigation.requiredPathLength starts at 16.
	return max(16, m.followRange(), m.kind().pathLength)
}

// findPath runs A* from one cell to another and returns the waypoints after
// the start, and whether the target was reached. A node reaches the target
// within reachRange manhattan (PathTarget.considered), so a seeker stops at
// the edge of its reach. Either way the path ends at the opened node nearest
// the target (getBestNode, scored as Target.updateBest), so an unreachable
// place is still approached; a best node that is the start gives the empty
// path, which the spider's climb reads.
//
// Deliberate differences from vanilla's PathFinder.findPath: a start the
// body cannot stand in is exempt from the reach test, and maxRange bounds
// the Chebyshev distance from the start rather than walkedDistance. The
// open-cell budget is sixteen per block of range (as PathNavigation sizes
// it over getMaxPathLength), the guard on paths that cannot be found. The
// heuristic (octile plus whole climbs, or just octile for XZ edge costs)
// never overestimates, so the first arrival is a shortest path.
// vanilla: PathFinder.findPath
func findPath(w World, f footprint, from, to Waypoint, maxRange, reachRange int32) ([]Waypoint, bool) {
	s := getPathScratch(manhattan(from, to))
	defer s.release()
	return s.search(w, f, from, to, maxRange, reachRange)
}

// search is findPath over the scratch's memory, which must be empty.
func (s *pathScratch) search(w World, f footprint, from, to Waypoint, maxRange, reachRange int32) ([]Waypoint, bool) {
	limit := max(maxRange, 16) * 16
	if !s.tiny {
		// The search does not change the world, so its reads share one cache.
		s.blocks.World = w
		w = &s.blocks
	}
	s.prepare(from, maxRange)
	evaluator := pathEvaluator{World: w, footprint: f}
	if s.side != 0 {
		evaluator.scratch = s
	}
	s.visit(from, from, 0)
	open := &s.open
	open.push(openCell{cell: from, price: f.remaining(from, to)})
	var counter int
	// The unstandable start skips the reach test: on the lip of a one-deep
	// pond the water cell is a block down and within reach, and testing the
	// start would give a burning body's water run a one-node path, leaving it
	// on the bank. It counts toward the best node only if floored somewhere
	// (WalkNodeEvaluator.canStartAt takes any non-OPEN node).
	startStandable := evaluator.traversable(from)
	best, bestAway := from, distanceTo(from, to)
	if !startStandable {
		bestAway = math.Inf(1)
	}
	steps := pathStepsFor(f.maxFall, f.amphibious)
	for opened := 0; len(*open) > 0; opened++ {
		if opened > int(limit) {
			break
		}
		node := open.pop().cell
		// vanilla: PathTarget.considered, node.distanceManhattan(target) <= reachRange
		if (node != from || startStandable) && manhattan(node, to) <= reachRange {
			return s.reconstruct(from, best), true
		}
		// Steps run by neighbour, shallow to deep, so once a column is blocked at
		// some depth every deeper drop is too.
		columnBlocked := false
		walked, _ := s.cost(node)
		for _, step := range steps {
			next := Waypoint{node.X + step.dx, node.Y + step.dy, node.Z + step.dz}
			if step.dy < -defaultMaxFall {
				if step.dy == -defaultMaxFall-1 {
					columnBlocked = !dropColumnOpen(w, f, node, next)
				} else if !columnBlocked {
					columnBlocked = blockExtent(w.BlockAt(next.X, next.Y+f.fh, next.Z)) != 0
				}
				if columnBlocked {
					continue
				}
			}
			if chebyshev(from, next) > maxRange {
				continue
			}
			if !evaluator.stepTaken(node, step) {
				continue
			}
			// A negative-malus cell is no node, unless the search already stands in
			// one.
			// vanilla: WalkNodeEvaluator.isNeighborValid
			malus := s.malus(w, &f, next)
			if malus < 0 && s.malus(w, &f, node) >= 0 {
				continue
			}
			cost := step.cost
			if f.flatDistance {
				cost = 1
				if step.dx != 0 && step.dz != 0 {
					cost = math.Sqrt2
				}
			}
			price := walked + cost + max(malus, 0)
			if !s.visit(next, node, price) {
				continue
			}
			// vanilla: Target.updateBest, as each node is opened.
			if away := distanceTo(next, to); away < bestAway {
				best, bestAway = next, away
			}
			counter++
			open.push(openCell{cell: next, price: price + f.remaining(next, to), order: counter})
		}
	}
	return s.reconstruct(from, best), false
}

// reconstruct walks the came-from chain back from end, excluding the start,
// and returns it in walking order. An end at the start gives the empty path.
func (s *pathScratch) reconstruct(from, end Waypoint) []Waypoint {
	var path []Waypoint
	for at := end; at != from; {
		path = append(path, at)
		if s.side != 0 {
			at = s.cell(at).parent
		} else {
			at = s.cameFrom[at]
		}
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// chebyshev is the largest axis distance, which the range limit uses.
func chebyshev(from, to Waypoint) int32 {
	dx, dy, dz := to.X-from.X, to.Y-from.Y, to.Z-from.Z
	widest := max(dx, -dx)
	widest = max(widest, dy, -dy)
	widest = max(widest, dz, -dz)
	return widest
}

// manhattan is Node.distanceManhattan, which reachRange is measured in.
func manhattan(a, b Waypoint) int32 {
	return max(a.X-b.X, b.X-a.X) + max(a.Y-b.Y, b.Y-a.Y) + max(a.Z-b.Z, b.Z-a.Z)
}

// distanceTo is Node.distanceTo, which best-node tracking scores by.
func distanceTo(a, b Waypoint) float64 {
	dx, dy, dz := float64(a.X-b.X), float64(a.Y-b.Y), float64(a.Z-b.Z)
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// remaining is the heuristic: octile distance plus the whole climb, which
// no path can beat.
func remaining(from, to Waypoint) float64 {
	dx := max(to.X-from.X, from.X-to.X)
	dz := max(to.Z-from.Z, from.Z-to.Z)
	flat := float64(max(dx, dz)) + math.Sqrt2*float64(min(dx, dz))
	height := max(to.Y-from.Y, from.Y-to.Y)
	return flat + float64(height)
}

// remaining omits the climb lower bound when edges ignore height. Best-node
// tracking and the reach test still use 3D distance, as in PathFinder.getBestH.
func (f *footprint) remaining(from, to Waypoint) float64 {
	if f.flatDistance {
		to.Y = from.Y
	}
	return remaining(from, to)
}

// walkableNear finds the nearest cell the footprint can stand in, ring by
// ring outward and level by level; false when none is in reach.
func walkableNear(w World, f footprint, x, y, z float64) (Waypoint, bool) {
	col := int32(math.Floor(x))
	row := int32(math.Floor(z))
	for r := int32(0); r <= 3; r++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if max(max(dx, -dx), max(dz, -dz)) != r {
					continue // only the ring itself
				}
				for _, dy := range [9]int32{0, -1, 1, -2, 2, -3, 3, -4, 4} {
					cell := Waypoint{col + dx, int32(math.Floor(y)) + dy, row + dz}
					if walkableAt(w, f, cell.X, cell.Y, cell.Z) {
						return cell, true
					}
				}
			}
		}
	}
	return Waypoint{}, false
}

// mobNav is a goal's walking order: the remaining waypoints, the pace, and
// the target block the path was planned to (PathNavigation.targetPos), which
// a new ask is matched against before replanning. The direct fields are the
// spider's: when WallClimberNavigation finds no path it drives straight at
// the target, pressing against the wall it then climbs.
type mobNav struct {
	path []Waypoint
	// waterFree prices water at 0 while a follow goal runs.
	// vanilla: FollowOwnerGoal.start, FollowMobGoal.start
	waterFree bool
	// origin and passed are the planned-from cell and the waypoints reached
	// since, the nodes before vanilla Path's next-node index.
	origin      Waypoint
	passed      []Waypoint
	speedFactor float64
	end         Waypoint
	// reach is the reach range the standing path was planned with, and
	// lastRecompute the navTick of its last recompute; delayedRecompute asks
	// navigate to retry one a block change could not make yet.
	// vanilla: PathNavigation.reach, timeLastRecompute, hasDelayedRecomputation
	reach            int32
	lastRecompute    int64
	delayedRecompute bool

	// avoidSun is GroundPathNavigation's avoidSun, held up by the sun goal in
	// bright hours: every seekTo path is cut where the sky begins (sun.go). It
	// outlives paths; only the goal's stop clears it.
	avoidSun bool

	// doorOps are the door changes the door goals queued for the server.
	doorOps []DoorOp

	// PathNavigation's two stuck watches: the walk's tick count and the
	// position of the last 100-tick progress sample; and the waypoint being
	// pressed at, its stalled ticks, and its timeout (the straight distance at
	// the body's pace). Together they drop a path the world has walled off.
	navTick                int64
	lastStuckCheck         int64
	stuckX, stuckY, stuckZ float64

	// swim marks path as a water-bound one, walked by swimNavTickAt.
	swim bool

	timeoutNode  Waypoint
	timeoutArmed bool
	timeoutTimer int64
	timeoutLimit float64

	// The wall climber's standing order: the block asked for by the last
	// seekTo, driven at once the path is spent. stopSeeking keeps it.
	// vanilla: WallClimberNavigation.pathToPosition
	climbTo  Waypoint
	climbSet bool

	// The flyer's order for the flying move control: the point to press toward
	// and whether one is set. Only a flyPath kind plans a path, and its order
	// is the next waypoint.
	flyActive        bool
	flyX, flyY, flyZ float64
	// flyTimeout is the ghastling's ticks left on the standing order.
	flyTimeout int
	// flyReached is whether the flight path held ends within reach of its
	// target. vanilla: Path.canReach
	flyReached bool

	// flyFloatDuration is Ghast.GhastMoveControl's floatDuration, ticks before
	// the grown body presses at its point again.
	flyFloatDuration int
}

// advance moves the path past its next waypoint.
func (n *mobNav) advance() {
	n.passed = append(n.passed, n.path[0])
	n.path = n.path[1:]
}

// prevNode is the last waypoint reached, or the planned-from cell.
// vanilla: Path.getPreviousNode
func (n *mobNav) prevNode() Waypoint {
	if len(n.passed) > 0 {
		return n.passed[len(n.passed)-1]
	}
	return n.origin
}

// PathNavigation's stuck watch numbers: progress samples 100 ticks apart,
// and the share of 100 ticks' travel at the pace a sample must cover.
const (
	stuckCheckInterval  = 100
	stuckDistanceFactor = 0.25
)

// navReach is PathNavigation.moveTo's createPath(x, y, z, 1): a node within
// one block of the target reaches it.
// vanilla: PathNavigation.moveTo
const navReach = 1

// navOf shares navigation only along the chain this rider controls.
// vanilla: Mob.getNavigation
func (m *Mob) navOf() *Mob {
	if v := m.Vehicle; v != nil && v.controllingPassenger() == m {
		return v.navOf()
	}
	return m
}

// canUpdatePath is GroundPathNavigation.canUpdatePath: on the ground or in
// a liquid.
func (m *Mob) canUpdatePath() bool {
	o := m.navOf()
	return o.OnGround || o.InWater() || o.InLava()
}

// createPath plans a path to the position without ordering it
// (PathNavigation.createPath). It is nil only on vanilla's early returns
// (cannot update the path, or below the world); an unreachable target gives
// the empty path to the body's own cell. The position's block is the
// targetPos later asks are matched against.
// vanilla: PathNavigation.createPath
func (m *Mob) createPath(w World, x, y, z float64) ([]Waypoint, Waypoint, bool) {
	o := m.navOf()
	if o.Type == TypeDolphin {
		return o.createSwimPath(w, x, y, z, 0)
	}
	if !o.canUpdatePath() || o.Y < float64(w.MinY()) {
		return nil, Waypoint{}, false
	}
	end := cellAt(x, y, z)
	foot := o.pathFootprint(w)
	target, ok := walkableNear(w, foot, x, y, z)
	if !ok {
		return nil, end, true
	}
	// vanilla: MeleeAttackGoal.canUse plans createPath(target, 0)
	path, _ := findPath(w, foot, o.cell(), target, int32(o.maxPathLength()), 0)
	return o.trimForSun(w, raiseCauldronNodes(w, path)), end, true
}

// setFireMaluses puts the kind's own fire and water prices on a footprint.
func (m *Mob) setFireMaluses(f *footprint) {
	if m.nav.waterFree || waterFreeKind(m) {
		f.waterMalus = 0
	}
	if m.kind().fireAverse {
		f.fireMalus, f.nearFireMalus = -1, 16
	}
	if m.Type == TypeSniffer {
		// vanilla: Sniffer.onPathfindingStart, the water price lifted to 0 for
		// one search begun on fire or in water
		f.waterMalus = -1
		if m.OnFire() || m.InWater() {
			f.waterMalus = 0
		}
	}
	if m.Type == TypeCopperGolem {
		// vanilla: the CopperGolem constructor
		f.nearDamageMalus = 16
	}
	if m.Type == TypeBreeze {
		f.fireMalus, f.trapdoorMalus = -1, -1
	}
	if m.Type == TypeEnderman {
		f.waterMalus = -1
	}
	if m.Type == TypeBlaze {
		// vanilla: Blaze.<init> setPathfindingMalus
		f.waterMalus, f.lavaMalus, f.nearFireMalus, f.fireMalus = -1, 8, 0, 0
	}
	f.lavaWalker = m.kind().walksOnLava
	if m.Type == TypeStrider {
		// vanilla: Strider.<init>
		f.fireMalus, f.nearFireMalus, f.lavaMalus, f.waterMalus = 0, 0, 0, -1
	}
	if m.Type == TypeWarden {
		// vanilla: Warden.<init> setPathfindingMalus
		f.railMalus, f.damageMalus, f.powderSnowMalus = 0, 8, 8
		f.lavaMalus, f.nearFireMalus, f.fireMalus = 8, 0, 0
	}
	switch m.Type {
	case TypeRavager:
		// vanilla: Ravager.<init> setPathfindingMalus
		f.leavesMalus = 0
	case TypeWitherSkeleton:
		// vanilla: WitherSkeleton.<init> setPathfindingMalus
		f.lavaMalus = 8
	case TypeZombifiedPiglin:
		// vanilla: ZombifiedPiglin.<init> setPathfindingMalus
		f.lavaMalus = 8
	case TypeCreaking:
		// vanilla: Creaking.<init> setPathfindingMalus
		f.damageMalus, f.powderSnowMalus = 8, 8
		f.lavaMalus, f.fireMalus, f.nearFireMalus = 8, 0, 0
	case TypeFox:
		// vanilla: Fox.<init> setPathfindingMalus
		f.damageMalus, f.nearDamageMalus = 0, 0
	case TypeCopperGolem:
		// vanilla: CopperGolem.<init> setPathfindingMalus
		f.nearDamageMalus = 16
	case TypeSniffer:
		// vanilla: Sniffer.<init> setPathfindingMalus
		f.cautiousMalus, f.onPowderSnowMalus = -1, -1
	case TypeGoat, TypeWolf:
		// vanilla: Goat.<init>, Wolf.<init> setPathfindingMalus
		f.onPowderSnowMalus = -1
	}
}

// cellAt is the block a world position falls in, vanilla's BlockPos.containing.
func cellAt(x, y, z float64) Waypoint {
	return Waypoint{int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))}
}

// seekTo points the mob at a position, called by a goal every tick. It plans
// to the nearest walkable cell when no path stands or the standing one goes
// to another block; navigate does the walking, so a path is not replanned
// until spent or invalid. The search may range as far as the follow range,
// so a target in range is plannable. reachRange is navReach for plain
// navigation, none for the wall climber. It reports whether a path is held
// (even a partial or empty one), as moveTo returns true.
func (m *Mob) seekTo(w World, x, y, z float64, speedFactor float64, reachRange int32) bool {
	o := m.navOf()
	if IsNautilus(o.Type) || o.Type == TypeTadpole || o.Type == TypeDolphin {
		return o.swimTo(w, x, y, z, speedFactor)
	}
	// A flyer without a path planner drives straight at the point.
	if o.kind().flying {
		if o.kind().flyPath != nil {
			held, _ := o.seekToFlyPath(w, x, y, z, speedFactor, reachRange, 1)
			return held
		}
		o.seekToFly(x, y, z, speedFactor)
		return true
	}
	o.nav.speedFactor = speedFactor
	end := cellAt(x, y, z)
	// vanilla: WallClimberNavigation.createPath records the target before the
	// search, so it stands even when no path is found.
	if o.is(TypeSpider) {
		o.nav.climbTo, o.nav.climbSet = end, true
	}
	// vanilla: PathNavigation.createPath — a standing, unfinished path to the
	// same target block is returned unchanged.
	if len(o.nav.path) > 0 && o.nav.end == end {
		// A successful moveTo re-arms the progress sample on every ask, so the
		// sampling only runs on a walk nothing keeps asking after.
		o.nav.lastStuckCheck = o.nav.navTick
		o.nav.stuckX, o.nav.stuckY, o.nav.stuckZ = o.X, o.Y, o.Z
		return true
	}
	o.nav.path = nil
	foot := o.pathFootprint(w)
	target, ok := walkableNear(w, foot, x, y, z)
	if !ok {
		return false
	}
	path, reached := findPath(w, foot, o.cell(), target, int32(o.maxPathLength()), reachRange)
	// A partial path to an unreachable target still stands; only no
	// standable cell, or an empty unreached result, is no path.
	if len(path) == 0 && !reached {
		return false
	}
	o.moveTo(o.trimForSun(w, raiseCauldronNodes(w, path)), end, speedFactor)
	o.nav.reach = reachRange
	return true
}

// recomputePath replans the standing path to its target now, or at the next
// tick when one was made within the last 20.
// vanilla: PathNavigation.recomputePath
func (m *Mob) recomputePath(w World) {
	o := m.navOf()
	if o.nav.navTick-o.nav.lastRecompute > 20 && o.canUpdatePath() {
		end, reach, pace := o.nav.end, o.nav.reach, o.nav.speedFactor
		o.nav.path = nil
		o.seekTo(w, float64(end.X)+0.5, float64(end.Y), float64(end.Z)+0.5, pace, reach)
		o.nav.lastRecompute = o.nav.navTick
		o.nav.delayedRecompute = false
	} else {
		o.nav.delayedRecompute = true
	}
}

// BlocksChanged replans the path of every mob whose walk a changed block's
// cell (one whose collision shape changed) lies near.
// vanilla: PathNavigation.shouldRecomputePath
func (h Mobs) BlocksChanged(w World, cells []Waypoint) {
	if len(cells) == 0 {
		return
	}
	for _, m := range h {
		if m.navOf() != m {
			continue
		}
		if m.nav.delayedRecompute || len(m.nav.path) == 0 || m.nav.swim || m.kind().flying {
			continue
		}
		end := m.nav.path[len(m.nav.path)-1]
		mx := (float64(end.X) + m.X) / 2
		my := (float64(end.Y) + m.Y) / 2
		mz := (float64(end.Z) + m.Z) / 2
		n := float64(len(m.nav.path))
		for _, c := range cells {
			dx, dy, dz := float64(c.X)+0.5-mx, float64(c.Y)+0.5-my, float64(c.Z)+0.5-mz
			if dx*dx+dy*dy+dz*dz < n*n {
				m.recomputePath(w)
				break
			}
		}
	}
}

// pathFootprint is the body the pathfinder fits through the world.
func (m *Mob) pathFootprint(w World) footprint {
	foot := footprintOf(m.width(), m.height())
	foot.jumpHeight = max(mobJumpHeight, m.stepHeight())
	foot.overFences = IsCamel(m.Type)
	foot.floats = m.canFloat
	foot.flatDistance = m.Type == TypeWarden
	foot.amphibious = m.Type == TypeTurtle || m.Type == TypeAxolotl || m.Type == TypeFrog || m.Type == TypeDrowned
	foot.openDoors = m.canOpenDoors()
	foot.maxFall = m.maxFallDistance(w)
	m.setFireMaluses(&foot)
	m.setRailMalus(w, &foot)
	m.setHomeLeash(&foot)
	return foot
}

// setHomeLeash keeps a bound creaking's paths within its heart's reach.
func (m *Mob) setHomeLeash(f *footprint) {
	if m.Type == TypeCreaking && m.creaking.bound {
		f.leashed, f.home = true, m.creaking.home
		f.mobDistSq = homeDistSq(f.home, cellAt(m.X, m.Y, m.Z))
	}
}

// setRailMalus lets a body on or over a rail (its cell or the one below) path
// over rails.
// vanilla: WalkNodeEvaluator.getPathTypeWithinMobBB
func (m *Mob) setRailMalus(w World, f *footprint) {
	c := m.cell()
	if blockPathType(w.BlockAt(c.X, c.Y, c.Z)) == pathRail || blockPathType(w.BlockAt(c.X, c.Y-1, c.Z)) == pathRail {
		f.railMalus = 0
	}
}

// moveTo installs a planned path (PathNavigation.moveTo): waypoints, target
// block, pace, and a fresh stuck watch. A nil path clears the navigation.
func (m *Mob) moveTo(path []Waypoint, end Waypoint, speedFactor float64) {
	o := m.navOf()
	o.nav.speedFactor = speedFactor
	o.nav.path = path
	o.nav.origin = o.cell()
	o.nav.passed = o.nav.passed[:0]
	o.nav.end = end
	o.nav.reach = 0
	o.nav.swim = false
	o.nav.timeoutArmed, o.nav.timeoutTimer, o.nav.timeoutLimit = false, 0, 0
	o.nav.lastStuckCheck = o.nav.navTick
	o.nav.stuckX, o.nav.stuckY, o.nav.stuckZ = o.X, o.Y, o.Z
}

// navDone is PathNavigation.isDone: no path left to walk, and no flight
// order standing.
func (m *Mob) navDone() bool {
	o := m.navOf()
	return len(o.nav.path) == 0 && !o.nav.flyActive
}

// stopSeeking drops the path (and a flyer's order) so the next goal starts
// from standing still.
func (m *Mob) stopSeeking() {
	o := m.navOf()
	o.nav.path = nil
	o.nav.flyActive = false
}

// waypointPoint is the x, z a body of the width steers to for a node: the
// centre of the footprint cornered at the node, so a two-cell body aims at
// the corner between its cells. The reached check and stuck watch use the
// cell centre instead.
// vanilla: Path.getEntityPosAtNode
func waypointPoint(node Waypoint, width float64) (x, z float64) {
	offset := math.Floor(width+1) * 0.5
	return float64(node.X) + offset, float64(node.Z) + offset
}

// atWaypoint reports whether dx, dz from a node's centre reach it. A body
// wider than 0.75 steers to its footprint corner, so it is reached per axis
// within half its width; narrower ones use waypointReached.
// vanilla: PathNavigation.maxDistanceToWaypoint
func (m *Mob) atWaypoint(dx, dz float64) bool {
	if w := m.width(); w > 0.75 {
		return math.Abs(dx) < w/2 && math.Abs(dz) < w/2
	}
	return math.Hypot(dx, dz) <= waypointReached
}

// waypointReached is the reach radius around a waypoint's centre, well
// within a step so the mob banks it a tick early rather than circling.
const waypointReached = 0.35

// closerToCentre is Vec3i.closerToCenterThan: pos within distance of the
// block's centre.
func closerToCentre(c Waypoint, pos [3]float64, distance float64) bool {
	return Dist2(float64(c.X)+0.5, float64(c.Y)+0.5, float64(c.Z)+0.5, pos[0], pos[1], pos[2]) < distance*distance
}

// navigate walks the standing path one tick: the next waypoint steers, the
// planning goal's pace drives the legs, and a jump is ordered when the
// waypoint is more than a step above the feet and within a body width, or
// when the mob's own cell holds a collision shape above its feet that is not
// a door or fence; headroom was checked by the pathfinder. With no path the
// legs idle, unless a wall climber's target stands.
func (m *Mob) navigate(w World) {
	o := m.navOf()
	// The strafe a rider asked for in its own tick is consumed here, once;
	// an earlier reset in this body's tick would drop it unseen.
	// vanilla: MoveControl.tick, MoveControl.Operation.STRAFE
	if o.riderWalk.set {
		o.walk.dirX, o.walk.dirZ = o.riderWalk.dirX, o.riderWalk.dirZ
		o.walk.speed = o.riderWalk.speed
		o.riderWalk.set = false
	}
	o.nav.navTick++
	if o.nav.delayedRecompute {
		o.recomputePath(w)
	}
	for len(o.nav.path) > 0 {
		centreX := float64(o.nav.path[0].X) + 0.5
		centreZ := float64(o.nav.path[0].Z) + 0.5
		if (!o.atWaypoint(centreX-o.X, centreZ-o.Z) || o.swimsBelow(o.nav.path[0])) && !o.turtleCutsCorner(w) {
			break
		}
		o.nav.advance()
	}
	if len(o.nav.path) == 0 {
		if !o.nav.climbSet {
			return
		}
		// vanilla: WallClimberNavigation.tick
		c, pos := o.nav.climbTo, [3]float64{o.X, o.Y, o.Z}
		if closerToCentre(c, pos, o.width()) ||
			o.Y > float64(c.Y) && closerToCentre(Waypoint{c.X, int32(math.Floor(o.Y)), c.Z}, pos, o.width()) {
			o.nav.climbSet = false
			return
		}
		// vanilla: MoveControl.MOVE_TO, aimed at the block's corner
		dx, dz := float64(c.X)-o.X, float64(c.Z)-o.Z
		length := math.Hypot(dx, dz)
		if length < 1e-9 {
			return
		}
		o.walk.dirX, o.walk.dirZ = dx/length, dz/length
		o.walk.speed = o.pathSpeed()
		o.Yaw = float32(yawToward(o.walk.dirX, o.walk.dirZ))
		return
	}

	// PathNavigation.doStuckDetection, wherever canUpdatePath allows: a walk
	// that stopped making way is dropped before the legs carry it on.
	feet := o.cell()
	if o.OnGround || rules.IsWater(w.BlockAt(feet.X, feet.Y, feet.Z)) {
		o.stuckStep()
		if len(o.nav.path) == 0 {
			return
		}
	}

	centreX, centreZ := waypointPoint(o.nav.path[0], o.width())
	dx := centreX - o.X
	dz := centreZ - o.Z
	distance := math.Hypot(dx, dz)
	o.walk.dirX, o.walk.dirZ = 0, 0
	if distance > 1e-9 {
		o.walk.dirX, o.walk.dirZ = dx/distance, dz/distance
	}
	o.walk.speed = o.pathSpeed()
	o.Yaw = float32(yawToward(o.walk.dirX, o.walk.dirZ))
	o.turtleSwimDrive(centreX, centreZ)

	// MoveControl's jump: the waypoint's floor top (so a half slab counts as
	// the step it is) more than the step height above the feet, and near; or
	// a shape in the mob's own cell that rises above its feet.
	// vanilla: MoveControl.tick
	if o.OnGround {
		node := o.nav.path[0]
		floor := float64(node.Y-1) + blockExtent(w.BlockAt(node.X, node.Y-1, node.Z))
		cellY := math.Floor(o.Y)
		here := w.BlockAt(int32(math.Floor(o.X)), int32(cellY), int32(math.Floor(o.Z)))
		top := blockExtent(here)
		// vanilla compares the squared distance with the width unsquared
		if floor-o.Y > o.stepHeight() && dx*dx+dz*dz < math.Max(1.0, o.width()) ||
			top > 0 && o.Y < cellY+top && !registry.BlockOf(here).InTag(registry.TagIDDoors) && !registry.BlockOf(here).InTag(registry.TagIDFences) {
			o.jump = true
		}
	}
}

// stuckStep is PathNavigation.doStuckDetection. A path is dropped when a
// 100-tick sample shows less than a quarter of 100 ticks' travel at the
// pace, or when the current waypoint has stalled three times its timeout.
// The stalled tally carries across reached waypoints, so a walk behind
// schedule all along is dropped at its next stall.
func (m *Mob) stuckStep() {
	m.stuckStepAt(m.navOf().pathSpeed())
}

// stuckStepAt is stuckStep at mob.getSpeed(), the pace times the walk's
// factor.
func (m *Mob) stuckStepAt(speed float64) {
	o := m.navOf()
	if o.nav.navTick-o.nav.lastStuckCheck > stuckCheckInterval {
		f := speed
		if f < 1.0 {
			f *= f
		}
		threshold := f * stuckCheckInterval * stuckDistanceFactor
		dx, dy, dz := o.X-o.nav.stuckX, o.Y-o.nav.stuckY, o.Z-o.nav.stuckZ
		madeWay := dx*dx+dy*dy+dz*dz >= threshold*threshold
		o.nav.lastStuckCheck = o.nav.navTick
		o.nav.stuckX, o.nav.stuckY, o.nav.stuckZ = o.X, o.Y, o.Z
		if !madeWay {
			o.nav.path = nil
			return
		}
	}
	node := o.nav.path[0]
	if o.nav.timeoutArmed && node == o.nav.timeoutNode {
		o.nav.timeoutTimer++
	} else {
		o.nav.timeoutNode = node
		o.nav.timeoutArmed = true
		// vanilla: Vec3.atBottomCenterOf(timeoutCachedNode)
		dx := o.X - (float64(node.X) + 0.5)
		dy := o.Y - float64(node.Y)
		dz := o.Z - (float64(node.Z) + 0.5)
		if speed > 0 {
			o.nav.timeoutLimit = math.Sqrt(dx*dx+dy*dy+dz*dz) / speed * 20
		} else {
			o.nav.timeoutLimit = 0
		}
	}
	if o.nav.timeoutLimit > 0 && float64(o.nav.timeoutTimer) > o.nav.timeoutLimit*3 {
		o.nav.timeoutArmed, o.nav.timeoutTimer, o.nav.timeoutLimit = false, 0, 0
		o.nav.path = nil
	}
}

// maxFallDistance is the step drop: 3 with no target; with one, 3 plus the
// health beyond a third of max, less 4 per difficulty level below hard. A
// creeper adds its health less one instead.
// vanilla: Mob.getMaxFallDistance, Creeper.getMaxFallDistance
func (m *Mob) maxFallDistance(w World) int32 {
	if m.TargetID == 0 {
		return defaultMaxFall
	}
	if m.Type == TypeCreeper {
		return int32(math.Floor(m.Health - 1 + 3))
	}
	// float32 as vanilla: in float64 100*0.33 truncates one block short.
	sacrifice := int32(float32(m.Health) - float32(m.healthCap())*0.33)
	sacrifice -= (3 - int32(w.Difficulty())) * 4
	return defaultMaxFall + max(sacrifice, 0)
}

// moveControl applies the navigation input for the mob's movement kind.
func (m *Mob) moveControl(w World) {
	if m.Type == TypeDolphin {
		m.dolphinMoveControl(w)
	} else if IsNautilus(m.Type) {
		m.nautilusMoveControl(w)
	} else if m.Type == TypeTadpole {
		_, m.tadpole.up = m.smoothSwimMoveControl(w, 0.02, 0.1)
		if m.InWater() {
			m.VY += 0.005
		}
	} else if m.Type == TypeAxolotl {
		m.axolotlMoveControl(w)
	} else if m.Type == TypeFrog {
		// vanilla: Frog.<init>'s SmoothSwimmingMoveControl(85, 10, 0.02f, 0.1f, true)
		m.frog.speed, m.frog.up = m.smoothSwimMoveControl(w, float64(float32(0.02)), float64(float32(0.1)))
		if m.InWater() {
			m.VY += 0.005
		}
	} else if m.Type == TypeDrowned && m.nav.swim {
		m.swimNavTickAt(w, m.pace()*meleePace)
	} else if !m.Sleeping {
		m.navigate(w)
	}
}

// waterFreeKind reports a kind whose constructor prices water at 0.
// vanilla: WaterAnimal, AgeableWaterCreature, Axolotl, Turtle, Chicken,
// AbstractNautilus, Guardian and Drowned <init>
func waterFreeKind(m *Mob) bool {
	switch m.Type {
	case TypeAxolotl, TypeTurtle, TypeChicken, TypeDrowned, TypeDolphin:
		return true
	}
	return m.kind().water || m.IsGuardian() || IsNautilus(m.Type)
}
