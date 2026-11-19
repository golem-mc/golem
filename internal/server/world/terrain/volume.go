package terrain

// Volume is a grid of sample positions, y innermost. vanilla: DensityVolume
type Volume struct {
	SizeX, SizeY, SizeZ int32
	MinX, MinY, MinZ    int32
	StepX, StepY, StepZ int32
}

// NewVolume returns a volume with unit steps.
func NewVolume(sizeX, sizeY, sizeZ, minX, minY, minZ int32) Volume {
	return Volume{sizeX, sizeY, sizeZ, minX, minY, minZ, 1, 1, 1}
}

// Index is the buffer index of a grid cell. vanilla: DensityVolume.indexUnchecked
func (v Volume) Index(x, y, z int32) int32 { return y + (x+z*v.SizeX)*v.SizeY }

// BlockX is the block x of grid column x. vanilla: DensityVolume.blockX
func (v Volume) BlockX(x int32) int32 { return v.MinX + x*v.StepX }

// BlockY is the block y of grid row y. vanilla: DensityVolume.blockY
func (v Volume) BlockY(y int32) int32 { return v.MinY + y*v.StepY }

// BlockZ is the block z of grid column z. vanilla: DensityVolume.blockZ
func (v Volume) BlockZ(z int32) int32 { return v.MinZ + z*v.StepZ }

// Size is the number of positions. vanilla: DensityVolume.size
func (v Volume) Size() int { return int(v.SizeX) * int(v.SizeY) * int(v.SizeZ) }

// IndexOfBlock is the buffer index of the grid position at a block, or -1
// when the volume has none there. vanilla: DensityVolume.indexOfBlock
func (v Volume) IndexOfBlock(x, y, z int32) int32 {
	rx, ry, rz := x-v.MinX, y-v.MinY, z-v.MinZ
	if v.StepX == 1 && v.StepY == 1 && v.StepZ == 1 {
		if rx >= 0 && ry >= 0 && rz >= 0 && rx < v.SizeX && ry < v.SizeY && rz < v.SizeZ {
			return v.Index(rx, ry, rz)
		}
		return -1
	}
	if rx >= 0 && ry >= 0 && rz >= 0 && rx < v.SizeX*v.StepX && ry < v.SizeY*v.StepY && rz < v.SizeZ*v.StepZ &&
		rx%v.StepX == 0 && ry%v.StepY == 0 && rz%v.StepZ == 0 {
		return v.Index(rx/v.StepX, ry/v.StepY, rz/v.StepZ)
	}
	return -1
}
