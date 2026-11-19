package rules

import (
	"crypto/sha256"
	"encoding/binary"
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Survival numbers. Health is in half-hearts and food in shanks, both up to
// 20; saturation is the food bar's hidden fill and exhaustion drains it.
// Exported ones are read outside these rules.
const (
	// MaxHealth is the base of the max_health attribute, in half-hearts:
	// how full a body's bar starts and heals to, raised by health boost.
	MaxHealth = 20.0
	// MaxFood is how full a fed player's food bar is, in shank icons.
	MaxFood = 20
	// HurtInvulnerableTicks is the half-second window vanilla gives a hurt
	// body before the next hit may land on it.
	HurtInvulnerableTicks = 10

	spawnSaturation     = 5.0  // what a new or respawned player starts with
	fallPowerEpsilon    = 1e-6 // the slack of calculateFallPower, so a fall of exactly the safe distance floors to nothing
	healExhaustion      = 6.0  // what one healed half-heart costs the food bar
	regenFood           = 18   // the food level natural regeneration runs from
	vitalsStepTicks     = 80   // ticks between natural heals and starvation hits
	saturatedRegenTicks = 10   // ticks between the heals of a full, saturated bar (FoodData.tick)
	maxSaturatedHeal    = 6.0  // the most saturation one of those heals spends, a half-heart's worth
	peacefulHealTicks   = 20   // ticks between peaceful's heals (ServerPlayer.tickRegeneration)
	peacefulFeedTicks   = 10   // ticks between peaceful's shanks
	knockbackStrength   = 0.4  // vanilla's strength for a hit, plus the upward push
	jumpExhaustion      = 0.05 // a jump; 0.2 out of a sprint, below
	criticalMultiplier  = 1.5  // the falling attacker's critical
	sprintExhaustion    = 0.1  // per block sprinted on the ground
	waterExhaustion     = 0.01 // per block swum, walked under water or waded (FoodConstants.EXHAUSTION_SWIM)
)

// The exhaustion of the things a player does rather than how they move,
// FoodConstants' EXHAUSTION_ATTACK and EXHAUSTION_MINE: a hit that lands
// (Player.attack) and a block broken in survival (Block.playerDestroy).
const (
	AttackExhaustion = 0.1
	MineExhaustion   = 0.005
)

// VoidDamage is the void's hit on whoever has fallen out of the world, in
// half-hearts. The hurt itself is the caller's pipeline.
const VoidDamage = 4.0

// Damage types this server hurts players with, by minecraft:damage_type
// name; the Damage Event names the type and the death message is picked by
// it. The environment's live in environment.go.
const (
	DamageTypeArrow  = registry.DamageTypeArrow
	DamageTypeThrown = registry.DamageTypeThrown
	// DamageTypeSpit is minecraft:spit, a llama's spit.
	DamageTypeSpit = registry.DamageTypeSpit
	// DamageTypeWindCharge is minecraft:wind_charge, the hurt a wind charge
	// deals the body it strikes.
	DamageTypeWindCharge = registry.DamageTypeWindCharge
	// DamageTypeFireball and DamageTypeUnattributedFireball are the hurt of a
	// small or large fireball with and without an owner behind it.
	DamageTypeFireball             = registry.DamageTypeFireball
	DamageTypeUnattributedFireball = registry.DamageTypeUnattributedFireball
	// DamageTypeWitherSkull is the hurt of a skull a living owner shot.
	DamageTypeWitherSkull = registry.DamageTypeWitherSkull
	// DamageTypeTrident is minecraft:trident, the hurt a thrown trident
	// deals the body it lands on, whose death message names the thrower
	// behind it.
	DamageTypeTrident = registry.DamageTypeTrident
	DamageTypeFall    = registry.DamageTypeFall
	// DamageTypeStalagmite is minecraft:stalagmite, the hurt of landing on an
	// upward dripstone tip.
	DamageTypeStalagmite = registry.DamageTypeStalagmite
	// DamageTypeEnderPearl is minecraft:ender_pearl, the hurt a pearl's
	// landing deals its own traveller: an armoured blow with no knockback,
	// whose death message is the fall family's (the type's message_id).
	DamageTypeEnderPearl   = registry.DamageTypeEnderPearl
	DamageTypePlayerAttack = registry.DamageTypePlayerAttack
	DamageTypeMobAttack    = registry.DamageTypeMobAttack
	// DamageTypeMobProjectile is a shulker bullet's blow.
	DamageTypeMobProjectile = registry.DamageTypeMobProjectile
	// DamageTypeMobAttackNoAggro is a goat ram's blow, in no_anger so the
	// victim does not turn on the goat.
	DamageTypeMobAttackNoAggro = registry.DamageTypeMobAttackNoAggro
	DamageTypeMaceSmash        = registry.DamageTypeMaceSmash
	DamageTypeThorns           = registry.DamageTypeThorns
	DamageTypeSting            = registry.DamageTypeSting
	DamageTypeExplosion        = registry.DamageTypeExplosion
	// DamageTypePlayerExplosion is chosen over minecraft:explosion whenever a
	// blast's direct and indirect sources are both entities, as for every
	// owned primed TNT.
	// vanilla: DamageSources.explosion
	DamageTypePlayerExplosion = registry.DamageTypePlayerExplosion
	DamageTypeOutOfWorld      = registry.DamageTypeOutOfWorld
	DamageTypeStarve          = registry.DamageTypeStarve
	DamageTypeOutsideBorder   = registry.DamageTypeOutsideBorder
	DamageTypeGeneric         = registry.DamageTypeGeneric
	DamageTypeGenericKill     = registry.DamageTypeGenericKill
	// DamageTypeBadRespawnPoint is minecraft:bad_respawn_point, the blast
	// sleeping in a bed where beds cannot work is: the nether's and the
	// end's own bed rule explodes it, the damage sources the blast of.
	DamageTypeBadRespawnPoint = registry.DamageTypeBadRespawnPoint
	// DamageTypeFireworks is a rocket burst's hurt. Its type scales only a
	// living non-player's bursts, so the player-launched ones here land whole
	// on any difficulty.
	DamageTypeFireworks = registry.DamageTypeFireworks
	// DamageTypeFallingAnvil is minecraft:falling_anvil, the hurt an anvil's
	// fall deals what it lands on; DamageTypeFallingBlock is the same hurt
	// for any other body carrying the numbers, which only a saved one is.
	DamageTypeFallingAnvil = registry.DamageTypeFallingAnvil
	DamageTypeFallingBlock = registry.DamageTypeFallingBlock
	// DamageTypeFallingStalactite is minecraft:falling_stalactite, the hurt
	// a speleothem's fall deals what it lands on.
	DamageTypeFallingStalactite = registry.DamageTypeFallingStalactite
)

// The difficulties, as the Difficulty byte of level.dat and the Change
// Difficulty packet number them.
const (
	DifficultyPeaceful uint8 = 0
	DifficultyEasy     uint8 = 1
	DifficultyNormal   uint8 = 2
	DifficultyHard     uint8 = 3
)

// starveFloors is the health starvation stops at per difficulty: easy 10,
// normal 1, hard 0. Peaceful shares easy's 10, as FoodData.tick's condition
// reads (health above 10, or hard, or above 1 on normal), though its feeding
// rarely leaves the bar empty.
var starveFloors = [4]float64{10, 10, 1, 0}

// FreshVitals is the state a player starts in: full health and food, the
// saturation a join or a respawn grants, the breath of a body that has
// never been underwater, and the mode they are in.
func FreshVitals(mode int32) player.Vitals {
	return player.Vitals{
		GameMode:   mode,
		Health:     MaxHealth,
		Food:       MaxFood,
		Saturation: spawnSaturation,
		AirSupply:  MaxAirSupply,
	}
}

// RespawnVitalsKeeping is a respawned body's vitals: fresh, with experience and
// score carried when keepLevel (and for a spectator). The bool is the
// inventory's keep decision, which the inventory resend also uses, so the two
// cannot disagree.
// vanilla: ServerPlayer.restoreFrom, transferInventoryXpAndScore
func RespawnVitalsKeeping(old player.Vitals, keepInventory, keepLevel bool) (player.Vitals, bool) {
	v := FreshVitals(old.GameMode)
	// vanilla: ServerPlayer.restoreFrom
	v.PreviousGameMode, v.HasPreviousGameMode = old.PreviousGameMode, old.HasPreviousGameMode
	v.LastDeath = old.LastDeath
	// vanilla: ServerPlayer.restoreFrom copies enteredNetherPosition even
	// without restoreAll
	v.EnteredNether = old.EnteredNether
	spectator := old.GameMode == protocol.GameModeSpectator
	kept := keepInventory || spectator
	if keepLevel || spectator {
		v.Score = old.Score
		v.ExperienceLevel = old.ExperienceLevel
		v.ExperienceProgress = old.ExperienceProgress
		v.ExperienceTotal = old.ExperienceTotal
	}
	return v, kept
}

// ModeInvulnerable reports whether the game mode keeps a player out of harm's
// way, which creative and spectator do and survival and adventure do not.
func ModeInvulnerable(mode int32) bool {
	return mode == protocol.GameModeCreative || mode == protocol.GameModeSpectator
}

// InvulnerabilityBypassed reports minecraft:bypasses_invulnerability types
// (the void's out_of_world and /kill's generic_kill), the only ones that
// reach creative and spectator.
// vanilla: DamageTypeTags.BYPASSES_INVULNERABILITY
func InvulnerabilityBypassed(damageType registry.DamageType) bool {
	return damageType.InTag(registry.DamageTypeTagIDBypassesInvulnerability)
}

// ModeMayBuild reports whether the game mode lets a player change blocks,
// which survival and creative do and adventure and spectator do not.
// vanilla: Player.mayBuild
func ModeMayBuild(mode int32) bool {
	return mode == protocol.GameModeSurvival || mode == protocol.GameModeCreative
}

// Exhaust charges a player for an action, Player.causeFoodExhaustion: the
// invulnerable modes are never charged, everyone else adds to the counter.
func Exhaust(v *player.Vitals, amount float64) {
	if ModeInvulnerable(v.GameMode) {
		return
	}
	exhaust(v, amount)
}

// DamageExhaustion is a damage type's exhaustion field. Player.actuallyHurt
// charges it after armour, only for a hurt that still deals damage.
func DamageExhaustion(damageType registry.DamageType) float64 {
	return damageType.Info().Exhaustion
}

// DamageRuleGate names the game rule gating a damage type for players, or
// RuleUnknown: is_drowning under drowning_damage, is_fall under fall_damage, is_fire
// under fire_damage. With the rule off a player takes nothing from it (no
// hurt, armour wear or exhaustion); mobs still take it, the check being on
// the player alone.
// vanilla: Player.isInvulnerableTo
func DamageRuleGate(damageType registry.DamageType) world.GameRuleIndex {
	switch {
	case damageType.InTag(registry.DamageTypeTagIDIsDrowning):
		return world.RuleDrowningDamage
	case damageType.InTag(registry.DamageTypeTagIDIsFall):
		return world.RuleFallDamage
	case damageType.InTag(registry.DamageTypeTagIDIsFire):
		return world.RuleFireDamage
	case damageType.InTag(registry.DamageTypeTagIDIsFreezing):
		return world.RuleFreezeDamage
	}
	return world.RuleUnknown
}

// exhaust accumulates exhaustion, FoodData.addExhaustion's way: the counter
// only piles up, to no more than forty, and drainExhaustion is what turns it
// into lost saturation and food on the tick.
func exhaust(v *player.Vitals, amount float64) {
	v.Exhaustion = math.Min(v.Exhaustion+amount, 40)
}

// drainExhaustion is FoodData.tick's head: past 4, 4 comes off and a point
// of saturation, or of food once saturation is gone (not on peaceful). One
// point a tick at most, so a large cost drains over several ticks. This is
// the only way food goes down.
func drainExhaustion(v *player.Vitals, difficulty uint8) {
	if v.Exhaustion <= 4 {
		return
	}
	v.Exhaustion -= 4
	if v.Saturation > 0 {
		v.Saturation = math.Max(0, v.Saturation-1)
	} else if difficulty != DifficultyPeaceful && v.Food > 0 {
		v.Food--
	}
}

// ObfuscateSeed is the world seed as the client gets it, the first eight bytes
// of the SHA-256 of the seed's little-endian bytes read little-endian.
// vanilla: BiomeManager.obfuscateSeed
func ObfuscateSeed(seed int64) int64 {
	var in [8]byte
	binary.LittleEndian.PutUint64(in[:], uint64(seed))
	sum := sha256.Sum256(in[:])
	return int64(binary.LittleEndian.Uint64(sum[:8]))
}
