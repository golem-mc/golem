package terrain

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// Fluid is the fluid of a fluid status. vanilla: Aquifer.FluidStatus.fluidType
type Fluid uint8

// The fluids an aquifer places.
const (
	FluidAir Fluid = iota
	FluidWater
	FluidLava
)

// Substance is what the aquifer decides for a block: the chunk's default
// block (vanilla's null), or air or a fluid.
type Substance uint8

// The substances. SubstanceDefault is the settings' default block.
const (
	SubstanceDefault Substance = iota
	SubstanceAir
	SubstanceWater
	SubstanceLava
)

func substanceOf(f Fluid) Substance { return Substance(f) + SubstanceAir }

// FluidStatus is a fluid and the level below which it fills.
// vanilla: Aquifer.FluidStatus
type FluidStatus struct {
	Level int32
	Fluid Fluid
}

// at is the fluid of the block at y. vanilla: Aquifer.FluidStatus.at
func (s FluidStatus) at(y int32) Fluid {
	if y < s.Level {
		return s.Fluid
	}
	return FluidAir
}

// FluidPicker is the fluid that fills open blocks where no aquifer decides.
// vanilla: Aquifer.FluidPicker
type FluidPicker func(x, y, z int32) FluidStatus

// NewFluidPicker is vanilla: NoiseBasedChunkGenerator.createFluidPicker.
func NewFluidPicker(seaLevel int32, defaultFluid Fluid) FluidPicker {
	lava := FluidStatus{-54, FluidLava}
	sea := FluidStatus{seaLevel, defaultFluid}
	return func(_, y, _ int32) FluidStatus {
		if y < min(-54, seaLevel) {
			return lava
		}
		return sea
	}
}

// Aquifer decides, for one chunk, which fluid fills an open block and where
// the barrier between two fluids stays solid. It belongs to its chunk's
// goroutine. vanilla: Aquifer, Aquifer.NoiseBasedAquifer
type Aquifer struct {
	ctx    *Context
	global FluidPicker
	// cfg is nil for a disabled aquifer. vanilla: Aquifer.createDisabled
	cfg    *AquiferConfig
	random levelgen.PositionalRandomFactory
	// siteRandom is reseeded per grid location by cacheNeighbors; nil until the first miss.
	siteRandom                   levelgen.RandomSource
	shouldSchedule               bool
	skipSamplingAboveY           int32
	minGridX, minGridY, minGridZ int32
	gridSizeX, gridSizeZ         int32
	status                       []FluidStatus
	haveStatus                   []bool
	location                     []int64
	minSurfaceQX, minSurfaceQZ   int32
	surfaceWidth, surfaceDepth   int32
	surfaceLevels                []int32
	surfaceCache                 map[int64]int32 // Samples outside the precomputed rectangle.
	neighbors                    aquiferNeighbors
}

const (
	aquiferXRange      = 10
	aquiferYRange      = 9
	aquiferZRange      = 10
	aquiferSampleX     = -5
	aquiferSampleY     = 1
	aquiferSampleZ     = -5
	noLocation         = math.MaxInt64
	wayBelowMinY       = -2032 << 4
	surfaceAdjustDelta = 8
)

// flowingSimilarity is vanilla: NoiseBasedAquifer.FLOWING_UPDATE_SIMULARITY.
var flowingSimilarity = aquiferSimilarity(10*10, 12*12)

// surfaceSamplingOffsets are the chunk offsets whose preliminary surface bounds
// an aquifer's fluid level. vanilla: NoiseBasedAquifer.SURFACE_SAMPLING_OFFSETS_IN_CHUNKS
var surfaceSamplingOffsets = [13][2]int32{{0, 0}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {-3, 0}, {-2, 0}, {-1, 0}, {1, 0}, {-2, 1}, {-1, 1}, {0, 1}, {1, 1}}

func gridXZ(b int32) int32          { return b >> 4 }
func fromGridXZ(g, off int32) int32 { return g<<4 + off }
func gridY(b int32) int32           { return floorDivI(b, 12) }
func fromGridY(g, off int32) int32  { return g*12 + off }

// packBlockPos is vanilla: BlockPos.asLong.
func packBlockPos(x, y, z int32) int64 {
	return int64(x&0x3FFFFFF)<<38 | int64(z&0x3FFFFFF)<<12 | int64(y&0xFFF)
}

func unpackX(p int64) int32 { return int32(p >> 38) }
func unpackY(p int64) int32 { return int32(p << 52 >> 52) }
func unpackZ(p int64) int32 { return int32(p << 26 >> 38) }

// NewDisabledAquifer is vanilla: Aquifer.createDisabled.
func NewDisabledAquifer(global FluidPicker) *Aquifer { return &Aquifer{global: global} }

// NewAquifer is vanilla: Aquifer.Config.create. It samples the surface level
// over the volume through ctx, as the constructor does.
func NewAquifer(ctx *Context, cfg *AquiferConfig, random levelgen.PositionalRandomFactory, v Volume, global FluidPicker) *Aquifer {
	a := &Aquifer{ctx: ctx, global: global, cfg: cfg, random: random}
	a.minGridX = gridXZ(v.MinX + aquiferSampleX)
	maxGridX := gridXZ(v.BlockX(v.SizeX-1)+aquiferSampleX) + 1
	a.gridSizeX = maxGridX - a.minGridX + 1
	a.minGridY = gridY(v.MinY+aquiferSampleY) - 1
	maxGridY := gridY(v.BlockY(v.SizeY-1)+aquiferSampleY) + 1
	gridSizeY := maxGridY - a.minGridY + 1
	a.minGridZ = gridXZ(v.MinZ + aquiferSampleZ)
	maxGridZ := gridXZ(v.BlockZ(v.SizeZ-1)+aquiferSampleZ) + 1
	a.gridSizeZ = maxGridZ - a.minGridZ + 1
	total := a.gridSizeX * gridSizeY * a.gridSizeZ
	a.status = make([]FluidStatus, total)
	a.haveStatus = make([]bool, total)
	a.location = make([]int64, total)
	for i := range a.location {
		a.location[i] = noLocation
	}
	maxAdjusted := a.adjustSurfaceLevel(a.maxSurfaceLevel(fromGridXZ(a.minGridX, 0), fromGridXZ(a.minGridZ, 0), fromGridXZ(maxGridX, 9), fromGridXZ(maxGridZ, 9)))
	skipAboveGridY := gridY(maxAdjusted+12) + 1
	a.skipSamplingAboveY = fromGridY(skipAboveGridY, 11) - 1
	return a
}

// chunkPosKey is vanilla: ChunkPos.pack.
func chunkPosKey(x, z int32) int64 { return int64(uint32(x)) | int64(uint32(z))<<32 }

// surfaceLevel is vanilla: NoiseBasedAquifer.surfaceLevel.
func (a *Aquifer) surfaceLevel(blockX, blockZ int32) int32 {
	qx, qz := blockX>>2, blockZ>>2
	x, z := qx-a.minSurfaceQX, qz-a.minSurfaceQZ
	if x >= 0 && x < a.surfaceWidth && z >= 0 && z < a.surfaceDepth {
		return a.surfaceLevels[z*a.surfaceWidth+x]
	}
	bx, bz := qx<<2, qz<<2
	key := chunkPosKey(bx, bz)
	if v, ok := a.surfaceCache[key]; ok {
		return v
	}
	v := floorInt(float64(a.cfg.SurfaceLevel.Sample(a.ctx, bx, 0, bz)))
	if a.surfaceCache == nil {
		a.surfaceCache = make(map[int64]int32)
	}
	a.surfaceCache[key] = v
	return v
}

// maxSurfaceLevel is vanilla: NoiseBasedAquifer.maxSurfaceLevel.
func (a *Aquifer) maxSurfaceLevel(minBlockX, minBlockZ, maxBlockX, maxBlockZ int32) int32 {
	minQX, maxQX := minBlockX>>2, maxBlockX>>2
	minQZ, maxQZ := minBlockZ>>2, maxBlockZ>>2
	v := Volume{maxQX - minQX + 1, 1, maxQZ - minQZ + 1, minQX << 2, 0, minQZ << 2, 4, 1, 4}
	buf := a.ctx.acquire(v.Size())
	a.cfg.SurfaceLevel.SampleVolume(a.ctx, buf, v)
	a.minSurfaceQX, a.minSurfaceQZ = minQX, minQZ
	a.surfaceWidth, a.surfaceDepth = v.SizeX, v.SizeZ
	a.surfaceLevels = make([]int32, v.Size())
	maxY := int32(math.MinInt32)
	for z := range v.SizeZ {
		for x := range v.SizeX {
			s := floorInt(float64(buf[v.Index(x, 0, z)]))
			a.surfaceLevels[z*v.SizeX+x] = s
			if s > maxY {
				maxY = s
			}
		}
	}
	a.ctx.release(buf)
	return maxY
}

func (a *Aquifer) index(gx, gy, gz int32) int32 {
	return ((gy-a.minGridY)*a.gridSizeZ+(gz-a.minGridZ))*a.gridSizeX + (gx - a.minGridX)
}

// ShouldScheduleFluidUpdate is vanilla: Aquifer.shouldScheduleFluidUpdate.
func (a *Aquifer) ShouldScheduleFluidUpdate() bool { return a.shouldSchedule }

func aquiferSimilarity(d1, d2 int32) float64 { return 1.0 - float64(d2-d1)/25.0 }

// ComputeSubstance is vanilla: Aquifer.computeSubstance.
func (a *Aquifer) ComputeSubstance(x, y, z int32, density float64) Substance {
	if a.cfg == nil {
		if density > 0.0 {
			return SubstanceDefault
		}
		return substanceOf(a.global(x, y, z).at(y))
	}
	if density > 0.0 {
		a.shouldSchedule = false
		return SubstanceDefault
	}
	globalFluid := a.global(x, y, z)
	if y > a.skipSamplingAboveY {
		a.shouldSchedule = false
		return substanceOf(globalFluid.at(y))
	}
	if globalFluid.at(y) == FluidLava {
		a.shouldSchedule = false
		return SubstanceLava
	}
	yAnchor := gridY(y + aquiferSampleY)
	n := &a.neighbors
	if !n.valid || n.x != x || n.z != z || n.anchorY != yAnchor {
		a.cacheNeighbors(x, yAnchor, z)
	}
	d1, d2, d3, d4 := int32(math.MaxInt32), int32(math.MaxInt32), int32(math.MaxInt32), int32(math.MaxInt32)
	var c1, c2, c3, c4 int
	for i, horizontal := range n.distanceXZ {
		dy := n.y[i] - y
		nd := horizontal + dy*dy
		// Equal distances prefer the later cell in vanilla's x/y/z visit order.
		switch {
		case d1 >= nd:
			c4, c3, c2, c1 = c3, c2, c1, i
			d4, d3, d2, d1 = d3, d2, d1, nd
		case d2 >= nd:
			c4, c3, c2 = c3, c2, i
			d4, d3, d2 = d3, d2, nd
		case d3 >= nd:
			c4, c3 = c3, i
			d4, d3 = d3, nd
		case d4 >= nd:
			c4 = i
			d4 = nd
		}
	}
	s1 := a.neighborStatus(c1)
	sim12 := aquiferSimilarity(d1, d2)
	fluid := s1.at(y)
	actual := substanceOf(fluid)
	if sim12 <= 0.0 {
		a.shouldSchedule = sim12 >= flowingSimilarity && s1 != a.neighborStatus(c2)
		return actual
	}
	if fluid == FluidWater && a.global(x, y-1, z).at(y-1) == FluidLava {
		a.shouldSchedule = true
		return actual
	}
	barrierValue := math.NaN()
	s2 := a.neighborStatus(c2)
	barrier12 := sim12 * a.pressure(x, y, z, &barrierValue, s1, s2)
	if density+barrier12 > 0.0 {
		a.shouldSchedule = false
		return SubstanceDefault
	}
	s3 := a.neighborStatus(c3)
	sim13 := aquiferSimilarity(d1, d3)
	if sim13 > 0.0 {
		if density+sim12*sim13*a.pressure(x, y, z, &barrierValue, s1, s3) > 0.0 {
			a.shouldSchedule = false
			return SubstanceDefault
		}
	}
	sim23 := aquiferSimilarity(d2, d3)
	if sim23 > 0.0 {
		if density+sim12*sim23*a.pressure(x, y, z, &barrierValue, s2, s3) > 0.0 {
			a.shouldSchedule = false
			return SubstanceDefault
		}
	}
	mayFlow12 := s1 != s2
	mayFlow23 := sim23 >= flowingSimilarity && s2 != s3
	mayFlow13 := sim13 >= flowingSimilarity && s1 != s3
	switch {
	case mayFlow12 || mayFlow23 || mayFlow13:
		a.shouldSchedule = true
	default:
		a.shouldSchedule = sim13 >= flowingSimilarity && aquiferSimilarity(d1, d4) >= flowingSimilarity && s1 != a.neighborStatus(c4)
	}
	return actual
}

// pressure is vanilla: NoiseBasedAquifer.calculatePressure.
func (a *Aquifer) pressure(x, y, z int32, barrierValue *float64, s1, s2 FluidStatus) float64 {
	t1, t2 := s1.at(y), s2.at(y)
	if t1 == FluidLava && t2 == FluidWater || t1 == FluidWater && t2 == FluidLava {
		return 2.0
	}
	diff := s1.Level - s2.Level
	if diff < 0 {
		diff = -diff
	}
	if diff == 0 {
		return 0.0
	}
	averageFluidY := 0.5 * float64(s1.Level+s2.Level)
	howFarAbove := float64(y) + 0.5 - averageFluidY
	baseValue := float64(diff) / 2.0
	distanceToMiddle := baseValue - math.Abs(howFarAbove)
	var gradient float64
	if howFarAbove > 0.0 {
		centerPoint := 0.0 + distanceToMiddle
		if centerPoint > 0.0 {
			gradient = centerPoint / 1.5
		} else {
			gradient = centerPoint / 2.5
		}
	} else {
		centerPoint := 3.0 + distanceToMiddle
		if centerPoint > 0.0 {
			gradient = centerPoint / 3.0
		} else {
			gradient = centerPoint / 10.0
		}
	}
	var noiseValue float64
	if gradient >= -2.0 && gradient <= 2.0 {
		if math.IsNaN(*barrierValue) {
			*barrierValue = float64(a.cfg.Barrier.Sample(a.ctx, x, y, z))
		}
		noiseValue = *barrierValue
	}
	return 2.0 * (noiseValue + gradient)
}

// aquiferNeighbors keeps the twelve cells in vanilla's x/y/z visit order.
// Horizontal distances remain valid within a column and vertical grid anchor.
// Status pointers are filled lazily so sampling order stays unchanged.
type aquiferNeighbors struct {
	x, z, anchorY int32
	valid         bool
	distanceXZ    [12]int32
	y             [12]int32
	index         [12]int32
	status        [12]*FluidStatus
}

func (a *Aquifer) cacheNeighbors(x, yAnchor, z int32) {
	n := &a.neighbors
	n.x, n.z, n.anchorY, n.valid = x, z, yAnchor, true
	xAnchor := gridXZ(x + aquiferSampleX)
	zAnchor := gridXZ(z + aquiferSampleZ)
	i := 0
	for x1 := int32(0); x1 <= 1; x1++ {
		for y1 := int32(-1); y1 <= 1; y1++ {
			for z1 := int32(0); z1 <= 1; z1++ {
				gx, gy, gz := xAnchor+x1, yAnchor+y1, zAnchor+z1
				idx := a.index(gx, gy, gz)
				loc := a.location[idx]
				if loc == noLocation {
					r := a.siteRandom
					if r == nil {
						r = a.random.At(gx, gy, gz)
						a.siteRandom = r
					} else {
						a.random.SeedAt(r, gx, gy, gz)
					}
					lx := fromGridXZ(gx, r.NextIntN(aquiferXRange))
					ly := fromGridY(gy, r.NextIntN(aquiferYRange))
					lz := fromGridXZ(gz, r.NextIntN(aquiferZRange))
					loc = packBlockPos(lx, ly, lz)
					a.location[idx] = loc
				}
				dx, dz := unpackX(loc)-x, unpackZ(loc)-z
				n.distanceXZ[i], n.y[i], n.index[i] = dx*dx+dz*dz, unpackY(loc), idx
				n.status[i] = nil
				if a.haveStatus[idx] {
					n.status[i] = &a.status[idx]
				}
				i++
			}
		}
	}
}

func (a *Aquifer) neighborStatus(i int) FluidStatus {
	if s := a.neighbors.status[i]; s != nil {
		return *s
	}
	idx := a.neighbors.index[i]
	loc := a.location[idx]
	s := a.computeFluid(unpackX(loc), unpackY(loc), unpackZ(loc))
	a.status[idx], a.haveStatus[idx] = s, true
	a.neighbors.status[i] = &a.status[idx]
	return s
}

func (a *Aquifer) adjustSurfaceLevel(level int32) int32 { return level + surfaceAdjustDelta }

// computeFluid is vanilla: NoiseBasedAquifer.computeFluid.
func (a *Aquifer) computeFluid(x, y, z int32) FluidStatus {
	global := a.global(x, y, z)
	lowest := int32(math.MaxInt32)
	top := y + 12
	bottom := y - 12
	centerUnderGlobal := false
	for _, off := range surfaceSamplingOffsets {
		sx := x + off[0]*16
		sz := z + off[1]*16
		surface := a.surfaceLevel(sx, sz)
		adjusted := a.adjustSurfaceLevel(surface)
		start := off[0] == 0 && off[1] == 0
		if start && bottom > adjusted {
			return global
		}
		pokes := top > adjusted
		if pokes || start {
			g := a.global(sx, adjusted, sz)
			if g.at(adjusted) != FluidAir {
				if start {
					centerUnderGlobal = true
				}
				if pokes {
					return g
				}
			}
		}
		lowest = min(lowest, surface)
	}
	level := a.computeSurfaceLevel(x, y, z, global, lowest, centerUnderGlobal)
	return FluidStatus{level, a.computeFluidType(x, y, z, global, level)}
}

// clampedMapD is vanilla: Mth.clampedMap(double, ...).
func clampedMapD(v, fromMin, fromMax, toMin, toMax float64) float64 {
	f := (v - fromMin) / (fromMax - fromMin)
	if f < 0.0 {
		return toMin
	}
	if f > 1.0 {
		return toMax
	}
	return toMin + float64(f*(toMax-toMin))
}

// mapD is vanilla: Mth.map(double, ...).
func mapD(v, fromMin, fromMax, toMin, toMax float64) float64 {
	f := (v - fromMin) / (fromMax - fromMin)
	return toMin + float64(f*(toMax-toMin))
}

// computeSurfaceLevel is vanilla: NoiseBasedAquifer.computeSurfaceLevel.
func (a *Aquifer) computeSurfaceLevel(x, y, z int32, global FluidStatus, lowestSurface int32, centerUnderGlobal bool) int32 {
	var partially, fully float64
	if float64(a.cfg.Exclusion.Sample(a.ctx, x, y, z)) > 0.0 {
		partially, fully = -1.0, -1.0
	} else {
		distanceBelowSurface := a.adjustSurfaceLevel(lowestSurface) - y
		factor := 0.0
		if centerUnderGlobal {
			factor = clampedMapD(float64(distanceBelowSurface), 0.0, 64.0, 1.0, 0.0)
		}
		noiseValue := float64(a.cfg.FluidLevelFloodedness.Sample(a.ctx, x, y, z))
		noiseValue = min(max(noiseValue, -1.0), 1.0)
		fullyThreshold := mapD(factor, 1.0, 0.0, -0.3, 0.8)
		partiallyThreshold := mapD(factor, 1.0, 0.0, -0.8, 0.4)
		partially = noiseValue - partiallyThreshold
		fully = noiseValue - fullyThreshold
	}
	switch {
	case fully > 0.0:
		return global.Level
	case partially > 0.0:
		return a.randomizedFluidSurfaceLevel(x, y, z, lowestSurface)
	}
	return wayBelowMinY
}

// randomizedFluidSurfaceLevel is vanilla: NoiseBasedAquifer.computeRandomizedFluidSurfaceLevel.
func (a *Aquifer) randomizedFluidSurfaceLevel(x, y, z, lowestSurface int32) int32 {
	cellX := floorDivI(x, 16)
	cellY := floorDivI(y, 40)
	cellZ := floorDivI(z, 16)
	middleY := cellY*40 + 20
	spread := float64(a.cfg.FluidLevelSpread.Sample(a.ctx, cellX, cellY, cellZ) * 10.0)
	quantized := floorInt(spread/3.0) * 3
	return min(lowestSurface, middleY+quantized)
}

// computeFluidType is vanilla: NoiseBasedAquifer.computeFluidType.
func (a *Aquifer) computeFluidType(x, y, z int32, global FluidStatus, level int32) Fluid {
	fluid := global.Fluid
	if level <= -10 && level != wayBelowMinY && global.Fluid != FluidLava {
		lava := float64(a.cfg.Lava.Sample(a.ctx, floorDivI(x, 64), floorDivI(y, 40), floorDivI(z, 64)))
		if math.Abs(lava) > 0.3 {
			fluid = FluidLava
		}
	}
	return fluid
}
