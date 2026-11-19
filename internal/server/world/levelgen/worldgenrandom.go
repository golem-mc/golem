package levelgen

// WorldgenRandom is the feature-placement stream: a Xoroshiro source read
// through java.util.Random's bit interface, so every draw takes the top bits
// of a whole 64-bit step and NextLong spends two of them.
// vanilla: WorldgenRandom, BitRandomSource
type WorldgenRandom struct {
	source *XoroshiroRandomSource
	gauss  gaussian
}

// NewWorldgenRandom returns a stream over a Xoroshiro source seeded with seed;
// every use reseeds it before drawing.
func NewWorldgenRandom(seed int64) *WorldgenRandom {
	return &WorldgenRandom{source: NewXoroshiroRandom(seed)}
}

// next returns the top bits of the source's next long as a signed int.
// vanilla: WorldgenRandom.next
func (r *WorldgenRandom) next(bits uint) int32 {
	return int32(uint64(r.source.NextLong()) >> (64 - bits))
}

// SetDecorationSeed seeds the stream for a chunk's decoration from the world
// seed and the block coordinates of the chunk's corner, and returns the seed
// it settled on. vanilla: WorldgenRandom.setDecorationSeed
func (r *WorldgenRandom) SetDecorationSeed(seed int64, blockX, blockZ int32) int64 {
	r.SetSeed(seed)
	xScale := r.NextLong() | 1
	zScale := r.NextLong() | 1
	result := int64(blockX)*xScale + int64(blockZ)*zScale ^ seed
	r.SetSeed(result)
	return result
}

// SetFeatureSeed seeds the stream for the feature at index of a decoration
// step. vanilla: WorldgenRandom.setFeatureSeed
func (r *WorldgenRandom) SetFeatureSeed(decorationSeed int64, index, step int32) {
	r.SetSeed(decorationSeed + int64(index) + int64(10000*step))
}

// Fork forks the wrapped source. vanilla: WorldgenRandom.fork
func (r *WorldgenRandom) Fork() RandomSource { return r.source.Fork() }

// ForkPositional forks the wrapped source. vanilla: WorldgenRandom.forkPositional
func (r *WorldgenRandom) ForkPositional() PositionalRandomFactory { return r.source.ForkPositional() }

// SetSeed reseeds the wrapped source. Unlike LegacyRandomSource.setSeed it
// keeps a cached gaussian: WorldgenRandom overrides setSeed without calling
// super. vanilla: WorldgenRandom.setSeed
func (r *WorldgenRandom) SetSeed(seed int64) {
	r.source.SetSeed(seed)
}

// NextInt is vanilla: BitRandomSource.nextInt.
func (r *WorldgenRandom) NextInt() int32 { return r.next(32) }

// NextIntN is java.util.Random.nextInt(bound). vanilla: BitRandomSource.nextInt
func (r *WorldgenRandom) NextIntN(bound int32) int32 {
	if bound <= 0 {
		panic("levelgen: bound must be positive")
	}
	if bound&(bound-1) == 0 {
		return int32(int64(bound) * int64(r.next(31)) >> 31)
	}
	for {
		sample := r.next(31)
		modulo := sample % bound
		if sample-modulo+(bound-1) >= 0 {
			return modulo
		}
	}
}

// NextLong joins two 32-bit draws. vanilla: BitRandomSource.nextLong
func (r *WorldgenRandom) NextLong() int64 {
	upper := int64(r.next(32))
	return upper<<32 + int64(r.next(32))
}

// NextBoolean is vanilla: BitRandomSource.nextBoolean.
func (r *WorldgenRandom) NextBoolean() bool { return r.next(1) != 0 }

// NextFloat is vanilla: BitRandomSource.nextFloat.
func (r *WorldgenRandom) NextFloat() float32 { return float32(r.next(24)) * floatUnit }

// NextDouble joins a 26-bit and a 27-bit draw. vanilla: BitRandomSource.nextDouble
func (r *WorldgenRandom) NextDouble() float64 {
	upper := int64(r.next(26))
	return float64(upper<<27+int64(r.next(27))) * doubleUnit
}

// NextGaussian is vanilla: MarsagliaPolarGaussian.nextGaussian.
func (r *WorldgenRandom) NextGaussian() float64 { return r.gauss.nextGaussian(r) }

// ConsumeCount discards n ints. vanilla: RandomSource.consumeCount
func (r *WorldgenRandom) ConsumeCount(n int) {
	for range n {
		r.next(32)
	}
}
