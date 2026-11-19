package terrain

import "math"

// splineNode is a node of a cubic Hermite spline tree evaluated in float32:
// a leaf is a constant, an inner node interpolates its points' values along
// one coordinate. Every product that feeds a sum is wrapped in float32(...),
// which stops Go fusing it into an FMA. vanilla: CubicSpline
type splineNode struct {
	// coord indexes splineSet.coords; -1 for a constant leaf.
	coord       int
	constant    float32
	locations   []float32
	values      []*splineNode
	derivatives []float32
}

// splineSet collects the coordinate samplers its splines read; a coordinate
// used by several nodes is sampled once per evaluation.
// vanilla: SplineFunction.Sampler
type splineSet struct{ coords []Sampler }

func (ss *splineSet) index(s Sampler) int {
	for i, c := range ss.coords {
		if c == s {
			return i
		}
	}
	ss.coords = append(ss.coords, s)
	return len(ss.coords) - 1
}

// splineBuilder collects points in ascending order. vanilla: CubicSpline.Builder
type splineBuilder struct {
	n         splineNode
	transform func(float32) float32
}

func identity(v float32) float32 { return v }

func (ss *splineSet) builder(coord Sampler, transform func(float32) float32) *splineBuilder {
	return &splineBuilder{n: splineNode{coord: ss.index(coord)}, transform: transform}
}

// add is a point with a constant value and slope 0.
func (b *splineBuilder) add(location, value float32) *splineBuilder {
	return b.addSlope(location, value, 0)
}

// addSlope is a point with a constant value and the given slope.
func (b *splineBuilder) addSlope(location, value, slope float32) *splineBuilder {
	return b.push(location, &splineNode{coord: -1, constant: b.transform(value)}, slope)
}

// addSpline is a point whose value is another spline, with slope 0.
func (b *splineBuilder) addSpline(location float32, value *splineNode) *splineBuilder {
	return b.push(location, value, 0)
}

func (b *splineBuilder) push(location float32, value *splineNode, slope float32) *splineBuilder {
	if n := len(b.n.locations); n > 0 && location <= b.n.locations[n-1] {
		panic("spline points must be registered in ascending order")
	}
	b.n.locations = append(b.n.locations, location)
	b.n.values = append(b.n.values, value)
	b.n.derivatives = append(b.n.derivatives, slope)
	return b
}

func (b *splineBuilder) build() *splineNode {
	if len(b.n.locations) == 0 {
		panic("a spline needs a point")
	}
	n := b.n
	return &n
}

// lerpF32 is vanilla: Mth.lerp(float, float, float).
func lerpF32(alpha, p0, p1 float32) float32 { return lerp(alpha, p0, p1) }

// splineInput hands a spline its coordinates' values.
type splineInput interface{ coordinate(i int) float32 }

func (n *splineNode) linearExtend(input, value float32, index int) float32 {
	d := n.derivatives[index]
	if d == 0 {
		return value
	}
	return value + float32(d*float32(input-n.locations[index]))
}

// sample is vanilla: CubicSpline.sample.
func (n *splineNode) sample(in splineInput) float32 {
	if n.coord < 0 {
		return n.constant
	}
	input := in.coordinate(n.coord)
	// vanilla: Multipoint.findIntervalStart, Mth.binarySearch: the first
	// location above the input, less one.
	lo, hi := 0, len(n.locations)
	for hi > lo {
		mid := lo + (hi-lo)/2
		if input < n.locations[mid] {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	start := lo - 1
	last := len(n.locations) - 1
	if start < 0 {
		return n.linearExtend(input, n.values[0].sample(in), 0)
	}
	if start == last {
		return n.linearExtend(input, n.values[last].sample(in), last)
	}
	x1, x2 := n.locations[start], n.locations[start+1]
	t := (input - x1) / (x2 - x1)
	d1, d2 := n.derivatives[start], n.derivatives[start+1]
	y1 := n.values[start].sample(in)
	y2 := n.values[start+1].sample(in)
	a := float32(d1*float32(x2-x1)) - float32(y2-y1)
	b := float32(-d2*float32(x2-x1)) + float32(y2-y1)
	return lerp(t, y1, y2) + float32(float32(t*float32(1-t))*lerp(t, a, b))
}

// splineSampler is vanilla: SplineFunction.Sampler.
type splineSampler struct {
	root   *splineNode
	coords []Sampler
}

// Spline is vanilla: DensityFunctions.spline.
func (ss *splineSet) Spline(root *splineNode) Sampler { return &splineSampler{root, ss.coords} }

type pointInput struct {
	s       *splineSampler
	c       *Context
	x, y, z int32
	cached  [8]float32
}

func (p *pointInput) coordinate(i int) float32 {
	if v := p.cached[i]; !math.IsNaN(float64(v)) {
		return v
	}
	v := p.s.coords[i].Sample(p.c, p.x, p.y, p.z)
	p.cached[i] = v
	return v
}

func (s *splineSampler) Sample(c *Context, x, y, z int32) float32 {
	in := pointInput{s: s, c: c, x: x, y: y, z: z}
	nan := float32(math.NaN())
	for i := range in.cached {
		in.cached[i] = nan
	}
	return s.root.sample(&in)
}

type bufferInput struct {
	s     *splineSampler
	c     *Context
	v     Volume
	bufs  [][]float32
	index int
}

func (b *bufferInput) coordinate(i int) float32 {
	if b.bufs[i] == nil {
		b.bufs[i] = b.c.acquire(b.v.Size())
		b.s.coords[i].SampleVolume(b.c, b.bufs[i], b.v)
	}
	return b.bufs[i][b.index]
}

func (s *splineSampler) SampleVolume(c *Context, out []float32, v Volume) {
	in := bufferInput{s: s, c: c, v: v, bufs: make([][]float32, len(s.coords))}
	for i := range out {
		in.index = i
		out[i] = s.root.sample(&in)
	}
	for _, b := range in.bufs {
		if b != nil {
			c.release(b)
		}
	}
}
