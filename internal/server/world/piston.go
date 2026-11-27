package world

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// Pistons: the block event a piston's power reading queues, the structure
// resolver that says what moves, the rides that carry the blocks and the
// landings that put them down.
// vanilla: PistonBaseBlock

// pistonShift moves a position the given direction the given cells.
func pistonShift(x, y, z int32, d, n int) (int32, int32, int32) {
	delta := dirDelta[d]
	return x + delta[0]*int32(n), y + delta[1]*int32(n), z + delta[2]*int32(n)
}

// pistonStateOf is a piston base's state with that facing and extension.
func pistonStateOf(sticky bool, d int, extended bool) uint16 {
	return pistonStates[b2i(sticky)][d][b2i(extended)]
}

// pistonStates holds the piston base states by sticky, facing and extended.
var pistonStates = func() (t [2][6][2]uint16) {
	for s, name := range [2]string{"minecraft:piston", "minecraft:sticky_piston"} {
		for d := range 6 {
			for e, ext := range [2]string{"false", "true"} {
				t[s][d][e], _ = registry.FindState(name, map[string]string{"facing": blocklogic.FaceName(d), "extended": ext})
			}
		}
	}
	return t
}()

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// pistonHeadStateOf is the head a piston extends with, facing the push with the
// piston's type. short stays false: a short extended head leaves a four-pixel
// gap the arm does not reach.
// vanilla: PistonBaseBlock.moveBlocks
func pistonHeadStateOf(d int, sticky bool) uint16 {
	return pistonRideStates[0][d][b2i(sticky)]
}

// pistonRideStates holds the head states (short false) and moving piston
// states, by facing and sticky.
var pistonRideStates = func() (t [2][6][2]uint16) {
	for d := range 6 {
		for s, typ := range [2]string{"normal", "sticky"} {
			t[0][d][s], _ = registry.FindState("minecraft:piston_head",
				map[string]string{"facing": blocklogic.FaceName(d), "type": typ, "short": "false"})
			t[1][d][s], _ = registry.FindState("minecraft:moving_piston",
				map[string]string{"facing": blocklogic.FaceName(d), "type": typ})
		}
	}
	return t
}()

// movingPistonStateOf is the moving piston state with the ride's facing and the
// piston's type. Pushed blocks' rides take the default type (moveBlocks sets
// only their facing); the head's ride and the base's retraction take the
// piston's own.
func movingPistonStateOf(d int, sticky bool) uint16 {
	return pistonRideStates[1][d][b2i(sticky)]
}

// pistonHardNames are the obsidian family, which isPushable refuses before
// reading any push reaction.
var pistonHardBlocks = blocksNamed("minecraft:obsidian", "minecraft:crying_obsidian",
	"minecraft:respawn_anchor", "minecraft:reinforced_deepslate")

// pistonStickyBlocks marks the blocks pistonIsSticky accepts.
var pistonStickyBlocks = blockSet(registry.BlockSlimeBlock, registry.BlockHoneyBlock)

// pistonIsSticky reports the two sticky blocks that chain a move; a sticky
// piston's own head-pull is the retraction's business.
// vanilla: PistonStructureResolver.isSticky
func pistonIsSticky(b registry.Block) bool { return pistonStickyBlocks[b] }

// pistonCanStickToEachOther reports whether two neighbours ride one move
// together: honey and slime refuse each other, and a sticky block takes
// anything else it touches.
// vanilla: PistonStructureResolver.canStickToEachOther
func pistonCanStickToEachOther(a, b registry.Block) bool {
	if a == registry.BlockHoneyBlock && b == registry.BlockSlimeBlock {
		return false
	}
	if a == registry.BlockSlimeBlock && b == registry.BlockHoneyBlock {
		return false
	}
	return pistonIsSticky(a) || pistonIsSticky(b)
}

// pistonFittingBase reports a base the head fits on: an extended piston of the
// head's type facing its way, or the ride putting one there with that facing
// (canSurvive's moving-piston clause).
func (p *Physics) pistonFittingBase(x, y, z int32, headFacing int, sticky bool) bool {
	state := p.w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	want := registry.BlockPiston
	if sticky {
		want = registry.BlockStickyPiston
	}
	if block == want {
		return facingOfState(state) == headFacing && registry.StateBool(state, registry.PropExtended)
	}
	return block == registry.BlockMovingPiston && facingOfState(state) == headFacing
}

// pistonHeadCanSurvive reports an extended base of its kind facing its way, or
// a ride of its facing, behind the head.
// vanilla: PistonHeadBlock.canSurvive
func (p *Physics) pistonHeadCanSurvive(x, y, z int32, state uint16) bool {
	f := facingOfState(state)
	bx, by, bz := pistonShift(x, y, z, blocklogic.Opposite(f), 1)
	return p.pistonFittingBase(bx, by, bz, f, registry.StateValue(state, registry.PropType) == "sticky")
}

// pistonPushable says what a move may do with the cell's block: air pushes;
// obsidian, the unbreakable and the stand-fast never move; destroy-on-push go
// only when the move may break them; push-only go only along the piston's
// facing; anything else moves unless it has a block entity. An extended piston
// never moves, and the dimension's build height and the world border end a
// move.
// vanilla: PistonBaseBlock.isPushable
func (p *Physics) pistonPushable(state uint16, x, y, z int32, moveDir, pistonFacing int, allowDestroy bool) bool {
	if int(state) >= registry.StateCount {
		return false
	}
	minY, maxY := p.w.MinY(), p.w.MaxY()
	if y < minY || y >= maxY || !p.w.InBorder(x, z) {
		return false
	}
	b := registry.BlockOf(state)
	if airBlocks[b] {
		return true
	}
	if pistonHardBlocks[b] {
		return false
	}
	if moveDir == dirDown && y == minY || moveDir == dirUp && y == maxY-1 {
		return false
	}
	if pistonBlocks[b] && registry.StateBool(state, registry.PropExtended) {
		return false
	}
	if registry.PistonUnbreakableBlocks[b] || registry.PistonImmovableBlocks[b] {
		return false
	}
	if registry.PistonPushOnlyBlocks[b] {
		return moveDir == pistonFacing
	}
	if registry.PistonDestroyOnPushBlocks[b] {
		return allowDestroy
	}
	return !registry.PistonHasBlockEntityBlocks[b]
}

// pistonNeighborSignal reads power at the five faces but the front, then around
// the cell above: the quasi-connectivity only pistons have.
// vanilla: PistonBaseBlock.getNeighborSignal
func (p *Physics) pistonNeighborSignal(x, y, z int32, facing int) bool {
	for d := range 6 {
		if d == facing {
			continue
		}
		if p.signalAt(x, y, z, d) > 0 {
			return true
		}
	}
	ux, uy, uz := x, y+1, z
	for _, d := range updateNeighborOrder {
		if d == dirDown {
			continue
		}
		if p.signalAt(ux, uy, uz, d) > 0 {
			return true
		}
	}
	return false
}

// pistonResolver works out which cells a move takes and breaks: a line out from
// the start cell, sticky blocks reaching back for the chains they drag and
// sideways for their own lines, ended by twelve cells or a refusal.
// vanilla: PistonStructureResolver
type pistonResolver struct {
	p                      *Physics
	x, y, z                int32
	dir                    int
	pushDir                int
	extending              bool
	startX, startY, startZ int32
	toPush                 [][3]int32
	toDestroy              [][3]int32
}

// newPistonResolver builds one move's resolver: an extend pushes the facing
// from the cell in front; a retraction pulls the other way from one cell
// further, the block it brings home.
func (p *Physics) newPistonResolver(x, y, z int32, dir int, extending bool) pistonResolver {
	pushDir := dir
	sx, sy, sz := pistonShift(x, y, z, dir, 1)
	if !extending {
		pushDir = blocklogic.Opposite(dir)
		sx, sy, sz = pistonShift(x, y, z, dir, 2)
	}
	return pistonResolver{p: p, x: x, y: y, z: z, dir: dir, pushDir: pushDir,
		extending: extending, startX: sx, startY: sy, startZ: sz}
}

// resolve: the first cell decides whether the move runs at all (an immovable
// non-breakable stops it, a breakable in an extend is broken with nothing
// pushed), then the line and the sticky branches fill the lists.
// vanilla: PistonStructureResolver.resolve
func (r *pistonResolver) resolve() bool {
	r.toPush, r.toDestroy = nil, nil
	state := r.p.w.BlockAt(r.startX, r.startY, r.startZ)
	if !r.p.pistonPushable(state, r.startX, r.startY, r.startZ, r.pushDir, r.dir, false) {
		if r.extending {
			if registry.PistonDestroyOnPushBlocks[registry.BlockOf(state)] {
				r.toDestroy = append(r.toDestroy, [3]int32{r.startX, r.startY, r.startZ})
				return true
			}
		}
		return false
	}
	if !r.addBlockLine(r.startX, r.startY, r.startZ, r.pushDir) {
		return false
	}
	for i := 0; i < len(r.toPush); i++ {
		pos := r.toPush[i]
		if pistonIsSticky(registry.BlockOf(r.p.w.BlockAt(pos[0], pos[1], pos[2]))) && !r.addBranchingBlocks(pos) {
			return false
		}
	}
	return true
}

// addBlockLine walks one direction from one cell: a sticky origin reaches back
// for the chain it drags, the walk runs forward to the line's end, and a
// collision with a carried cell splices the lists.
// vanilla: PistonStructureResolver.addBlockLine
func (r *pistonResolver) addBlockLine(ox, oy, oz int32, direction int) bool {
	state := r.p.w.BlockAt(ox, oy, oz)
	block := registry.BlockOf(state)
	if airBlocks[block] {
		return true
	}
	if !r.p.pistonPushable(state, ox, oy, oz, r.pushDir, direction, false) {
		return true
	}
	if ox == r.x && oy == r.y && oz == r.z {
		return true
	}
	for _, pos := range r.toPush {
		if pos[0] == ox && pos[1] == oy && pos[2] == oz {
			return true
		}
	}
	numToMove := 1
	if numToMove+len(r.toPush) > 12 {
		return false
	}
	if pistonIsSticky(block) {
		// Walk back along the dragged chain. The last block that sticks but is
		// not sticky still rides: the count is taken before the walk asks
		// whether to go on.
		bx, by, bz := pistonShift(ox, oy, oz, blocklogic.Opposite(r.pushDir), numToMove)
		prev := block
		for {
			cur := r.p.w.BlockAt(bx, by, bz)
			curBlock := registry.BlockOf(cur)
			if airBlocks[curBlock] {
				break
			}
			if !pistonCanStickToEachOther(prev, curBlock) {
				break
			}
			if !r.p.pistonPushable(cur, bx, by, bz, r.pushDir, blocklogic.Opposite(r.pushDir), false) {
				break
			}
			if bx == r.x && by == r.y && bz == r.z {
				break
			}
			numToMove++
			if numToMove+len(r.toPush) > 12 {
				return false
			}
			prev = curBlock
			if !pistonIsSticky(curBlock) {
				break
			}
			bx, by, bz = pistonShift(ox, oy, oz, blocklogic.Opposite(r.pushDir), numToMove)
		}
	}
	// The chain, from the farthest back to the origin itself.
	numMoved := 0
	for k := numToMove - 1; k >= 0; k-- {
		cx, cy, cz := pistonShift(ox, oy, oz, blocklogic.Opposite(r.pushDir), k)
		r.toPush = append(r.toPush, [3]int32{cx, cy, cz})
		numMoved++
	}
	// Forward until the line ends, hits something breakable, or collides with a
	// carried cell.
	for l := 1; ; l++ {
		qx, qy, qz := pistonShift(ox, oy, oz, r.pushDir, l)
		index := -1
		for i, pos := range r.toPush {
			if pos[0] == qx && pos[1] == qy && pos[2] == qz {
				index = i
				break
			}
		}
		if index > -1 {
			r.reorderListAtCollision(numMoved, index)
			for n := 0; n <= index+numMoved; n++ {
				pos := r.toPush[n]
				if pistonIsSticky(registry.BlockOf(r.p.w.BlockAt(pos[0], pos[1], pos[2]))) && !r.addBranchingBlocks(pos) {
					return false
				}
			}
			return true
		}
		qState := r.p.w.BlockAt(qx, qy, qz)
		qBlock := registry.BlockOf(qState)
		if airBlocks[qBlock] {
			return true
		}
		if !r.p.pistonPushable(qState, qx, qy, qz, r.pushDir, r.pushDir, true) ||
			qx == r.x && qy == r.y && qz == r.z {
			return false
		}
		if registry.PistonDestroyOnPushBlocks[qBlock] {
			r.toDestroy = append(r.toDestroy, [3]int32{qx, qy, qz})
			return true
		}
		if len(r.toPush) >= 12 {
			return false
		}
		r.toPush = append(r.toPush, [3]int32{qx, qy, qz})
		numMoved++
	}
}

// reorderListAtCollision moves the line's cells (backward chain and forward
// walk) ahead of the collided cell, so the move carries the line up to the
// collision.
// vanilla: PistonStructureResolver.reorderListAtCollision
func (r *pistonResolver) reorderListAtCollision(numMoved, index int) {
	head := append([][3]int32{}, r.toPush[:index]...)
	moved := append([][3]int32{}, r.toPush[len(r.toPush)-numMoved:]...)
	tail := append([][3]int32{}, r.toPush[index:len(r.toPush)-numMoved]...)
	r.toPush = append(append(head, moved...), tail...)
}

// addBranchingBlocks has a sticky block in the line reach across every side off
// the move's axis for the lines it drags.
// vanilla: PistonStructureResolver.addBranchingBlocks
func (r *pistonResolver) addBranchingBlocks(root [3]int32) bool {
	rootBlock := registry.BlockOf(r.p.w.BlockAt(root[0], root[1], root[2]))
	for d := range 6 {
		if axisOf(d) == axisOf(r.pushDir) {
			continue
		}
		qx, qy, qz := pistonShift(root[0], root[1], root[2], d, 1)
		qBlock := registry.BlockOf(r.p.w.BlockAt(qx, qy, qz))
		if !pistonCanStickToEachOther(rootBlock, qBlock) {
			continue
		}
		if !r.addBlockLine(qx, qy, qz, d) {
			return false
		}
	}
	return true
}

// axisOf is a direction's axis: 0 Y, 1 X, 2 Z.
// vanilla: Direction.getAxis
func axisOf(d int) int {
	switch d {
	case dirDown, dirUp:
		return 0
	case dirWest, dirEast:
		return 1
	default:
		return 2
	}
}

// pistonMoveBlocks drops and airs what breaks, turns every moving block into
// its ride, rides the head out on an extend, airs the vacated cells with their
// shape passes and wakes the disturbed cells, in vanilla's order with the write
// flags mapped onto the engine's.
// vanilla: PistonBaseBlock.moveBlocks
func (p *Physics) pistonMoveBlocks(x, y, z int32, dir int, extending, sticky bool, changes *[]BlockChange) bool {
	fx, fy, fz := pistonShift(x, y, z, dir, 1)
	if !extending {
		// A retraction first removes the head with the invisible flags-276
		// write.
		if hs := p.w.BlockAt(fx, fy, fz); registry.BlockOf(hs) == registry.BlockPistonHead {
			p.writeBlock(fx, fy, fz, airState)
		}
	}
	r := p.newPistonResolver(x, y, z, dir, extending)
	if !r.resolve() {
		return false
	}
	// All carried states are read before any write, so a ride carries what
	// stood in its cell, not what a later write left.
	states := make([]uint16, len(r.toPush))
	for i, pos := range r.toPush {
		states[i] = p.w.BlockAt(pos[0], pos[1], pos[2])
	}
	pushDir := dir
	if !extending {
		pushDir = blocklogic.Opposite(dir)
	}
	// Breaks run from the farthest back: loot rolls, the cell airs without its
	// own wake (flags 18), and any block entity goes too.
	destroyed := make([]uint16, len(r.toDestroy))
	for j := len(r.toDestroy) - 1; j >= 0; j-- {
		pos := r.toDestroy[j]
		destroyed[j] = p.w.BlockAt(pos[0], pos[1], pos[2])
		// The block entity rides the drop before removal, so a skull pushed to
		// destruction keeps its profile and name.
		tag, _ := p.w.BlockEntityTag(pos[0], pos[1], pos[2])
		p.w.RemoveBlockEntity(pos[0], pos[1], pos[2])
		p.setClient(pos[0], pos[1], pos[2], airState, changes)
		p.effects = append(p.effects, Effect{BreakDrop: destroyed[j], BreakTag: tag, X: pos[0], Y: pos[1], Z: pos[2]})
	}
	// Landing cells are spared the air-out: a ride moving into a cell another
	// leaves takes it silently.
	dests := make(map[[3]int32]bool, len(r.toPush)+1)
	for _, pos := range r.toPush {
		dx, dy, dz := pistonShift(pos[0], pos[1], pos[2], pushDir, 1)
		dests[[3]int32{dx, dy, dz}] = true
	}
	if extending {
		dests[[3]int32{fx, fy, fz}] = true
	}
	// Every moving block becomes its ride, in reverse, each written with the
	// invisible flags-324 and the shape pass it books, its block entity
	// carrying what the ride takes home.
	for j, pos := range slices.Backward(r.toPush) {

		dx, dy, dz := pistonShift(pos[0], pos[1], pos[2], pushDir, 1)
		rideState := movingPistonStateOf(dir, false)
		p.writeBlock(dx, dy, dz, rideState)
		p.shapeUpdateNeighbors(dx, dy, dz, changes)
		p.rideCells = append(p.rideCells, [3]int32{dx, dy, dz})
		p.pistonRideCreate([3]int32{dx, dy, dz}, rideState, states[j], dir, extending, false)
	}
	if extending {
		// The head's own ride, carrying the head out in front.
		rideState := movingPistonStateOf(dir, sticky)
		p.writeBlock(fx, fy, fz, rideState)
		p.shapeUpdateNeighbors(fx, fy, fz, changes)
		p.rideCells = append(p.rideCells, [3]int32{fx, fy, fz})
		p.pistonRideCreate([3]int32{fx, fy, fz}, rideState, pistonHeadStateOf(dir, sticky), dir, true, true)
	}
	// Vacated cells go to air without their own wake (flags 82), in the order
	// vanilla's map iterates.
	for _, pos := range r.toPush {
		if dests[pos] {
			continue
		}
		p.setClientMoved(pos[0], pos[1], pos[2], airState, changes)
	}
	for _, pos := range r.toPush {
		if dests[pos] {
			continue
		}
		p.shapeUpdateNeighbors(pos[0], pos[1], pos[2], changes)
	}
	// Destroyed cells' neighbours, pushed cells' origins and the extending
	// head's cell wake their neighbours, vanilla's last pass; the wakes cover
	// its direct and indirect shape updates.
	for j, pos := range slices.Backward(r.toDestroy) {

		p.redstoneAfterRemoval(pos[0], pos[1], pos[2], destroyed[j], false, false, changes)
		p.shapeUpdateNeighbors(pos[0], pos[1], pos[2], changes)
		p.wakeNeighbors(pos[0], pos[1], pos[2], destroyed[j], destroyed[j], changes)
	}
	for j, pos := range slices.Backward(r.toPush) {

		p.wakeNeighbors(pos[0], pos[1], pos[2], states[j], states[j], changes)
	}
	if extending {
		head := pistonHeadStateOf(dir, sticky)
		p.wakeNeighbors(fx, fy, fz, head, head, changes)
	}
	return true
}

// pistonWriteBase writes the extended piston's cell: the client copy, the
// neighbour update the flags-67 write gives the old block's neighbours, and the
// shape pass. The cell itself takes no visit; the piston's re-reading is the
// neighbour change after the wake.
func (p *Physics) pistonWriteBase(x, y, z int32, state, oldState uint16, changes *[]BlockChange) {
	p.setClientMoved(x, y, z, state, changes)
	p.wakeNeighbors(x, y, z, oldState, oldState, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
}

// pistonRemoveFront airs the head's cell like removeBlock's flags-3 write: a
// cell already air is declined, as setBlock declines an unchanged state;
// otherwise the block and any block entity go, neighbours wake and the shape
// pass runs.
func (p *Physics) pistonRemoveFront(x, y, z int32, changes *[]BlockChange) {
	if p.w.BlockAt(x, y, z) == airState {
		return
	}
	p.w.RemoveBlockEntity(x, y, z)
	p.setMoved(x, y, z, airState, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
}

// pistonRideCreate writes the moving piston's ride (carried state, direction,
// creation tick, read by the landing two ticks later) and books the landing.
func (p *Physics) pistonRideCreate(pos [3]int32, rideState, movedState uint16, dir int, extending, isSource bool) {
	if !p.w.setPistonRide(pistonRide{
		x: pos[0], y: pos[1], z: pos[2],
		movedState:  movedState,
		dir:         int8(dir),
		extending:   extending,
		isSource:    isSource,
		createdTick: p.now,
	}) {
		return // a ride whose chunk is gone carries nothing
	}
	if _, ok := p.rideSet[pos]; !ok {
		p.rides = append(p.rides, pos)
		p.rideSet[pos] = struct{}{}
	}
	p.schedulePistonFinalize(pos[0], pos[1], pos[2], p.now+2)
}

// pistonLandedHooks runs a landing's two passes after the write:
// updateFromNeighbourShapes's six shape offers, which re-book a moved
// observer's pulse, and onPlace, which stops the pulse of one landing powered
// and books a landed frosted ice's melt tick.
// oldState is the cell's state before the landing, onPlace's old block.
// vanilla: PistonMovingBlockEntity.tick, PistonMovingBlockEntity.finalTick
func (p *Physics) pistonLandedHooks(x, y, z int32, oldState uint16, changes *[]BlockChange) {
	for _, d := range updateShapeOrder {
		p.shapeUpdate(x, y, z, [3]int32{-d[0], -d[1], -d[2]}, changes)
	}
	p.redstoneOnPlace(x, y, z, oldState, changes)
	if registry.BlockOf(p.w.BlockAt(x, y, z)) == registry.BlockFrostedIce {
		// vanilla: FrostedIceBlock.onPlace
		p.schedule(x, y, z, p.now+frostedIcePlaceDelay(p.rng))
	}
}

// pistonEventTick runs one of the piston's block events. Vanilla's state
// identity check is an own-name check here (the engine gives pistons no block
// entity, so nothing carrying one can stand in), and the move uses the event's
// packed facing, which a retraction restores to the base however the cell reads
// now.
// vanilla: doBlockEvent
func (p *Physics) pistonEventTick(x, y, z int32, state uint16, event uint8, changes *[]BlockChange) {
	block := registry.BlockOf(state)
	if !pistonBlocks[block] {
		return
	}
	sticky := block == registry.BlockStickyPiston
	typ := int(event&3) - 1
	eventDir := int(event >> 2)
	p.pistonTriggerEvent(x, y, z, sticky, eventDir, typ, changes)
}

// pistonTriggerEvent runs the move: extend moves what the resolver found and
// writes the extended piston; retract takes the head's ride in, rides the base
// back and pulls one sticky chain; abort does so without the pull, leaving what
// the extend pushed. The animation packet and sound runBlockEvents adds are the
// effects the engine keeps.
// vanilla: PistonBaseBlock.triggerEvent
func (p *Physics) pistonTriggerEvent(x, y, z int32, sticky bool, eventDir, typ int, changes *[]BlockChange) bool {
	// The power reading uses the cell's facing now; the retraction's ride uses
	// the event's.
	dir := facingOfState(p.w.BlockAt(x, y, z))
	powered := p.pistonNeighborSignal(x, y, z, dir)
	if typ != 0 && powered {
		// A retract or abort with power back only sets the piston back to
		// extended; the head has nothing to bring home.
		p.setClient(x, y, z, pistonStateOf(sticky, dir, true), changes)
		return false
	}
	if typ == 0 && !powered {
		return false
	}
	if p.listens(HookPiston) && p.pistonVetoed(x, y, z, dir, typ, sticky) {
		return false
	}
	if typ == 0 {
		if !p.pistonMoveBlocks(x, y, z, dir, true, sticky, changes) {
			return false
		}
		p.pistonWriteBase(x, y, z, pistonStateOf(sticky, dir, true), pistonStateOf(sticky, dir, false), changes)
		p.effects = append(p.effects, Effect{
			SoundID: registry.SoundBlockPistonExtend,
			X:       x, Y: y, Z: z, Volume: 0.5,
			Pitch:            p.rng.Float32()*0.25 + 0.6,
			HasBlockAction:   true,
			BlockAction:      0,
			BlockActionData:  uint8(eventDir),
			BlockActionBlock: pistonBlockID(sticky),
		})
		return true
	}
	// Retract and abort share a path: the head's ride comes in, the piston
	// rides back, and a sticky piston checks whether the extend one tick ago
	// left it something two ahead to pull.
	fx, fy, fz := pistonShift(x, y, z, dir, 1)
	if _, ok := p.w.pistonRideAt(fx, fy, fz); ok {
		p.pistonForceFinalize(fx, fy, fz, changes)
	}
	movingState := movingPistonStateOf(dir, sticky)
	// vanilla: PistonBaseBlock.triggerEvent, setBlock flags 276, then
	// updateNeighborsAt and updateNeighbourShapes
	p.writeBlock(x, y, z, movingState)
	p.pistonRideCreate([3]int32{x, y, z}, movingState, pistonStateOf(sticky, eventDir, false), dir, false, true)
	p.wakeNeighbors(x, y, z, movingState, movingState, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
	if sticky {
		abort := false
		tx, ty, tz := pistonShift(x, y, z, dir, 2)
		ts := p.w.BlockAt(tx, ty, tz)
		if registry.BlockOf(ts) == registry.BlockMovingPiston {
			// The extending ride of last tick's extend finishes where it
			// stands, whatever its age: the zero-tick push. The test reads the
			// entity's direction, not the state's.
			if r, ok := p.w.pistonRideAt(tx, ty, tz); ok &&
				r.extending && int(r.dir) == dir {
				p.pistonForceFinalize(tx, ty, tz, changes)
				abort = true
			}
		}
		if !abort {
			if typ == 1 {
				tState := p.w.BlockAt(tx, ty, tz)
				if !airBlocks[registry.BlockOf(tState)] && p.pistonPushable(tState, tx, ty, tz, blocklogic.Opposite(dir), dir, false) &&
					p.pistonPullable(registry.BlockOf(tState)) {
					p.pistonMoveBlocks(x, y, z, dir, false, sticky, changes)
				} else {
					p.pistonRemoveFront(fx, fy, fz, changes)
				}
			} else {
				p.pistonRemoveFront(fx, fy, fz, changes)
			}
		}
	} else {
		p.pistonRemoveFront(fx, fy, fz, changes)
	}
	p.effects = append(p.effects, Effect{
		SoundID: registry.SoundBlockPistonContract,
		X:       x, Y: y, Z: z, Volume: 0.5,
		Pitch:            p.rng.Float32()*0.15 + 0.6,
		HasBlockAction:   true,
		BlockAction:      uint8(typ),
		BlockActionData:  uint8(eventDir),
		BlockActionBlock: pistonBlockID(sticky),
	})
	return true
}

// pistonPullable is the pull check's reaction half: the block two ahead must
// react like a plain block or be a piston; breakers, stand-fast and push-only
// blocks are not pulled.
func (p *Physics) pistonPullable(b registry.Block) bool {
	return pistonBlocks[b] || (!registry.PistonImmovableBlocks[b] &&
		!registry.PistonDestroyOnPushBlocks[b] && !registry.PistonPushOnlyBlocks[b])
}

// pistonBlockID is the block id the block action packet draws the move with.
func pistonBlockID(sticky bool) int32 {
	if sticky {
		return protocol.BlockStickyPiston
	}
	return protocol.BlockPiston
}

// pistonCheckIfExtend queues the move the piston's power calls for: power on an
// unextended piston queues an extend where the structure allows; power gone
// from an extended one queues a retract, or an abort when an extend's ride from
// two ticks ago is still running in front inside the handling tick (the
// zero-tick retraction, which leaves what the extend pushed).
// vanilla: checkIfExtend
func (p *Physics) pistonCheckIfExtend(x, y, z int32, state uint16, changes *[]BlockChange) {
	dir := facingOfState(state)
	extended := registry.StateBool(state, registry.PropExtended)
	typ := 0
	if p.pistonNeighborSignal(x, y, z, dir) {
		if extended {
			return
		}
		r := p.newPistonResolver(x, y, z, dir, true)
		if !r.resolve() {
			return
		}
	} else {
		if !extended {
			return
		}
		typ = 1
		tx, ty, tz := pistonShift(x, y, z, dir, 2)
		ts := p.w.BlockAt(tx, ty, tz)
		if registry.BlockOf(ts) == registry.BlockMovingPiston && facingOfState(ts) == dir {
			if r, ok := p.w.pistonRideAt(tx, ty, tz); ok &&
				r.extending && int(r.dir) == dir &&
				p.now >= r.createdTick+2 && p.handlingTick {
				typ = 2
			}
		}
	}
	p.schedulePistonEvent(x, y, z, int8(dir), uint8(typ))
}

// movingPistonFinalize is a ride's landing: a ride younger than its visit, or a
// cell that lost or rewrote it, is left alone; a finished ride lands its
// carried state, dropping waterlogging. pistonLandedHooks then runs the shape
// and on-place passes, but the carried state is not re-derived, so a landing
// never rewrites what it carried.
// vanilla: PistonMovingBlockEntity.tick
func (p *Physics) movingPistonFinalize(x, y, z int32, changes *[]BlockChange) {
	r, ok := p.w.pistonRideAt(x, y, z)
	if !ok {
		return
	}
	if p.now < r.createdTick+2 {
		// The landing is one tick after the ride finished; a younger ride waits
		// for it.
		p.schedulePistonFinalize(x, y, z, r.createdTick+2)
		return
	}
	movedState := r.movedState
	rideState := p.w.BlockAt(x, y, z)
	p.w.RemoveBlockEntity(x, y, z)
	if registry.BlockOf(rideState) != registry.BlockMovingPiston {
		return
	}
	if int(movedState) < registry.StateCount && registry.BlockOf(movedState) == registry.BlockAir {
		// A ride carrying nothing ends in the raw flags-340 write, nothing told
		// or woken: the carried state is air and updateOrDestroy does nothing.
		p.writeBlock(x, y, z, airState)
		return
	}
	// A landing out of water drops waterlogged, keeping the other properties.
	// vanilla: PistonMovingBlockEntity.tick
	finalState, _ := registry.WithBool(movedState, registry.PropWaterlogged, false)
	p.setClientMoved(x, y, z, finalState, changes)
	p.pistonLandedHooks(x, y, z, rideState, changes)
	p.wakeNeighbors(x, y, z, rideState, rideState, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
	p.updateAsNeighbor(x, y, z, changes, false, rideState, finalState, [3]int32{})
}

// pistonForceFinalize lands a ride at once for a retraction. A two-tick-old
// ride is already finished with its landing due this tick, and finalTick
// declines it; a running ride is taken down where it stands (the head's to air,
// a carried block's to that block) and the neighbours wake.
// vanilla: PistonMovingBlockEntity.finalTick
func (p *Physics) pistonForceFinalize(x, y, z int32, changes *[]BlockChange) {
	r, ok := p.w.pistonRideAt(x, y, z)
	if !ok {
		return
	}
	if p.now >= r.createdTick+3 {
		return
	}
	rideState := p.w.BlockAt(x, y, z)
	p.w.RemoveBlockEntity(x, y, z)
	if registry.BlockOf(rideState) != registry.BlockMovingPiston {
		return
	}
	// The ride is gone, so its landing must not run against the block this
	// landing writes.
	p.cancelPistonFinalize(x, y, z)
	finalState := uint16(airState)
	if !r.isSource {
		finalState = r.movedState
	}
	p.setClientMoved(x, y, z, finalState, changes)
	if !r.isSource {
		// The final tick lands the carried block through
		// updateFromNeighbourShapes and onPlace like a normal landing; the
		// head's ride lands only air, which needs neither.
		p.pistonLandedHooks(x, y, z, rideState, changes)
	}
	p.wakeNeighbors(x, y, z, rideState, rideState, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
	p.updateAsNeighbor(x, y, z, changes, false, rideState, finalState, [3]int32{})
}

// pistonVetoed asks the listeners before a move writes anything, with the cells
// an extend pushes (or breaks) or the one a sticky retraction pulls. A resolver
// run that finds the extend blocked asks nothing: no move happens anyway.
// vanilla: PistonBaseBlock.moveBlocks
func (p *Physics) pistonVetoed(x, y, z int32, dir, typ int, sticky bool) bool {
	d := HookDecision{Kind: HookPiston, X: x, Y: y, Z: z, Old: p.w.BlockAt(x, y, z), Dir: dir, Extending: typ == 0}
	if typ == 0 {
		r := p.newPistonResolver(x, y, z, dir, true)
		if !r.resolve() {
			return false
		}
		d.Blocks = append(append(d.Blocks, r.toPush...), r.toDestroy...)
	} else if sticky && typ == 1 {
		tx, ty, tz := pistonShift(x, y, z, dir, 2)
		ts := p.w.BlockAt(tx, ty, tz)
		if !airBlocks[registry.BlockOf(ts)] && p.pistonPushable(ts, tx, ty, tz, blocklogic.Opposite(dir), dir, false) &&
			p.pistonPullable(registry.BlockOf(ts)) {
			d.Blocks = [][3]int32{{tx, ty, tz}}
		}
	}
	return p.ask(&d)
}

// TakeRideCells returns and clears the cells a piston move turned into rides.
// The ride's write is invisible, so it is no block change; the server reshapes
// the fences, panes and walls beside it like any other cell's shape pass.
func (p *Physics) TakeRideCells() [][3]int32 {
	out := p.rideCells
	p.rideCells = nil
	return out
}
