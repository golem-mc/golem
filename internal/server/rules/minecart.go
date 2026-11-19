package rules

import "math"

// Minecart rules: where a cart item places its cart (MinecartRailSpot) and
// where a rider dismounts (MinecartDismountSpot). The cart's simulation is
// internal/server/entity's.

// MinecartRailSpot is where a cart item places a cart on the rail at (x, y,
// z): the cell centre, 1/16 over the floor, half a block higher on an
// ascending shape.
// vanilla: MinecartItem.useOn
func MinecartRailSpot(x, y, z int32, shape string) (float64, float64, float64) {
	offset := 0.0
	if isAscendingShape(shape) {
		offset = 0.5
	}
	return float64(x) + 0.5, float64(y) + 0.0625 + offset, float64(z) + 0.5
}

// isAscendingShape reports whether a rail shape is one of the four
// slopes, the vanilla RailShape.isSlope.
func isAscendingShape(shape string) bool {
	switch shape {
	case "ascending_east", "ascending_west", "ascending_north", "ascending_south":
		return true
	}
	return false
}

// MinecartDismountSpot is where a rider leaving a cart at (x, y, z) goes: the
// cells around in the travel direction's order (dx, dz, one of them zero),
// at the heights steps offer, the first whose floor fits the box clear of
// blocks; else on top of the cart.
// vanilla: AbstractMinecart.getDismountLocationForPassenger,
// DismountHelper.offsetsForDirection
func MinecartDismountSpot(w BoatWorld, x, y, z float64, dx, dz int32, poses []DismountPose) (px, py, pz float64) {
	cx, cy, cz := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	offsets := dismountOffsets(dx, dz)
	for _, p := range poses {
		halfW := p.Dims.Width / 2
		// The swimmer's own walk omits the cell below the feet, vanilla's
		// POSE_DISMOUNT_HEIGHTS: {0, 1} where the standing shapes also try
		// {-1}.
		heights := []int32{0, 1, -1}
		if p.Dims.Height <= 1 {
			heights = heights[:2]
		}
		for _, height := range heights {
			for _, off := range offsets {
				tx, ty, tz := cx+off[0], cy+height, cz+off[1]
				floor := DismountHeightAt(w, tx, ty, tz)
				if !CanDismountInBlock(floor) {
					continue
				}
				loc := [3]float64{float64(tx) + 0.5, float64(ty) + floor, float64(tz) + 0.5}
				if !w.BoxCollides(loc[0]-halfW, loc[1], loc[2]-halfW,
					loc[0]+halfW, loc[1]+p.Dims.Height, loc[2]+halfW) {
					return loc[0], loc[1], loc[2]
				}
			}
		}
	}
	// Nothing around takes the rider: the fallback that costs the world
	// least is the cart's own top, the seat it leaves.
	return x, y + 0.7, z
}

// dismountOffsets are the eight cells around a vehicle travelling in the
// direction (dx, dz), in the order vanilla walks them: the two sides, the
// diagonals behind, the diagonals ahead, the back, the front.
// vanilla: DismountHelper.offsetsForDirection
func dismountOffsets(dx, dz int32) [8][2]int32 {
	var right [2]int32
	switch {
	case dx == 0 && dz > 0: // south, whose right is west
		right = [2]int32{-1, 0}
	case dx == 0 && dz < 0: // north, whose right is east
		right = [2]int32{1, 0}
	case dz == 0 && dx > 0: // east, whose right is south
		right = [2]int32{0, 1}
	default: // west, whose right is north
		right = [2]int32{0, -1}
	}
	back := [2]int32{-dx, -dz}
	ahead := [2]int32{dx, dz}
	return [8][2]int32{
		{right[0], right[1]},
		{-right[0], -right[1]},
		{back[0] + right[0], back[1] + right[1]},
		{back[0] - right[0], back[1] - right[1]},
		{ahead[0] + right[0], ahead[1] + right[1]},
		{ahead[0] - right[0], ahead[1] - right[1]},
		{back[0], back[1]},
		{ahead[0], ahead[1]},
	}
}

// AnimalDismountSpot is the first of the eight cells around the animal, in
// travel order, with a floor under a full block and room for one of the
// poses in order; false when none fits and the caller puts the passenger on
// top.
// vanilla: Animal.getDismountLocationForPassenger
func AnimalDismountSpot(w BoatWorld, x, y, z float64, dx, dz int32, poses []DismountPose) (px, py, pz float64, ok bool) {
	cx, cy, cz := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	offsets := dismountOffsets(dx, dz)
	for _, p := range poses {
		halfW := p.Dims.Width / 2
		for _, off := range offsets {
			tx, tz := cx+off[0], cz+off[1]
			floor := DismountHeightAt(w, tx, cy, tz)
			if !CanDismountInBlock(floor) {
				continue
			}
			lx, ly, lz := float64(tx)+0.5, float64(cy)+floor, float64(tz)+0.5
			if !w.BoxCollides(lx-halfW, ly, lz-halfW, lx+halfW, ly+p.Dims.Height, lz+halfW) {
				return lx, ly, lz, true
			}
		}
	}
	return 0, 0, 0, false
}
