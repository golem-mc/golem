package world

import (
	"github.com/golem-mc/golem/registry"
)

// lakeConfig is LakeFeature.Configuration.
type lakeConfig struct {
	fluid, barrier               stateProvider
	canPlace                     blockPred
	replaceFluid, replaceBarrier blockPred
}

// lakeGridSize is the lake's 16 x 16 x 8 occupancy grid.
const lakeGridSize = 2048

// lakeRim reports whether a cell outside the lake touches one inside it.
func lakeRim(grid *[lakeGridSize]bool, x, z, y int) bool {
	at := func(x, z, y int) bool { return grid[(x*16+z)*8+y] }
	return !at(x, z, y) && (x < 15 && at(x+1, z, y) || x > 0 && at(x-1, z, y) ||
		z < 15 && at(x, z+1, y) || z > 0 && at(x, z-1, y) ||
		y < 7 && at(x, z, y+1) || y > 0 && at(x, z, y-1))
}

// placeLake is LakeFeature: the lake's lower four layers hold the fluid, its
// upper four air, ringed by barrier blocks.
// vanilla: LakeFeature.place
func (r *decorRegion) placeLake(cfg lakeConfig, p [3]int32) bool {
	if p[1] <= r.minY+4 {
		return false
	}
	ox, oy, oz := p[0]-8, p[1]-4, p[2]-8
	caveAir := registry.BlockCaveAir.DefaultState()
	var grid [lakeGridSize]bool
	rng := r.rng
	for range rng.NextIntN(4) + 4 {
		xr := rng.NextDouble()*6 + 3
		yr := rng.NextDouble()*4 + 2
		zr := rng.NextDouble()*6 + 3
		xp := rng.NextDouble()*(16-xr-2) + 1 + xr/2
		yp := rng.NextDouble()*(8-yr-4) + 2 + yr/2
		zp := rng.NextDouble()*(16-zr-2) + 1 + zr/2
		for x := 1; x < 15; x++ {
			for z := 1; z < 15; z++ {
				for y := 1; y < 7; y++ {
					xd := (float64(x) - xp) / (xr / 2)
					yd := (float64(y) - yp) / (yr / 2)
					zd := (float64(z) - zp) / (zr / 2)
					if xd*xd+yd*yd+zd*zd < 1 {
						grid[(x*16+z)*8+y] = true
					}
				}
			}
		}
	}
	at := func(x, y, z int) [3]int32 { return [3]int32{ox + int32(x), oy + int32(y), oz + int32(z)} }
	origin := [3]int32{ox, oy, oz}
	fluid := cfg.fluid(r, origin)
	for x := range 16 {
		for z := range 16 {
			for y := range 8 {
				if !lakeRim(&grid, x, z, y) {
					continue
				}
				q := at(x, y, z)
				s := r.get(q[0], q[1], q[2])
				if y >= 4 && liquidBlock(s) || y < 4 && !registry.IsSolid(s) && s != fluid || !cfg.canPlace(r, q) {
					return false
				}
			}
		}
	}
	for x := range 16 {
		for z := range 16 {
			for y := range 8 {
				q := at(x, y, z)
				if !grid[(x*16+z)*8+y] || !cfg.replaceFluid(r, q) {
					continue
				}
				if y >= 4 {
					r.set(q[0], q[1], q[2], caveAir)
					r.markAbove(q[0], q[1], q[2])
				} else {
					r.set(q[0], q[1], q[2], fluid)
				}
			}
		}
	}
	barrier := cfg.barrier(r, origin)
	if !r.src.isAir(barrier) {
		for x := range 16 {
			for z := range 16 {
				for y := range 8 {
					if !lakeRim(&grid, x, z, y) || y >= 4 && rng.NextIntN(2) == 0 {
						continue
					}
					q := at(x, y, z)
					if s := r.get(q[0], q[1], q[2]); registry.IsSolid(s) && cfg.replaceBarrier(r, q) {
						r.set(q[0], q[1], q[2], barrier)
						r.markAbove(q[0], q[1], q[2])
					}
				}
			}
		}
	}
	if fluidHolding(fluid).kind == fluidWater {
		for x := range 16 {
			for z := range 16 {
				q := at(x, 4, z)
				climate := overworldBiomeIDs[r.src.biomeIndex(r, q[0], q[1], q[2])].Climate()
				if r.inBuildHeight(q[1]) && heightAdjustedTemperature(SeaLevel, q[0], q[1], q[2], climate) < rainTemperatureFloor &&
					registry.BlockOf(r.get(q[0], q[1], q[2])) == registry.BlockWater && cfg.replaceFluid(r, q) {
					r.set(q[0], q[1], q[2], iceState)
				}
			}
		}
	}
	return true
}

// heightmapMod is HeightmapPlacement over a heightmap read.
func heightmapMod(height func(r *decorRegion, x, z int32) int32) placementMod {
	return func(r *decorRegion, p [3]int32, next walkNext) {
		if y := height(r, p[0], p[2]); y > r.minY {
			next.call([3]int32{p[0], y, p[2]})
		}
	}
}

// surfaceRelativeMaxMod is SurfaceRelativeThresholdFilter with no minimum.
// vanilla: SurfaceRelativeThresholdFilter.shouldPlace
func surfaceRelativeMaxMod(height func(r *decorRegion, x, z int32) int32, maxInclusive int32) placementMod {
	return filterMod(func(r *decorRegion, p [3]int32) bool {
		return p[1] <= height(r, p[0], p[2])+maxInclusive
	})
}

// oceanFloorWGHeight reads the heightmap frozen before decoration; structures
// must not lift the threshold of underwater magma into their own floors.
// vanilla: ProtoChunk.setBlockState, ChunkStatus.CARVERS
func oceanFloorWGHeight(r *decorRegion, x, z int32) int32 {
	return r.oceanFloorHeight(x, z)
}
