package world

import "math"

// InSendView reports whether a chunk is sent to a client with the given view
// radius: the tracking view with its neighbor ring, so the client can light the
// edge chunks it renders. vanilla: ChunkTrackingView.contains
func InSendView(pos, center ChunkPos, radius int32) bool {
	return inBufferedView(pos, center, radius, 2)
}

// InViewOf reports whether a chunk is within radius chunks of the centre, as a
// square; the chunks that are sent are the rounded InSendView.
func InViewOf(pos, center ChunkPos, radius int32) bool {
	dx, dz := pos.X-center.X, pos.Z-center.Z
	return Abs32(dx) <= radius && Abs32(dz) <= radius
}

// InChunkTrackingView reports whether a chunk is within the buffered view distance.
// vanilla: ChunkTrackingView.isInViewDistance
func InChunkTrackingView(pos, center ChunkPos, radius int32) bool {
	return inBufferedView(pos, center, radius, 1)
}

// inBufferedView compares the squared chunk distance, less buffer chunks on
// each axis, with the radius. vanilla: ChunkTrackingView.isWithinDistance
func inBufferedView(pos, center ChunkPos, radius, buffer int32) bool {
	dx := int64(max(0, Abs32(pos.X-center.X)-buffer))
	dz := int64(max(0, Abs32(pos.Z-center.Z)-buffer))
	r := int64(radius)
	return dx*dx+dz*dz < r*r
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
