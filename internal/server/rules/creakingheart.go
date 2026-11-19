package rules

import (
	"math"
	"math/rand"
)

// The creaking heart's link to its creaking: when the heart stands one up,
// keeps or drops it, and what a blow on it sends back.
// vanilla: CreakingHeartBlockEntity

// The heart's numbers.
// vanilla: CreakingHeartBlockEntity
const (
	// CreakingPlayerRange is how near a player must stand for the heart to
	// stand a creaking up, and the distance the comparator scales to.
	CreakingPlayerRange = 32.0
	// CreakingTooFar is the distance past which the heart lets its creaking go.
	CreakingTooFar = 34.0
	// CreakingGraceTicks is how long a link to a creaking nowhere in the world
	// survives.
	CreakingGraceTicks = 30
	// CreakingTrailTicks is the length of the particle trail a hurt creaking
	// starts, and CreakingTrailStream the count it must still have left for the
	// stream to run.
	CreakingTrailTicks  = 100
	CreakingTrailStream = 50
	// CreakingHurtParticles is the burst of the blow itself.
	CreakingHurtParticles = 20

	// The trail's colours: orange toward the creaking, gray toward the heart.
	CreakingTrailOrange int32 = 16545810
	CreakingTrailGray   int32 = 0x5F5F5F

	creakingTrailBase     = 10
	creakingTrailVariance = 40
)

// CreakingHeartOutput is the comparator strength of a heart whose creaking
// stands distance blocks from the bottom centre of its block: 15 beside it,
// 0 from 32 blocks on.
// vanilla: CreakingHeartBlockEntity.computeAnalogOutputSignal
func CreakingHeartOutput(distance float64) int {
	return 15 - int(math.Floor(math.Min(math.Max(distance, 0), CreakingPlayerRange)/CreakingPlayerRange*15))
}

// CreakingProtectorKept reports whether the heart keeps its creaking at one
// of its checks: it must have its night (or be set not to despawn), stay
// within CreakingTooFar and have no player's eyes inside it.
// vanilla: CreakingHeartBlockEntity.serverTick
func CreakingProtectorKept(nightActive, persistent bool, distance float64, stuck bool) bool {
	return (nightActive || persistent) && distance <= CreakingTooFar && !stuck
}

// CreakingMaySpawn reports whether an awake heart without a creaking stands
// one up when a player is near: monsters spawn in the world and it is not
// peaceful.
// vanilla: CreakingHeartBlockEntity.serverTick
func CreakingMaySpawn(awake, spawningMonsters, peaceful bool) bool {
	return awake && spawningMonsters && !peaceful
}

// CreakingTrailPoint is one trail particle: it starts at (X, Y, Z), runs to
// (ToX, ToY, ToZ) and lasts Ticks.
type CreakingTrailPoint struct {
	X, Y, Z, ToX, ToY, ToZ float64
	Ticks                  int32
}

// CreakingTrail picks a trail particle between a point in the creaking's box
// (min and max corners) and a point in the heart's block, running toward the
// creaking when towardsCreaking and toward the heart otherwise.
// vanilla: CreakingHeartBlockEntity.emitParticles
func CreakingTrail(rng *rand.Rand, boxMin, boxMax, heart [3]float64, towardsCreaking bool) CreakingTrailPoint {
	var from, to [3]float64
	for i := range 3 {
		from[i] = boxMin[i] + rng.Float64()*(boxMax[i]-boxMin[i])
	}
	for i := range 3 {
		to[i] = heart[i] + rng.Float64()
	}
	ticks := int32(rng.Intn(creakingTrailVariance) + creakingTrailBase)
	if towardsCreaking {
		from, to = to, from
	}
	return CreakingTrailPoint{from[0], from[1], from[2], to[0], to[1], to[2], ticks}
}

// CreakingHurtSound is where and how loud the heart's hurt sound plays while
// the trail has emitter ticks left: a block cell on the line from the
// creaking's centre to the heart's, nearer the heart as the trail ages.
// vanilla: CreakingHeartBlockEntity.serverTick
func CreakingHurtSound(emitter int32, target, heart [3]float64) (x, y, z int32, volume float32) {
	progress := 0.2 + 0.8*float64(CreakingTrailTicks-emitter)/CreakingTrailTicks
	at := [3]float64{}
	for i := range 3 {
		at[i] = (heart[i]-target[i])*progress + target[i]
	}
	volume = float32(emitter)/2/CreakingTrailTicks + 0.5
	return int32(math.Floor(at[0])), int32(math.Floor(at[1])), int32(math.Floor(at[2])), volume
}
