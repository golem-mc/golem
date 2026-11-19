package terrain

// NoiseChunk is the samplers and aquifer of one chunk's terrain fill.
// vanilla: NoiseChunk
type NoiseChunk struct {
	state   *RandomState
	volume  Volume
	ctx     *Context
	aquifer *Aquifer
}

// NewNoiseChunk builds the chunk's caching context and aquifer over a volume.
// vanilla: NoiseChunk.<init>
func NewNoiseChunk(rs *RandomState, b *Beardifier, v Volume) *NoiseChunk {
	pool := 0
	if v.Size() >= 1<<12 {
		pool = 1
	}
	n := &NoiseChunk{state: rs, volume: v, ctx: newContext(b, pool)}
	global := NewFluidPicker(rs.SeaLevel, rs.defaultFluid)
	if rs.Router.Aquifer != nil {
		n.aquifer = NewAquifer(n.ctx, rs.Router.Aquifer, rs.RandomFactory("minecraft:aquifer"), v, global)
	} else {
		n.aquifer = NewDisabledAquifer(global)
	}
	return n
}

// ChunkVolume is the volume of a chunk's fill. vanilla: NoiseBasedChunkGenerator.chunkVolume
func (rs *RandomState) ChunkVolume(chunkX, chunkZ int32) Volume {
	return NewVolume(16, rs.Height, 16, chunkX*16, rs.MinY, chunkZ*16)
}

// Volume is the chunk's sample volume. vanilla: NoiseChunk.volume
func (n *NoiseChunk) Volume() Volume { return n.volume }

// Aquifer is the chunk's aquifer. vanilla: NoiseChunk.aquifer
func (n *NoiseChunk) Aquifer() *Aquifer { return n.aquifer }

// Context is the chunk's caching sampler context. vanilla: NoiseChunk.cachingSamplers
func (n *NoiseChunk) Context() *Context { return n.ctx }
