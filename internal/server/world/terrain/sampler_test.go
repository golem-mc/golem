package terrain

import "testing"

// columnSampler depends on x and z only and records the volumes it samples.
type columnSampler struct{ volumes []Volume }

func (s *columnSampler) Sample(_ *Context, x, _, z int32) float32 { return float32(x*31 + z) }

func (s *columnSampler) SampleVolume(c *Context, out []float32, v Volume) {
	s.volumes = append(s.volumes, v)
	sampleVolumeNaive(c, out, v, s)
}

// TestSliceColumnInput checks that an explicit slice samples an input that does not
// depend on y once per column, and that the values stay those of a block by
// block walk.
func TestSliceColumnInput(t *testing.T) {
	col := &columnSampler{}
	sum := Add(Slice(AxisY, 0, col), YClampedGradient(0, 8, 0, 1))
	v := NewVolume(4, 8, 4, 16, 0, -32)
	out := make([]float32, v.Size())
	sum.SampleVolume(NewContext(nil), out, v)
	want := make([]float32, v.Size())
	sampleVolumeNaive(NewUncachedContext(), want, v, sum)
	for i := range out {
		if out[i] != want[i] {
			t.Fatalf("value %d is %v, want %v", i, out[i], want[i])
		}
	}
	for _, sv := range col.volumes {
		if sv.SizeY != 1 || sv.MinY != 0 {
			t.Errorf("the column input sampled %+v, want one y at 0", sv)
		}
	}
}
