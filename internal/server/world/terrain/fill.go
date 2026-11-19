package terrain

// SampleDensity samples the final density over the chunk's volume once, as
// the fill does. The buffer belongs to the chunk's context.
// vanilla: NoiseBasedChunkGenerator.doFill
func (n *NoiseChunk) SampleDensity() []float32 {
	buf := n.ctx.acquire(n.volume.Size())
	n.state.Router.FinalDensity.SampleVolume(n.ctx, buf, n.volume)
	return buf
}

// Recycle returns the chunk's scratch buffers for reuse; call it after the last
// sample.
func (n *NoiseChunk) Recycle() { n.ctx.recycle() }

// ReleaseDensity hands a SampleDensity buffer back to the chunk's context.
func (n *NoiseChunk) ReleaseDensity(buf []float32) { n.ctx.release(buf) }

// DoFill walks the chunk's columns in the order of the fill (z, then x, then
// y downward) and reports what the aquifer decides for every block.
// vanilla: NoiseBasedChunkGenerator.doFill
func (n *NoiseChunk) DoFill(density []float32, set func(x, y, z int32, s Substance)) {
	v := n.volume
	for z := range v.SizeZ {
		bz := v.BlockZ(z)
		for x := range v.SizeX {
			bx := v.BlockX(x)
			for y := v.SizeY - 1; y >= 0; y-- {
				by := v.BlockY(y)
				set(bx, by, bz, n.aquifer.ComputeSubstance(bx, by, bz, float64(density[v.Index(x, y, z)])))
			}
		}
	}
}
