package levelgen

import "unicode/utf16"

// LegacyRandomSource is java.util.Random's 48-bit LCG. vanilla: LegacyRandomSource
type LegacyRandomSource struct {
	seed  int64
	gauss gaussian
}

// NewLegacyRandom seeds a stream as new java.util.Random(seed) would.
func NewLegacyRandom(seed int64) *LegacyRandomSource {
	r := &LegacyRandomSource{}
	r.SetSeed(seed)
	return r
}

// SetSeed scrambles the seed and drops a cached gaussian.
func (r *LegacyRandomSource) SetSeed(seed int64) {
	r.seed = (seed ^ 0x5DEECE66D) & 0xFFFFFFFFFFFF
	r.gauss.have = false
}

// SetDecorationSeed reseeds the stream for a chunk's decoration from the
// world seed and the block coordinates of the chunk's corner, and returns the
// seed it settled on. vanilla: WorldgenRandom.setDecorationSeed over a
// LegacyRandomSource
func (r *LegacyRandomSource) SetDecorationSeed(seed int64, blockX, blockZ int32) int64 {
	r.SetSeed(seed)
	xScale := r.NextLong() | 1
	zScale := r.NextLong() | 1
	result := int64(blockX)*xScale + int64(blockZ)*zScale ^ seed
	r.SetSeed(result)
	return result
}

// Next returns the top n of the 48 state bits as a signed int.
// vanilla: LegacyRandomSource.next
func (r *LegacyRandomSource) Next(n uint) int32 {
	r.seed = (r.seed*25214903917 + 11) & 0xFFFFFFFFFFFF
	return int32(r.seed >> (48 - n))
}

// Fork seeds the new stream from NextLong.
func (r *LegacyRandomSource) Fork() RandomSource { return NewLegacyRandom(r.NextLong()) }

// ForkPositional seeds the factory from NextLong.
func (r *LegacyRandomSource) ForkPositional() PositionalRandomFactory {
	return LegacyPositionalRandomFactory{r.NextLong()}
}

// NextInt is vanilla: BitRandomSource.nextInt.
func (r *LegacyRandomSource) NextInt() int32 { return r.Next(32) }

// NextIntN is java.util.Random.nextInt(bound), int overflow included.
// vanilla: BitRandomSource.nextInt
func (r *LegacyRandomSource) NextIntN(bound int32) int32 {
	if bound <= 0 {
		panic("levelgen: bound must be positive")
	}
	if bound&(bound-1) == 0 {
		return int32(int64(bound) * int64(r.Next(31)) >> 31)
	}
	for {
		sample := r.Next(31)
		modulo := sample % bound
		if sample-modulo+(bound-1) >= 0 {
			return modulo
		}
	}
}

// NextLong joins two 32-bit draws; the low one is sign-extended before the add.
func (r *LegacyRandomSource) NextLong() int64 {
	upper := int64(r.Next(32))
	return upper<<32 + int64(r.Next(32))
}

// NextBoolean is vanilla: BitRandomSource.nextBoolean.
func (r *LegacyRandomSource) NextBoolean() bool { return r.Next(1) != 0 }

// NextFloat is vanilla: BitRandomSource.nextFloat.
func (r *LegacyRandomSource) NextFloat() float32 { return float32(r.Next(24)) * floatUnit }

// NextDouble joins a 26-bit and a 27-bit draw. vanilla: BitRandomSource.nextDouble
func (r *LegacyRandomSource) NextDouble() float64 {
	upper := int64(r.Next(26))
	return float64(upper<<27+int64(r.Next(27))) * doubleUnit
}

// NextGaussian is vanilla: MarsagliaPolarGaussian.nextGaussian.
func (r *LegacyRandomSource) NextGaussian() float64 { return r.gauss.nextGaussian(r) }

// ConsumeCount discards n ints. vanilla: RandomSource.consumeCount
func (r *LegacyRandomSource) ConsumeCount(n int) {
	for range n {
		r.Next(32)
	}
}

// SetLargeFeatureSeed seeds the stream for the carvers or large features a
// chunk starts. A WorldgenRandom over a LegacyRandomSource draws exactly as
// the legacy source does, so the carvers use this source directly.
// vanilla: WorldgenRandom.setLargeFeatureSeed
func (r *LegacyRandomSource) SetLargeFeatureSeed(seed int64, chunkX, chunkZ int32) {
	r.SetSeed(seed)
	xScale := r.NextLong()
	zScale := r.NextLong()
	r.SetSeed(int64(chunkX)*xScale ^ int64(chunkZ)*zScale ^ seed)
}

// LegacyPositionalRandomFactory is vanilla: LegacyRandomSource.LegacyPositionalRandomFactory.
type LegacyPositionalRandomFactory struct{ Seed int64 }

// At xors the position's seed into the factory's.
func (f LegacyPositionalRandomFactory) At(x, y, z int32) RandomSource {
	return NewLegacyRandom(PositionSeed(x, y, z) ^ f.Seed)
}

// SeedAt resets r in place to At(x, y, z).
func (f LegacyPositionalRandomFactory) SeedAt(r RandomSource, x, y, z int32) {
	r.(*LegacyRandomSource).SetSeed(PositionSeed(x, y, z) ^ f.Seed)
}

// FromHashOf xors Java's String.hashCode of the name, sign-extended.
func (f LegacyPositionalRandomFactory) FromHashOf(name string) RandomSource {
	return NewLegacyRandom(int64(javaStringHash(name)) ^ f.Seed)
}

// FromSeed ignores the factory's own seed.
func (f LegacyPositionalRandomFactory) FromSeed(seed int64) RandomSource {
	return NewLegacyRandom(seed)
}

// javaStringHash is String.hashCode over the UTF-16 code units.
func javaStringHash(s string) int32 {
	var h int32
	for _, c := range utf16.Encode([]rune(s)) {
		h = 31*h + int32(c)
	}
	return h
}
