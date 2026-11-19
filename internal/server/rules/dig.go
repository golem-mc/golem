package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/registry"
)

// Digger is what a player's destroy progress on a block depends on besides
// the block itself.
type Digger struct {
	// Held is the main-hand stack, whose tool rules set the speed.
	Held protocol.ItemStack
	// MiningEfficiency, BlockBreakSpeed and SubmergedMiningSpeed are the
	// player's attribute values, equipment modifiers included.
	MiningEfficiency, BlockBreakSpeed, SubmergedMiningSpeed float64
	EyesInWater                                             bool
	OnGround                                                bool
	// Haste is the higher of the Haste and Conduit Power amplifiers plus
	// one, MiningFatigue its amplifier plus one; zero for no effect.
	// vanilla: MobEffectUtil.getDigSpeedAmplification
	Haste, MiningFatigue int
}

// DigEffects returns what a body's effects add to a dig: the higher of the
// haste and conduit_power amplifiers plus one, and the mining_fatigue
// amplifier plus one, each zero when no such effect is active.
// vanilla: MobEffectUtil.getDigSpeedAmplification, Player.getDestroySpeed
func DigEffects(effects []effect.Instance) (haste, miningFatigue int) {
	for _, in := range effects {
		switch in.ID {
		case effectHaste, effectConduitPower:
			haste = max(haste, int(in.Amplifier)+1)
		case effectMiningFatigue:
			miningFatigue = int(in.Amplifier) + 1
		}
	}
	return haste, miningFatigue
}

// DestroyStageRange is how far, in blocks, a dig's crack reaches: the players
// within it of the block see it.
// vanilla: ServerLevel.destroyBlockProgress (distance squared below 1024)
const DestroyStageRange = 32

// DestroyStage is the crack stage a dig that has made progress shows. Past
// 9 the client shows none.
// vanilla: ServerPlayerGameMode.incrementDestroyProgress
func DestroyStage(progress float32) int32 { return int32(progress * 10) }

// DestroyProgress is the fraction of a block one tick of digging takes off,
// a dig breaking it once the sum reaches one. A block no dig breaks, and one
// this version does not name, gives zero. The arithmetic is float32 like
// vanilla's, so a tool exactly on the threshold (an iron hoe on leaves)
// lands on the same side.
// vanilla: BlockBehaviour.getDestroyProgress
func DestroyProgress(b registry.Block, d Digger) float32 {
	hardness, ok := registry.Hardness(b)
	if !ok || hardness == -1 {
		return 0
	}
	divisor := float32(100)
	if HasCorrectToolForDrops(d.Held, b) {
		divisor = 30
	}
	return destroySpeed(b, d) / hardness / divisor
}

// HasCorrectToolForDrops reports whether a block drops for a player holding
// the stack: it needs no tool, or the stack is the correct one.
// vanilla: Player.hasCorrectToolForDrops
func HasCorrectToolForDrops(stack protocol.ItemStack, b registry.Block) bool {
	return !registry.RequiresCorrectTool(b) || StackCorrectForDrops(stack, b)
}

// destroySpeed is the player's mining speed against a block.
// vanilla: Player.getDestroySpeed
func destroySpeed(b registry.Block, d Digger) float32 {
	speed := float32(1)
	if !d.Held.Empty() {
		speed = StackMiningSpeed(d.Held, b)
	}
	if speed > 1 {
		speed += float32(d.MiningEfficiency)
	}
	if d.Haste > 0 {
		speed *= 1 + float32(d.Haste)*0.2
	}
	if d.MiningFatigue > 0 {
		speed *= float32(math.Pow(0.3, float64(d.MiningFatigue)))
	}
	speed *= float32(d.BlockBreakSpeed)
	if d.EyesInWater {
		speed *= float32(d.SubmergedMiningSpeed)
	}
	if !d.OnGround {
		speed /= 5
	}
	return speed
}
