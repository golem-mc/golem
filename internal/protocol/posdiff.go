package protocol

// PackPosDiff packs the offset from (x, y, z) to (tx, ty, tz), each axis
// biased by its radius into one byte: x in bits 16-23, y in 8-15, z in 0-7.
// The caller keeps each offset within its radius, and the radii at most 127.
// vanilla: BlockUtil.packDifferenceInPosition
func PackPosDiff(x, y, z, tx, ty, tz, rx, ry, rz int32) int32 {
	return (tx-x+rx)&0xFF<<16 | (ty-y+ry)&0xFF<<8 | (tz-z+rz)&0xFF
}

// ClampedPackPosDiff is PackPosDiff with radii 127 and each offset clamped
// to ±127, for a teleport that may go further.
// vanilla: BlockUtil.clampedPackDifferenceInPosition
func ClampedPackPosDiff(x, y, z, tx, ty, tz int32) int32 {
	c := func(d int32) int32 { return max(-127, min(127, d)) }
	return PackPosDiff(0, 0, 0, c(tx-x), c(ty-y), c(tz-z), 127, 127, 127)
}
