package terrain

import "math"

type mapKind uint8

const (
	mapAbs mapKind = mapKind(opMax) + 1 + iota
	mapSquare
	mapCube
	mapNegate
	mapNegative
	mapSqueeze
	mapClamp
)

type mapOp struct {
	kind  mapKind
	k, hi float32
}

// mappedSampler compiles a chain without moving operations across a cache,
// interpolation or branch. Each operation still rounds separately to float32.
// vanilla: UnaryFunction.*Sampler, BinaryFunction.Const*Sampler
type mappedSampler struct {
	in  Sampler
	ops []mapOp
}

// foldedConstant keeps expression values out of literal-only sampler dispatch.
// vanilla: BinaryFunction
type foldedConstant struct{ constant }

func mapSampler(in Sampler, op mapOp) Sampler {
	k, ok := constOf(in)
	if folded, isFolded := in.(foldedConstant); isFolded {
		k, ok = float32(folded.constant), true
	}
	if ok {
		value := op.apply(k)
		volume := [1]float32{k}
		op.applyVolume(volume[:])
		// Scalar Math.min/max and volume comparisons differ for NaN and -0.
		if math.Float32bits(value) == math.Float32bits(volume[0]) {
			return foldedConstant{constant(value)}
		}
	}
	if previous, ok := in.(*mappedSampler); ok {
		ops := make([]mapOp, len(previous.ops)+1)
		copy(ops, previous.ops)
		ops[len(previous.ops)] = op
		return &mappedSampler{previous.in, ops}
	}
	return &mappedSampler{in, []mapOp{op}}
}

func (s *mappedSampler) Sample(c *Context, x, y, z int32) float32 {
	v := s.in.Sample(c, x, y, z)
	for _, op := range s.ops {
		v = op.apply(v)
	}
	return v
}

func (s *mappedSampler) SampleVolume(c *Context, out []float32, v Volume) {
	s.in.SampleVolume(c, out, v)
	for _, op := range s.ops {
		op.applyVolume(out)
	}
}

func (op mapOp) apply(v float32) float32 {
	switch op.kind {
	case mapKind(opAdd):
		return v + op.k
	case mapKind(opSub):
		return op.k - v
	case mapKind(opMul):
		return v * op.k
	case mapKind(opDiv):
		return op.k / v
	case mapKind(opMin):
		return javaMin(v, op.k)
	case mapKind(opMax):
		return javaMax(v, op.k)
	case mapAbs:
		return absF(v)
	case mapSquare:
		return squareF(v)
	case mapCube:
		return cubeF(v)
	case mapNegate:
		return -v
	case mapNegative:
		if v > 0 {
			return v
		}
		return v * op.k
	case mapSqueeze:
		c := clampF(v, -1, 1)
		return float32(c/2.0) - float32(cubeF(c)/24.0)
	case mapClamp:
		return clampF(v, op.k, op.hi)
	}
	panic("terrain: unknown map operation")
}

// applyVolume dispatches once per volume, keeping indirect calls out of the
// value loop. Min/max keep the volume API's comparison semantics.
func (op mapOp) applyVolume(out []float32) {
	switch op.kind {
	case mapKind(opAdd):
		for i := range out {
			out[i] += op.k
		}
	case mapKind(opSub):
		for i := range out {
			out[i] = op.k - out[i]
		}
	case mapKind(opMul):
		for i := range out {
			out[i] *= op.k
		}
	case mapKind(opDiv):
		for i := range out {
			out[i] = op.k / out[i]
		}
	case mapKind(opMin):
		for i, v := range out {
			if op.k < v {
				out[i] = op.k
			}
		}
	case mapKind(opMax):
		for i, v := range out {
			if op.k > v {
				out[i] = op.k
			}
		}
	case mapAbs:
		for i, v := range out {
			out[i] = absF(v)
		}
	case mapSquare:
		for i, v := range out {
			out[i] = squareF(v)
		}
	case mapCube:
		for i, v := range out {
			out[i] = cubeF(v)
		}
	case mapNegate:
		for i, v := range out {
			out[i] = -v
		}
	case mapNegative:
		for i, v := range out {
			if !(v > 0) {
				out[i] = v * op.k
			}
		}
	case mapSqueeze:
		for i, v := range out {
			c := clampF(v, -1, 1)
			out[i] = float32(c/2.0) - float32(cubeF(c)/24.0)
		}
	case mapClamp:
		for i, v := range out {
			out[i] = clampF(v, op.k, op.hi)
		}
	default:
		panic("terrain: unknown map operation")
	}
}
