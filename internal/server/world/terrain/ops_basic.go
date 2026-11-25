package terrain

import (
	"math"
	"slices"
)

// constant is vanilla: ConstantFunction.Sampler.
type constant float32

func (k constant) Sample(*Context, int32, int32, int32) float32 { return float32(k) }
func (k constant) SampleVolume(_ *Context, out []float32, _ Volume) {
	for i := range out {
		out[i] = float32(k)
	}
}

// constOf reports whether a sampler is a literal constant function.
func constOf(s Sampler) (float32, bool) {
	k, ok := s.(constant)
	return float32(k), ok
}

// Axis is a coordinate axis. vanilla: Direction.Axis
type Axis int

// The axes.
const (
	AxisX Axis = iota
	AxisY
	AxisZ
)

func (a Axis) choose(x, y, z int32) int32 {
	switch a {
	case AxisX:
		return x
	case AxisY:
		return y
	}
	return z
}

// gradient is a clamp-to-edge linear gradient along an axis.
// vanilla: GradientFunction.ClampedSampler
type gradientSampler struct {
	axis              Axis
	from, min, max    int32
	fromValue, factor float32
}

// Gradient is vanilla: DensityFunctions.gradient with TilingMode.CLAMP_TO_EDGE.
func Gradient(axis Axis, from, to int32, fromValue, toValue float32) Sampler {
	return &gradientSampler{axis, from, min(from, to), max(from, to), fromValue, (toValue - fromValue) / float32(to-from)}
}

// YClampedGradient is vanilla: DensityFunctions.yClampedGradient.
func YClampedGradient(from, to int32, fromValue, toValue float32) Sampler {
	return Gradient(AxisY, from, to, fromValue, toValue)
}

func (g *gradientSampler) compute(coordinate int32) float32 {
	rel := min(max(coordinate, g.min), g.max) - g.from
	return g.fromValue + float32(float32(rel)*g.factor)
}

func (g *gradientSampler) Sample(_ *Context, x, y, z int32) float32 {
	return g.compute(g.axis.choose(x, y, z))
}

// SampleVolume is vanilla: GradientFunction.GradientSampler.sampleVolume.
func (g *gradientSampler) SampleVolume(_ *Context, out []float32, v Volume) {
	switch g.axis {
	case AxisX:
		for x := range v.SizeX {
			value := g.compute(v.BlockX(x))
			for z := range v.SizeZ {
				i := v.Index(x, 0, z)
				for k := range v.SizeY {
					out[i+k] = value
				}
			}
		}
	case AxisY:
		for y := range v.SizeY {
			value := g.compute(v.BlockY(y))
			for z := range v.SizeZ {
				for x := range v.SizeX {
					out[v.Index(x, y, z)] = value
				}
			}
		}
	case AxisZ:
		for z := range v.SizeZ {
			value := g.compute(v.BlockZ(z))
			i := v.Index(0, 0, z)
			for k := range v.SizeX * v.SizeY {
				out[i+k] = value
			}
		}
	}
}

func absF(v float32) float32 { return math.Float32frombits(math.Float32bits(v) &^ (1 << 31)) }

func squareF(v float32) float32 { return v * v }
func cubeF(v float32) float32   { return v * v * v }

// javaMin is Math.min(float, float).
func javaMin(a, b float32) float32 {
	if a != a {
		return a
	}
	if a == 0 && b == 0 && math.Signbit(float64(b)) {
		return b
	}
	if a <= b {
		return a
	}
	return b
}

// javaMax is Math.max(float, float).
func javaMax(a, b float32) float32 {
	if a != a {
		return a
	}
	if a == 0 && b == 0 && math.Signbit(float64(a)) {
		return b
	}
	if a >= b {
		return a
	}
	return b
}

// Abs is vanilla: DensityFunction.abs.
func Abs(in Sampler) Sampler { return mapSampler(in, mapOp{kind: mapAbs}) }

// Square is vanilla: DensityFunction.square.
func Square(in Sampler) Sampler { return mapSampler(in, mapOp{kind: mapSquare}) }

// Cube is vanilla: DensityFunction.cube.
func Cube(in Sampler) Sampler { return mapSampler(in, mapOp{kind: mapCube}) }

// Negate is vanilla: DensityFunction.negate.
func Negate(in Sampler) Sampler { return mapSampler(in, mapOp{kind: mapNegate}) }

// HalfNegative is vanilla: DensityFunction.halfNegative.
func HalfNegative(in Sampler) Sampler { return mapSampler(in, mapOp{kind: mapNegative, k: 0.5}) }

// QuarterNegative is vanilla: DensityFunction.quarterNegative.
func QuarterNegative(in Sampler) Sampler { return mapSampler(in, mapOp{kind: mapNegative, k: 0.25}) }

// Squeeze is vanilla: DensityFunction.squeeze.
func Squeeze(in Sampler) Sampler { return mapSampler(in, mapOp{kind: mapSqueeze}) }

// Clamp is vanilla: DensityFunctions.clamp.
func Clamp(in Sampler, lo, hi float32) Sampler {
	return mapSampler(in, mapOp{kind: mapClamp, k: lo, hi: hi})
}

type binOp int

const (
	opAdd binOp = iota
	opSub
	opMul
	opDiv
	opMin
	opMax
)

// binary combines two samplers. vanilla: BinaryFunction.*Sampler
type binary struct {
	op   binOp
	l, r Sampler
}

func newBinary(op binOp, l, r Sampler) Sampler {
	lk, lc := constOf(l)
	rk, rc := constOf(r)
	switch op {
	case opAdd, opMul, opMin, opMax:
		if lc {
			return mapSampler(r, mapOp{kind: mapKind(op), k: lk})
		}
		if rc {
			return mapSampler(l, mapOp{kind: mapKind(op), k: rk})
		}
	case opSub:
		if lc {
			return mapSampler(r, mapOp{kind: mapKind(opSub), k: lk})
		}
		if rc {
			return mapSampler(l, mapOp{kind: mapKind(opAdd), k: -rk})
		}
	case opDiv:
		if lc {
			return mapSampler(r, mapOp{kind: mapKind(opDiv), k: lk})
		}
		if rc {
			return mapSampler(l, mapOp{kind: mapKind(opMul), k: 1.0 / rk})
		}
	}
	return &binary{op, l, r}
}

// Add is vanilla: DensityFunctions.add.
func Add(l, r Sampler) Sampler { return newBinary(opAdd, l, r) }

// Sub is vanilla: DensityFunctions.sub.
func Sub(l, r Sampler) Sampler { return newBinary(opSub, l, r) }

// Mul is vanilla: DensityFunctions.mul.
func Mul(l, r Sampler) Sampler { return newBinary(opMul, l, r) }

// Div is vanilla: DensityFunctions.div.
func Div(l, r Sampler) Sampler { return newBinary(opDiv, l, r) }

// Min is vanilla: DensityFunctions.min.
func Min(l, r Sampler) Sampler { return newBinary(opMin, l, r) }

// Max is vanilla: DensityFunctions.max.
func Max(l, r Sampler) Sampler { return newBinary(opMax, l, r) }

func (b *binary) Sample(c *Context, x, y, z int32) float32 {
	l := b.l.Sample(c, x, y, z)
	switch b.op {
	case opAdd:
		return l + b.r.Sample(c, x, y, z)
	case opSub:
		return l - b.r.Sample(c, x, y, z)
	case opMul:
		if l == 0 {
			return 0
		}
		return l * b.r.Sample(c, x, y, z)
	case opDiv:
		if l == 0 {
			return 0
		}
		return l / b.r.Sample(c, x, y, z)
	case opMin:
		return javaMin(l, b.r.Sample(c, x, y, z))
	}
	return javaMax(l, b.r.Sample(c, x, y, z))
}

func (b *binary) SampleVolume(c *Context, out []float32, v Volume) {
	b.l.SampleVolume(c, out, v)
	rb := c.acquire(len(out))
	b.r.SampleVolume(c, rb, v)
	switch b.op {
	case opAdd:
		for i := range out {
			out[i] += rb[i]
		}
	case opSub:
		for i := range out {
			out[i] += -rb[i]
		}
	case opMul:
		for i := range out {
			out[i] *= rb[i]
		}
	case opDiv:
		for i := range out {
			out[i] /= rb[i]
		}
	case opMin:
		for i := range out {
			if rb[i] < out[i] {
				out[i] = rb[i]
			}
		}
	case opMax:
		for i := range out {
			if rb[i] > out[i] {
				out[i] = rb[i]
			}
		}
	}
	c.release(rb)
}

// Remap maps [fromMin, fromMax] linearly onto [toMin, toMax]. vanilla: DensityFunctions.remap
func Remap(in Sampler, fromMin, fromMax, toMin, toMax float32) Sampler {
	factor := (toMax - toMin) / (fromMax - fromMin)
	offset := toMin - float32(fromMin*factor)
	m := Mul(in, constant(factor))
	if offset == 0 {
		return m
	}
	return Add(m, constant(offset))
}

// ClampedMap is vanilla: DensityFunctions.clampedMap.
func ClampedMap(in Sampler, fromMin, fromMax, toMin, toMax float32) Sampler {
	return Remap(Clamp(in, fromMin, fromMax), fromMin, fromMax, toMin, toMax)
}

// lerpSampler interpolates between two samplers by a third.
// vanilla: LerpFunction.Sampler, ConstFirstSampler, ConstSecondSampler
type lerpSampler struct {
	alpha, first, second Sampler
	// firstK and secondK are the constant side, when hasFirstK or hasSecondK.
	firstK, secondK       float32
	hasFirstK, hasSecondK bool
}

// Lerp is vanilla: DensityFunctions.lerp.
func Lerp(alpha, first, second Sampler) Sampler {
	l := &lerpSampler{alpha: alpha, first: first, second: second}
	if k, ok := constOf(first); ok {
		l.firstK, l.hasFirstK = k, true
	} else if k, ok := constOf(second); ok {
		l.secondK, l.hasSecondK = k, true
	}
	return l
}

func (l *lerpSampler) pick(alpha, first, second float32) float32 {
	if alpha == 0 {
		return first
	}
	if alpha == 1 {
		return second
	}
	return lerp(alpha, first, second)
}

func (l *lerpSampler) Sample(c *Context, x, y, z int32) float32 {
	alpha := l.alpha.Sample(c, x, y, z)
	switch {
	case l.hasFirstK:
		if alpha == 0 {
			return l.firstK
		}
		if alpha == 1 {
			return l.second.Sample(c, x, y, z)
		}
		return lerp(alpha, l.firstK, l.second.Sample(c, x, y, z))
	case l.hasSecondK:
		if alpha == 0 {
			return l.first.Sample(c, x, y, z)
		}
		if alpha == 1 {
			return l.secondK
		}
		return lerp(alpha, l.first.Sample(c, x, y, z), l.secondK)
	}
	if alpha == 0 {
		return l.first.Sample(c, x, y, z)
	}
	if alpha == 1 {
		return l.second.Sample(c, x, y, z)
	}
	return lerp(alpha, l.first.Sample(c, x, y, z), l.second.Sample(c, x, y, z))
}

func (l *lerpSampler) SampleVolume(c *Context, out []float32, v Volume) {
	l.alpha.SampleVolume(c, out, v)
	switch {
	case l.hasFirstK:
		sb := c.acquire(len(out))
		l.second.SampleVolume(c, sb, v)
		for i := range out {
			out[i] = l.pick(out[i], l.firstK, sb[i])
		}
		c.release(sb)
	case l.hasSecondK:
		fb := c.acquire(len(out))
		l.first.SampleVolume(c, fb, v)
		for i := range out {
			out[i] = l.pick(out[i], fb[i], l.secondK)
		}
		c.release(fb)
	default:
		fb := c.acquire(len(out))
		l.first.SampleVolume(c, fb, v)
		sb := c.acquire(len(out))
		l.second.SampleVolume(c, sb, v)
		for i := range out {
			out[i] = l.pick(out[i], fb[i], sb[i])
		}
		c.release(sb)
		c.release(fb)
	}
}

// rangeChoice picks between two samplers by whether the input is in a range.
// vanilla: RangeChoiceFunction.Sampler, ConstSampler
type rangeChoice struct {
	input                       Sampler
	min, max                    float32
	inRange, outOfRange         Sampler
	inRangeK, outOfRangeK       float32
	hasInRangeK, hasOutOfRangeK bool
}

// RangeChoice is vanilla: DensityFunctions.rangeChoice.
func RangeChoice(input Sampler, minInclusive, maxExclusive float32, whenInRange, whenOutOfRange Sampler) Sampler {
	r := &rangeChoice{input: input, min: minInclusive, max: maxExclusive, inRange: whenInRange, outOfRange: whenOutOfRange}
	r.inRangeK, r.hasInRangeK = constOf(whenInRange)
	r.outOfRangeK, r.hasOutOfRangeK = constOf(whenOutOfRange)
	return r
}

func (r *rangeChoice) in(v float32) bool { return v >= r.min && v < r.max }

func (r *rangeChoice) Sample(c *Context, x, y, z int32) float32 {
	v := r.input.Sample(c, x, y, z)
	if r.hasInRangeK && r.hasOutOfRangeK {
		if r.in(v) {
			return r.inRangeK
		}
		return r.outOfRangeK
	}
	if r.in(v) {
		return r.inRange.Sample(c, x, y, z)
	}
	return r.outOfRange.Sample(c, x, y, z)
}

func (r *rangeChoice) SampleVolume(c *Context, out []float32, v Volume) {
	if len(out) == 0 {
		return
	}
	if r.hasInRangeK && r.hasOutOfRangeK {
		r.input.SampleVolume(c, out, v)
		for i := range out {
			if r.in(out[i]) {
				out[i] = r.inRangeK
			} else {
				out[i] = r.outOfRangeK
			}
		}
		return
	}
	selector := c.acquire(len(out))
	r.input.SampleVolume(c, selector, v)
	firstInRange := r.in(selector[0])
	uniform := true
	for _, value := range selector[1:] {
		if r.in(value) != firstInRange {
			uniform = false
			break
		}
	}
	if uniform {
		c.release(selector)
		if firstInRange {
			r.inRange.SampleVolume(c, out, v)
		} else {
			r.outOfRange.SampleVolume(c, out, v)
		}
		return
	}

	switch {
	case r.hasInRangeK:
		r.outOfRange.SampleVolume(c, out, v)
		for i := range out {
			if r.in(selector[i]) {
				out[i] = r.inRangeK
			}
		}
	case r.hasOutOfRangeK:
		r.inRange.SampleVolume(c, out, v)
		for i := range out {
			if !r.in(selector[i]) {
				out[i] = r.outOfRangeK
			}
		}
	default:
		r.inRange.SampleVolume(c, out, v)
		outside := c.acquire(len(out))
		r.outOfRange.SampleVolume(c, outside, v)
		for i := range out {
			if !r.in(selector[i]) {
				out[i] = outside[i]
			}
		}
		c.release(outside)
	}
	c.release(selector)
}

// intervalSelect picks one of several samplers by the input's interval.
// vanilla: IntervalSelectFunction.Sampler, SingleThresholdSampler
type intervalSelect struct {
	input      Sampler
	thresholds []float32
	samplers   []Sampler
}

// IntervalSelect is vanilla: DensityFunctions.intervalSelect.
func IntervalSelect(input Sampler, thresholds []float32, samplers []Sampler) Sampler {
	return &intervalSelect{input, thresholds, samplers}
}

func (s *intervalSelect) index(v float32) int {
	for i, t := range s.thresholds {
		if v < t {
			return i
		}
	}
	return len(s.samplers) - 1
}

func (s *intervalSelect) Sample(c *Context, x, y, z int32) float32 {
	return s.samplers[s.index(s.input.Sample(c, x, y, z))].Sample(c, x, y, z)
}

func (s *intervalSelect) SampleVolume(c *Context, out []float32, v Volume) {
	s.input.SampleVolume(c, out, v)
	bufs := make([][]float32, len(s.samplers))
	for i, sm := range s.samplers {
		bufs[i] = c.acquire(len(out))
		sm.SampleVolume(c, bufs[i], v)
	}
	for i := range out {
		out[i] = bufs[s.index(out[i])][i]
	}
	for _, buf := range slices.Backward(bufs) {
		c.release(buf)
	}
}
