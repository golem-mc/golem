package terrain

import (
	"math"
	"math/bits"
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

const (
	// carverRange is how many chunks around a chunk may start a carver that
	// reaches into it. vanilla: NoiseBasedChunkGenerator.generateCarvers
	carverRange = 8
	// carverTunnelRange is vanilla: WorldCarver.getRange, which sets the longest tunnel.
	carverTunnelRange = 4
	// carverProtectedTop is how many blocks under the generation range's top a
	// carver leaves alone in a chunk that is not being upgraded.
	carverProtectedTop = 7
)

type carverRandom = levelgen.LegacyRandomSource

// floatProvider is a configured float provider.
type floatProvider func(r *carverRandom) float32

// constantFloat draws nothing. vanilla: ConstantFloat
func constantFloat(v float32) floatProvider { return func(*carverRandom) float32 { return v } }

// uniformFloat is vanilla: UniformFloat.sample (Mth.randomBetween).
func uniformFloat(lo, hi float32) floatProvider {
	return func(r *carverRandom) float32 { return float32(r.NextFloat()*(hi-lo)) + lo }
}

// trapezoidFloat is vanilla: TrapezoidFloat.sample.
func trapezoidFloat(lo, hi, plateau float32) floatProvider {
	span := hi - lo
	plateauStart := (span - plateau) / 2.0
	plateauEnd := span - plateauStart
	return func(r *carverRandom) float32 {
		a := float32(r.NextFloat() * plateauEnd)
		b := float32(r.NextFloat() * plateauStart)
		return lo + a + b
	}
}

// veryBiasedToBottomInt is vanilla: VeryBiasedToBottomInt.sample.
func veryBiasedToBottomInt(lo, hi int32) func(r *carverRandom) int32 {
	return func(r *carverRandom) int32 {
		return lo + r.NextIntN(r.NextIntN(r.NextIntN(hi-lo+1)+1)+1)
	}
}

// uniformHeight is a start height between two anchors, both inclusive.
// vanilla: UniformHeight.sample
type uniformHeight struct{ lo, hi anchor }

func (h uniformHeight) sample(r *carverRandom, g carveRange) int32 {
	lo, hi := h.lo.resolve(g.minY, g.depth), h.hi.resolve(g.minY, g.depth)
	if lo > hi {
		return lo
	}
	return r.NextIntN(hi-lo+1) + lo
}

// carveRange is the generation range of the chunk being carved.
// vanilla: WorldGenerationContext
type carveRange struct{ minY, depth int32 }

// CarvingMask records the blocks of one chunk the carvers reached, indexed by
// local x and z and the y above the mask's bottom. vanilla: CarvingMask
type CarvingMask struct {
	minY, maxY, height int32
	bits               []uint64
}

// NewCarvingMask returns an empty mask for the y range minY..maxY.
func NewCarvingMask(minY, maxY int32) *CarvingMask {
	h := maxY - minY + 1
	return &CarvingMask{minY: minY, maxY: maxY, height: h, bits: make([]uint64, (256*int(h)+63)/64)}
}

func (m *CarvingMask) index(x, y, z int32) int { return int(y-m.minY) + int(z+x<<4)*int(m.height) }

// Carve marks the block at local x, z and world y. vanilla: CarvingMask.carve
func (m *CarvingMask) Carve(x, y, z int32) {
	i := m.index(x, y, z)
	m.bits[i>>6] |= 1 << (i & 63)
}

func (m *CarvingMask) get(x, y, z int32) bool {
	i := m.index(x, y, z)
	return m.bits[i>>6]&(1<<(i&63)) != 0
}

// Get reports whether a block at local x, z and world y was reached by a carver.
// Positions outside the mask are unmarked.
func (m *CarvingMask) Get(x, y, z int32) bool {
	return x >= 0 && x < 16 && z >= 0 && z < 16 && y >= m.minY && y <= m.maxY && m.get(x, y, z)
}

// Empty is vanilla: CarvingMask.isEmpty.
func (m *CarvingMask) Empty() bool {
	for _, w := range m.bits {
		if w != 0 {
			return false
		}
	}
	return true
}

// Visit calls f for every run of marked blocks in a column, topY and bottomY
// inclusive; the columns come in index order. vanilla: CarvingMask.visit
func (m *CarvingMask) Visit(f func(x, z, bottomY, topY int32)) {
	height := int(m.height)
	start, end := 0, 0
	emit := func() {
		for start < end {
			column := start / height
			stop := min(end, (column+1)*height)
			f(int32(column>>4), int32(column&15), m.minY+int32(start%height), m.minY+int32(stop-column*height-1))
			start = stop
		}
	}
	for i, word := range m.bits {
		for word != 0 {
			lo := bits.TrailingZeros64(word)
			n := min(64-lo, bits.TrailingZeros64(^(word >> lo)))
			next := i*64 + lo
			if next != end {
				emit()
				start = next
			}
			end = next + n
			word &^= (^uint64(0) >> (64 - n)) << lo
		}
	}
	emit()
}

// Carver is a configured carver. vanilla: WorldCarver
type Carver interface {
	isStartChunk(r *carverRandom) bool
	// carve marks the part of the carver's tunnels that reaches chunk, started
	// in the chunk at sourceX, sourceZ.
	carve(g carveRange, r *carverRandom, chunkX, chunkZ, sourceX, sourceZ int32, out *CarvingMask)
}

// carveEllipsoid marks an ellipsoid's part inside the chunk, skipping the
// blocks skip rejects. vanilla: WorldCarver.carveEllipsoid
func carveEllipsoid(chunkX, chunkZ int32, x, y, z, horizontalRadius, verticalRadius float64, out *CarvingMask, skip func(xd, yd, zd float64, y int32) bool) {
	chunkMinX, chunkMinZ := chunkX*16, chunkZ*16
	maxDelta := 16 + horizontalRadius*2
	if math.Abs(x-float64(chunkMinX+8)) > maxDelta || math.Abs(z-float64(chunkMinZ+8)) > maxDelta {
		return
	}
	minXIndex := max(floorInt(x-horizontalRadius)-chunkMinX-1, 0)
	maxXIndex := min(floorInt(x+horizontalRadius)-chunkMinX, 15)
	minY := max(floorInt(y-verticalRadius)-1, out.minY)
	maxY := min(floorInt(y+verticalRadius)+1, out.maxY)
	minZIndex := max(floorInt(z-horizontalRadius)-chunkMinZ-1, 0)
	maxZIndex := min(floorInt(z+horizontalRadius)-chunkMinZ, 15)
	for xi := minXIndex; xi <= maxXIndex; xi++ {
		xd := (float64(chunkMinX+xi) + 0.5 - x) / horizontalRadius
		for zi := minZIndex; zi <= maxZIndex; zi++ {
			zd := (float64(chunkMinZ+zi) + 0.5 - z) / horizontalRadius
			if float64(xd*xd)+float64(zd*zd) >= 1 {
				continue
			}
			for worldY := maxY; worldY > minY; worldY-- {
				yd := (float64(worldY) - 0.5 - y) / verticalRadius
				if !skip(xd, yd, zd, worldY) {
					out.Carve(xi, worldY, zi)
				}
			}
		}
	}
}

// canReach reports whether a tunnel at (x, z) with the steps it has left can
// still reach the chunk. vanilla: WorldCarver.canReach
func canReach(chunkX, chunkZ int32, x, z float64, currentStep, totalSteps int32, thickness float32) bool {
	xd := x - float64(chunkX*16+8)
	zd := z - float64(chunkZ*16+8)
	remaining := float64(totalSteps - currentStep)
	rr := float64(thickness + 2 + 16)
	return float64(xd*xd)+float64(zd*zd)-float64(remaining*remaining) <= float64(rr*rr)
}

// caveCarver is the cave carver, which the Nether's variant configures.
// vanilla: CaveWorldCarver
type caveCarver struct {
	probability                          float32
	y                                    uniformHeight
	count                                func(r *carverRandom) int32
	thickness                            floatProvider
	weirdThicknessBias                   bool
	roomVerticalRadius, hRadius, vRadius floatProvider
	startVerticalRadius, floorLevel      floatProvider
}

func (c *caveCarver) isStartChunk(r *carverRandom) bool { return r.NextFloat() <= c.probability }

// vanilla: CaveWorldCarver.carve
func (c *caveCarver) carve(g carveRange, r *carverRandom, chunkX, chunkZ, sourceX, sourceZ int32, out *CarvingMask) {
	maxDistance := int32(16 * (carverTunnelRange*2 - 1))
	caveCount := c.count(r)
	for range caveCount {
		x := float64(sourceX*16 + r.NextIntN(16))
		y := float64(c.y.sample(r, g))
		z := float64(sourceZ*16 + r.NextIntN(16))
		hMul := float64(c.hRadius(r))
		vMul := float64(c.vRadius(r))
		startVMul := float64(c.startVerticalRadius(r))
		floorLevel := float64(c.floorLevel(r))
		skip := func(xd, yd, zd float64, _ int32) bool {
			return yd <= floorLevel || float64(xd*xd)+float64(yd*yd)+float64(zd*zd) >= 1
		}
		tunnels := int32(1)
		if r.NextIntN(4) == 0 {
			yScale := float64(c.roomVerticalRadius(r))
			thickness := 1 + float32(r.NextFloat()*6)
			c.createRoom(chunkX, chunkZ, x, y, z, thickness, yScale, out, skip)
			tunnels += r.NextIntN(4)
		}
		for range tunnels {
			horizontalRotation := r.NextFloat() * (float32(math.Pi) * 2)
			verticalRotation := (r.NextFloat() - 0.5) / 4
			thickness := c.getThickness(r)
			distance := maxDistance - r.NextIntN(maxDistance/4)
			c.createTunnel(chunkX, chunkZ, r.NextLong(), x, y, z, hMul, vMul, thickness, horizontalRotation, verticalRotation, 0, distance, startVMul, out, skip)
		}
	}
}

// vanilla: CaveWorldCarver.getThickness
func (c *caveCarver) getThickness(r *carverRandom) float32 {
	t := c.thickness(r)
	if c.weirdThicknessBias && r.NextIntN(10) == 0 {
		a, b := r.NextFloat(), r.NextFloat()
		t *= float32(float32(a*b)*3) + 1
	}
	return t
}

// createRoom marks a room's single ellipsoid, offset one block east.
// vanilla: CaveWorldCarver.createRoom
func (c *caveCarver) createRoom(chunkX, chunkZ int32, x, y, z float64, thickness float32, yScale float64, out *CarvingMask, skip func(xd, yd, zd float64, y int32) bool) {
	horizontalRadius := 1.5 + float64(float32(mthSin(1.5707963705062866)*thickness))
	carveEllipsoid(chunkX, chunkZ, x+1, y, z, horizontalRadius, horizontalRadius*yScale, out, skip)
}

// createTunnel walks one tunnel, marking an ellipsoid at most steps; at its
// split point a thick tunnel forks into two thinner ones and ends.
// vanilla: CaveWorldCarver.createTunnel
func (c *caveCarver) createTunnel(chunkX, chunkZ int32, tunnelSeed int64, x, y, z, hMul, vMul float64, thickness, horizontalRotation, verticalRotation float32, step, dist int32, yScale float64, out *CarvingMask, skip func(xd, yd, zd float64, y int32) bool) {
	r := levelgen.NewLegacyRandom(tunnelSeed)
	splitPoint := r.NextIntN(dist/2) + dist/4
	steep := r.NextIntN(6) == 0
	var yRota, xRota float32
	for currentStep := step; currentStep < dist; currentStep++ {
		angle := float32(math.Pi) * float32(currentStep) / float32(dist)
		horizontalRadius := 1.5 + float64(float32(mthSin(float64(angle))*thickness))
		verticalRadius := horizontalRadius * yScale
		cosX := mthCos(float64(verticalRotation))
		x += float64(float32(mthCos(float64(horizontalRotation)) * cosX))
		y += float64(mthSin(float64(verticalRotation)))
		z += float64(float32(mthSin(float64(horizontalRotation)) * cosX))
		if steep {
			verticalRotation *= 0.92
		} else {
			verticalRotation *= 0.7
		}
		verticalRotation += float32(xRota * 0.1)
		horizontalRotation += float32(yRota * 0.1)
		xRota *= 0.9
		yRota *= 0.75
		a, b, d := r.NextFloat(), r.NextFloat(), r.NextFloat()
		xRota += float32((a-b)*d) * 2
		a, b, d = r.NextFloat(), r.NextFloat(), r.NextFloat()
		yRota += float32((a-b)*d) * 4
		if currentStep == splitPoint && thickness > 1 {
			for _, turn := range [2]float32{-1.5707964, 1.5707964} {
				seed := r.NextLong()
				branch := float32(r.NextFloat()*0.5) + 0.5
				c.createTunnel(chunkX, chunkZ, seed, x, y, z, hMul, vMul, branch, horizontalRotation+turn, verticalRotation/3, currentStep, dist, 1, out, skip)
			}
			return
		}
		if r.NextIntN(4) == 0 {
			continue
		}
		if !canReach(chunkX, chunkZ, x, z, currentStep, dist, thickness) {
			return
		}
		carveEllipsoid(chunkX, chunkZ, x, y, z, horizontalRadius*hMul, verticalRadius*vMul, out, skip)
	}
}

// canyonCarver is the canyon carver. vanilla: CanyonWorldCarver
type canyonCarver struct {
	probability      float32
	y                uniformHeight
	verticalRotation floatProvider
	// shape is vanilla: CanyonWorldCarver.Shape
	distanceFactor, thickness, horizontalRadiusFactor, yScale floatProvider
	widthSmoothness                                           int32
	verticalRadiusDefaultFactor, verticalRadiusCenterFactor   float32
}

func (c *canyonCarver) isStartChunk(r *carverRandom) bool { return r.NextFloat() <= c.probability }

// vanilla: CanyonWorldCarver.carve
func (c *canyonCarver) carve(g carveRange, r *carverRandom, chunkX, chunkZ, sourceX, sourceZ int32, out *CarvingMask) {
	maxDistance := int32((carverTunnelRange*2 - 1) * 16)
	x := float64(sourceX*16 + r.NextIntN(16))
	y := float64(c.y.sample(r, g))
	z := float64(sourceZ*16 + r.NextIntN(16))
	horizontalRotation := r.NextFloat() * (float32(math.Pi) * 2)
	verticalRotation := c.verticalRotation(r)
	yScale := float64(c.yScale(r))
	thickness := c.thickness(r)
	distance := int32(float32(maxDistance) * c.distanceFactor(r))
	c.doCarve(g, chunkX, chunkZ, r.NextLong(), x, y, z, thickness, horizontalRotation, verticalRotation, distance, yScale, out)
}

// vanilla: CanyonWorldCarver.doCarve
func (c *canyonCarver) doCarve(g carveRange, chunkX, chunkZ int32, tunnelSeed int64, x, y, z float64, thickness, horizontalRotation, verticalRotation float32, distance int32, yScale float64, out *CarvingMask) {
	r := levelgen.NewLegacyRandom(tunnelSeed)
	widthFactors := c.widthFactors(g, r)
	skip := func(xd, yd, zd float64, worldY int32) bool {
		wf := widthFactors[worldY-g.minY-1]
		return float64(float64(xd*xd)+float64(zd*zd))*float64(wf)+float64(yd*yd)/6 >= 1
	}
	var yRota, xRota float32
	for currentStep := range distance {
		horizontalRadius := 1.5 + float64(float32(mthSin(float64(float32(currentStep)*float32(math.Pi)/float32(distance)))*thickness))
		verticalRadius := horizontalRadius * yScale
		horizontalRadius *= float64(c.horizontalRadiusFactor(r))
		verticalRadius = c.updateVerticalRadius(r, verticalRadius, distance, currentStep)
		xc := mthCos(float64(verticalRotation))
		xs := mthSin(float64(verticalRotation))
		x += float64(float32(mthCos(float64(horizontalRotation)) * xc))
		y += float64(xs)
		z += float64(float32(mthSin(float64(horizontalRotation)) * xc))
		verticalRotation *= 0.7
		verticalRotation += float32(xRota * 0.05)
		horizontalRotation += float32(yRota * 0.05)
		xRota *= 0.8
		yRota *= 0.5
		a, b, d := r.NextFloat(), r.NextFloat(), r.NextFloat()
		xRota += float32((a-b)*d) * 2
		a, b, d = r.NextFloat(), r.NextFloat(), r.NextFloat()
		yRota += float32((a-b)*d) * 4
		if r.NextIntN(4) == 0 {
			continue
		}
		if !canReach(chunkX, chunkZ, x, z, currentStep, distance, thickness) {
			return
		}
		carveEllipsoid(chunkX, chunkZ, x, y, z, horizontalRadius, verticalRadius, out, skip)
	}
}

// widthFactors draws each height's squared width factor, redrawn every
// widthSmoothness heights on average. vanilla: CanyonWorldCarver.initWidthFactors
func (c *canyonCarver) widthFactors(g carveRange, r *carverRandom) []float32 {
	factors := make([]float32, g.depth)
	widthFactor := float32(1)
	for i := range factors {
		if i == 0 || r.NextIntN(c.widthSmoothness) == 0 {
			a, b := r.NextFloat(), r.NextFloat()
			widthFactor = 1 + float32(a*b)
		}
		factors[i] = float32(widthFactor * widthFactor)
	}
	return factors
}

// updateVerticalRadius narrows the canyon toward its ends.
// vanilla: CanyonWorldCarver.updateVerticalRadius
func (c *canyonCarver) updateVerticalRadius(r *carverRandom, verticalRadius float64, distance, currentStep int32) float64 {
	multiplier := 1 - float32(math.Abs(float64(0.5-float32(currentStep)/float32(distance))))*2
	factor := c.verticalRadiusDefaultFactor + float32(c.verticalRadiusCenterFactor*multiplier)
	jitter := float32(r.NextFloat()*(1-0.75)) + 0.75
	return float64(float64(factor)*verticalRadius) * float64(jitter)
}

// mthSin reads vanilla's 65536-entry sine table. vanilla: Mth.sin
func mthSin(v float64) float32 {
	return sinTableEntry(int64(float64(v*10430.378350470453)) & 0xFFFF)
}

// mthCos is mthSin a quarter turn on. vanilla: Mth.cos
func mthCos(v float64) float32 {
	return sinTableEntry(int64(float64(v*10430.378350470453)+16384) & 0xFFFF)
}

// sinTableEntry is entry i of Mth.SIN, which holds float sines.
func sinTableEntry(i int64) float32 { return sinTable()[i] }

// sinTable is built on first use; 256 KiB is too much for every importer's
// start-up.
var sinTable = sync.OnceValue(func() *[65536]float32 {
	var t [65536]float32
	for i := range t {
		t[i] = float32(math.Sin(float64(i) / 10430.378350470453))
	}
	return &t
})
