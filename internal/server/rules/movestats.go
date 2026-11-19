package rules

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// Mover is what a player's own step was, for the distance statistic it
// counts toward.
type Mover struct {
	Swimming   bool
	Water      Water
	Climbing   bool
	OnGround   bool
	Sprinting  bool
	Crouching  bool
	FallFlying bool
}

// flyMinCentimetres is the horizontal distance a step in the air must pass
// to count as flown, in centimetres. vanilla: ServerPlayer.checkMovementStatistics
const flyMinCentimetres = 25

// MovementStat is the minecraft:custom stat and centimetres a player's step
// adds, or registry.NoCustomStat for none. First match wins: swim, walk under water, wade, climb
// (rise only), ground step (sprint, crouch or walk), glide, fly.
// vanilla: ServerPlayer.checkMovementStatistics
func MovementStat(dx, dy, dz float64, m Mover) (registry.CustomStat, int32) {
	if dx == 0 && dy == 0 && dz == 0 {
		return registry.NoCustomStat, 0
	}
	whole := centimetres(math.Sqrt(dx*dx + dy*dy + dz*dz))
	flat := centimetres(math.Sqrt(dx*dx + dz*dz))
	switch {
	case m.Swimming:
		return positive(registry.CustomStatSwimOneCm, whole)
	case m.Water == Submerged:
		return positive(registry.CustomStatWalkUnderWaterOneCm, whole)
	case m.Water == Wading:
		return positive(registry.CustomStatWalkOnWaterOneCm, flat)
	case m.Climbing:
		if dy > 0 {
			return registry.CustomStatClimbOneCm, int32(math.Round(dy * 100))
		}
	case m.OnGround:
		switch {
		case m.Sprinting:
			return positive(registry.CustomStatSprintOneCm, flat)
		case m.Crouching:
			return positive(registry.CustomStatCrouchOneCm, flat)
		default:
			return positive(registry.CustomStatWalkOneCm, flat)
		}
	case m.FallFlying:
		return registry.CustomStatAviateOneCm, whole
	case flat > flyMinCentimetres:
		return registry.CustomStatFlyOneCm, flat
	}
	return registry.NoCustomStat, 0
}

// fallStatMinimum is the fall, in blocks, below which a landing counts no
// fall_one_cm. vanilla: Player.causeFallDamage
const fallStatMinimum = 2.0

// FallStat is the fall_one_cm a landing adds: the distance the block under the
// player hands to causeFallDamage, in whole centimetres, or zero for a fall
// short of two blocks. vanilla: Player.causeFallDamage, Block.fallOn
func FallStat(distance float64) int32 {
	if distance < fallStatMinimum {
		return 0
	}
	return int32(math.Round(distance * 100))
}

// RidingStat is the minecraft:custom stat and centimetres a passenger's
// carry adds by vehicle ("minecart", "boat" or a mob type); registry.NoCustomStat for an
// uncounted mob or no movement.
// vanilla: ServerPlayer.checkRidingStatistics
func RidingStat(dx, dy, dz float64, vehicle string) (registry.CustomStat, int32) {
	if dx == 0 && dy == 0 && dz == 0 {
		return registry.NoCustomStat, 0
	}
	cm := centimetres(math.Sqrt(dx*dx + dy*dy + dz*dz))
	switch vehicle {
	case "minecart":
		return registry.CustomStatMinecartOneCm, cm
	case "boat":
		return registry.CustomStatBoatOneCm, cm
	case "minecraft:pig":
		return registry.CustomStatPigOneCm, cm
	case "minecraft:horse", "minecraft:donkey", "minecraft:mule", "minecraft:skeleton_horse",
		"minecraft:zombie_horse", "minecraft:llama", "minecraft:trader_llama",
		"minecraft:camel", "minecraft:camel_husk":
		return registry.CustomStatHorseOneCm, cm
	case "minecraft:nautilus", "minecraft:zombie_nautilus":
		return registry.CustomStatNautilusOneCm, cm
	case "minecraft:strider":
		return registry.CustomStatStriderOneCm, cm
	case "minecraft:happy_ghast":
		return registry.CustomStatHappyGhastOneCm, cm
	}
	return registry.NoCustomStat, 0
}

// centimetres rounds a length in blocks to centimetres in float32:
// Math.round((float) length * 100f).
func centimetres(blocks float64) int32 {
	return int32(math.Floor(float64(float32(blocks)*100 + 0.5)))
}

// positive is the statistic and distance when the distance is above zero.
func positive(stat registry.CustomStat, cm int32) (registry.CustomStat, int32) {
	if cm > 0 {
		return stat, cm
	}
	return registry.NoCustomStat, 0
}

// Climbing is LivingEntity.onClimbable for the statistics: the feet cell and
// the one below it climbable (IsClimbable), except for a spectator and for a
// glider inside a block it may glide through.
// vanilla: LivingEntity.onClimbable
func Climbing(feet, below uint16, spectator, fallFlying bool) bool {
	if spectator {
		return false
	}
	if fallFlying && registry.BlockOf(feet).InTag(canGlideThroughBlocks) {
		return false
	}
	return IsClimbable(feet, below)
}

// canGlideThroughBlocks marks the can_glide_through tag.
var canGlideThroughBlocks = registry.BlockTagOf("minecraft:can_glide_through")

// JumpedFromGround reports whether a step left the ground rising.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer's jumpFromGround
func JumpedFromGround(wasOnGround, nowOnGround bool, dy float64) bool {
	return wasOnGround && !nowOnGround && dy > 0
}
