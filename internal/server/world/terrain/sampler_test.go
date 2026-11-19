package terrain

// columnSampler depends on x and z only and records the volumes it samples.
type columnSampler struct{ volumes []Volume }

func (s *columnSampler) Sample(_ *Context, x, _, z int32) float32 { return float32(x*31 + z) }

func (s *columnSampler) SampleVolume(c *Context, out []float32, v Volume) {
	s.volumes = append(s.volumes, v)
	sampleVolumeNaive(c, out, v, s)
}
