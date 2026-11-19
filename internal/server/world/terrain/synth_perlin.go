package terrain

import "github.com/golem-mc/golem/internal/server/world/levelgen"

// Noise is a float noise field. vanilla: Noise
type Noise interface {
	Get2D(x, y float64) float32
	Get3D(x, y, z float64) float32
	// AddToVolume adds amplitude times the field at every volume position,
	// the position scaled by xzScale and yScale. vanilla: Noise.addToVolume
	AddToVolume(out []float32, v Volume, xzScale, yScale float64, amplitude float32)
}

// PerlinNoise is a single octave of improved Perlin noise. vanilla: PerlinNoise
type PerlinNoise struct {
	gradientNoise
	// fudgeYScale is SmearedPerlinNoise's y quantization; 0 for a plain octave.
	fudgeYScale float64
	smeared     bool
}

// NewPerlinNoise draws an octave from the random source. vanilla: PerlinNoise.<init>
func NewPerlinNoise(r levelgen.RandomSource) *PerlinNoise {
	return &PerlinNoise{gradientNoise: newGradientNoise(r, 256.0)}
}

// NewSmearedPerlinNoise draws an octave whose y is smeared. vanilla: SmearedPerlinNoise.<init>
func NewSmearedPerlinNoise(r levelgen.RandomSource, fudgeYScale float64) *PerlinNoise {
	p := NewPerlinNoise(r)
	p.fudgeYScale = fudgeYScale
	p.smeared = true
	return p
}

// Get2D is vanilla: PerlinNoise.get(double, double).
func (p *PerlinNoise) Get2D(x, y float64) float32 { return p.Get3D(wrap(x), 0.0, wrap(y)) }

// Get3D is vanilla: PerlinNoise.get(double, double, double) and
// SmearedPerlinNoise.get.
func (p *PerlinNoise) Get3D(ox, oy, oz float64) float32 {
	x := wrap(ox) + p.offsetX
	y := wrap(oy) + p.offsetY
	z := wrap(oz) + p.offsetZ
	fx, fy, fz := floorInt(x), floorInt(y), floorInt(z)
	rx := float32(x - float64(fx))
	rz := float32(z - float64(fz))
	if !p.smeared {
		ry := float32(y - float64(fy))
		return p.sampleAndLerp(fx, fy, fz, rx, ry, rz, ry)
	}
	ry := y - float64(fy)
	fudged := float32(ry - p.computeFudgeY(oy, ry))
	return p.sampleAndLerp(fx, fy, fz, rx, fudged, rz, float32(ry))
}

// computeFudgeY is vanilla: SmearedPerlinNoise.computeFudgeY.
func (p *PerlinNoise) computeFudgeY(originalY, relativeY float64) float64 {
	limit := relativeY
	if originalY >= 0.0 && originalY < relativeY {
		limit = originalY
	}
	return float64(floorInt(limit/p.fudgeYScale+float64(float32(1.0e-7)))) * p.fudgeYScale
}

// sampleAndLerp is vanilla: PerlinNoise.sampleAndLerp.
func (p *PerlinNoise) sampleAndLerp(x, y, z int32, rx, ry, rz, originalRY float32) float32 {
	x0 := p.permute(x)
	x1 := p.permute(x + 1)
	xy00 := p.permute(x0 + y)
	xy01 := p.permute(x0 + y + 1)
	xy10 := p.permute(x1 + y)
	xy11 := p.permute(x1 + y + 1)
	d000 := gradients[p.permute(xy00+z)&0xF].dot(rx, ry, rz)
	d100 := gradients[p.permute(xy10+z)&0xF].dot(rx-1.0, ry, rz)
	d010 := gradients[p.permute(xy01+z)&0xF].dot(rx, ry-1.0, rz)
	d110 := gradients[p.permute(xy11+z)&0xF].dot(rx-1.0, ry-1.0, rz)
	d001 := gradients[p.permute(xy00+z+1)&0xF].dot(rx, ry, rz-1.0)
	d101 := gradients[p.permute(xy10+z+1)&0xF].dot(rx-1.0, ry, rz-1.0)
	d011 := gradients[p.permute(xy01+z+1)&0xF].dot(rx, ry-1.0, rz-1.0)
	d111 := gradients[p.permute(xy11+z+1)&0xF].dot(rx-1.0, ry-1.0, rz-1.0)
	return lerp3(smoothstep(rx), smoothstep(originalRY), smoothstep(rz), d000, d100, d010, d110, d001, d101, d011, d111)
}

// AddToVolume is vanilla: PerlinNoise.addToVolume and SmearedPerlinNoise.addToVolume.
func (p *PerlinNoise) AddToVolume(out []float32, v Volume, xzScale, yScale float64, amplitude float32) {
	var d000xz, d100xz, d010xz, d110xz, d001xz, d101xz, d011xz, d111xz float32
	var g000y, g100y, g010y, g110y, g001y, g101y, g011y, g111y float32
	// The y terms depend on iy alone, so every column shares them.
	// Permutations use only the low eight floor bits. Splitting runs at
	// 65535 keeps their counts and floor in the same four bytes.
	type yTerm struct {
		floor             uint8
		count             uint16
		alphaY, relativeY float32
	}
	ys := make([]yTerm, v.SizeY)
	cells, start := 0, 0
	lastFloor := int32(-2147483648)
	for iy := range ys {
		originalY := float64(v.BlockY(int32(iy))) * yScale
		y := wrap(originalY) + p.offsetY
		fy := floorInt(y)
		ryd := y - float64(fy)
		ry := float32(ryd)
		fry := ry
		if p.smeared {
			fry = float32(ryd - p.computeFudgeY(originalY, ryd))
		}
		ys[iy] = yTerm{floor: uint8(fy), alphaY: smoothstep(ry), relativeY: fry}
		if iy == 0 || lastFloor != fy || iy-start == 65535 {
			if iy > 0 {
				ys[start].count = uint16(iy - start)
			}
			start = iy
			cells++
			lastFloor = fy
		}
	}
	if len(ys) > 0 {
		ys[start].count = uint16(len(ys) - start)
	}
	// Four dotXZ values per gradient amortize over at least eight cells;
	// sparse floor changes already reuse the corner terms.
	useTable := cells >= 8 && cells*2 >= len(ys)
	index := 0
	for iz := range v.SizeZ {
		z := wrap(float64(v.BlockZ(iz))*xzScale) + p.offsetZ
		fz := floorInt(z)
		rz := float32(z - float64(fz))
		alphaZ := smoothstep(rz)
		for ix := range v.SizeX {
			x := wrap(float64(v.BlockX(ix))*xzScale) + p.offsetX
			fx := floorInt(x)
			rx := float32(x - float64(fx))
			x0 := p.permute(fx)
			x1 := p.permute(fx + 1)
			alphaX := smoothstep(rx)
			var table [16]struct{ d00, d10, d01, d11, y float32 }
			if useTable {
				for i, g := range gradients {
					table[i].d00 = g.dotXZ(rx, rz)
					table[i].d10 = g.dotXZ(rx-1.0, rz)
					table[i].d01 = g.dotXZ(rx, rz-1.0)
					table[i].d11 = g.dotXZ(rx-1.0, rz-1.0)
					table[i].y = float32(g.y)
				}
			}
			if useTable {
				// Frequent floor changes need no inner batch loop.
				for _, yt := range ys {
					fy, alphaY, fry := int32(yt.floor), yt.alphaY, yt.relativeY
					xy00 := p.permute(x0 + fy)
					xy01 := p.permute(x0 + fy + 1)
					xy10 := p.permute(x1 + fy)
					xy11 := p.permute(x1 + fy + 1)
					g := table[p.permute(xy00+fz)&0xF]
					d000xz, g000y = g.d00, g.y
					g = table[p.permute(xy10+fz)&0xF]
					d100xz, g100y = g.d10, g.y
					g = table[p.permute(xy01+fz)&0xF]
					d010xz, g010y = g.d00, g.y
					g = table[p.permute(xy11+fz)&0xF]
					d110xz, g110y = g.d10, g.y
					g = table[p.permute(xy00+fz+1)&0xF]
					d001xz, g001y = g.d01, g.y
					g = table[p.permute(xy10+fz+1)&0xF]
					d101xz, g101y = g.d11, g.y
					g = table[p.permute(xy01+fz+1)&0xF]
					d011xz, g011y = g.d01, g.y
					g = table[p.permute(xy11+fz+1)&0xF]
					d111xz, g111y = g.d11, g.y
					out[index] += float32(amplitude * lerp3(alphaX, alphaY, alphaZ,
						d000xz+float32(g000y*fry), d100xz+float32(g100y*fry),
						d010xz+float32(g010y*(fry-1.0)), d110xz+float32(g110y*(fry-1.0)),
						d001xz+float32(g001y*fry), d101xz+float32(g101y*fry),
						d011xz+float32(g011y*(fry-1.0)), d111xz+float32(g111y*(fry-1.0))))
					index++
				}
				continue
			}
			for iy := 0; iy < len(ys); {
				fy, end := int32(ys[iy].floor), iy+int(ys[iy].count)
				xy00 := p.permute(x0 + fy)
				xy01 := p.permute(x0 + fy + 1)
				xy10 := p.permute(x1 + fy)
				xy11 := p.permute(x1 + fy + 1)
				g := p.permuteToGrad(xy00 + fz)
				d000xz, g000y = g.dotXZ(rx, rz), float32(g.y)
				g = p.permuteToGrad(xy10 + fz)
				d100xz, g100y = g.dotXZ(rx-1.0, rz), float32(g.y)
				g = p.permuteToGrad(xy01 + fz)
				d010xz, g010y = g.dotXZ(rx, rz), float32(g.y)
				g = p.permuteToGrad(xy11 + fz)
				d110xz, g110y = g.dotXZ(rx-1.0, rz), float32(g.y)
				g = p.permuteToGrad(xy00 + fz + 1)
				d001xz, g001y = g.dotXZ(rx, rz-1.0), float32(g.y)
				g = p.permuteToGrad(xy10 + fz + 1)
				d101xz, g101y = g.dotXZ(rx-1.0, rz-1.0), float32(g.y)
				g = p.permuteToGrad(xy01 + fz + 1)
				d011xz, g011y = g.dotXZ(rx, rz-1.0), float32(g.y)
				g = p.permuteToGrad(xy11 + fz + 1)
				d111xz, g111y = g.dotXZ(rx-1.0, rz-1.0), float32(g.y)
				for ; iy < end; iy++ {
					alphaY, fry := ys[iy].alphaY, ys[iy].relativeY
					out[index] += float32(amplitude * lerp3(alphaX, alphaY, alphaZ,
						d000xz+float32(g000y*fry), d100xz+float32(g100y*fry),
						d010xz+float32(g010y*(fry-1.0)), d110xz+float32(g110y*(fry-1.0)),
						d001xz+float32(g001y*fry), d101xz+float32(g101y*fry),
						d011xz+float32(g011y*(fry-1.0)), d111xz+float32(g111y*(fry-1.0))))
					index++
				}
			}
		}
	}
}
