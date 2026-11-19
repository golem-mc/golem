package entity

import "math/rand"

// Dice is the random a spawn rule rolls: a levelgen.RandomSource during
// chunk generation, GoDice elsewhere.
type Dice interface {
	NextFloat() float32
	// NextIntN returns a value in [0, n); n must be positive.
	NextIntN(n int32) int32
	NextDouble() float64
	NextBoolean() bool
}

// GoDice reads a math/rand stream as Dice, one Go draw per roll.
type GoDice struct{ *rand.Rand }

// NextFloat is rand.Float32.
func (d GoDice) NextFloat() float32 { return d.Float32() }

// NextIntN is rand.Intn.
func (d GoDice) NextIntN(n int32) int32 { return int32(d.Intn(int(n))) }

// NextDouble is rand.Float64.
func (d GoDice) NextDouble() float64 { return d.Float64() }

// NextBoolean is rand.Intn(2) == 0.
func (d GoDice) NextBoolean() bool { return d.Intn(2) == 0 }
