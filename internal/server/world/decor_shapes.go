package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// shapeCheckBlocks are the blocks a structure marks for a shape update.
// vanilla: StructurePiece.SHAPE_CHECK_BLOCKS
var shapeCheckBlocks = blocksNamed(
	"minecraft:nether_brick_fence", "minecraft:oak_fence", "minecraft:spruce_fence", "minecraft:dark_oak_fence",
	"minecraft:pale_oak_fence", "minecraft:acacia_fence", "minecraft:birch_fence", "minecraft:jungle_fence",
	"minecraft:iron_bars", "minecraft:torch", "minecraft:wall_torch", "minecraft:ladder",
)

var (
	torchBlock     = shapeBlock("minecraft:torch")
	wallTorchBlock = shapeBlock("minecraft:wall_torch")
	ladderBlock    = shapeBlock("minecraft:ladder")
	ironBarsBlock  = shapeBlock("minecraft:iron_bars")
)

func shapeBlock(name string) registry.Block {
	b, ok := registry.BlockByName(name)
	if !ok {
		panic("world: unknown block " + name)
	}
	return b
}

// fenceSideProps are the connection properties in fenceDirNames order.
var fenceSideProps = [4]registry.Prop{registry.PropNorth, registry.PropEast, registry.PropSouth, registry.PropWest}

// Direction ordinals: down, up, north, south, west, east.
const shFaceUp = 1

// shFaceOrdinal is the ordinal of each horizontal direction, in fenceDirNames order.
var shFaceOrdinal = [4]int{2, 5, 3, 4}

// markShape records a placed block for reconnectShapes if it is one of
// shapeCheckBlocks. vanilla: StructurePiece.placeBlock, markPosForPostProcessing
func (r *decorRegion) markShape(x, y, z int32) {
	if shapeCheckBlocks[registry.BlockOf(r.get(x, y, z))] {
		ps := r.protoAt(x, z)
		ps.shapes = append(ps.shapes, posKey{x & 15, y - r.minY, z & 15})
	}
}

// markPost records a block placed by a feature for reconnectShapes whatever
// its kind. vanilla: ChunkAccess.markPosForPostProcessing
func (r *decorRegion) markPost(x, y, z int32) {
	ps := r.protoAt(x, z)
	ps.shapes = append(ps.shapes, posKey{x & 15, y - r.minY, z & 15})
}

// multifaceUpdateOrder is Direction.UPDATE_SHAPE_ORDER as direction indices
// (west, east, north, south, down, up).
var multifaceUpdateOrder = [dirCount]int{dirWest, dirEast, dirNorth, dirSouth, dirDown, dirUp}

// multifaceShape drops the faces of a multiface block whose host cannot hold
// them, and the block with its last face.
// vanilla: MultifaceBlock.updateShape
func (r *decorRegion) multifaceShape(state uint16, x, y, z int32) uint16 {
	for _, d := range multifaceUpdateOrder {
		if stateFaceMask(state) == 0 {
			return airState
		}
		o := dirDelta[d]
		if registry.StateBool(state, facePropIDs[d]) &&
			!registry.FaceAttachable(r.get(x+o[0], y+o[1], z+o[2]), blocklogic.Opposite(d)) {
			state, _ = registry.WithBool(state, facePropIDs[d], false)
		}
	}
	return state
}

// reconnectShapes is the shape update the chunk's marked blocks get once their
// neighbours exist: fences and bars connect, a torch or ladder without support
// breaks. vanilla: ProtoChunk.markPosForPostProcessing, Block.updateFromNeighbourShapes
func (r *decorRegion) reconnectShapes(own *decorProto) {
	if len(own.shapes) == 0 {
		return
	}
	c := r.center
	r.bindProtos(ChunkPos{X: c.x, Z: c.z})
	r.nx, r.nz = c.x, c.z
	for _, k := range own.shapes {
		x, y, z := c.x*sectionSize+k[0], k[1]+r.minY, c.z*sectionSize+k[2]
		state := r.get(x, y, z)
		b := registry.BlockOf(state)
		if b == registry.BlockWater && bubbleWaterSource(state) {
			// vanilla: LiquidBlock.tick, BubbleColumnBlock.updateColumn
			if col := bubbleColumnStateFor(r.get(x, y-1, z), state); col != state {
				r.set(x, y, z, col)
				r.breakSeagrassAround(x, y, z)
				for yy := y + 1; yy < r.minY+r.height && bubbleCanOccupy(r.get(x, yy, z)); yy++ {
					r.set(x, yy, z, col)
					r.breakSeagrassAround(x, yy, z)
				}
			}
			continue
		}
		if b.IsWall() {
			r.set(x, y, z, r.wallUpdatedFromNeighbours(state, x, y, z))
			continue
		}
		switch b {
		case torchBlock:
			if !registry.FaceSturdy(r.get(x, y-1, z), shFaceUp, registry.SupportCenter) {
				r.set(x, y, z, airState)
			}
		case ladderBlock, wallTorchBlock:
			// The support is behind the facing; its face toward the block must be full.
			facing := fenceDirIndex(registry.StateValue(state, registry.PropFacing))
			o := fenceOffsets[(facing+2)&3]
			if !registry.FaceSturdy(r.get(x+o[0], y, z+o[1]), shFaceOrdinal[facing], registry.SupportFull) {
				r.set(x, y, z, airState)
			}
		case registry.BlockBrownMushroom, registry.BlockRedMushroom:
			below := r.get(x, y-1, z)
			if !hasBit(below, tagMushroomLight) && (!registry.IsSolidRender(below) || r.finalLightAtLeast13(x, y, z)) {
				r.set(x, y, z, airState)
			}
		case registry.BlockGlowLichen, registry.BlockSculkVein, registry.BlockResinClump:
			if ns := r.multifaceShape(state, x, y, z); ns != state {
				r.set(x, y, z, ns)
			}
		default:
			if plantState[state] {
				r.plantShape(b, state, x, y, z)
				r.dropUnsupported(x, y, z)
				continue
			}
			if b.InTag(registry.TagIDSpeleothems) { // a lake's mark above its air or barrier
				r.templateShape(x, y, z)
				continue
			}
			if !shapeCheckBlocks[b] { // a disk's mark: a plant that lost its support
				r.dropUnsupported(x, y, z)
				continue
			}
			for d, o := range fenceOffsets {
				n := r.get(x+o[0], y, z+o[1])
				connects := registry.ConnectsToFence(b, n, fenceDirNames[d])
				if b == ironBarsBlock {
					connects = registry.ConnectsToBars(n, fenceDirNames[d])
				}
				state, _ = registry.WithBool(state, fenceSideProps[d], connects)
			}
			r.set(x, y, z, state)
		}
	}
}

// breakSeagrassAround is the shape update a write sends its neighbours, for
// the only blocks a bubble column's write can break: a seagrass that no longer
// survives (over magma, or a half without its partner) breaks into its water,
// whose own write updates on. The bubble column's write is flag 2, without
// UPDATE_KNOWN_SHAPE, unlike the 276 of the marked block's own update.
// vanilla: BubbleColumnBlock.updateColumn, Level.setBlock, DoublePlantBlock.updateShape, Block.updateOrDestroy, Level.destroyBlock
func (r *decorRegion) breakSeagrassAround(x, y, z int32) {
	for _, d := range multifaceUpdateOrder {
		o := dirDelta[d]
		nx, ny, nz := x+o[0], y+o[1], z+o[2]
		s := r.get(nx, ny, nz)
		b := registry.BlockOf(s)
		if b != registry.BlockSeagrass && b != registry.BlockTallSeagrass {
			continue
		}
		survives := stateSurvives(r, s, [3]int32{nx, ny, nz})
		if b == registry.BlockTallSeagrass && registry.StateValue(s, registry.PropHalf) == "lower" {
			survives = survives && registry.BlockOf(r.get(nx, ny+1, nz)) == b && registry.StateValue(r.get(nx, ny+1, nz), registry.PropHalf) == "upper"
		}
		if !survives {
			r.set(nx, ny, nz, RemovalLeaves(s))
			r.breakSeagrassAround(nx, ny, nz)
		}
	}
}

// plantShape turns a marked growing-plant body with no plant ahead into its
// head, and a head with plant ahead into its body. The head's age comes from a
// random seeded by the world seed and position rather than the level random,
// so generation stays independent of order.
// vanilla: GrowingPlantBodyBlock.updateShape, GrowingPlantHeadBlock.updateShape
func (r *decorRegion) plantShape(b registry.Block, state uint16, x, y, z int32) {
	g := growingPlantOf(b)
	n := registry.BlockOf(r.get(x+g.dir[0], y+g.dir[1], z+g.dir[2]))
	ahead := n == g.head || n == g.body
	var next uint16
	switch {
	case b == g.body && !ahead:
		pos := int64(x&0x3FFFFFF)<<38 | int64(z&0x3FFFFFF)<<12 | int64(y&0xFFF) // BlockPos.asLong
		age := levelgen.NewLegacyRandom(r.worldSeed ^ pos).NextIntN(plantHeadMaxAge)
		next, _ = registry.WithInt(g.head.DefaultState(), registry.PropAge, int(age))
	case b == g.head && ahead:
		next = g.body.DefaultState()
	default:
		return
	}
	if b == registry.BlockCaveVines || b == registry.BlockCaveVinesPlant {
		// vanilla: CaveVinesPlantBlock.updateHeadAfterConvertedFromBody, CaveVinesBlock.updateBodyAfterConvertedFromHead
		next, _ = registry.WithBool(next, registry.PropBerries, registry.StateBool(state, registry.PropBerries))
	}
	r.set(x, y, z, next)
}

// wallUpdatedFromNeighbours runs a wall's updateShape from every side in
// update order, so its arms and post follow the blocks around and above.
// vanilla: Block.updateFromNeighbourShapes, WallBlock.updateShape
func (r *decorRegion) wallUpdatedFromNeighbours(state uint16, x, y, z int32) uint16 {
	b := registry.BlockOf(state)
	for _, d := range multifaceUpdateOrder {
		if d != dirDown {
			state = registry.WallStateForNeighbor(r, b, state, x, y, z, [6]string{"", "", "north", "south", "west", "east"}[d])
		}
	}
	return state
}

var fenceOffsets = [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

var (
	potentSulfurState, _ = registry.PropNamed("potent_sulfur_state")
	continuousGeyser     = registry.BlockTagOf("minecraft:causes_continuous_geyser_eruptions")
	periodicGeyser       = registry.BlockTagOf("minecraft:causes_periodic_geyser_eruptions")
)

func fenceDirIndex(name string) int {
	for d, n := range fenceDirNames {
		if n == name {
			return d
		}
	}
	return 0
}

// templateShapes settles placed blocks and their neighbours.
func (r *decorRegion) templateShapes(placed []posKey) {
	if len(placed) == 0 {
		return
	}
	lo, hi := treePos(placed[0]), treePos(placed[0])
	for _, k := range placed {
		for d := range 3 {
			lo[d], hi[d] = min(lo[d], k[d]), max(hi[d], k[d])
		}
	}
	shape := newTreeShape(lo, hi)
	for _, k := range placed {
		shape.fill(treePos(k))
	}
	// Edge updates are directional and ordered; eagerly settling every
	// neighbour changes the sides of stacked walls that vanilla leaves low.
	// vanilla: StructureTemplate.placeInWorld, updateShapeAtEdge
	(&treeBuild{r: r}).updateShapeAtEdge(shape)
	settleTemplateShapes(r, placed)
}

type templateShapeRegion interface {
	BlockReader
	get(x, y, z int32) uint16
	set(x, y, z int32, state uint16)
}

// settleTemplateShapes updates the placed blocks and their horizontal neighbours.
func settleTemplateShapes(r templateShapeRegion, placed []posKey) {
	for _, k := range placed {
		templateShape(r, k[0], k[1], k[2])
		for _, o := range fenceOffsets {
			x, y, z := k[0]+o[0], k[1], k[2]+o[1]
			if !registry.BlockOf(r.get(x, y, z)).IsWall() {
				templateShape(r, x, y, z)
			}
		}
	}
}

func (r *decorRegion) templateShape(x, y, z int32) { templateShape(r, x, y, z) }

// templateShape updates the neighbour-dependent states after template placement.
// vanilla: StructureTemplate.placeInWorld, Block.updateFromNeighbourShapes
func templateShape(r templateShapeRegion, x, y, z int32) {
	state := r.get(x, y, z)
	b := registry.BlockOf(state)
	switch {
	case b.IsStairs():
		state = registry.StairsStateWithShape(r, b, state, x, y, z)
	case b.IsWall():
		state = registry.WallStateFromState(r, b, state, x, y, z)
	case b == registry.BlockPotentSulfur:
		state, _ = PotentSulfurState(r.get, state, x, y, z)
	case b.InTag(registry.TagIDSpeleothems):
		if stateSurvives(r, state, [3]int32{x, y, z}) {
			tip := dirIndex(registry.StateValue(state, registry.PropVerticalDirection))
			thickness := SpeleothemThickness(r, b, tip, registry.StateValue(state, registry.PropThickness) == "tip_merge", x, y, z)
			state, _ = registry.WithProp(state, registry.PropThickness, thickness)
		}
	case b.InTag(registry.TagIDDoors):
		// vanilla: DoorBlock.updateShape
		half := registry.StateValue(state, registry.PropHalf)
		dy, other := int32(1), "upper"
		if half == "upper" {
			dy, other = -1, "lower"
		}
		neighbour := r.get(x, y+dy, z)
		if registry.BlockOf(neighbour) == b && registry.StateValue(neighbour, registry.PropHalf) == other {
			state, _ = registry.WithProp(neighbour, registry.PropHalf, half)
		}
	case b.Type() == registry.BlockTypeFence:
		// vanilla: FenceBlock.updateShape
		for d, o := range fenceOffsets {
			state, _ = registry.WithBool(state, fenceSideProps[d], registry.ConnectsToFence(b, r.get(x+o[0], y, z+o[1]), fenceDirNames[d]))
		}
	case b == registry.BlockGlassPane || b == registry.BlockIronBars:
		for d, o := range fenceOffsets {
			state, _ = registry.WithBool(state, fenceSideProps[d], registry.ConnectsToBars(r.get(x+o[0], y, z+o[1]), fenceDirNames[d]))
		}
	case b == registry.BlockChest:
		for _, d := range []int{dirWest, dirEast, dirNorth, dirSouth} {
			o := dirDelta[d]
			n := r.get(x+o[0], y, z+o[2])
			if registry.BlockOf(n) == b {
				nt := registry.StateValue(n, registry.PropType)
				if registry.StateValue(state, registry.PropType) == "single" && nt != "single" && facingOfState(n) == facingOfState(state) && chestConnectedDir(n) == blocklogic.Opposite(d) {
					state, _ = registry.WithProp(state, registry.PropType, swapLeftRight(nt))
				}
			} else if chestConnectedDir(state) == d {
				state, _ = registry.WithProp(state, registry.PropType, "single")
			}
		}
	default:
		if survivalOf[state].kind != surviveSolid || canSurvive(r, x, y, z, state) {
			return
		}
		state = airState
	}
	r.set(x, y, z, state)
}

// finalLightAtLeast13 checks the only paths short enough to carry light
// of 13: emission is at most 15 and every edge attenuates by at least one.
func (r *decorRegion) finalLightAtLeast13(x, y, z int32) bool {
	return r.lightAtLeast13(x, y, z, nil)
}

func (r *decorRegion) lightAtLeast13(x, y, z int32, sourceEnabled func(x, z int32) bool) bool {
	if y < r.minY || y >= r.minY+r.height {
		return false
	}
	floors := map[[2]int32]int32{}
	sourceLight := func(x, y, z int32, state uint16) int {
		if sourceEnabled != nil && !sourceEnabled(x, z) {
			return 0
		}
		emission := registry.LightEmission(state)
		if r.g != nil {
			return emission
		}
		key := [2]int32{x, z}
		floor, ok := floors[key]
		if !ok {
			floor = r.src.decor().replayedSkyFloor(r.get, x, z, r.minY, r.height)
			floors[key] = floor
		}
		if y >= floor {
			return 15
		}
		return emission
	}
	target := r.get(x, y, z)
	if sourceLight(x, y, z, target) >= 13 {
		return true
	}
	for d, o := range dirDelta {
		nx, ny, nz := x-o[0], y-o[1], z-o[2]
		if ny < r.minY || r.g != nil && ny >= r.minY+r.height {
			continue
		}
		middle := r.get(nx, ny, nz)
		if emission := sourceLight(nx, ny, nz, middle); emission >= 14 && spread(uint8(emission), d, middle, target) >= 13 {
			return true
		}
		for sd, so := range dirDelta {
			sx, sy, sz := nx-so[0], ny-so[1], nz-so[2]
			if sy < r.minY || r.g != nil && sy >= r.minY+r.height {
				continue
			}
			source := r.get(sx, sy, sz)
			emission := sourceLight(sx, sy, sz, source)
			if emission < 15 {
				continue
			}
			v := spread(uint8(emission), sd, source, middle)
			if spread(v, d, middle, target) >= 13 {
				return true
			}
		}
	}
	return false
}

// replayedSkyFloor is the column's sky source floor as the replayed light
// engine holds it: captured by PrimeLightSources, else read from get.
func (g *decorState) replayedSkyFloor(get func(x, y, z int32) uint16, x, z, minY, height int32) int32 {
	if light := g.lightReplay[ChunkPos{X: x >> 4, Z: z >> 4}]; light.sources != nil {
		return light.sources[(z&15)*16+x&15]
	}
	return skySourceFloor(get, x, z, minY, height)
}

// skySourceFloor is the first source above an occluded downward edge.
// vanilla: ChunkSkyLightSources.findLowestSourceY; lightCtx.fillSkyColumns
func skySourceFloor(get func(x, y, z int32) uint16, x, z, minY, height int32) int32 {
	return skySourceBelow(get, x, z, minY+height, minY, airState)
}

func skySourceBelow(get func(x, y, z int32) uint16, x, z, topY, minY int32, top uint16) int32 {
	for y := topY - 1; y >= minY; y-- {
		lower := get(x, y, z)
		if registry.LightDampening(lower) != 0 || registry.LightFacesSeal(top, dirDown, lower) {
			return y + 1
		}
		top = lower
	}
	return minY
}

// updateSkySourceFloor checks only the two changed edges; an unrelated write
// must not repair columns missed while INITIALIZE_LIGHT was pending.
// vanilla: ChunkSkyLightSources.update, updateEdge
func updateSkySourceFloor(get func(x, y, z int32) uint16, x, y, z, minY, floor int32) int32 {
	if y+1 < floor {
		return floor
	}
	for upperY := y + 1; upperY >= y; upperY-- {
		upper, lower := get(x, upperY, z), get(x, upperY-1, z)
		if registry.LightDampening(lower) != 0 || registry.LightFacesSeal(upper, dirDown, lower) {
			if upperY > floor {
				return upperY
			}
		} else if upperY == floor {
			return skySourceBelow(get, x, z, upperY-1, minY, lower)
		}
	}
	return floor
}
