package levelgen

import (
	"crypto/md5"
	"encoding/binary"
	"math"
	"math/bits"
)

// RandomSource is a vanilla random stream. vanilla: RandomSource
type RandomSource interface {
	// Fork returns an independent stream seeded from this one.
	Fork() RandomSource
	// ForkPositional returns a factory of per-position streams seeded from this one.
	ForkPositional() PositionalRandomFactory
	SetSeed(seed int64)
	NextInt() int32
	// NextIntN returns a value in [0, bound); bound must be positive.
	NextIntN(bound int32) int32
	NextLong() int64
	NextBoolean() bool
	NextFloat() float32
	NextDouble() float64
	NextGaussian() float64
	// ConsumeCount discards n draws.
	ConsumeCount(n int)
}

// PositionalRandomFactory derives streams from a position, a name or a seed.
// vanilla: PositionalRandomFactory
type PositionalRandomFactory interface {
	At(x, y, z int32) RandomSource
	// SeedAt resets r, a stream At returned, to exactly the stream At(x, y, z) returns.
	SeedAt(r RandomSource, x, y, z int32)
	FromHashOf(name string) RandomSource
	FromSeed(seed int64) RandomSource
}

// The constants of vanilla: RandomSupport.
const (
	goldenRatio64 = -7046029254386353131
	silverRatio64 = 7640891576956012809
)

// floatUnit and doubleUnit scale random bits into [0, 1); doubleUnit is
// vanilla's float literal 1.110223E-16f widened to double.
var (
	floatUnit  = float32(5.9604645e-8)
	doubleUnit = float64(float32(1.110223e-16))
)

// Seed128 is a 128-bit seed. vanilla: RandomSupport.Seed128bit
type Seed128 struct{ Lo, Hi int64 }

// mixStafford13 is vanilla: RandomSupport.mixStafford13.
func mixStafford13(z int64) int64 {
	u := uint64(z)
	u = (u ^ u>>30) * 0xBF58476D1CE4E5B9
	u = (u ^ u>>27) * 0x94D049BB133111EB
	return int64(u ^ u>>31)
}

// UpgradeSeedTo128bit expands a world seed into a xoroshiro seed. vanilla: RandomSupport.upgradeSeedTo128bit
func UpgradeSeedTo128bit(seed int64) Seed128 {
	lo := seed ^ 0x6A09E667F3BCC909
	hi := lo + goldenRatio64
	return Seed128{mixStafford13(lo), mixStafford13(hi)}
}

// SeedFromHashOf is the big-endian halves of the MD5 of the UTF-8 name.
// vanilla: RandomSupport.seedFromHashOf
func SeedFromHashOf(name string) Seed128 {
	h := md5.Sum([]byte(name))
	return Seed128{int64(binary.BigEndian.Uint64(h[:8])), int64(binary.BigEndian.Uint64(h[8:]))}
}

// Xoroshiro128PlusPlus is vanilla: Xoroshiro128PlusPlus.
type Xoroshiro128PlusPlus struct{ lo, hi uint64 }

// NewXoroshiro128PlusPlus seeds the generator; an all-zero seed, which would
// only ever yield zeros, is replaced by the golden and silver ratios.
func NewXoroshiro128PlusPlus(lo, hi int64) Xoroshiro128PlusPlus {
	if lo|hi == 0 {
		lo, hi = goldenRatio64, silverRatio64
	}
	return Xoroshiro128PlusPlus{uint64(lo), uint64(hi)}
}

// NextLong advances the generator.
func (x *Xoroshiro128PlusPlus) NextLong() int64 {
	s0, s1 := x.lo, x.hi
	result := bits.RotateLeft64(s0+s1, 17) + s0
	s1 ^= s0
	x.lo = bits.RotateLeft64(s0, 49) ^ s1 ^ s1<<21
	x.hi = bits.RotateLeft64(s1, 28)
	return int64(result)
}

// gaussian is the Marsaglia polar method's cached second value.
// vanilla: MarsagliaPolarGaussian
type gaussian struct {
	next float64
	have bool
}

func (g *gaussian) nextGaussian(r RandomSource) float64 {
	if g.have {
		g.have = false
		return g.next
	}
	var x, y, rr float64
	for {
		x = float64(2.0*r.NextDouble()) - 1.0
		y = float64(2.0*r.NextDouble()) - 1.0
		rr = float64(x*x) + float64(y*y)
		if rr < 1.0 && rr != 0.0 {
			break
		}
	}
	// Java's Math.log is specified only to 1 ulp, and HotSpot's x86 intrinsic
	// and Go's math.Log round a few inputs differently, so a gaussian can
	// differ from the reference server in its last bit. World generation never
	// draws one.
	m := math.Sqrt(-2.0 * math.Log(rr) / rr)
	g.next = y * m
	g.have = true
	return x * m
}

// XoroshiroRandomSource is vanilla: XoroshiroRandomSource.
type XoroshiroRandomSource struct {
	gen   Xoroshiro128PlusPlus
	gauss gaussian
}

// NewXoroshiroRandom seeds a stream from a 64-bit seed via UpgradeSeedTo128bit.
func NewXoroshiroRandom(seed int64) *XoroshiroRandomSource {
	s := UpgradeSeedTo128bit(seed)
	return NewXoroshiroRandom128(s.Lo, s.Hi)
}

// NewXoroshiroRandom128 seeds a stream from both halves directly.
func NewXoroshiroRandom128(lo, hi int64) *XoroshiroRandomSource {
	return &XoroshiroRandomSource{gen: NewXoroshiro128PlusPlus(lo, hi)}
}

// Fork draws the new stream's two seed halves.
func (r *XoroshiroRandomSource) Fork() RandomSource {
	lo := r.gen.NextLong()
	return NewXoroshiroRandom128(lo, r.gen.NextLong())
}

// ForkPositional draws the factory's two seed halves.
func (r *XoroshiroRandomSource) ForkPositional() PositionalRandomFactory {
	lo := r.gen.NextLong()
	return XoroshiroPositionalRandomFactory{lo, r.gen.NextLong()}
}

// SetSeed reseeds and drops a cached gaussian.
func (r *XoroshiroRandomSource) SetSeed(seed int64) {
	s := UpgradeSeedTo128bit(seed)
	r.gen = NewXoroshiro128PlusPlus(s.Lo, s.Hi)
	r.gauss.have = false
}

// NextInt is the low 32 bits of the next long.
func (r *XoroshiroRandomSource) NextInt() int32 { return int32(r.gen.NextLong()) }

// NextIntN is Lemire's multiply-shift with rejection of the biased buckets.
func (r *XoroshiroRandomSource) NextIntN(bound int32) int32 {
	if bound <= 0 {
		panic("levelgen: bound must be positive")
	}
	b := uint64(uint32(bound))
	m := uint64(uint32(r.NextInt())) * b
	frac := m & 0xFFFFFFFF
	if frac < b {
		threshold := uint64(uint32(-bound) % uint32(bound))
		for frac < threshold {
			m = uint64(uint32(r.NextInt())) * b
			frac = m & 0xFFFFFFFF
		}
	}
	return int32(m >> 32)
}

// NextLong is vanilla: XoroshiroRandomSource.nextLong.
func (r *XoroshiroRandomSource) NextLong() int64 { return r.gen.NextLong() }

// NextBoolean is the lowest bit of the next long.
func (r *XoroshiroRandomSource) NextBoolean() bool { return r.gen.NextLong()&1 != 0 }

// NextFloat is the top 24 bits scaled into [0, 1).
func (r *XoroshiroRandomSource) NextFloat() float32 {
	return float32(r.nextBits(24)) * floatUnit
}

// NextDouble is the top 53 bits scaled into [0, 1).
func (r *XoroshiroRandomSource) NextDouble() float64 {
	return float64(r.nextBits(53)) * doubleUnit
}

// NextGaussian is vanilla: MarsagliaPolarGaussian.nextGaussian.
func (r *XoroshiroRandomSource) NextGaussian() float64 { return r.gauss.nextGaussian(r) }

// ConsumeCount discards n longs.
func (r *XoroshiroRandomSource) ConsumeCount(n int) {
	for range n {
		r.gen.NextLong()
	}
}

func (r *XoroshiroRandomSource) nextBits(n uint) uint64 {
	return uint64(r.gen.NextLong()) >> (64 - n)
}

// XoroshiroPositionalRandomFactory is vanilla: XoroshiroRandomSource.XoroshiroPositionalRandomFactory.
type XoroshiroPositionalRandomFactory struct{ Lo, Hi int64 }

// At xors the position's seed into the low half.
func (f XoroshiroPositionalRandomFactory) At(x, y, z int32) RandomSource {
	return NewXoroshiroRandom128(PositionSeed(x, y, z)^f.Lo, f.Hi)
}

// SeedAt resets r in place to At(x, y, z), cached gaussian included.
func (f XoroshiroPositionalRandomFactory) SeedAt(r RandomSource, x, y, z int32) {
	*r.(*XoroshiroRandomSource) = XoroshiroRandomSource{gen: NewXoroshiro128PlusPlus(PositionSeed(x, y, z)^f.Lo, f.Hi)}
}

// NextFloatAt is At(x, y, z).NextFloat() without allocating the stream.
func (f XoroshiroPositionalRandomFactory) NextFloatAt(x, y, z int32) float32 {
	g := NewXoroshiro128PlusPlus(PositionSeed(x, y, z)^f.Lo, f.Hi)
	return float32(uint64(g.NextLong())>>(64-24)) * floatUnit
}

// FromHashOf xors SeedFromHashOf(name) into both halves.
func (f XoroshiroPositionalRandomFactory) FromHashOf(name string) RandomSource {
	s := SeedFromHashOf(name)
	return NewXoroshiroRandom128(s.Lo^f.Lo, s.Hi^f.Hi)
}

// FromSeed xors the seed into both halves.
func (f XoroshiroPositionalRandomFactory) FromSeed(seed int64) RandomSource {
	return NewXoroshiroRandom128(seed^f.Lo, seed^f.Hi)
}
