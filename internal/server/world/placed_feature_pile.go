package world

import "github.com/golem-mc/golem/registry"

// blockPile is BlockPileFeature: a lumpy pile of blocks on sturdy ground.
// vanilla: BlockPileFeature.place
func blockPile(provider stateProvider) featureFn {
	return func(r *decorRegion, origin [3]int32) bool {
		if origin[1] < worldMinY+5 {
			return false
		}
		xr := 2 + r.rng.NextIntN(2)
		zr := 2 + r.rng.NextIntN(2)
		// BlockPos.betweenClosed walks x fastest, then y, then z.
		for z := origin[2] - zr; z <= origin[2]+zr; z++ {
			for y := origin[1]; y <= origin[1]+1; y++ {
				for x := origin[0] - xr; x <= origin[0]+xr; x++ {
					xd, zd := origin[0]-x, origin[2]-z
					if float32(xd*xd+zd*zd) <= r.rng.NextFloat()*10-r.rng.NextFloat()*6 ||
						float64(r.rng.NextFloat()) < 0.031 {
						blockPileTry(r, provider, [3]int32{x, y, z})
					}
				}
			}
		}
		return true
	}
}

func blockPileTry(r *decorRegion, provider stateProvider, p [3]int32) {
	if !r.isAir(p[0], p[1], p[2]) {
		return
	}
	below := r.get(p[0], p[1]-1, p[2])
	if registry.BlockOf(below) == registry.BlockDirtPath {
		if !r.rng.NextBoolean() {
			return
		}
	} else if !registry.FaceSturdy(below, dirUp, registry.SupportFull) {
		return
	}
	r.set(p[0], p[1], p[2], provider(r, p))
}

// rotatedBlock is RotatedBlockProvider: the block with a random axis.
// vanilla: RotatedBlockProvider.getState
func rotatedBlock(state uint16) stateProvider {
	return func(r *decorRegion, _ [3]int32) uint16 {
		s, _ := registry.WithProp(state, registry.PropAxis, [...]string{"x", "y", "z"}[r.rng.NextIntN(3)])
		return s
	}
}
