package rules

// JavaRandom is java.util.Random (LegacyRandomSource), the 48-bit LCG a
// Level rolls with. The behaviours reproduced exactly (table offers, a
// broken container's spill) are sequences of its calls, so matching vanilla
// needs the same generator; a Go source would look legal but never agree.
//
// Go's int32/int64 wrap and shift as the JVM does, and float32/float64 round
// as IEEE, as the JVM does. Not safe for concurrent use; vanilla's Level
// guards its own too.
type JavaRandom struct {
	seed int64
}

// javaRandomMultiplier and javaRandomIncrement are the generator's own
// constants, and javaRandomMask keeps the state to the 48 bits it lives in.
const (
	javaRandomMultiplier = 0x5DEECE66D
	javaRandomIncrement  = 11
	javaRandomMask       = 1<<48 - 1
)

// NewJavaRandom seeds a fresh source the way the vanilla constructor does:
// the seed is scrambled with the multiplier before the first draw, so two
// sources seeded a call apart do not fall into step.
func NewJavaRandom(seed int64) *JavaRandom {
	r := &JavaRandom{}
	r.SetSeed(seed)
	return r
}

// SetSeed re-seeds the source, scrambling the way java.util.Random and
// LegacyRandomSource.setSeed both do.
func (r *JavaRandom) SetSeed(seed int64) {
	r.seed = (seed ^ javaRandomMultiplier) & javaRandomMask
}

// next draws bits of state: step by multiplier and increment, mask to 48
// bits, take the top bits. Callers ask for at most 32, so the result is
// never negative, which NextIntBound's uniformity relies on.
// vanilla: java.util.Random.next
func (r *JavaRandom) next(bits uint) int32 {
	r.seed = (r.seed*javaRandomMultiplier + javaRandomIncrement) & javaRandomMask
	return int32(r.seed >> (48 - bits))
}

// NextInt returns the next int32 the source draws, the bare nextInt() a
// full-width draw is.
func (r *JavaRandom) NextInt() int32 { return r.next(32) }

// NextIntBound returns [0, bound): one 31-bit draw, scaled for a power of two
// and rejected otherwise, so the state sequence matches vanilla's. int32
// wraps where the JDK's overflow trips its rejection loop.
// vanilla: java.util.Random.nextInt
func (r *JavaRandom) NextIntBound(bound int32) int32 {
	if bound <= 0 {
		// Java throws for a non-positive bound; a caller that asks for
		// one is broken in a way this cannot repair, and no vanilla
		// behaviour this reproduces asks for one.
		panic("rules: JavaRandom.NextIntBound with a non-positive bound")
	}
	m := bound - 1
	if bound&m == 0 {
		// A power of two: the draw scales straight down, no rejection.
		return int32((int64(bound) * int64(r.next(31))) >> 31)
	}
	u := r.next(31)
	v := u % bound
	for u-v+m < 0 {
		u = r.next(31)
		v = u % bound
	}
	return v
}

// NextFloat returns the next float32 in [0, 1), the 24 top bits of a draw
// over 2^24, the way java.util.Random.nextFloat rounds its own.
func (r *JavaRandom) NextFloat() float32 {
	return float32(r.next(24)) / 0x1p24
}

// NextDouble returns the next float64 in [0, 1), built from two draws the
// way java.util.Random.nextDouble builds its own: 26 bits high, 27 low.
func (r *JavaRandom) NextDouble() float64 {
	return float64((int64(r.next(26))<<27)+int64(r.next(27))) * (1.0 / (1 << 53))
}

// Triangle returns mean plus spread times the difference of two draws,
// peaking in the middle; the container spill's velocities use it.
// vanilla: RandomSource.triangle
func (r *JavaRandom) Triangle(mean, spread float64) float64 {
	return mean + spread*(r.NextDouble()-r.NextDouble())
}

// TriangleFloat is the float32 form of Triangle, the one vanilla's own
// float callers run.
func (r *JavaRandom) TriangleFloat(mean, spread float32) float32 {
	return mean + spread*(r.NextFloat()-r.NextFloat())
}

// LootRand is the random source a loot roll draws from.
type LootRand interface {
	Intn(n int) int
	Int31n(n int32) int32
	Float32() float32
}

// SeededLootRandom returns the explicit source for an optional loot seed.
// vanilla: LootContext.Builder.withOptionalRandomSeed
func SeededLootRandom(seed int64) LootRand {
	if seed == 0 {
		return nil
	}
	return NewJavaRandom(seed)
}

// Intn is vanilla: RandomSource.nextInt(bound).
func (r *JavaRandom) Intn(n int) int { return int(r.NextIntBound(int32(n))) }

// Int31n is vanilla: RandomSource.nextInt(bound).
func (r *JavaRandom) Int31n(n int32) int32 { return r.NextIntBound(n) }

// NextBoolean is vanilla: BitRandomSource.nextBoolean, next(1) != 0.
func (r *JavaRandom) NextBoolean() bool { return r.next(1) != 0 }

// vanilla: RandomSource.nextBoolean
func nextBoolean(rng LootRand) bool {
	if bits, ok := rng.(interface{ NextBoolean() bool }); ok {
		return bits.NextBoolean()
	}
	return rng.Intn(2) != 0
}

// lootEnchantRand lets a loot roll's source feed SelectEnchantment, which
// vanilla's enchantItem draws from the context random itself.
type lootEnchantRand struct{ LootRand }

func (r lootEnchantRand) NextIntBound(bound int32) int32 { return r.Int31n(bound) }
func (r lootEnchantRand) NextFloat() float32             { return r.Float32() }

// Float32 is vanilla: RandomSource.nextFloat.
func (r *JavaRandom) Float32() float32 { return r.NextFloat() }
