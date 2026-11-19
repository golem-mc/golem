package terrain

// floorDivI is Math.floorDiv.
func floorDivI(a, b int32) int32 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// floorModI is Math.floorMod.
func floorModI(a, b int32) int32 { return a - floorDivI(a, b)*b }

// interpolated samples its input at cell corners and interpolates the rest.
// vanilla: InterpolatedFunction.Sampler
type interpolated struct {
	input               Sampler
	cellXZ, cellY       int32
	cellXZInv, cellYInv float32
}

// Interpolated is vanilla: DensityFunctions.interpolated.
func Interpolated(input Sampler, cellSizeXZ, cellSizeY int32) Sampler {
	return &interpolated{input, cellSizeXZ, cellSizeY, 1.0 / float32(cellSizeXZ), 1.0 / float32(cellSizeY)}
}

// SampleVolume is vanilla: InterpolatedFunction.Sampler.sampleVolume.
func (s *interpolated) SampleVolume(c *Context, out []float32, v Volume) {
	mismatch := v.StepX != s.cellXZ && v.SizeX != 1 || v.StepY != s.cellY && v.SizeY != 1 || v.StepZ != s.cellXZ && v.SizeZ != 1 ||
		floorModI(v.MinX, s.cellXZ) != 0 || floorModI(v.MinY, s.cellY) != 0 || floorModI(v.MinZ, s.cellXZ) != 0
	if !mismatch {
		s.input.SampleVolume(c, out, v)
		return
	}
	if v.StepX != 1 || v.StepY != 1 || v.StepZ != 1 {
		s.sampleWithNonBlockStep(c, out, v)
		return
	}
	s.sampleWithBlockStep(c, out, v)
}

func (s *interpolated) sampleWithNonBlockStep(c *Context, out []float32, v Volume) {
	bv := NewVolume(v.SizeX*v.StepX, v.SizeY*v.StepY, v.SizeZ*v.StepZ, v.MinX, v.MinY, v.MinZ)
	bb := c.acquire(bv.Size())
	s.sampleWithBlockStep(c, bb, bv)
	for z := range v.SizeZ {
		for x := range v.SizeX {
			for y := range v.SizeY {
				out[v.Index(x, y, z)] = bb[bv.Index(x*v.StepX, y*v.StepY, z*v.StepZ)]
			}
		}
	}
	c.release(bb)
}

func (s *interpolated) sampleWithBlockStep(c *Context, out []float32, v Volume) {
	maxX, maxY, maxZ := v.BlockX(v.SizeX-1), v.BlockY(v.SizeY-1), v.BlockZ(v.SizeZ-1)
	minCellX := floorDivI(v.MinX, s.cellXZ)
	minCellY := floorDivI(v.MinY, s.cellY)
	minCellZ := floorDivI(v.MinZ, s.cellXZ)
	cellCountX := floorDivI(maxX, s.cellXZ) - minCellX + 1
	cellCountY := floorDivI(maxY, s.cellY) - minCellY + 1
	cellCountZ := floorDivI(maxZ, s.cellXZ) - minCellZ + 1
	sx, sy, sz := cellCountX, cellCountY, cellCountZ
	if floorModI(maxX, s.cellXZ) != 0 {
		sx++
	}
	if floorModI(maxY, s.cellY) != 0 {
		sy++
	}
	if floorModI(maxZ, s.cellXZ) != 0 {
		sz++
	}
	cv := Volume{sx, sy, sz, minCellX * s.cellXZ, minCellY * s.cellY, minCellZ * s.cellXZ, s.cellXZ, s.cellY, s.cellXZ}
	cb := c.acquire(cv.Size())
	s.input.SampleVolume(c, cb, cv)
	for cz := range cellCountZ {
		nextZ := min(cz+1, cv.SizeZ-1)
		for cx := range cellCountX {
			nextX := min(cx+1, cv.SizeX-1)
			v000 := cb[cv.Index(cx, 0, cz)]
			v100 := cb[cv.Index(nextX, 0, cz)]
			v001 := cb[cv.Index(cx, 0, nextZ)]
			v101 := cb[cv.Index(nextX, 0, nextZ)]
			for cy := range cellCountY {
				nextY := min(cy+1, cv.SizeY-1)
				v010 := cb[cv.Index(cx, nextY, cz)]
				v110 := cb[cv.Index(nextX, nextY, cz)]
				v011 := cb[cv.Index(cx, nextY, nextZ)]
				v111 := cb[cv.Index(nextX, nextY, nextZ)]
				s.fillCell(out, v, cv, cx, cy, cz, v000, v100, v010, v110, v001, v101, v011, v111)
				v000, v100, v001, v101 = v010, v110, v011, v111
			}
		}
	}
	c.release(cb)
}

// fillCell is vanilla: InterpolatedFunction.Sampler.fillCell.
func (s *interpolated) fillCell(out []float32, ov, cv Volume, cellX, cellY, cellZ int32, v000, v100, v010, v110, v001, v101, v011, v111 float32) {
	cox := cv.BlockX(cellX) - ov.MinX
	coy := cv.BlockY(cellY) - ov.MinY
	coz := cv.BlockZ(cellZ) - ov.MinZ
	x0 := max(0, -cox)
	y0 := max(0, -coy)
	z0 := max(0, -coz)
	x1 := min(s.cellXZ, ov.SizeX-cox) - 1
	y1 := min(s.cellY, ov.SizeY-coy) - 1
	z1 := min(s.cellXZ, ov.SizeZ-coz) - 1
	for z := z0; z <= z1; z++ {
		oz := coz + z
		alphaZ := float32(z) * s.cellXZInv
		v00 := lerp(alphaZ, v000, v001)
		v01 := lerp(alphaZ, v010, v011)
		v10 := lerp(alphaZ, v100, v101)
		v11 := lerp(alphaZ, v110, v111)
		for x := x0; x <= x1; x++ {
			ox := cox + x
			alphaX := float32(x) * s.cellXZInv
			v0 := lerp(alphaX, v00, v10)
			v1 := lerp(alphaX, v01, v11)
			valueStep := (v1 - v0) * s.cellYInv
			value := v0 + float32(valueStep*float32(y0))
			i := ov.Index(ox, coy+y0, oz)
			for y := y0; y <= y1; y++ {
				out[i] = value
				i++
				value += valueStep
			}
		}
	}
}

// Sample is vanilla: InterpolatedFunction.Sampler.sampleValue.
func (s *interpolated) Sample(c *Context, x, y, z int32) float32 {
	xIn := floorModI(x, s.cellXZ)
	yIn := floorModI(y, s.cellY)
	zIn := floorModI(z, s.cellXZ)
	if xIn == 0 && yIn == 0 && zIn == 0 {
		return s.input.Sample(c, x, y, z)
	}
	v := Volume{2, 2, 2, x - xIn, y - yIn, z - zIn, s.cellXZ, s.cellY, s.cellXZ}
	b := c.acquire(v.Size())
	s.input.SampleVolume(c, b, v)
	r := lerp3(float32(xIn)/float32(s.cellXZ), float32(yIn)/float32(s.cellY), float32(zIn)/float32(s.cellXZ),
		b[v.Index(0, 0, 0)], b[v.Index(1, 0, 0)], b[v.Index(0, 1, 0)], b[v.Index(1, 1, 0)],
		b[v.Index(0, 0, 1)], b[v.Index(1, 0, 1)], b[v.Index(0, 1, 1)], b[v.Index(1, 1, 1)])
	c.release(b)
	return r
}
