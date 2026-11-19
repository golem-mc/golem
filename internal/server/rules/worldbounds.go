package rules

// InWorldBoundsHorizontal reports whether the block column lies inside the
// world's horizontal extent, [-30000000, 30000000) on both axes.
// vanilla: Level.isInWorldBoundsHorizontal
func InWorldBoundsHorizontal(x, z int32) bool {
	return x >= -worldBorderLimit && z >= -worldBorderLimit && x < worldBorderLimit && z < worldBorderLimit
}
