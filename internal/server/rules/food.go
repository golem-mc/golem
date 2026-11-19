package rules

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Food maths: a finished meal's effect on the bars, its duration, and its
// bites. The eating session and packets are internal/server/food.go's.

// ticksPerSecond is the rate of the game clock a meal is counted down on:
// 20 game ticks a second, the same rate the server's main loop runs at.
const ticksPerSecond = 20

// Eat applies a meal: nutrition onto the food bar, then the saturation,
// capped at the new food level, so saturation never exceeds food.
func Eat(v *player.Vitals, food registry.Food) {
	v.Food = int32(math.Min(float64(MaxFood), float64(v.Food)+float64(food.Nutrition)))
	v.Saturation = math.Min(float64(v.Food), v.Saturation+float64(food.Saturation))
}

// EatTicks is the consumable's seconds as ticks: seconds times the tick rate
// in float32, truncated (Consumable.consumeTime), so 1.6 s is 32 ticks, not
// 33; float32's 1.6 sits a hair over 32 already.
func EatTicks(food registry.Food) int32 {
	return ConsumeTicks(food.ConsumeSeconds)
}

// ConsumeTicks is EatTicks for any consumable's seconds.
func ConsumeTicks(seconds float32) int32 {
	return int32(seconds * float32(ticksPerSecond))
}

// ConsumableSound rolls the volume and pitch for a bite or sip.
// vanilla: Consumable.emitParticlesAndSounds
func ConsumableSound(rng *rand.Rand, drink bool) (volume, pitch float32) {
	volume = 1
	if rng.Int31n(2) == 0 {
		volume = 0.5
	}
	pitch = 1 + (rng.Float32()-rng.Float32())*0.2
	// Both animations roll all three values before selecting what to play.
	// vanilla: Mth.randomBetween uses the difference of the float32 bounds.
	drinkPitch := rng.Float32()*(float32(1)-float32(0.9)) + 0.9
	if drink {
		return 0.5, drinkPitch
	}
	return volume, pitch
}

// EatBiteTicks reports whether remaining (the use's countdown) is a bite
// tick: past the first fifth-and-a-bit of the meal, every fourth tick.
// vanilla: Consumable.shouldEmitParticlesAndSounds
func EatBiteTicks(consumeTicks, remaining int32) bool {
	interval := int32(float32(consumeTicks) * 0.21875)
	return consumeTicks-remaining > interval && remaining%4 == 0
}

// SlotOfHand is the inventory slot the given hand holds, the inventory's
// lock being the caller's to hold.
func SlotOfHand(inv *player.Inventory, hand int32) int {
	if hand == protocol.HandOff {
		return player.OffHandSlot
	}
	return player.HotbarSlot + int(inv.Selected)
}

// BlockOf is the block a coordinate is in, for the packets that name sounds
// and effects by where in the world they happen.
func BlockOf(coordinate float64) int32 {
	return int32(math.Floor(coordinate))
}
