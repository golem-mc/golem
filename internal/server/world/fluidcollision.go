package world

import (
	"math"
	"strconv"
	"sync/atomic"

	"github.com/golem-mc/golem/registry"
)

// The collision shapes the fluid engine reads, and FlowingFluid's face test on
// them. A fluid spreads only across a face both shapes leave open, into a block
// the flow can hold against: what makes a slab's underside a ledge water sits
// on, a closed trapdoor a wall it climbs, and keeps a flooded chest's water
// inside its rim.
//
// Shapes come from the block classes (Block.box, Block.column and Block.boxZ in
// sixteenths, rotated by the block's direction property). Only blocks a flow
// can reach, the waterloggable containers and loose non-solid blocks, are
// modelled; other waterloggable blocks read the entity collision table, and
// everything else gets the registry's coarse open or full-cube answer.

// box is one axis-aligned piece of a collision shape, in block units (the cell
// spans 0 to 1).
type box struct{ x0, y0, z0, x1, y1, z1 float64 }

// unitBox is the full cube.
func unitBox() box { return box{0, 0, 0, 1, 1, 1} }

// boxF is a box given in sixteenths, as the block classes write them.
func boxF(minX, minY, minZ, maxX, maxY, maxZ float64) box {
	return box{minX / 16, minY / 16, minZ / 16, maxX / 16, maxY / 16, maxZ / 16}
}

// slabBox is a full-footprint slab between from and to (slab halves, snow
// layers, the blocky sensors).
func slabBox(from, to float64) box { return boxF(0, from, 0, 16, to, 16) }

// fluidCollision is a state's collision as the fluid engine reads it: the
// boxes, and whether they are the single full cube the face test short-circuits
// on.
type fluidCollision struct {
	boxes    []box
	fullCube bool
}

// fluidCollisions caches each state's collision by ID; entries fill lazily and
// lock-free because worlds tick on separate goroutines.
var fluidCollisions [registry.StateCount]atomic.Pointer[fluidCollision]

// fluidFaceOcclusions shares immutable state-pair results across worlds. A hash
// collision evicts only work; the key and result are published together.
var fluidFaceOcclusions [256]atomic.Uint64

const (
	fluidFaceResult = uint64(1) << 36
	fluidFaceValid  = uint64(1) << 63
)

var emptyFluidCollision fluidCollision

// collisionOf returns an immutable cached shape; states outside the registry
// (the air of the world's edges) have none.
func collisionOf(state uint16) *fluidCollision {
	if int(state) >= len(fluidCollisions) {
		return &emptyFluidCollision
	}
	if c := fluidCollisions[state].Load(); c != nil {
		return c
	}
	c := fluidCollision{}
	if _, props, ok := registry.BlockStateName(state); ok {
		c.boxes = collisionGeometry(state, registry.BlockOf(state), props)
		c.fullCube = len(c.boxes) == 1 && c.boxes[0] == unitBox()
	}
	fluidCollisions[state].Store(&c)
	return &c
}

// collisionSolid is BlockBehaviour.isSolid, read by the farm blocks' survival.
func collisionSolid(state uint16) bool {
	return registry.IsSolid(state)
}

// collisionGeometry is a block's collision boxes: a modelled family's shape,
// else a waterloggable's entity collision or the registry's coarse open or full cube.
func collisionGeometry(state uint16, block registry.Block, props string) []box {
	boxes, modelled := modelledGeometry(state, block, registry.ParseStateProps(props))
	if !modelled {
		if block.NoCollision() || thinPlate(state) {
			return nil
		}
		if waterloggable[block] || openBush[block] {
			// Leaves, grates and barriers are full cubes; conduits, decorated
			// pots and azaleas cover no whole side face and stay open to flow.
			// vanilla: FlowingFluid.canPassThroughWall
			collision := CollisionBoxes(state)
			boxes = make([]box, len(collision))
			for i, b := range collision {
				boxes[i] = box{b.MinX, b.MinY, b.MinZ, b.MaxX, b.MaxY, b.MaxZ}
			}
			return boxes
		}
		return []box{unitBox()}
	}
	return boxes
}

// thinPlate reports a flat collision no higher than a carpet, which covers
// no side face and so reads as open like a loose block.
func thinPlate(state uint16) bool {
	return registry.CollisionFlat(state) && registry.CollisionTop(state) <= 1.0/16
}

// modelledGeometry is collisionGeometry's modelled half; modelled says the block
// is answered here, nil boxes meaning an empty shape.
func modelledGeometry(state uint16, block registry.Block, p map[string]string) ([]box, bool) {
	if boxes, ok := attachedShape(state, block, p); ok {
		return boxes, true
	}
	switch {
	case block == registry.BlockWater || block == registry.BlockLava:
		// Pure fluid blocks have no collision; the face test reads them open.
		return nil, true
	case block == registry.BlockMangroveRoots:
		// Roots are a plain block in vanilla with no shape of their own, so a full
		// cube whatever the registry's sturdy-face set says.
		return []box{unitBox()}, true
	case block == registry.BlockFire || block == registry.BlockSoulFire || block == registry.BlockLight ||
		block == registry.BlockPowderSnow || block == registry.BlockRedstoneWire || block == registry.BlockTripwire ||
		block == registry.BlockChorusPlant ||
		block == registry.BlockPumpkinStem || block == registry.BlockAttachedPumpkinStem ||
		block == registry.BlockMelonStem || block == registry.BlockAttachedMelonStem ||
		block == registry.BlockCarrots || block == registry.BlockPotatoes || block == registry.BlockBeetroots ||
		block == registry.BlockTorchflowerCrop || block == registry.BlockPitcherCrop ||
		block == registry.BlockSweetBerryBush || block == registry.BlockCaveVines ||
		block == registry.BlockCaveVinesPlant || block == registry.BlockTwistingVinesPlant ||
		block == registry.BlockWeepingVinesPlant:
		// Blocks a flow washes away: no collision worth a face test.
		return nil, true
	case block.InGroup(registry.GroupWallTorch) || block.InGroup(registry.GroupWallBanner) ||
		block.InGroup(registry.GroupFlowerPot) || block == registry.BlockFlowerPot:
		// Wall-hung things and floor pots: no full face either.
		return nil, true
	case block.InGroup(registry.GroupSlab):
		switch p["type"] {
		case "top":
			return []box{slabBox(8, 16)}, true
		case "double":
			return []box{unitBox()}, true
		default:
			return []box{slabBox(0, 8)}, true
		}
	case block.InGroup(registry.GroupStairs):
		// The slab with the tall half at the back (where facing points),
		// corners cut or winged per the shape property; drawn for north and
		// rotated.
		// vanilla: StairBlock.getShape
		shapes, ok := stairBottomShapes[p["shape"]]
		if p["half"] == "top" {
			shapes, ok = stairTopShapes[p["shape"]]
		}
		if !ok {
			shapes = stairBottomShapes["straight"]
		}
		f := dirIndex(p["facing"])
		boxes := make([]box, len(shapes))
		for i, b := range shapes {
			boxes[i] = rotateNorth(b, f)
		}
		return boxes, true
	case block.InGroup(registry.GroupTrapdoor):
		// Closed, a 3/16 plate on the half's floor or ceiling; open, the plate
		// against the far edge (facing names the hinge).
		// vanilla: TrapDoorBlock
		if p["open"] == "true" {
			switch p["facing"] {
			case "south":
				return []box{boxF(0, 0, 0, 16, 16, 3)}, true
			case "west":
				return []box{boxF(13, 0, 0, 16, 16, 16)}, true
			case "east":
				return []box{boxF(0, 0, 0, 3, 16, 16)}, true
			default: // north
				return []box{boxF(0, 0, 13, 16, 16, 16)}, true
			}
		}
		if p["half"] == "top" {
			return []box{boxF(0, 13, 0, 16, 16, 16)}, true
		}
		return []box{boxF(0, 0, 0, 16, 3, 16)}, true
	case block == registry.BlockLadder:
		// A 3/16 plate against the wall opposite its facing.
		// vanilla: LadderBlock.SHAPES
		switch p["facing"] {
		case "south":
			return []box{boxF(0, 0, 0, 16, 16, 3)}, true
		case "west":
			return []box{boxF(13, 0, 0, 16, 16, 16)}, true
		case "east":
			return []box{boxF(0, 0, 0, 3, 16, 16)}, true
		default: // north
			return []box{boxF(0, 0, 13, 16, 16, 16)}, true
		}
	case block == registry.BlockSnow:
		// SnowLayerBlock's collision is a layer short of its look.
		layers := snowLayers(p["layers"])
		if layers == 1 {
			return nil, true
		}
		return []box{boxF(0, 0, 0, 16, float64(layers-1)*2, 16)}, true
	case block == registry.BlockChest || block.InGroup(registry.GroupChest):
		// 14/16 tall, inset 1/16 on every side but where a double's other half
		// joins.
		// vanilla: ChestBlock.AABB
		x0, z0, x1, z1 := 1.0, 1.0, 15.0, 15.0
		switch p["facing"] + " " + p["type"] {
		case "north left", "south right":
			x1 = 16
		case "north right", "south left":
			x0 = 0
		case "east left", "west right":
			z1 = 16
		case "east right", "west left":
			z0 = 0
		}
		return []box{boxF(x0, 0, z0, x1, 14, z1)}, true
	case block == registry.BlockCampfire || block == registry.BlockSoulCampfire:
		return []box{boxF(0, 0, 0, 16, 7, 16)}, true
	case block == registry.BlockEndPortalFrame:
		// A 13/16 base plate, plus the eye's 8x8 column to 16/16.
		// vanilla: EndPortalFrameBlock.getShape, SHAPE_EMPTY, SHAPE_FULL
		if p["eye"] == "true" {
			return []box{boxF(0, 0, 0, 16, 13, 16), boxF(4, 13, 4, 12, 16, 12)}, true
		}
		return []box{boxF(0, 0, 0, 16, 13, 16)}, true
	case block == registry.BlockSculkSensor || block == registry.BlockCalibratedSculkSensor ||
		block == registry.BlockSculkShrieker:
		return []box{boxF(0, 0, 0, 16, 8, 16)}, true
	case block == registry.BlockScaffolding:
		// The top plate, four corner posts, and a bottom plate for pieces at
		// the bottom of a column.
		// vanilla: ScaffoldingBlock
		boxes := []box{
			boxF(0, 14, 0, 16, 16, 16),
			boxF(0, 0, 0, 2, 16, 2),
			boxF(14, 0, 0, 16, 16, 2),
			boxF(0, 0, 14, 2, 16, 16),
			boxF(14, 0, 14, 16, 16, 16),
		}
		if p["bottom"] == "true" {
			boxes = append(boxes, boxF(0, 0, 0, 16, 2, 16))
		}
		return boxes, true
	case block.InGroup(registry.GroupShelf):
		// A back panel on the wall the facing turns away from, a plate above
		// and a lip below.
		// vanilla: ShelfBlock
		switch p["facing"] {
		case "south":
			return []box{boxF(0, 0, 0, 16, 16, 3), boxF(0, 12, 3, 16, 16, 5), boxF(0, 0, 3, 16, 4, 5)}, true
		case "west":
			return []box{boxF(13, 0, 0, 16, 16, 16), boxF(11, 12, 0, 13, 16, 16), boxF(11, 0, 0, 13, 4, 16)}, true
		case "east":
			return []box{boxF(0, 0, 0, 3, 16, 16), boxF(3, 12, 0, 5, 16, 16), boxF(3, 0, 0, 5, 4, 16)}, true
		default: // north
			return []box{boxF(0, 0, 13, 16, 16, 16), boxF(0, 12, 11, 16, 16, 13), boxF(0, 0, 11, 16, 4, 13)}, true
		}
	case block == registry.BlockFrogspawn:
		return []box{boxF(0, 0, 0, 16, 1.5, 16)}, true
	case block == registry.BlockLilyPad:
		// vanilla: LilyPadBlock.SHAPE, Block.column(14.0, 0.0, 1.5)
		return []box{boxF(1, 0, 1, 15, 1.5, 15)}, true
	case block == registry.BlockPiston || block == registry.BlockStickyPiston:
		// Retracted, a full cube; extended, minus the four-pixel strip at the
		// facing end (the notch the head's arm crosses, which the outline
		// raytrace reads too).
		// vanilla: PistonBaseBlock.getShape
		if p["extended"] != "true" {
			return []box{unitBox()}, true
		}
		return []box{pistonShaftBox(p["facing"])}, true
	case block == registry.BlockPistonHead:
		// The four-pixel plate across the facing face and the 4x4 arm behind
		// it.
		// vanilla: PistonHeadBlock.getShape
		return pistonHeadBoxes(p["facing"], p["short"] == "true"), true
	}
	return nil, false
}

// snowLayers reads the layer count a snow state carries.
func snowLayers(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 1
	}
	if n > 8 {
		return 8
	}
	return n
}

// pistonShaftBox is the extended base: the full cube minus the four-pixel strip
// at the facing end.
// vanilla: Shapes.rotateAll(Block.boxZ(16.0, 4.0, 16.0))
func pistonShaftBox(facing string) box {
	return facingBox(dirIndex(facing), 16, 0, 12)
}

// pistonHeadBoxes is the plate at the facing tip and the 4x4 arm behind it,
// which runs four pixels into the base's cell for a whole head and stops at the
// cell's edge for a short one.
// vanilla: PistonHeadBlock.getShape, SHAPE_PLATFORM, SHAPES, SHAPES_SHORT
func pistonHeadBoxes(facing string, short bool) []box {
	d := dirIndex(facing)
	from := -4.0
	if short {
		from = 0
	}
	return []box{facingBox(d, 16, 12, 16), facingBox(d, 4, from, 12)}
}

// openBush is the azaleas, whose top slab and stem cover no side face
// (AzaleaBlock.SHAPE) though they are not waterloggable.
var openBush = blockSet(registry.BlockAzalea, registry.BlockFloweringAzalea)

// waterloggable marks blocks with a waterlogged state (LiquidBlockContainers),
// which a flow may enter however solid.
var waterloggable = func() *[registry.BlockCount]bool {
	t := new([registry.BlockCount]bool)
	for b := range registry.Block(registry.BlockCount) {
		if _, ok := registry.StateProps(b.DefaultState())["waterlogged"]; ok {
			t[b] = true
		}
	}
	return t
}()

// templateTakesWater reports whether a template's placeLiquid waterlogs state:
// a waterloggable block, except a double slab, which refuses a liquid.
// vanilla: SlabBlock.placeLiquid
func templateTakesWater(state uint16) bool {
	b := registry.BlockOf(state)
	return waterloggable[b] && (!b.InGroup(registry.GroupSlab) || registry.StateValue(state, registry.PropType) != "double")
}

// faceRect is a rectangle on a face, the slices Shapes.mergedFaceOccludes
// unions.
type faceRect struct{ a0, b0, a1, b1 float64 }

// cachedFaceOccludes memoizes the nontrivial face test after the caller's full
// cube and both-empty shortcuts.
// vanilla: FlowingFluid.canPassThroughWall
func cachedFaceOccludes(srcState, dstState uint16, axis int, positive bool) bool {
	direction := uint64(axis * 2)
	if positive {
		direction++
	}
	key := uint64(srcState) | uint64(dstState)<<16 | direction<<32
	entry := &fluidFaceOcclusions[key*0x9e3779b97f4a7c15>>56]
	key |= fluidFaceValid
	if cached := entry.Load(); cached&^fluidFaceResult == key {
		return cached&fluidFaceResult != 0
	}
	occludes := mergedFaceOccludes(collisionOf(srcState).boxes, collisionOf(dstState).boxes, axis, positive)
	if occludes {
		key |= fluidFaceResult
	}
	entry.Store(key)
	return occludes
}

// mergedFaceOccludes reports whether the face between two shapes is blocked:
// either is the full cube, or the union of their edge slices (each reaching the
// face from its own side) covers it.
// vanilla: Shapes.mergedFaceOccludes
func mergedFaceOccludes(src, dst []box, axis int, positive bool) bool {
	first, second := src, dst
	if !positive {
		first, second = dst, src
	}
	eps := 1e-7
	var face []faceRect
	for _, b := range first {
		if boxMax(b, axis) >= 1-eps {
			face = append(face, projectFace(b, axis))
		}
	}
	for _, b := range second {
		if boxMin(b, axis) <= eps {
			face = append(face, projectFace(b, axis))
		}
	}
	return faceCoversUnit(face)
}

// faceCoversUnit reports whether the rectangles cover the unit square, by
// vanilla's coordinate compression: cut at every edge, each cell must lie under
// some rectangle.
func faceCoversUnit(rects []faceRect) bool {
	if len(rects) == 0 {
		return false
	}
	eps := 1e-7
	for range 2 {
		for _, r := range rects {
			if r.a0 < -eps || r.a1 > 1+eps || r.b0 < -eps || r.b1 > 1+eps {
				// Out of range: the vanilla shape never leaves the block.
				return false
			}
		}
	}
	us := faceEdges(rects, 0)
	vs := faceEdges(rects, 1)
	for i := 0; i+1 < len(us); i++ {
		for j := 0; j+1 < len(vs); j++ {
			loU, hiU := us[i], us[i+1]
			loV, hiV := vs[j], vs[j+1]
			covered := false
			for _, r := range rects {
				if r.a0 <= loU+eps && r.a1 >= hiU-eps && r.b0 <= loV+eps && r.b1 >= hiV-eps {
					covered = true
					break
				}
			}
			if !covered {
				return false
			}
		}
	}
	return true
}

// faceEdges collects the rectangle edges along one face axis, clamped, sorted,
// with 0 and 1 included.
func faceEdges(rects []faceRect, which int) []float64 {
	vals := []float64{0, 1}
	for _, r := range rects {
		lo, hi := r.a0, r.a1
		if which == 1 {
			lo, hi = r.b0, r.b1
		}
		for _, v := range []float64{lo, hi} {
			if v > 0 && v < 1 {
				vals = append(vals, v)
			}
		}
	}
	const eps = 1e-7
	for i := 0; i < len(vals); i++ {
		for j := i + 1; j < len(vals); j++ {
			if math.Abs(vals[j]-vals[i]) < eps {
				vals = append(vals[:j], vals[j+1:]...)
				j--
			}
		}
	}
	for i := 1; i < len(vals); i++ {
		for j := i; j > 0 && vals[j] < vals[j-1]; j-- {
			vals[j], vals[j-1] = vals[j-1], vals[j]
		}
	}
	return vals
}

// projectFace flattens a box onto the two axes perpendicular to axis.
func projectFace(b box, axis int) faceRect {
	switch axis {
	case 0:
		return faceRect{b.y0, b.z0, b.y1, b.z1}
	case 1:
		return faceRect{b.x0, b.z0, b.x1, b.z1}
	default:
		return faceRect{b.x0, b.y0, b.x1, b.y1}
	}
}

func boxMin(b box, axis int) float64 {
	switch axis {
	case 0:
		return b.x0
	case 1:
		return b.y0
	default:
		return b.z0
	}
}

func boxMax(b box, axis int) float64 {
	switch axis {
	case 0:
		return b.x1
	case 1:
		return b.y1
	default:
		return b.z1
	}
}
