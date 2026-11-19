package terrain

import (
	"math"
	"testing"
)

type mapTestValues []float32

func (s mapTestValues) Sample(_ *Context, _, y, _ int32) float32 { return s[y] }
func (s mapTestValues) SampleVolume(_ *Context, out []float32, _ Volume) {
	copy(out, s)
}

func TestDensityMapChainRounding(t *testing.T) {
	values := mapTestValues{0, math.Float32frombits(0x80000000), -0.25, 0.25, -1, 1,
		math.SmallestNonzeroFloat32, math.MaxFloat32, float32(math.Inf(-1)), float32(math.Inf(1)), float32(math.NaN())}
	// Deliberately sensitive to reassociation and FMA: the multiply must round
	// before the addition. The following squeeze must retain that result.
	values = append(values, math.Float32frombits(0x3f800001))
	product := Mul(values, constant(math.Float32frombits(0x3f7fffff)))
	chain := Squeeze(Add(product, constant(-1)))
	other := Add(product, constant(7))
	out := make([]float32, len(values))
	chain.SampleVolume(NewUncachedContext(), out, NewVolume(1, int32(len(values)), 1, 0, 0, 0))
	for i, v := range values {
		product := float32(v * math.Float32frombits(0x3f7fffff))
		c := clampF(product-1, -1, 1)
		want := float32(c/2) - float32(float32(float32(c*c)*c)/24)
		for path, got := range map[string]float32{"scalar": chain.Sample(nil, 0, int32(i), 0), "volume": out[i]} {
			if math.Float32bits(got) != math.Float32bits(want) {
				t.Errorf("%s input %08x: got %08x, want %08x", path, math.Float32bits(v), math.Float32bits(got), math.Float32bits(want))
			}
		}
		if got := other.Sample(nil, 0, int32(i), 0); math.Float32bits(got) != math.Float32bits(product+7) {
			t.Errorf("shared prefix changed at input %08x", math.Float32bits(v))
		}
	}
}

func TestDensityMapScalarVolumeEdges(t *testing.T) {
	negativeZero := math.Float32frombits(0x80000000)
	nan := math.Float32frombits(0x7fc00123)
	for _, tc := range []struct {
		name                  string
		build                 func(Sampler) Sampler
		input, scalar, volume float32
	}{
		{"min-zero", func(s Sampler) Sampler { return Min(s, constant(negativeZero)) }, 0, negativeZero, 0},
		{"max-zero", func(s Sampler) Sampler { return Max(s, constant(0)) }, negativeZero, 0, negativeZero},
		{"min-nan", func(s Sampler) Sampler { return Min(s, constant(nan)) }, 1, nan, 1},
		{"max-nan", func(s Sampler) Sampler { return Max(s, constant(nan)) }, 1, nan, 1},
		{"negative-zero", HalfNegative, negativeZero, negativeZero, negativeZero},
		{"division-zero", func(s Sampler) Sampler { return Div(constant(1), s) }, negativeZero, float32(math.Inf(-1)), float32(math.Inf(-1))},
		{"constant-min-zero", func(Sampler) Sampler { return Min(constant(negativeZero), constant(0)) }, 0, negativeZero, 0},
		{"constant-max-zero", func(Sampler) Sampler { return Max(constant(0), constant(negativeZero)) }, 0, 0, negativeZero},
		{"constant-min-nan", func(Sampler) Sampler { return Min(constant(nan), constant(1)) }, 0, nan, 1},
		{"constant-max-nan", func(Sampler) Sampler { return Max(constant(nan), constant(1)) }, 0, nan, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.build(mapTestValues{tc.input})
			out := [1]float32{}
			s.SampleVolume(nil, out[:], NewVolume(1, 1, 1, 0, 0, 0))
			if got := s.Sample(nil, 0, 0, 0); math.Float32bits(got) != math.Float32bits(tc.scalar) {
				t.Errorf("scalar %08x, want %08x", math.Float32bits(got), math.Float32bits(tc.scalar))
			}
			if math.Float32bits(out[0]) != math.Float32bits(tc.volume) {
				t.Errorf("volume %08x, want %08x", math.Float32bits(out[0]), math.Float32bits(tc.volume))
			}
		})
	}
}

func TestDensityMapConstantFolding(t *testing.T) {
	for _, tc := range []struct {
		s    Sampler
		want float32
	}{
		{Add(constant(2), constant(3)), 5}, {Sub(constant(8), constant(3)), 5},
		{Mul(constant(2.5), constant(2)), 5}, {Div(constant(10), constant(2)), 5},
		{Min(constant(5), constant(6)), 5}, {Max(constant(4), constant(5)), 5},
		{Abs(constant(-5)), 5}, {Negate(constant(-5)), 5},
		{Square(constant(2)), 4}, {Cube(constant(2)), 8},
		{HalfNegative(constant(-2)), -1}, {QuarterNegative(constant(-4)), -1},
		{Clamp(constant(8), 0, 5), 5}, {Squeeze(constant(0)), 0},
		{Square(Add(constant(1), constant(2))), 9},
		{Add(Square(constant(2)), constant(1)), 5},
	} {
		if got, ok := tc.s.(foldedConstant); !ok || float32(got.constant) != tc.want {
			t.Errorf("constant expression produced %v (%T), want folded constant %v", tc.s, tc.s, tc.want)
		}
		if _, literal := constOf(tc.s); literal {
			t.Error("folded expression selected literal constant dispatch")
		}
	}
}

func TestDensityMapFoldedDivisor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		divisor Sampler
		want    uint32
	}{
		{"binary", Add(constant(1), constant(2)), 0x40155555},
		{"unary", Abs(constant(-3)), 0x40155555},
		{"chain", Negate(Negate(constant(3))), 0x40155555},
		// A literal denominator selects vanilla's reciprocal multiply sampler.
		{"literal", constant(3), 0x40155556},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Div(YClampedGradient(0, 1, 7, 8), tc.divisor)
			out := [1]float32{}
			s.SampleVolume(NewUncachedContext(), out[:], NewVolume(1, 1, 1, 0, 0, 0))
			for path, got := range map[string]float32{"scalar": s.Sample(nil, 0, 0, 0), "volume": out[0]} {
				if bits := math.Float32bits(got); bits != tc.want {
					t.Errorf("%s: got %08x, want %08x", path, bits, tc.want)
				}
			}
		})
	}
}
