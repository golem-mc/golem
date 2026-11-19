package world

import (
	mrand "math/rand"

	"github.com/golem-mc/golem/registry"
)

// The creaking heart: its support and the overworld's hour decide awake,
// dormant or uprooted, asked by its block tick, its block entity's ticker every
// 20-24 ticks and its placement.
// vanilla: CreakingHeartBlock, CreakingHeartBlockEntity

// CreakingActive reads gameplay/creaking_active.
func (w *World) CreakingActive() bool {
	return w.environmentGlobal(envCreakingActive) != 0
}

// CreakingHeartHasRequiredLogs reports whether the heart at (x, y, z) has a
// pale oak log at both ends of its axis, each log aligned with it; failing this
// uproots it.
// vanilla: CreakingHeartBlock.hasRequiredLogs
func CreakingHeartHasRequiredLogs(w *World, state uint16, x, y, z int32) bool {
	if _, _, ok := registry.BlockStateName(state); !ok {
		return false
	}
	axis := registry.StateValue(state, registry.PropAxis)
	var step [3]int32
	switch axis {
	case "x":
		step = [3]int32{1, 0, 0}
	case "z":
		step = [3]int32{0, 0, 1}
	default:
		step = [3]int32{0, 1, 0}
	}
	for _, d := range [2]int32{1, -1} {
		neighbour := w.BlockAt(x+step[0]*d, y+step[1]*d, z+step[2]*d)
		if !registry.BlockOf(neighbour).InTag(registry.TagIDPaleOakLogs) ||
			registry.StateValue(neighbour, registry.PropAxis) != axis {
			return false
		}
	}
	return true
}

// CreakingHeartNextState is the state the heart should have now (uprooted
// without both logs unless it holds a creaking, otherwise awake or dormant by
// the hour) and whether it differs: the block entity ticker's reread.
// vanilla: CreakingHeartBlockEntity.updateCreakingState
func CreakingHeartNextState(w *World, state uint16, x, y, z int32, linked bool) (uint16, bool) {
	if registry.BlockOf(state) != registry.BlockCreakingHeart {
		return state, false
	}
	want := "uprooted"
	if linked || CreakingHeartHasRequiredLogs(w, state, x, y, z) {
		want = "dormant"
		if w.CreakingActive() {
			want = "awake"
		}
	}
	if registry.StateValue(state, registry.PropCreakingHeartState) == want {
		return state, false
	}
	next, ok := registry.WithProp(state, registry.PropCreakingHeartState, want)
	if !ok {
		return state, false
	}
	return next, true
}

// CreakingHeartRootedState is the block tick's state, one game tick after a
// neighbour change: an uprooted heart that now has its logs takes the hour's
// state; anything else is unchanged.
// vanilla: CreakingHeartBlock.updateState
func CreakingHeartRootedState(w *World, state uint16, x, y, z int32) (uint16, bool) {
	if registry.BlockOf(state) != registry.BlockCreakingHeart {
		return state, false
	}
	if registry.StateValue(state, registry.PropCreakingHeartState) != "uprooted" || !CreakingHeartHasRequiredLogs(w, state, x, y, z) {
		return state, false
	}
	want := "dormant"
	if w.CreakingActive() {
		want = "awake"
	}
	next, ok := registry.WithProp(state, registry.PropCreakingHeartState, want)
	if !ok {
		return state, false
	}
	return next, true
}

// CreakingHeartPlacedState is a placed heart's state: uprooted stays while the
// logs are missing, otherwise awake or dormant by the hour; the axis is kept,
// and the item's block_state component applies after, getStateForPlacement's
// order.
// vanilla: CreakingHeartBlock.getStateForPlacement, updateState
func CreakingHeartPlacedState(w *World, state uint16, x, y, z int32) uint16 {
	next, _ := CreakingHeartRootedState(w, state, x, y, z)
	return next
}

// creakingHeartTick is the block tick a neighbour change books one game tick
// out: an uprooted heart whose logs are back roots, waking the neighbours.
// vanilla: CreakingHeartBlock.tick
func (p *Physics) creakingHeartTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if next, changed := CreakingHeartRootedState(p.w, state, x, y, z); changed {
		p.setShaped(x, y, z, next, changes)
	}
}

// The heart's protector spawn.
// vanilla: CreakingHeartBlockEntity.spawnProtector, SpawnUtil.trySpawnMob
const (
	creakingSpawnAttempts = 5
	creakingSpawnRangeXZ  = 16
	creakingSpawnRangeY   = 8
	// The creaking's box, for the spawn's collision check.
	// vanilla: EntityTypes.CREAKING
	creakingWidth  = 0.9
	creakingHeight = 2.7
)

// CreakingSpawnSpot finds where a heart at (x, y, z) stands its creaking: five
// tries at a random column within 16 blocks, each searching down from 8 above
// the heart for a block with a full top face, not leaves, under a cell with no
// collision, and taking it when the creaking's box fits there. It returns the
// feet, at the centre of the cell. The world border and entities in the way are
// not asked.
// vanilla: SpawnUtil.trySpawnMob, Strategy.ON_TOP_OF_COLLIDER_NO_LEAVES
func CreakingSpawnSpot(w *World, rng *mrand.Rand, x, y, z int32) (sx, sy, sz float64, ok bool) {
	for range creakingSpawnAttempts {
		cx := x + int32(rng.Intn(2*creakingSpawnRangeXZ+1)) - creakingSpawnRangeXZ
		cz := z + int32(rng.Intn(2*creakingSpawnRangeXZ+1)) - creakingSpawnRangeXZ
		if !w.ChunkLoadedAt(cx, cz) {
			continue
		}
		cy := y + creakingSpawnRangeY
		above := w.BlockAt(cx, cy, cz)
		for range 2*creakingSpawnRangeY + 1 {
			cy--
			here := w.BlockAt(cx, cy, cz)
			if CollisionTop(above) == 0 && !registry.BlockOf(here).InTag(registry.TagIDLeaves) &&
				registry.CollisionTopFull(here) {
				fx, fy, fz := float64(cx)+0.5, float64(cy+1), float64(cz)+0.5
				if !w.BoxCollides(fx-creakingWidth/2, fy, fz-creakingWidth/2,
					fx+creakingWidth/2, fy+creakingHeight, fz+creakingWidth/2) {
					return fx, fy, fz, true
				}
				break
			}
			above = here
		}
	}
	return 0, 0, 0, false
}

// The resin traversal's limits.
// vanilla: CreakingHeartBlockEntity.MAX_DEPTH, MAX_COUNT
const (
	creakingResinDepth = 2
	creakingResinCount = 64
)

// CreakingResinSpread grows one resin clump face from the heart at (x, y, z):
// a breadth-first walk over the pale oak logs within two steps of it, at most 64
// cells, each cell's six neighbours in a fresh shuffle. The first log with an
// air or source water neighbour, or a resin clump lacking the face toward the
// log, gets that face. It returns the cell and the state to write.
// vanilla: CreakingHeartBlockEntity.spreadResin, BlockPos.breadthFirstTraversal
func CreakingResinSpread(w *World, rng *mrand.Rand, x, y, z int32) (rx, ry, rz int32, state uint16, ok bool) {
	type node struct {
		x, y, z int32
		depth   int
	}
	// The order is Direction.values(), so index^1 is the opposite.
	dirs := [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	shuffled := func() [6]int {
		order := [6]int{0, 1, 2, 3, 4, 5}
		rng.Shuffle(6, func(i, j int) { order[i], order[j] = order[j], order[i] })
		return order
	}
	isLog := func(cx, cy, cz int32) bool {
		return registry.BlockOf(w.BlockAt(cx, cy, cz)).InTag(registry.TagIDPaleOakLogs)
	}
	queue := []node{{x, y, z, 0}}
	seen := map[[3]int32]bool{}
	count := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if seen[[3]int32{n.x, n.y, n.z}] {
			continue
		}
		seen[[3]int32{n.x, n.y, n.z}] = true
		if isLog(n.x, n.y, n.z) {
			for _, i := range shuffled() {
				d := dirs[i]
				nx, ny, nz := n.x+d[0], n.y+d[1], n.z+d[2]
				if next, found := resinFace(w.BlockAt(nx, ny, nz), facePropIDs[i^1]); found {
					return nx, ny, nz, next, true
				}
			}
		}
		count++
		if count >= creakingResinCount {
			break
		}
		if n.depth >= creakingResinDepth {
			continue
		}
		for _, i := range shuffled() {
			d := dirs[i]
			if nx, ny, nz := n.x+d[0], n.y+d[1], n.z+d[2]; isLog(nx, ny, nz) {
				queue = append(queue, node{nx, ny, nz, n.depth + 1})
			}
		}
	}
	return 0, 0, 0, 0, false
}

// resinFace is the resin clump a cell becomes with the face added: from air, a
// fresh clump; from source water, a waterlogged one; from a clump, itself when
// it lacks the face. false for any other cell.
// vanilla: CreakingHeartBlockEntity.spreadResin
func resinFace(cell uint16, face registry.Prop) (uint16, bool) {
	state := registry.BlockResinClump.DefaultState()
	switch {
	case IsKnownAirState(cell):
	case bubbleWaterSource(cell):
		state, _ = registry.WithBool(state, registry.PropWaterlogged, true)
	case registry.BlockOf(cell) == registry.BlockResinClump:
		state = cell
	default:
		return 0, false
	}
	if registry.StateBool(state, face) {
		return 0, false
	}
	return registry.WithBool(state, face, true)
}
