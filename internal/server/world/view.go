package world

import "math"

// InViewOf reports whether a chunk is within radius chunks of the centre. The
// view is a square, the way the chunks that are sent are.
func InViewOf(pos, center ChunkPos, radius int32) bool {
	dx, dz := pos.X-center.X, pos.Z-center.Z
	return Abs32(dx) <= radius && Abs32(dz) <= radius
}

// DistanceTo is the squared distance between two chunks, which is all that is
// needed to put chunks in order of how near they are.
func DistanceTo(pos, center ChunkPos) int {
	dx, dz := int(pos.X-center.X), int(pos.Z-center.Z)
	return dx*dx + dz*dz
}

// ChunkOf is the chunk coordinate of a block coordinate, rounding down rather
// than toward zero, or everything just west or north of the origin lands in the
// wrong chunk.
func ChunkOf(coordinate float64) int32 {
	return int32(math.Floor(coordinate)) >> 4
}

// Abs32 is the absolute value of an int32, for the ring walks and the view
// squares.
func Abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
