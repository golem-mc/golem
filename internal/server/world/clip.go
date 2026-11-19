package world

import (
	"strconv"

	"github.com/golem-mc/golem/registry"
)

// ClipSourceFluid is Item.getPlayerPOVHitResult's clip with
// ClipContext.Fluid.SOURCE_ONLY: the first cell along the ray from (ox, oy, oz)
// in unit direction (dx, dy, dz), within reach, whose block or source fluid the
// ray crosses. Block outlines read as full cubes; air and fluid blocks have
// none. liquid means the hit was a water or lava source block's fluid (not a
// waterlogged block's). face is the entry face, which mayUseItemAt reads
// behind.
func (w *World) ClipSourceFluid(ox, oy, oz, dx, dy, dz, reach float64) (x, y, z, face int32, liquid, hit bool) {
	return w.clipFluid(ox, oy, oz, dx, dy, dz, reach, true)
}

// ClipAnyFluid is the same clip with ClipContext.Fluid.ANY (a boat item's use):
// flows count like sources, at the depth their level fills to. liquid means the
// hit was fluid.
func (w *World) ClipAnyFluid(ox, oy, oz, dx, dy, dz, reach float64) (x, y, z int32, liquid, hit bool) {
	x, y, z, _, liquid, hit = w.clipFluid(ox, oy, oz, dx, dy, dz, reach, false)
	return
}

// clipFluid walks the cells the ray crosses within reach: a block stops it, a
// fluid cell where the ray crosses the fluid shape (sources only with
// onlySource). face is the entry face: the cell walk's for a block, the fluid
// shape's for a fluid.
func (w *World) clipFluid(ox, oy, oz, dx, dy, dz, reach float64, onlySource bool) (x, y, z, face int32, liquid, hit bool) {
	origin, dir := [3]float64{ox, oy, oz}, [3]float64{dx, dy, dz}
	TraverseBlocks(ox, oy, oz, dx*reach, dy*reach, dz*reach, func(cx, cy, cz, cellFace int32) bool {
		state := w.BlockAt(cx, cy, cz)
		switch b := registry.BlockOf(state); b {
		case registry.BlockAir, registry.BlockCaveAir, registry.BlockVoidAir,
			registry.BlockLight, registry.BlockMovingPiston:
		case registry.BlockWater, registry.BlockLava:
			level := registry.StateValue(state, registry.PropLevel)
			if !onlySource || level == "0" {
				if f, ok := w.rayHitsFluid([3]int32{cx, cy, cz}, b, level, origin, dir, reach); ok {
					x, y, z, face, liquid, hit = cx, cy, cz, f, true, true
					return true
				}
			}
		default:
			x, y, z, face, liquid, hit = cx, cy, cz, cellFace, false, true
			return true
		}
		return false
	})
	return
}

// rayHitsFluid reports the entry face of the fluid shape in the cell and
// whether the ray crosses it: 8/9 for a source or falling column, 8/9 less the
// level for a flow, the whole cell under the same fluid.
// vanilla: FluidState.getShape
func (w *World) rayHitsFluid(cell [3]int32, fluid registry.Block, level string, origin, dir [3]float64, reach float64) (int32, bool) {
	height := 8.0 / 9
	if parsed, err := strconv.Atoi(level); err == nil && parsed > 0 && parsed < 8 {
		height = (8.0 - float64(parsed)) / 9
	}
	above := w.BlockAt(cell[0], cell[1]+1, cell[2])
	if registry.BlockOf(above) == fluid || fluid == registry.BlockWater && registry.StateBool(above, registry.PropWaterlogged) {
		height = 1
	}
	lo := [3]float64{float64(cell[0]), float64(cell[1]), float64(cell[2])}
	_, face, ok := SegmentAABB(origin[0], origin[1], origin[2],
		dir[0]*reach, dir[1]*reach, dir[2]*reach,
		lo[0], lo[1], lo[2], lo[0]+1, lo[1]+height, lo[2]+1)
	return face, ok
}
