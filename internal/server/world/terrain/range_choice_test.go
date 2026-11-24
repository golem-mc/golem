package terrain

import (
	"math"
	"testing"
)

type rangeChoiceCountingSampler struct {
	t      *testing.T
	values []float32
	calls  int
}

func (s *rangeChoiceCountingSampler) Sample(*Context, int32, int32, int32) float32 {
	s.t.Fatal("range choice used a scalar sample in a volume")
	return 0
}

func (s *rangeChoiceCountingSampler) SampleVolume(_ *Context, out []float32, _ Volume) {
	s.calls++
	copy(out, s.values)
}

func TestRangeChoiceVolumeBranchSelection(t *testing.T) {
	const (
		lower, upper = 1, 2
		negativeZero = 0x80000000
		nanPayload   = 0x7fc01234
		one          = 0x3f800000
		minusOne     = 0xbf800000
	)
	nan := math.Float32frombits(nanPayload)
	negZero := math.Float32frombits(negativeZero)
	mixed := []float32{lower, upper, nan, 0, 1.5}
	for _, test := range []struct {
		name                  string
		selector              []float32
		inLiteral, outLiteral bool
		inCalls, outCalls     int
		want                  []uint32
	}{
		{
			name: "uniform in", selector: []float32{lower, 1.5},
			inCalls: 1, want: []uint32{negativeZero, one},
		},
		{
			name: "uniform out", selector: []float32{upper, nan, 0},
			outCalls: 1, want: []uint32{minusOne, negativeZero, nanPayload},
		},
		{
			name: "uniform literal", selector: []float32{nan, upper}, outLiteral: true,
			want: []uint32{nanPayload, nanPayload},
		},
		{
			name: "mixed dynamic", selector: mixed,
			inCalls: 1, outCalls: 1, want: []uint32{negativeZero, negativeZero, nanPayload, one, nanPayload},
		},
		{
			name: "mixed literal in", selector: mixed, inLiteral: true,
			outCalls: 1, want: []uint32{negativeZero, negativeZero, nanPayload, one, negativeZero},
		},
		{
			name: "mixed literal out", selector: []float32{upper, lower, nan, 0, 1.5}, outLiteral: true,
			inCalls: 1, want: []uint32{nanPayload, one, nanPayload, nanPayload, nanPayload},
		},
		{
			name: "mixed literals", selector: mixed, inLiteral: true, outLiteral: true,
			want: []uint32{negativeZero, nanPayload, nanPayload, nanPayload, negativeZero},
		},
		{name: "empty"},
		{name: "empty with literals", inLiteral: true, outLiteral: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := &rangeChoiceCountingSampler{t: t, values: test.selector}
			inside := &rangeChoiceCountingSampler{t: t, values: []float32{negZero, 1, 1, 1, nan}}
			outside := &rangeChoiceCountingSampler{t: t, values: []float32{-1, negZero, nan, 1, -1}}
			var inRange, outOfRange Sampler = inside, outside
			if test.inLiteral {
				inRange = constant(negZero)
			}
			if test.outLiteral {
				outOfRange = constant(nan)
			}
			choice := RangeChoice(input, lower, upper, inRange, outOfRange)
			volume := NewVolume(1, int32(len(test.selector)), 1, 0, 0, 0)
			out := make([]float32, volume.Size())
			choice.SampleVolume(NewUncachedContext(), out, volume)

			wantInputCalls := 1
			if len(out) == 0 {
				wantInputCalls = 0
			}
			if input.calls != wantInputCalls || inside.calls != test.inCalls || outside.calls != test.outCalls {
				t.Errorf("volume calls (selector, in, out) = (%d, %d, %d), want (%d, %d, %d)",
					input.calls, inside.calls, outside.calls, wantInputCalls, test.inCalls, test.outCalls)
			}
			for i, want := range test.want {
				if got := math.Float32bits(out[i]); got != want {
					t.Errorf("output[%d] bits = %08x, want %08x", i, got, want)
				}
			}
		})
	}
}
