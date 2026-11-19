package rules

import (
	"math"
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// PlacementContext is everything about a Use Item On packet that decides which
// way a placed block faces, short of what the world around it looks like.
type PlacementContext struct {
	Face    int32   // the face the player clicked on
	Yaw     float32 // the player's horizontal facing
	Pitch   float32 // the player's vertical look, positive down the way the packets carry it
	CursorX float32 // where across the block the crosshair was, 0 to 1, west to east
	CursorY float32 // where up the block the crosshair was, 0 to 1
	CursorZ float32 // where along the block the crosshair was, 0 to 1, north to south

	// ReplacingClickedOn says the clicked block is replaceable and so is
	// the target itself; rules reading the cell behind the placement need
	// it, as that cell is then the clicked block's own wall.
	// vanilla: BlockPlaceContext.replaceClicked
	ReplacingClickedOn bool

	// Sneaking is vanilla's isSecondaryUseActive, which keeps a candle,
	// pickle, egg or segment from joining the one clicked.
	Sneaking bool

	// Inside is Use Item On's "inside block" flag (the eyes inside a
	// collision), which scaffolding's sneaking walk answers by placing on
	// the clicked face's far side.
	Inside bool
}

// sixDirFacing is the blocks whose facing has all six directions.
var sixDirFacing = registry.BlocksOf(func(b registry.Block) bool {
	_, ok := registry.WithProp(b.DefaultState(), registry.PropFacing, "up")
	return ok
})

// PlacementOverrides returns the property overrides that turn a block's
// default state into the placed one: a log's axis, a furnace's facing, a
// stair's half. Neighbour-derived properties (fence and wall connections,
// stair shape) are the server's.
//
// The rules fill in each recognised property a block has rather than being
// listed per block, which covers most oriented blocks.
func PlacementOverrides(block registry.Block, ctx PlacementContext) []registry.PropValue {
	deflt := block.DefaultState()
	var overrides []registry.PropValue
	trapdoorSide := block.InGroup(registry.GroupTrapdoor) && trapdoorSidePlacement(ctx)

	if registry.StateHas(deflt, registry.PropAxis) {
		overrides = append(overrides, registry.PropValue{P: registry.PropAxis, V: axisFromFace(ctx.Face)})
	}
	if registry.StateHas(deflt, registry.PropFacing) {
		switch {
		case nearestLookingBlocks[block]:
			// These face away from the look, pitch included: looking straight
			// down stands a barrel's opening up, and a piston placed looking at
			// the floor points up at the placer.
			overrides = append(overrides, registry.PropValue{P: registry.PropFacing, V: blocklogic.FaceName(blocklogic.Opposite(orderedByNearest(ctx.Yaw, ctx.Pitch)[0]))})
		case block == registry.BlockHopper:
			// The hopper's spout points out of the face it was placed
			// against, opposite the clicked face; a face whose opposite is
			// vertical stands the spout down, the hopper having no up.
			overrides = append(overrides, registry.PropValue{P: registry.PropFacing, V: hopperFacingFromFace(ctx.Face)})
		case block.InGroup(registry.GroupDoor), block.InGroup(registry.GroupBed):
			// Doors and beds face the look, not back: getPlacementState takes the
			// bare horizontal direction, so a bed placed looking south has its
			// head one block further south.
			overrides = append(overrides, registry.PropValue{P: registry.PropFacing, V: facingFromLook(ctx.Yaw)})
		case block == registry.BlockAnvil, block == registry.BlockChippedAnvil, block == registry.BlockDamagedAnvil:
			// AnvilBlock.getStateForPlacement: FACING is the look direction
			// turned a quarter clockwise.
			overrides = append(overrides, registry.PropValue{P: registry.PropFacing, V: blocklogic.FaceName(blocklogic.Clockwise(registry.FacingIndex(facingFromLook(ctx.Yaw))))})
		case block == registry.BlockObserver:
			// ObserverBlock.getStateForPlacement: FACING is the nearest
			// looking direction bare — the watched face points away from
			// the placer. Powered stays at its default false.
			overrides = append(overrides, registry.PropValue{P: registry.PropFacing, V: blocklogic.FaceName(orderedByNearest(ctx.Yaw, ctx.Pitch)[0])})
		case block == registry.BlockCampfire, block == registry.BlockSoulCampfire,
			block == registry.BlockDecoratedPot, block == registry.BlockCalibratedSculkSensor,
			block.InGroup(registry.GroupFenceGate):
			// vanilla: CampfireBlock, DecoratedPotBlock, FenceGateBlock and
			// CalibratedSculkSensorBlock getStateForPlacement take the
			// horizontal look direction bare.
			overrides = append(overrides, registry.PropValue{P: registry.PropFacing, V: facingFromLook(ctx.Yaw)})
		case block.InGroup(registry.GroupStairs):
			// StairBlock.getStateForPlacement: FACING is the look direction
			// bare, the tall half standing toward the look.
			overrides = append(overrides, registry.PropValue{P: registry.PropFacing, V: facingFromLook(ctx.Yaw)})
		case block.InGroup(registry.GroupTrapdoor):
			// TrapDoorBlock.getStateForPlacement: a click on another
			// block's side face stands the leaf on that face; only a top
			// or bottom click faces it back across the placer.
			if trapdoorSide {
				overrides = append(overrides, registry.PropValue{P: registry.PropFacing, V: facingFromFace(ctx.Face)})
			} else {
				overrides = append(overrides, registry.PropValue{P: registry.PropFacing, V: facingFromPlayer(ctx.Yaw)})
			}
		case sixDirFacing[block]:
			overrides = append(overrides, registry.PropValue{P: registry.PropFacing, V: facingFromFace(ctx.Face)})
		default:
			overrides = append(overrides, registry.PropValue{P: registry.PropFacing, V: facingFromPlayer(ctx.Yaw)})
		}
	}
	if registry.StateHas(deflt, registry.PropType) {
		// "type" is either a slab (top/bottom/double) or a chest
		// (single/left/right). Slabs are placed by cursor height; a
		// chest's pairing reads the world (ChestPlacementState).
		if isSlab(block) {
			overrides = append(overrides, registry.PropValue{P: registry.PropType, V: typeFromCursor(ctx.Face, ctx.CursorY)})
		}
	}
	// The stairs' and trapdoors' half is top or bottom; a door's or a
	// two-tall plant's lower half is its default, and a top or bottom
	// asked of it would find no state and lose the facing with it.
	if _, _, ok := registry.BlockStateName(deflt); ok {
		if half := registry.StateValue(deflt, registry.PropHalf); half == "top" || half == "bottom" {
			switch {
			case trapdoorSide:
				// vanilla: TrapDoorBlock.getStateForPlacement. The cursor
				// decides alone, exactly halfway staying the bottom half.
				if ctx.CursorY > 0.5 {
					overrides = append(overrides, registry.PropValue{P: registry.PropHalf, V: "top"})
				} else {
					overrides = append(overrides, registry.PropValue{P: registry.PropHalf, V: "bottom"})
				}
			case block.InGroup(registry.GroupTrapdoor):
				// A look-read trapdoor stands in the bottom half only
				// under a top click.
				if ctx.Face == protocol.FaceTop {
					overrides = append(overrides, registry.PropValue{P: registry.PropHalf, V: "bottom"})
				} else {
					overrides = append(overrides, registry.PropValue{P: registry.PropHalf, V: "top"})
				}
			default:
				overrides = append(overrides, registry.PropValue{P: registry.PropHalf, V: halfFromFace(ctx.Face, ctx.CursorY)})
			}
		}
	}
	if registry.StateHas(deflt, registry.PropRotation) {
		overrides = append(overrides, registry.PropValue{P: registry.PropRotation, V: rotationFromYaw(ctx.Yaw)})
	}
	// A rail's shape is the placement axis (east_west looking east or
	// west, else north_south); the on-place pass then re-derives it from
	// the rails around (internal/server/world/rail.go, RailState's twin).
	// vanilla: BaseRailBlock.getStateForPlacement
	if registry.StateHas(deflt, registry.PropShape) && block.InTag(registry.TagIDRails) {
		switch PlayerDirection(ctx.Yaw) {
		case "east", "west":
			overrides = append(overrides, registry.PropValue{P: registry.PropShape, V: "east_west"})
		default:
			overrides = append(overrides, registry.PropValue{P: registry.PropShape, V: "north_south"})
		}
	}
	if block == registry.BlockCrafter {
		overrides = append(overrides, registry.PropValue{P: registry.PropOrientation, V: crafterOrientation(ctx)})
	}
	if block == registry.BlockJigsaw {
		overrides = append(overrides, registry.PropValue{P: registry.PropOrientation, V: jigsawOrientation(ctx)})
	}

	return overrides
}

// BigDripleafPlacementState copies the facing of a big dripleaf or stem
// below; only with neither below does the leaf face the player, which the
// generic override gives. The cell read is the placement's own
// (getClickedPos), not the clicked block's.
// vanilla: BigDripleafBlock.getStateForPlacement
func BigDripleafPlacementState(w BlockReader, block registry.Block, placed uint16, x, y, z int32) (uint16, bool) {
	if block != registry.BlockBigDripleaf {
		return placed, false
	}
	below := w.BlockAt(x, y-1, z)
	belowBlock := registry.BlockOf(below)
	if belowBlock != registry.BlockBigDripleaf && belowBlock != registry.BlockBigDripleafStem {
		return placed, false
	}
	return registry.WithProp(placed, registry.PropFacing, registry.StateValue(below, registry.PropFacing))
}

// crafterOrientation is a placed crafter's ORIENTATION: front the nearest
// look reversed; top up for a horizontal front, the horizontal look for an
// up front and its opposite for a down front.
// vanilla: CrafterBlock.getStateForPlacement, FrontAndTop.fromFrontAndTop
func crafterOrientation(ctx PlacementContext) string {
	front := blocklogic.Opposite(orderedByNearest(ctx.Yaw, ctx.Pitch)[0])
	top := dirUp
	switch front {
	case dirUp:
		top = registry.FacingIndex(PlayerDirection(ctx.Yaw))
	case dirDown:
		top = blocklogic.Opposite(registry.FacingIndex(PlayerDirection(ctx.Yaw)))
	}
	return blocklogic.FaceName(front) + "_" + blocklogic.FaceName(top)
}

// jigsawOrientation is a placed jigsaw's ORIENTATION: front the clicked
// face; top up for a horizontal front, else the horizontal look reversed.
// vanilla: JigsawBlock.getStateForPlacement
func jigsawOrientation(ctx PlacementContext) string {
	front := facingFromFace(ctx.Face)
	top := "up"
	if front == "up" || front == "down" {
		top = blocklogic.FaceName(blocklogic.Opposite(registry.FacingIndex(PlayerDirection(ctx.Yaw))))
	}
	return front + "_" + top
}

// isSlab reports whether the block is a slab, whose cursor height picks top
// or bottom; other "type" blocks (a chest's is "single") keep their default.
func isSlab(b registry.Block) bool { return b.InGroup(registry.GroupSlab) }

// axisFromFace returns the axis a block with an "axis" property gets from the
// face the player clicked on: a log placed on the ground stands up (y), one
// placed against a wall lies flat (x or z).
func axisFromFace(face int32) string {
	switch face {
	case protocol.FaceTop, protocol.FaceBottom:
		return "y"
	case protocol.FaceNorth, protocol.FaceSouth:
		return "z"
	default:
		return "x"
	}
}

// facingFromFace returns the "facing" of a 6-directional block (piston, observer,
// etc.) from the face clicked: a piston placed against the north wall faces
// north, the way it would push.
func facingFromFace(face int32) string {
	switch face {
	case protocol.FaceBottom:
		return "down"
	case protocol.FaceTop:
		return "up"
	case protocol.FaceNorth:
		return "north"
	case protocol.FaceSouth:
		return "south"
	case protocol.FaceWest:
		return "west"
	default:
		return "east"
	}
}

// nearestLookingBlocks take getNearestLookingDirection().getOpposite():
// BarrelBlock, DispenserBlock (and the dropper), PistonBaseBlock (and the
// sticky piston) and CommandBlock (all three kinds).
var nearestLookingBlocks = registry.BlocksOf(func(b registry.Block) bool {
	switch b {
	case registry.BlockBarrel, registry.BlockChainCommandBlock, registry.BlockCommandBlock,
		registry.BlockDispenser, registry.BlockDropper, registry.BlockPiston,
		registry.BlockRepeatingCommandBlock, registry.BlockStickyPiston:
		return true
	}
	return false
})

// hopperFacingFromFace returns the hopper's "facing" from the face clicked:
// the spout points out of the face, opposite what was clicked, except that
// the two vertical faces both stand it down — the hopper has no up.
func hopperFacingFromFace(face int32) string {
	switch face {
	case protocol.FaceTop:
		return "down"
	case protocol.FaceBottom:
		return "down"
	case protocol.FaceNorth:
		return "south"
	case protocol.FaceSouth:
		return "north"
	case protocol.FaceWest:
		return "east"
	default:
		return "west"
	}
}

// PlayerDirection returns the horizontal direction the player is looking
// in, the vanilla Entity.getDirection: yaw 0 looks south — the opposite of
// what facingFromPlayer hands a block that faces its placer.
func PlayerDirection(yaw float32) string {
	deg := math.Mod(float64(yaw), 360)
	if deg < 0 {
		deg += 360
	}
	switch {
	case deg < 45 || deg >= 315:
		return "south"
	case deg < 135:
		return "west"
	case deg < 225:
		return "north"
	default:
		return "east"
	}
}

// facingFromPlayer is a 4-directional block's facing (furnace, chest):
// towards the player, the opposite of the look.
func facingFromPlayer(yaw float32) string {
	return blocklogic.FaceName(blocklogic.Opposite(registry.FacingIndex(PlayerDirection(yaw))))
}

// facingFromLook is the facing of blocks that face the look (doors, beds),
// Direction.fromYRot with quadrants breaking at 45 degrees.
func facingFromLook(yaw float32) string {
	return PlayerDirection(yaw)
}

// typeFromCursor returns the "type" of a slab from the cursor height and the
// face clicked: a slab placed on the ground is "bottom", one placed under a
// block is "top", and a slab placed against a wall follows the cursor.
func typeFromCursor(face int32, cursorY float32) string {
	if face == protocol.FaceBottom {
		return "top"
	}
	if face == protocol.FaceTop {
		return "bottom"
	}
	if cursorY > 0.5 {
		return "top"
	}
	return "bottom"
}

// halfFromFace returns the "half" of a stair (or trapdoor) from the face and
// cursor: a stair placed on the ground is "bottom", one placed under a block is
// "top", and a stair placed against a wall follows the cursor.
func halfFromFace(face int32, cursorY float32) string {
	if face == protocol.FaceBottom {
		return "top"
	}
	if face == protocol.FaceTop {
		return "bottom"
	}
	if cursorY > 0.5 {
		return "top"
	}
	return "bottom"
}

// trapdoorSidePlacement reports TrapDoorBlock.getStateForPlacement's side
// branch: a click on another block's horizontal face, not a replace. Only
// that branch takes its facing from the face.
func trapdoorSidePlacement(ctx PlacementContext) bool {
	if ctx.ReplacingClickedOn {
		return false
	}
	switch ctx.Face {
	case protocol.FaceBottom, protocol.FaceTop:
		return false
	}
	return true
}

// rotationFromYaw returns the "rotation" of a sign or banner (0-15) from the
// player's yaw. Each step is 22.5 degrees, starting from south at 0, going
// clockwise: 0=south, 4=west, 8=north, 12=east.
func rotationFromYaw(yaw float32) string {
	// Normalise to [0, 360).
	deg := math.Mod(float64(yaw)+360, 360)
	// Rotation increases clockwise from south, which is the same direction yaw
	// increases, offset by half a turn because the sign faces the player.
	r := int(math.Round(deg/22.5)) % 16
	if r < 0 {
		r += 16
	}
	return strconv.Itoa(r)
}

// DoorStateWithHinge returns the door state with the hinge a door placed at
// (x, y, z) gets. ok is false when the block has no hinge to set.
func DoorStateWithHinge(w BlockReader, placed uint16, ctx PlacementContext, x, y, z int32) (uint16, bool) {
	if !registry.StateHas(placed, registry.PropHinge) {
		return placed, false
	}
	// vanilla: DoorBlock.getHinge
	place := registry.FacingIndex(facingFromLook(ctx.Yaw))
	left, right := blocklogic.CounterClockwise(place), blocklogic.Clockwise(place)
	balance := 0
	for _, side := range [2]struct{ d, weight int }{
		{left, -1},
		{right, 1},
	} {
		for _, dy := range [2]int32{0, 1} {
			if world.CollisionFull(w.BlockAt(x+dirDeltas[side.d][0], y+dy, z+dirDeltas[side.d][2])) {
				balance += side.weight
			}
		}
	}
	doorLeft := lowerDoorAt(w, x, y, z, left)
	doorRight := lowerDoorAt(w, x, y, z, right)

	hinge := "left"
	if doorLeft && !doorRight || balance > 0 {
		hinge = "right"
	} else if (!doorRight || doorLeft) && balance >= 0 {
		// The cursor is relative to the clicked block; vanilla measures
		// from the placement's own cell.
		clickX, clickZ := ctx.CursorX, ctx.CursorZ
		if !ctx.ReplacingClickedOn {
			off := protocol.FaceOffset[ctx.Face]
			clickX -= float32(off[0])
			clickZ -= float32(off[2])
		}
		stepX, stepZ := dirDeltas[place][0], dirDeltas[place][2]
		if stepX < 0 && clickZ < 0.5 || stepX > 0 && clickZ > 0.5 ||
			stepZ < 0 && clickX > 0.5 || stepZ > 0 && clickX < 0.5 {
			hinge = "right"
		}
	}
	return registry.WithProp(placed, registry.PropHinge, hinge)
}

// DoorStateWithPower sets OPEN and POWERED from the neighbour signal, so a
// door, trapdoor or fence gate placed powered arrives open instead of the
// redstone pass swinging it with a second sound.
// vanilla: DoorBlock.getStateForPlacement
func DoorStateWithPower(placed uint16, powered bool) uint16 {
	if !registry.StateHas(placed, registry.PropOpen) {
		return placed
	}
	s, _ := registry.WithProps(placed,
		registry.PropValue{P: registry.PropOpen, V: strconv.FormatBool(powered)},
		registry.PropValue{P: registry.PropPowered, V: strconv.FormatBool(powered)},
	)
	return s
}

// lowerDoorAt reports whether the cell beside (x, y, z) along d holds the
// lower half of any door.
func lowerDoorAt(w BlockReader, x, y, z int32, d int) bool {
	state := w.BlockAt(x+dirDeltas[d][0], y, z+dirDeltas[d][2])
	return registry.BlockOf(state).InGroup(registry.GroupDoor) && registry.StateValue(state, registry.PropHalf) == "lower"
}

// WaterloggedForPlacement sets waterlogged from the cell's fluid, as
// getStateForPlacement reads it: a source waterlogs, a level 1-7 flow does
// not (FenceBlock, TrapDoorBlock, slab). Coral and the conduit test
// is(FluidTags.WATER) && isFull, which a falling column (level 8+) passes;
// big dripleaf and multiface blocks use isSourceOfType(WATER). ok is false
// without the property, for the light (no getStateForPlacement), and for a
// block placed onto its own kind, whose stacking already decided: a slab
// doubling into water stays dry.
// vanilla: TrapDoorBlock.getStateForPlacement, FenceBlock.getStateForPlacement
func WaterloggedForPlacement(w BlockReader, block registry.Block, placed uint16, x, y, z int32) (uint16, bool) {
	if !registry.StateHas(block.DefaultState(), registry.PropWaterlogged) {
		return placed, false
	}
	if block.Type() == registry.BlockTypeLight {
		return placed, false
	}
	if registry.BlockOf(w.BlockAt(x, y, z)) == block {
		return placed, false
	}
	water := waterAt(w, x, y, z)
	if fullWaterFluid(block) {
		water = waterFullAt(w, x, y, z)
	}
	if registry.BlockOf(placed).InTag(registry.TagIDCampfires) {
		// CampfireBlock.getStateForPlacement: a campfire the water waterlogs
		// arrives unlit; a hay bale below makes it a signal fire.
		s, _ := registry.WithProps(placed,
			registry.PropValue{P: registry.PropSignalFire, V: strconv.FormatBool(registry.BlockOf(w.BlockAt(x, y-1, z)) == registry.BlockHayBlock)},
			registry.PropValue{P: registry.PropWaterlogged, V: strconv.FormatBool(water)},
			registry.PropValue{P: registry.PropLit, V: strconv.FormatBool(!water)},
		)
		return s, true
	}
	return registry.WithBool(placed, registry.PropWaterlogged, water)
}

// ConcretePowderState returns the concrete a placed powder solidifies into;
// false for other blocks or a powder that stays. shouldSolidify reads the
// cell's own block as the replaced one: water in the cell solidifies, as
// does a neighbour's water behind a face that is not sturdy toward it.
// vanilla: ConcretePowderBlock.getStateForPlacement, shouldSolidify
func ConcretePowderState(w BlockReader, powder registry.Block, x, y, z int32) (uint16, bool) {
	concrete, isPowder := powder.Concrete()
	if !isPowder {
		return 0, false
	}
	if !world.HoldsWater(w.BlockAt(x, y, z)) && !world.ConcretePowderTouchesLiquid(w, x, y, z) {
		return 0, false
	}
	return concrete.DefaultState(), true
}

// BlockStateFromItemComponent applies the stack's minecraft:block_state
// properties over the placed state, skipping properties the block lacks and
// values it rejects.
// vanilla: BlockItem.updateBlockStateFromTag, BlockItemStateProperties.apply
func BlockStateFromItemComponent(placed uint16, props map[string]string) uint16 {
	if len(props) == 0 {
		return placed
	}
	state := placed
	for key, value := range props {
		if p, ok := registry.PropNamed(key); ok {
			state, _ = registry.WithProp(state, p, value)
		}
	}
	return state
}

// fullWaterFluid reports whether the block's getStateForPlacement tests the
// fluid with is(FluidTags.WATER) && isFull: the coral family and the conduit,
// whose waterlogged read a falling column too.
func fullWaterFluid(b registry.Block) bool {
	switch b.Type() {
	case registry.BlockTypeCoralPlant, registry.BlockTypeCoralFan, registry.BlockTypeCoralWallFan,
		registry.BlockTypeBaseCoralPlant, registry.BlockTypeBaseCoralFan, registry.BlockTypeBaseCoralWallFan,
		registry.BlockTypeConduit:
		return true
	}
	return false
}
