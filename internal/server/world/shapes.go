package world

import (
	"strconv"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// Collision shapes of the families modelled from the block classes;
// collisionGeometry asks these first.

// candleBoxes is CandleBlock's shape by candle count.
var candleBoxes = [5][]box{
	1: {boxF(7, 0, 7, 9, 6, 9)},
	2: {boxF(5, 0, 6, 11, 6, 9)},
	3: {boxF(5, 0, 6, 10, 6, 11)},
	4: {boxF(5, 0, 5, 11, 6, 10)},
}

// amethystSizes is each AmethystClusterBlock's height and width.
var amethystSizes = map[registry.Block][2]float64{
	registry.BlockAmethystCluster:   {7, 10},
	registry.BlockLargeAmethystBud:  {5, 10},
	registry.BlockMediumAmethystBud: {4, 10},
	registry.BlockSmallAmethystBud:  {3, 8},
}

// dripstoneWidths is SpeleothemBlock's column width by thickness.
var dripstoneWidths = map[string]float64{
	"tip_merge": 6, "tip": 6, "frustum": 8, "middle": 10, "base": 12,
}

// grindstoneNorth is GrindstoneBlock's wall shape facing north.
var grindstoneNorth = []box{
	boxF(4, 2, 0, 12, 14, 12),
	boxF(2, 6, 7, 4, 10, 16), boxF(2, 5, 3, 4, 11, 9),
	boxF(12, 6, 7, 14, 10, 16), boxF(12, 5, 3, 14, 11, 9),
}

// stairBottomShapes and stairTopShapes are each half's shapes drawn for north:
// the slab, the tall back strip on the facing side, outer corners' half strip,
// inner corners' side wing, as the class's merge decomposes them.
// vanilla: StairBlock.getShape
var stairBottomShapes = map[string][]box{
	"straight":    {boxF(0, 0, 0, 16, 8, 16), boxF(0, 8, 0, 16, 16, 8)},
	"outer_left":  {boxF(0, 0, 0, 16, 8, 16), boxF(0, 8, 0, 8, 16, 8)},
	"outer_right": {boxF(0, 0, 0, 16, 8, 16), boxF(8, 8, 0, 16, 16, 8)},
	"inner_left":  {boxF(0, 0, 0, 16, 8, 16), boxF(0, 8, 0, 8, 16, 16), boxF(8, 8, 0, 16, 16, 8)},
	"inner_right": {boxF(0, 0, 0, 16, 8, 16), boxF(0, 8, 0, 16, 16, 8), boxF(8, 8, 8, 16, 16, 16)},
}

var stairTopShapes = map[string][]box{
	"straight":    {boxF(0, 0, 0, 16, 16, 8), boxF(0, 8, 8, 16, 16, 16)},
	"outer_left":  {boxF(0, 0, 0, 8, 16, 8), boxF(0, 8, 8, 16, 16, 16), boxF(8, 8, 0, 16, 16, 8)},
	"outer_right": {boxF(8, 0, 0, 16, 16, 8), boxF(0, 8, 0, 8, 16, 16), boxF(8, 8, 8, 16, 16, 16)},
	"inner_left":  {boxF(0, 0, 0, 8, 16, 16), boxF(8, 0, 0, 16, 16, 8), boxF(8, 8, 8, 16, 16, 16)},
	"inner_right": {boxF(0, 0, 0, 16, 16, 8), boxF(8, 0, 8, 16, 16, 16), boxF(0, 8, 8, 8, 16, 16)},
}

// attachedShape is a modelled family's collision; ok is false for other blocks.
func attachedShape(state uint16, block registry.Block, p map[string]string) ([]box, bool) {
	facing := dirIndex(p["facing"])
	switch block.Type() {
	case registry.BlockTypeBed:
		// Bed boxes come from the generated table: the fluid engine reads them
		// here, every other reader through CollisionBoxes.
		if raw, ok := registry.BlockShapeBoxes(state); ok {
			boxes := make([]box, len(raw))
			for i, b := range raw {
				boxes[i] = box{
					float64(b[0]) / 32, float64(b[1]) / 32, float64(b[2]) / 32,
					float64(b[3]) / 32, float64(b[4]) / 32, float64(b[5]) / 32,
				}
			}
			return boxes, true
		}
		return []box{boxF(0, 0, 0, 16, 9, 16)}, true
	case registry.BlockTypeCandle:
		n, _ := strconv.Atoi(p["candles"])
		if n < 1 || n > 4 {
			n = 1
		}
		return candleBoxes[n], true
	case registry.BlockTypeShelfMushroom:
		// The cap and stalk drawn for north, against the south wall.
		// vanilla: ShelfMushroomBlock.SHAPES
		boxes := []box{boxF(3, 9, 9, 13, 11, 16), boxF(5, 8, 12, 11, 9, 16)}
		if p["age"] == "1" {
			boxes = []box{boxF(1, 8, 6, 15, 11, 16), boxF(4, 6, 10, 12, 8, 16)}
		}
		for i, b := range boxes {
			boxes[i] = rotateNorth(b, facing)
		}
		return boxes, true
	case registry.BlockTypeLantern, registry.BlockTypeWeatheringLantern:
		y := 0.0
		if p["hanging"] == "true" {
			y = 1
		}
		return []box{boxF(5, y, 5, 11, y+7, 11), boxF(6, y+7, 6, 10, y+9, 10)}, true
	case registry.BlockTypeEndRod, registry.BlockTypeLightningRod, registry.BlockTypeWeatheringLightningRod:
		return []box{facingBox(facing, 4, 0, 16)}, true
	case registry.BlockTypeAmethystCluster:
		s := amethystSizes[block]
		return []box{facingBox(facing, s[1], 0, s[0])}, true
	case registry.BlockTypeSkull, registry.BlockTypeWitherSkull, registry.BlockTypePlayerHead:
		if block == registry.BlockPiglinHead {
			return []box{boxF(3, 0, 3, 13, 8, 13)}, true
		}
		return []box{boxF(4, 0, 4, 12, 8, 12)}, true
	case registry.BlockTypeWallSkull, registry.BlockTypeWitherWallSkull, registry.BlockTypePlayerWallHead:
		return []box{rotateNorth(boxF(4, 4, 8, 12, 12, 16), facing)}, true
	case registry.BlockTypePiglinwallskull:
		return []box{rotateNorth(boxF(3, 4, 8, 13, 12, 16), facing)}, true
	case registry.BlockTypeGrindstone:
		boxes := make([]box, len(grindstoneNorth))
		for i, b := range grindstoneNorth {
			switch p["face"] {
			case "floor":
				b = box{b.x0, 1 - b.z1, b.y0, b.x1, 1 - b.z0, b.y1}
			case "ceiling":
				b = box{b.x0, b.z0, 1 - b.y1, b.x1, b.z1, 1 - b.y0}
			}
			boxes[i] = rotateNorth(b, facing)
		}
		return boxes, true
	case registry.BlockTypeBambooStalk:
		// Before the per-position offset collisionOffset adds.
		return []box{boxF(6.5, 0, 6.5, 9.5, 16, 9.5)}, true
	case registry.BlockTypePointedDripstone, registry.BlockTypeSulfurSpike:
		w := dripstoneWidths[p["thickness"]]
		y0, y1 := 0.0, 16.0
		if p["thickness"] == "tip" {
			if p["vertical_direction"] == "down" {
				y0 = 5
			} else {
				y1 = 11
			}
		}
		return []box{boxF(8-w/2, y0, 8-w/2, 8+w/2, y1, 8+w/2)}, true
	case registry.BlockTypeBigDripleaf:
		switch p["tilt"] {
		case "partial":
			return []box{boxF(0, 11, 0, 16, 13, 16)}, true
		case "full":
			return nil, true
		}
		return []box{boxF(0, 11, 0, 16, 15, 16)}, true
	case registry.BlockTypeLadder:
		return []box{rotateNorth(boxF(0, 0, 13, 16, 16, 16), facing)}, true
	case registry.BlockTypeWallHangingSign:
		// WallHangingSignBlock.SHAPES_PLANK: the plank across the facing.
		if facing == dirEast || facing == dirWest {
			return []box{boxF(6, 14, 0, 10, 16, 16)}, true
		}
		return []box{boxF(0, 14, 6, 16, 16, 10)}, true
	case registry.BlockTypeRepeater, registry.BlockTypeComparator:
		return []box{boxF(0, 0, 0, 16, 2, 16)}, true
	case registry.BlockTypeCocoa:
		// The pod grows two pixels each way per age, hanging from the log its
		// facing points at.
		// vanilla: CocoaBlock
		age, _ := strconv.Atoi(p["age"])
		w := float64(4 + 2*age)
		return []box{rotateNorth(boxF(8-w/2, 12-w-1, 1, 8+w/2, 12, 1+w), facing)}, true
	case registry.BlockTypeDoor, registry.BlockTypeWeatheringCopperDoor:
		// The panel on the far side of facing; open, a quarter turn toward the
		// hinge's other side.
		// vanilla: DoorBlock
		d := facing
		if p["open"] == "true" {
			d = blocklogic.Clockwise(facing)
			if p["hinge"] == "right" {
				d = blocklogic.CounterClockwise(facing)
			}
		}
		return []box{rotateNorth(boxF(0, 0, 13, 16, 16, 16), d)}, true
	case registry.BlockTypeFence:
		// The 4-pixel post and an arm of its width to each connected face,
		// 24/16 tall.
		// vanilla: FenceBlock (collisionHeight 24)
		return crossCollisionBoxes(p, 6, 10, 24), true
	case registry.BlockTypeIronBars, registry.BlockTypeWeatheringCopperBar, registry.BlockTypeStainedGlassPane:
		// The panes: the same cross, 2 pixels wide and cell-tall.
		return crossCollisionBoxes(p, 7, 9, 16), true
	case registry.BlockTypeWall:
		// All 24/16 tall, low and tall sides alike: up stands the post, each
		// connected side a 6-pixel arm reaching 11/16 in.
		// vanilla: WallBlock (postHeight 24, wallTop 24)
		var boxes []box
		if p["up"] == "true" {
			boxes = append(boxes, boxF(4, 0, 4, 12, 24, 12))
		}
		if p["north"] != "none" {
			boxes = append(boxes, boxF(5, 0, 0, 11, 24, 11))
		}
		if p["south"] != "none" {
			boxes = append(boxes, boxF(5, 0, 5, 11, 24, 16))
		}
		if p["west"] != "none" {
			boxes = append(boxes, boxF(0, 0, 5, 11, 24, 11))
		}
		if p["east"] != "none" {
			boxes = append(boxes, boxF(5, 0, 5, 16, 24, 11))
		}
		return boxes, true
	case registry.BlockTypeChain, registry.BlockTypeWeatheringCopperChain:
		// ChainBlock: the 3-pixel column along the axis.
		switch p["axis"] {
		case "x":
			return []box{boxF(0, 6.5, 6.5, 16, 9.5, 9.5)}, true
		case "z":
			return []box{boxF(6.5, 6.5, 0, 9.5, 9.5, 16)}, true
		}
		return []box{boxF(6.5, 0, 6.5, 9.5, 16, 9.5)}, true
	case registry.BlockTypeFenceGate:
		if p["open"] == "true" {
			return nil, true
		}
		// Closed, a 4-pixel plate across the cell, 24/16 tall; in_wall only
		// changes the look.
		// vanilla: FenceGateBlock
		if facing == dirEast || facing == dirWest {
			return []box{boxF(6, 0, 0, 10, 24, 16)}, true
		}
		return []box{boxF(0, 0, 6, 16, 24, 10)}, true
	}
	return nil, false
}

// crossCollisionBoxes is a centred post between lo and hi on X and Z plus an
// arm of its width to each connected face, all top/16 tall.
// vanilla: CrossCollisionBlock.makeShapes
func crossCollisionBoxes(p map[string]string, lo, hi, top float64) []box {
	boxes := []box{boxF(lo, 0, lo, hi, top, hi)}
	if p["north"] == "true" {
		boxes = append(boxes, boxF(lo, 0, 0, hi, top, lo))
	}
	if p["south"] == "true" {
		boxes = append(boxes, boxF(lo, 0, hi, hi, top, 16))
	}
	if p["west"] == "true" {
		boxes = append(boxes, boxF(0, 0, lo, lo, top, hi))
	}
	if p["east"] == "true" {
		boxes = append(boxes, boxF(hi, 0, lo, 16, top, hi))
	}
	return boxes
}

// facingBox is a w-wide column centred across facing's axis, running from..to
// away from the face the block stands on.
func facingBox(facing int, w, from, to float64) box {
	lo, hi := 8-w/2, 8+w/2
	switch facing {
	case dirDown:
		return boxF(lo, 16-to, lo, hi, 16-from, hi)
	case dirNorth:
		return boxF(lo, lo, 16-to, hi, hi, 16-from)
	case dirSouth:
		return boxF(lo, lo, from, hi, hi, to)
	case dirWest:
		return boxF(16-to, lo, lo, 16-from, hi, hi)
	case dirEast:
		return boxF(from, lo, lo, to, hi, hi)
	}
	return boxF(lo, from, lo, hi, to, hi)
}

// rotateNorth turns a box drawn for north to another horizontal facing about
// the block's centre.
// vanilla: Shapes.rotateHorizontal
func rotateNorth(b box, facing int) box {
	switch facing {
	case dirEast:
		return box{1 - b.z1, b.y0, b.x0, 1 - b.z0, b.y1, b.x1}
	case dirSouth:
		return box{1 - b.x1, b.y0, 1 - b.z1, 1 - b.x0, b.y1, 1 - b.z0}
	case dirWest:
		return box{b.z0, b.y0, 1 - b.x1, b.z1, b.y1, 1 - b.x0}
	}
	return b
}

// offsetKind is a state's maximum XZ offset in sixteenths, zero for none
// (bamboo 4, speleothems 2).
var offsetKind [65536]uint8

func init() {
	for id := range offsetKind {
		if _, _, ok := registry.BlockStateName(uint16(id)); !ok {
			continue
		}
		switch registry.BlockOf(uint16(id)).Type() {
		case registry.BlockTypeBambooStalk:
			offsetKind[id] = 4
		case registry.BlockTypePointedDripstone, registry.BlockTypeSulfurSpike:
			offsetKind[id] = 2
		}
	}
}

// collisionOffset is BlockBehaviour's OffsetType.XZ offset at (x, z).
func collisionOffset(state uint16, x, z int32) (dx, dz float64) {
	k := offsetKind[state]
	if k == 0 {
		return 0, 0
	}
	seed := int64(x*3129871) ^ int64(z)*116129781
	seed = (seed*seed*42317861 + seed*11) >> 16
	limit := float64(k) / 16
	clamp := func(v float64) float64 {
		return min(max(v, -limit), limit)
	}
	dx = clamp((float64(float32(seed&15)/15) - 0.5) * 0.5)
	dz = clamp((float64(float32(seed>>8&15)/15) - 0.5) * 0.5)
	return dx, dz
}
