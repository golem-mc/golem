package rules

import (
	"math"
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// SurvivalReader is a BlockReader that also answers the world's canSurvive
// for a state at a cell, which the placement walks test each candidate with.
type SurvivalReader interface {
	BlockReader
	Survives(x, y, z int32, state uint16) bool
}

// standingAttach is the attachmentDirection of each StandingAndWallBlockItem
// family, by the type of the block the item places.
var standingAttach = map[registry.BlockType]int{
	registry.BlockTypeBaseCoralFan:       dirDown,
	registry.BlockTypeCoralFan:           dirDown,
	registry.BlockTypeTorch:              dirDown,
	registry.BlockTypeRedstoneTorch:      dirDown,
	registry.BlockTypeStandingSign:       dirDown,
	registry.BlockTypeBanner:             dirDown,
	registry.BlockTypeSkull:              dirDown,
	registry.BlockTypeWitherSkull:        dirDown,
	registry.BlockTypePlayerHead:         dirDown,
	registry.BlockTypeCeilingHangingSign: dirUp,
}

// AttachPlacementState is getStateForPlacement for the blocks that hang on
// something: its state (of the wall variant on a side), and whether a
// rule here applies. A zero state refuses the placement.
func AttachPlacementState(w SurvivalReader, b registry.Block, ctx PlacementContext, x, y, z int32) (uint16, bool) {
	typ := b.Type()
	looking := nearestLookingDirections(ctx)
	water := waterAt(w, x, y, z)
	if attach, ok := standingAttach[typ]; ok {
		return standingAndWallState(w, b, attach, looking, ctx, water, x, y, z)
	}
	clicked := int(ctx.Face)
	switch typ {
	case registry.BlockTypeLantern, registry.BlockTypeWeatheringLantern:
		for _, d := range looking {
			if d == dirUp || d == dirDown {
				s := placeState(b, water, registry.PropValue{P: registry.PropHanging, V: strconv.FormatBool(d == dirUp)})
				if w.Survives(x, y, z, s) {
					return s, true
				}
			}
		}
		return 0, true
	case registry.BlockTypeButton, registry.BlockTypeLever, registry.BlockTypeGrindstone:
		// FaceAttachedHorizontalDirectionalBlock: on a floor or ceiling
		// the player's heading, on a wall away from the wall.
		for _, d := range looking {
			face, facing := "wall", blocklogic.FaceName(blocklogic.Opposite(d))
			switch d {
			case dirDown:
				face, facing = "floor", facingFromLook(ctx.Yaw)
			case dirUp:
				face, facing = "ceiling", facingFromLook(ctx.Yaw)
			}
			s := placeState(b, water, registry.PropValue{P: registry.PropFace, V: face}, registry.PropValue{P: registry.PropFacing, V: facing})
			if w.Survives(x, y, z, s) {
				return s, true
			}
		}
		return 0, true
	case registry.BlockTypeAmethystCluster, registry.BlockTypeLightningRod, registry.BlockTypeWeatheringLightningRod:
		return placeState(b, water, facingValue(clicked)), true
	case registry.BlockTypeEndRod:
		// EndRodBlock: clicking the tip of a rod facing the same way
		// turns the new one around.
		facing := blocklogic.FaceName(clicked)
		back := dirDeltas[blocklogic.Opposite(clicked)]
		backState := w.BlockAt(x+back[0], y+back[1], z+back[2])
		if registry.BlockOf(backState) == b && registry.StateValue(backState, registry.PropFacing) == facing {
			facing = blocklogic.FaceName(blocklogic.Opposite(clicked))
		}
		return placeState(b, water, registry.PropValue{P: registry.PropFacing, V: facing}), true
	case registry.BlockTypeTripWireHook, registry.BlockTypeLadder, registry.BlockTypeCocoa, registry.BlockTypeShelfMushroom:
		if typ == registry.BlockTypeLadder && ladderFrontClicked(w, ctx, x, y, z) {
			return 0, true
		}
		for _, d := range looking {
			if d == dirUp || d == dirDown {
				continue
			}
			// The cocoa pod faces its log; the others face away from
			// their wall.
			facing := blocklogic.FaceName(blocklogic.Opposite(d))
			if typ == registry.BlockTypeCocoa {
				facing = blocklogic.FaceName(d)
			}
			s := placeState(b, water, registry.PropValue{P: registry.PropFacing, V: facing}, registry.PropValue{P: registry.PropAttached, V: "false"}, registry.PropValue{P: registry.PropPowered, V: "false"})
			if w.Survives(x, y, z, s) {
				return s, true
			}
		}
		return 0, true
	case registry.BlockTypePointedDripstone, registry.BlockTypeSulfurSpike:
		return speleothemState(w, b, ctx, water, x, y, z), true
	case registry.BlockTypeGlowLichen, registry.BlockTypeSculkVein, registry.BlockTypeMultiface:
		return multifaceState(w, b, looking, water, x, y, z), true
	case registry.BlockTypeHugeMushroom:
		return hugeMushroomState(w, b, x, y, z), true
	case registry.BlockTypeBambooStalk:
		return bambooState(w, b, water, x, y, z), true
	case registry.BlockTypeBell:
		return bellPlacementState(w, ctx, x, y, z), true
	}
	return 0, false
}

// bellPlacementState: a top or bottom click stands the bell on the floor or
// hangs it from the ceiling facing the look; a side click hangs it on that
// wall (double when both sides are sturdy), falling back to floor or ceiling
// facing the face's opposite. An unsurvivable state is refused.
// vanilla: BellBlock.getStateForPlacement
func bellPlacementState(w SurvivalReader, ctx PlacementContext, x, y, z int32) uint16 {
	facing := facingFromLook(ctx.Yaw)
	if axisOf(int(ctx.Face)) == 1 {
		attachment := "floor"
		if ctx.Face == protocol.FaceBottom {
			attachment = "ceiling"
		}
		if s := placeState(registry.BlockBell, false, registry.PropValue{P: registry.PropAttachment, V: attachment}, registry.PropValue{P: registry.PropFacing, V: facing}); w.Survives(x, y, z, s) {
			return s
		}
		return 0
	}
	facing = blocklogic.FaceName(blocklogic.Opposite(int(ctx.Face)))
	attachment := "single_wall"
	if bellDoubleWallNeighbours(w, ctx.Face, x, y, z) {
		attachment = "double_wall"
	}
	if s := placeState(registry.BlockBell, false, registry.PropValue{P: registry.PropAttachment, V: attachment}, registry.PropValue{P: registry.PropFacing, V: facing}); w.Survives(x, y, z, s) {
		return s
	}
	// The wall held nothing; the face's opposite stays the facing, and the
	// bell settles on the floor when the block below is sturdy, else the
	// ceiling.
	attachment = "ceiling"
	if registry.FaceSturdy(w.BlockAt(x, y-1, z), dirUp, registry.SupportFull) {
		attachment = "floor"
	}
	if s := placeState(registry.BlockBell, false, registry.PropValue{P: registry.PropAttachment, V: attachment}, registry.PropValue{P: registry.PropFacing, V: facing}); w.Survives(x, y, z, s) {
		return s
	}
	return 0
}

// bellDoubleWallNeighbours is the getStateForPlacement doubleAttached test:
// both blocks on the clicked face's horizontal axis are sturdy toward the
// cell.
func bellDoubleWallNeighbours(w BlockReader, face int32, x, y, z int32) bool {
	switch axisOf(int(face)) {
	case 0: // east or west
		return registry.FaceSturdy(w.BlockAt(x-1, y, z), dirEast, registry.SupportFull) &&
			registry.FaceSturdy(w.BlockAt(x+1, y, z), dirWest, registry.SupportFull)
	case 2: // north or south
		return registry.FaceSturdy(w.BlockAt(x, y, z-1), dirSouth, registry.SupportFull) &&
			registry.FaceSturdy(w.BlockAt(x, y, z+1), dirNorth, registry.SupportFull)
	}
	return false
}

// bambooState is BambooStalkBlock.getStateForPlacement: a sapling on soil, a
// stalk over a stalk or a sapling, or under a stalk, with the age it takes
// from there; zero in a fluid or off the bamboo soil.
func bambooState(w BlockReader, b registry.Block, water bool, x, y, z int32) uint16 {
	if cur := registry.BlockOf(w.BlockAt(x, y, z)); water || cur == registry.BlockWater || cur == registry.BlockLava {
		return 0
	}
	belowState := w.BlockAt(x, y-1, z)
	below := registry.BlockOf(belowState)
	if !below.InTag(registry.TagIDSupportsBamboo) {
		return 0
	}
	age := "0"
	switch below {
	case registry.BlockBambooSapling:
	case registry.BlockBamboo:
		if registry.StateValue(belowState, registry.PropAge) != "0" {
			age = "1"
		}
	default:
		aboveState := w.BlockAt(x, y+1, z)
		if registry.BlockOf(aboveState) != registry.BlockBamboo {
			return registry.BlockBambooSapling.DefaultState()
		}
		age = registry.StateValue(aboveState, registry.PropAge)
	}
	s, _ := registry.WithProp(b.DefaultState(), registry.PropAge, age)
	return s
}

// isMultiface reports whether the block is a MultifaceBlock: glow lichen,
// sculk vein, resin clump.
func isMultiface(b registry.Block) bool {
	switch b.Type() {
	case registry.BlockTypeGlowLichen, registry.BlockTypeSculkVein, registry.BlockTypeMultiface:
		return true
	}
	return false
}

// multifaceState is MultifaceBlock.getStateForPlacement: the first looking
// direction whose face is free and can hold the block is added to the one
// already in the cell, or to a fresh one; zero when none is.
func multifaceState(w BlockReader, b registry.Block, looking [6]int, water bool, x, y, z int32) uint16 {
	base := w.BlockAt(x, y, z)
	if registry.BlockOf(base) != b {
		base = b.DefaultState()
		base, _ = registry.WithBool(base, registry.PropWaterlogged, water)
	}
	for _, d := range looking {
		if registry.StateBool(base, blocklogic.FaceProp(d)) ||
			!registry.FaceAttachable(w.BlockAt(x+dirDeltas[d][0], y+dirDeltas[d][1], z+dirDeltas[d][2]), blocklogic.Opposite(d)) {
			continue
		}
		s, _ := registry.WithBool(base, blocklogic.FaceProp(d), true)
		return s
	}
	return 0
}

// hugeMushroomState is HugeMushroomBlock.getStateForPlacement: a face is set
// unless the block beside it is the same block.
func hugeMushroomState(w BlockReader, b registry.Block, x, y, z int32) uint16 {
	s := b.DefaultState()
	for d, delta := range dirDeltas {
		n := w.BlockAt(x+delta[0], y+delta[1], z+delta[2])
		s, _ = registry.WithBool(s, blocklogic.FaceProp(d), registry.BlockOf(n) != b)
	}
	return s
}

// standingAndWallState is StandingAndWallBlockItem.getPlacementState: along
// the looking directions, the block itself toward attach and the wall
// variant's own placement toward a side, the first that survives winning.
func standingAndWallState(w SurvivalReader, b registry.Block, attach int, looking [6]int, ctx PlacementContext, water bool, x, y, z int32) (uint16, bool) {
	wallBlock, hasWall := b.WallVariant()
	var wall uint16
	if hasWall {
		wall = wallPlacementState(w, wallBlock, looking, ctx, water, x, y, z)
	}
	var standing uint16
	switch b.Type() {
	case registry.BlockTypeStandingSign, registry.BlockTypeBanner:
		standing = placeState(b, water, rotationValue(rotationSegment(ctx.Yaw+180)))
	case registry.BlockTypeSkull, registry.BlockTypeWitherSkull, registry.BlockTypePlayerHead:
		standing = placeState(b, water, rotationValue(rotationSegment(ctx.Yaw)))
	case registry.BlockTypeCeilingHangingSign:
		standing = ceilingHangingSignState(w, b, ctx, water, x, y, z)
	default:
		standing = placeState(b, water)
	}
	for _, d := range looking {
		if d == blocklogic.Opposite(attach) {
			continue
		}
		s := standing
		if d != attach {
			s = wall
		}
		if s != 0 && w.Survives(x, y, z, s) {
			return s, true
		}
	}
	return 0, true
}

// wallPlacementState is the wall variant's getStateForPlacement: the first
// horizontal looking direction whose wall takes it, facing away from it.
func wallPlacementState(w SurvivalReader, wall registry.Block, looking [6]int, ctx PlacementContext, water bool, x, y, z int32) uint16 {
	typ := wall.Type()
	for _, d := range looking {
		if d == dirUp || d == dirDown {
			continue
		}
		off := dirDeltas[d]
		switch typ {
		case registry.BlockTypeWallSkull, registry.BlockTypeWitherWallSkull, registry.BlockTypePiglinwallskull, registry.BlockTypePlayerWallHead:
			// WallSkullBlock has no canSurvive; it wants a block that is
			// not replaceable behind it.
			if IsReplaceableState(w.BlockAt(x+off[0], y+off[1], z+off[2])) {
				continue
			}
		case registry.BlockTypeWallHangingSign:
			// It hangs across the clicked face's axis, from a block or
			// a sign beside it.
			if axisOf(d) == axisOf(int(ctx.Face)) || !wallHangingSignCanPlace(w, x, y, z, blocklogic.Opposite(d)) {
				continue
			}
		}
		s := placeState(wall, water, facingValue(blocklogic.Opposite(d)))
		if w.Survives(x, y, z, s) {
			return s
		}
	}
	return 0
}

// ceilingHangingSignState places a ceiling sign: on its middle chain, unless
// the block above has a full bottom or is a hanging sign along the player's
// axis, when it turns to the player's heading; sneaking always takes the chain.
// vanilla: CeilingHangingSignBlock.getStateForPlacement
func ceilingHangingSignState(w SurvivalReader, b registry.Block, ctx PlacementContext, water bool, x, y, z int32) uint16 {
	above := w.BlockAt(x, y+1, z)
	heading := registry.FacingIndex(facingFromLook(ctx.Yaw))
	middle := !registry.CollisionBottomFull(above) || ctx.Sneaking
	if !ctx.Sneaking && registry.BlockOf(above).InTag(registry.TagIDAllHangingSigns) {
		if registry.StateHasFacing(above) {
			if axisOf(registry.StateFacing(above)) == axisOf(heading) {
				middle = false
			}
		} else if r, ok := registry.StateInt(above, registry.PropRotation); ok && r%4 == 0 && (r%8 == 0) == (axisOf(heading) == axisOf(dirNorth)) {
			// RotationSegment.convertToDirection: 0 and 8 run north and
			// south, 4 and 12 east and west.
			middle = false
		}
	}
	rotation := rotationSegment(ctx.Yaw + 180)
	if !middle {
		rotation = directionSegment(blocklogic.Opposite(heading))
	}
	return placeState(b, water, registry.PropValue{P: registry.PropAttached, V: strconv.FormatBool(middle)}, rotationValue(rotation))
}

// wallHangingSignCanPlace is WallHangingSignBlock.canPlace: one end of the
// bar holds on a sturdy face or on a wall hanging sign along the same axis.
func wallHangingSignCanPlace(w BlockReader, x, y, z int32, facing int) bool {
	cw := blocklogic.Clockwise(facing)
	return wallHangingSignAttaches(w, x, y, z, cw, facing) || wallHangingSignAttaches(w, x, y, z, blocklogic.Opposite(cw), facing)
}

func wallHangingSignAttaches(w BlockReader, x, y, z int32, d, facing int) bool {
	off := dirDeltas[d]
	state := w.BlockAt(x+off[0], y+off[1], z+off[2])
	if registry.BlockOf(state).InTag(registry.TagIDWallHangingSigns) {
		return axisOf(registry.StateFacing(state)) == axisOf(facing)
	}
	return registry.FaceSturdy(state, blocklogic.Opposite(d), registry.SupportFull)
}

// speleothemState is SpeleothemBlock.getStateForPlacement for a player who
// is not sneaking: the tip points away from the vertical look, or the other
// way where only that holds, and merges with an opposing tip.
func speleothemState(w SurvivalReader, b registry.Block, ctx PlacementContext, water bool, x, y, z int32) uint16 {
	tip := dirUp
	if ctx.Pitch < 0 {
		tip = dirDown
	}
	if !w.Survives(x, y, z, placeState(b, water, registry.PropValue{P: registry.PropVerticalDirection, V: blocklogic.FaceName(tip)})) {
		tip = blocklogic.Opposite(tip)
		if !w.Survives(x, y, z, placeState(b, water, registry.PropValue{P: registry.PropVerticalDirection, V: blocklogic.FaceName(tip)})) {
			return 0
		}
	}
	thickness := world.SpeleothemThickness(w, b, tip, !ctx.Sneaking, x, y, z)
	return placeState(b, water, registry.PropValue{P: registry.PropVerticalDirection, V: blocklogic.FaceName(tip)}, registry.PropValue{P: registry.PropThickness, V: thickness})
}

// placeState is the block's default state with the given values it has,
// waterlogged when the block has the property and the cell held water; zero
// when a value does not fit.
func placeState(b registry.Block, water bool, values ...registry.PropValue) uint16 {
	def := b.DefaultState()
	kept := make([]registry.PropValue, 0, len(values)+1)
	for _, pv := range values {
		if registry.StateHas(def, pv.P) {
			kept = append(kept, pv)
		}
	}
	if registry.StateHas(def, registry.PropWaterlogged) {
		kept = append(kept, registry.PropValue{P: registry.PropWaterlogged, V: strconv.FormatBool(water)})
	}
	s, _ := registry.WithProps(def, kept...)
	return s
}

func facingValue(d int) registry.PropValue {
	return registry.PropValue{P: registry.PropFacing, V: blocklogic.FaceName(d)}
}

func rotationValue(r int) registry.PropValue {
	return registry.PropValue{P: registry.PropRotation, V: strconv.Itoa(r)}
}

// rotationSegment is RotationSegment.convertToSegment of an angle.
func rotationSegment(deg float32) int {
	return int(math.Floor(float64(deg*(16.0/360.0))+0.5)) & 15
}

// directionSegment is RotationSegment.convertToSegment of a horizontal
// direction: south 0, west 4, north 8, east 12.
func directionSegment(d int) int {
	switch d {
	case dirWest:
		return 4
	case dirNorth:
		return 8
	case dirEast:
		return 12
	}
	return 0
}

func axisOf(d int) int {
	switch d {
	case dirDown, dirUp:
		return 1
	case dirNorth, dirSouth:
		return 2
	}
	return 0
}
