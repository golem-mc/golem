package terrain

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

const (
	beardKernelRadius = 12
	beardKernelSize   = 2 * beardKernelRadius
)

// beardKernel is the Gaussian falloff by offset, z outer, then x, then y,
// sampled half a block above the offset's y. vanilla: Beardifier.BEARD_KERNEL
var beardKernel = func() []float32 {
	k := make([]float32, beardKernelSize*beardKernelSize*beardKernelSize)
	for zi := range beardKernelSize {
		for xi := range beardKernelSize {
			for yi := range beardKernelSize {
				dx := float64(xi - beardKernelRadius)
				dy := float64(yi-beardKernelRadius) + 0.5
				dz := float64(zi - beardKernelRadius)
				d := float64(dx*dx) + float64(dy*dy) + float64(dz*dz)
				k[(zi*beardKernelSize+xi)*beardKernelSize+yi] = float32(math.Pow(math.E, -d/16.0))
			}
		}
	}
	return k
}()

// Beardifier is the density the structures near a chunk add to its terrain.
// A nil Beardifier adds nothing. vanilla: Beardifier
type Beardifier struct {
	rigids    []levelgen.BeardRigid
	junctions []levelgen.BeardJunction
	affected  levelgen.BoundingBox
}

// NewBeardifier returns the beardifier over rigids and junctions, which
// changes the box around them grown by 24 blocks, or nil when there are none.
// vanilla: Beardifier.<init>
func NewBeardifier(rigids []levelgen.BeardRigid, junctions []levelgen.BeardJunction) *Beardifier {
	if len(rigids) == 0 && len(junctions) == 0 {
		return nil
	}
	b := &Beardifier{rigids: rigids, junctions: junctions}
	have := false
	include := func(box levelgen.BoundingBox) {
		if !have {
			b.affected, have = box, true
			return
		}
		a := &b.affected
		a.MinX, a.MinY, a.MinZ = min(a.MinX, box.MinX), min(a.MinY, box.MinY), min(a.MinZ, box.MinZ)
		a.MaxX, a.MaxY, a.MaxZ = max(a.MaxX, box.MaxX), max(a.MaxY, box.MaxY), max(a.MaxZ, box.MaxZ)
	}
	for _, r := range rigids {
		include(r.Box)
	}
	for _, j := range junctions {
		include(levelgen.BoundingBox{MinX: j.X, MinY: j.GroundY, MinZ: j.Z, MaxX: j.X, MaxY: j.GroundY, MaxZ: j.Z})
	}
	const grow = 2 * beardKernelRadius
	a := &b.affected
	a.MinX, a.MinY, a.MinZ, a.MaxX, a.MaxY, a.MaxZ = a.MinX-grow, a.MinY-grow, a.MinZ-grow, a.MaxX+grow, a.MaxY+grow, a.MaxZ+grow
	return b
}

func (b *Beardifier) inside(x, y, z int) bool {
	a := b.affected
	return x >= a.MinX && x <= a.MaxX && y >= a.MinY && y <= a.MaxY && z >= a.MinZ && z <= a.MaxZ
}

// beardifierSampler reads the chunk's beardifier from the context.
// vanilla: SimpleDensityFunction.BEARDIFIER
type beardifierSampler struct{}

// BeardifierSampler is vanilla: DensityFunctions.beardifier.
func BeardifierSampler() Sampler { return beardifierSampler{} }

func (beardifierSampler) Sample(c *Context, x, y, z int32) float32 {
	b := c.Beardifier
	if b == nil || !b.inside(int(x), int(y), int(z)) {
		return 0
	}
	return b.sampleUnchecked(int(x), int(y), int(z))
}

// SampleVolume is vanilla: Beardifier.sampleVolume.
func (beardifierSampler) SampleVolume(c *Context, out []float32, v Volume) {
	for i := range out {
		out[i] = 0
	}
	b := c.Beardifier
	if b == nil {
		return
	}
	a := b.affected
	if a.MaxX < int(v.MinX) || a.MinX > int(v.BlockX(v.SizeX-1)) || a.MaxY < int(v.MinY) || a.MinY > int(v.BlockY(v.SizeY-1)) ||
		a.MaxZ < int(v.MinZ) || a.MinZ > int(v.BlockZ(v.SizeZ-1)) {
		return
	}
	floorDiv := func(a, b int) int { return int(math.Floor(float64(a) / float64(b))) }
	minX := floorDiv(max(0, a.MinX-int(v.MinX)), int(v.StepX))
	minY := floorDiv(max(0, a.MinY-int(v.MinY)), int(v.StepY))
	minZ := floorDiv(max(0, a.MinZ-int(v.MinZ)), int(v.StepZ))
	maxX := min(int(v.SizeX)-1, floorDiv(a.MaxX-int(v.MinX), int(v.StepX)))
	maxY := min(int(v.SizeY)-1, floorDiv(a.MaxY-int(v.MinY), int(v.StepY)))
	maxZ := min(int(v.SizeZ)-1, floorDiv(a.MaxZ-int(v.MinZ), int(v.StepZ)))
	for z := minZ; z <= maxZ; z++ {
		bz := int(v.BlockZ(int32(z)))
		for x := minX; x <= maxX; x++ {
			bx := int(v.BlockX(int32(x)))
			for y := minY; y <= maxY; y++ {
				out[v.Index(int32(x), int32(y), int32(z))] = b.sampleUnchecked(bx, int(v.BlockY(int32(y))), bz)
			}
		}
	}
}

func (b *Beardifier) sampleUnchecked(x, y, z int) float32 {
	var value float32
	for _, r := range b.rigids {
		box := r.Box
		dx := max(0, box.MinX-x, x-box.MaxX)
		dz := max(0, box.MinZ-z, z-box.MaxZ)
		groundY := box.MinY + r.GroundLevelDelta
		dyToGround := y - groundY
		switch r.Adjustment {
		case levelgen.AdjustBury:
			value += buryContribution(float32(dx), float32(dyToGround)/2.0, float32(dz))
		case levelgen.AdjustBeardThin:
			value += float32(beardContribution(dx, dyToGround, dz, dyToGround) * 0.8)
		case levelgen.AdjustBeardBox:
			dy := max(0, groundY-y, y-box.MaxY)
			value += float32(beardContribution(dx, dy, dz, dyToGround) * 0.8)
		case levelgen.AdjustEncapsulate:
			dy := max(0, box.MinY-y, y-box.MaxY)
			value += float32(buryContribution(float32(dx)/2.0, float32(dy)/2.0, float32(dz)/2.0) * 0.8)
		}
	}
	for _, j := range b.junctions {
		dy := y - j.GroundY
		value += float32(beardContribution(x-j.X, dy, z-j.Z, dy) * 0.4)
	}
	return value
}

func lengthSquared3(x, y, z float32) float32 {
	return float32(x*x) + float32(y*y) + float32(z*z)
}

// buryContribution falls from 1 at the piece to 0 six blocks away.
// vanilla: Beardifier.getBuryContribution
func buryContribution(dx, dy, dz float32) float32 {
	d := lengthSquared3(dx, dy, dz)
	if d >= 36.0 {
		return 0
	}
	return 1.0 - float32(math.Sqrt(float64(d)))/6.0
}

// beardContribution is the kernel's weight at the offset, signed to fill
// below the ground and carve above it. vanilla: Beardifier.getBeardContribution
func beardContribution(dx, dy, dz, yToGround int) float32 {
	xi, yi, zi := dx+beardKernelRadius, dy+beardKernelRadius, dz+beardKernelRadius
	if xi < 0 || xi >= beardKernelSize || yi < 0 || yi >= beardKernelSize || zi < 0 || zi >= beardKernelSize {
		return 0
	}
	dyOffset := float32(yToGround) + 0.5
	d := lengthSquared3(float32(dx), dyOffset, float32(dz))
	value := float32(-dyOffset*float32(fastInvSqrt(float64(d/2.0)))) / 2.0
	return value * beardKernel[(zi*beardKernelSize+xi)*beardKernelSize+yi]
}

// fastInvSqrt is the bit-trick inverse square root with one Newton step.
// vanilla: Mth.fastInvSqrt
func fastInvSqrt(x float64) float64 {
	half := 0.5 * x
	i := 6910469410427058090 - int64(math.Float64bits(x))>>1
	x = math.Float64frombits(uint64(i))
	return x * (1.5 - float64(half*x*x))
}
