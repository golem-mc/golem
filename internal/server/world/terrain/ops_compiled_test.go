package terrain

import ()

type mapTestValues []float32

func (s mapTestValues) Sample(_ *Context, _, y, _ int32) float32 { return s[y] }
func (s mapTestValues) SampleVolume(_ *Context, out []float32, _ Volume) {
	copy(out, s)
}
