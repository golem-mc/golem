package rules

// UnderSpawnProtection reports whether the block column lies within radius
// of the spawn by the larger of its two axis distances; a radius of 0 or
// less protects nothing.
// vanilla: DedicatedServer.isUnderSpawnProtection
func UnderSpawnProtection(x, z, spawnX, spawnZ int32, radius int) bool {
	if radius <= 0 {
		return false
	}
	dx := max(x-spawnX, spawnX-x)
	dz := max(z-spawnZ, spawnZ-z)
	return int64(max(dx, dz)) <= int64(radius)
}
