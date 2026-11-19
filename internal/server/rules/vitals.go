package rules

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The survival rhythms on the tick clock and the cost of a step, as decisions
// over the vitals: they update them in place and return what happened,
// leaving locks, packets and the hurt pipeline to the caller.

// VitalsEvent is the outcome of a body's own survival tick. The hurting one
// goes through the caller's hurt pipeline, which sends its own health packet,
// so the caller skips its end-of-tick bars; the rest only changed bars, sent
// if they differ from the tick's start.
type VitalsEvent int

const (
	// VitalsSteady is a tick that came to nothing: the counters advanced,
	// but no heal, bite or blow was due.
	VitalsSteady VitalsEvent = iota
	// VitalsHealed is a half-heart natural regeneration gave back, paid
	// for out of the food bar - or the step of both that a peaceful world
	// takes every twenty ticks.
	VitalsHealed
	// VitalsStarved is a bite of starvation coming due on an empty food
	// bar, one the difficulty's floor has not stopped it from taking.
	VitalsStarved
)

// TickVitals runs one tick of a player's survival: the hurt invulnerability
// countdown, the void, natural regeneration and starvation as the difficulty
// allows, or peaceful's feed-and-heal. regen is natural_health_regeneration,
// which gates peaceful's feed-and-heal and FoodData.tick's two heal branches
// but not starvation.
// vanilla: FoodData.tick
//
// around is the caller-gathered habitat (fire, air, block blows, landed in
// vanilla's order); oxygenBonus decides which underwater ticks spare the
// breath.
//
// The vitals are updated in place under the caller's lock. It returns the
// tick's bars event and the blows (environment and void), each for the hurt
// pipeline in order; the window each hurt arms keeps later ones quiet.
//
// Effects tick where vanilla's baseTick ends, after the void and before food
// (effects may be nil); their changes are returned last, for the packets.
func TickVitals(v *player.Vitals, effects *effect.Set, a *player.Attributes, difficulty uint8, belowVoid bool, around Habitat, rng *rand.Rand, oxygenBonus float64, regen bool) (VitalsEvent, []Hurt, Douse, []effect.Change) {
	// The player's own age (Entity.tickCount, the level-up sound's and the
	// peaceful rhythms' clock) counts every tick, before anything the tick
	// may cut short.
	v.AgeTicks++
	if v.HurtTime > 0 {
		v.HurtTime--
	}
	if v.InvulnerableTicks > 0 {
		v.InvulnerableTicks--
	}
	if v.Dead {
		return VitalsSteady, nil, Douse{}, nil
	}
	// vanilla: LivingEntity.aiStep
	if v.ImpulseGraceTicks > 0 {
		v.ImpulseGraceTicks--
	}
	TickArrowCount(v)
	TickStingerCount(v)
	// Effects count down in every mode; the hurts they deal are refused
	// by the hurt pipeline for a body that cannot be hurt.
	invulnerable := ModeInvulnerable(v.GameMode)

	// Entity.baseTick halves the fall a body carries while its box touches
	// lava, a tick at a time and whatever the mode.
	if around.InLava {
		v.FallDistance *= 0.5
	}

	// Hurts go out in vanilla's order, since the first opens the
	// invulnerability window: baseTick's own (burn, wall, water, then the
	// void within the same baseTick), the effects' (tickEffects), the
	// blocks' (aiStep's applyEffectsFromBlocks), and the freeze bite last.
	// Burn, douses and breath run for every mode; invulnerable modes take
	// no new flame or blow, which their pipeline refuses anyway.
	var hurts, blocks []Hurt
	env, douse := TickEnvironment(v, around, rng, oxygenBonus)
	for _, h := range env {
		if baseTickHurt(h.DamageType) {
			hurts = append(hurts, h)
		} else {
			blocks = append(blocks, h)
		}
	}
	// The void's out_of_world carries the bypasses-invulnerability tag, so
	// it is generated for the invulnerable modes too and left for the hurt
	// pipeline to land or refuse.
	if belowVoid {
		hurts = append(hurts, Hurt{DamageTypeOutOfWorld, VoidDamage})
	}
	var changes []effect.Change
	if effects != nil {
		changes = tickEffects(v, effects, a, &hurts)
		// The wither rose applies after the effects tick (aiStep's
		// applyEffectsFromBlocks follows baseTick's tickEffects), so its first
		// bite falls the tick after stepping in. A spectator's noPhysics keeps
		// it out; creative takes the wither though its mode refuses the bite.
		// vanilla: WitherRoseBlock.entityInside, Entity.isAffectedByBlocks
		if v.GameMode != protocol.GameModeSpectator && overlaps(around.Overlapped, IsWitherRose) {
			if in, ok := WitherRoseEffect(difficulty, false); ok {
				if change, added := AddEffect(v, effects, in); added {
					changes = append(changes, change)
				}
			}
		}
	}
	hurts = append(hurts, blocks...)

	// The freeze counter runs for every body, invulnerability refusing only
	// the bite; the spectator's gate, folded into FreezeImmune, spares even
	// the counter, so a spectator sees no frost. The bite lands after the
	// blocks, as in aiStep.
	// vanilla: InsideBlockEffectType.FREEZE, LivingEntity.aiStep
	freezeHurts := TickFreeze(v, overlaps(around.Overlapped, IsPowderSnow), !around.FreezeImmune)
	if !invulnerable {
		hurts = append(hurts, freezeHurts...)
	}

	// Peaceful's rhythm and FoodData.tick run in every mode, so creative and
	// spectator heal from food; the starve bite is refused by the hurt
	// pipeline, as vanilla's isInvulnerableTo refuses it.
	// vanilla: ServerPlayer.doTick, ServerPlayer.tickRegeneration

	var starved, healed bool

	// The bar this tick's heals cap at, read after the effects' own tick so a
	// boost that ended on it is already gone.
	bar := MaxHealthOf(effects, a)

	if regen && difficulty == world.DifficultyPeaceful {
		// Peaceful heals 1 and adds a point of saturation every 20 ticks of
		// age and a shank every 10, if natural_health_regeneration allows,
		// which tickRegeneration checks first.
		// vanilla: ServerPlayer.tickRegeneration
		if v.AgeTicks%peacefulHealTicks == 0 {
			if v.Health < bar {
				v.Health = math.Min(bar, v.Health+1)
				healed = true
			}
			if v.Saturation < MaxFood {
				v.Saturation++
			}
		}
		if v.AgeTicks%peacefulFeedTicks == 0 && v.Food < MaxFood {
			v.Food++
			healed = true
		}
	}

	drainExhaustion(v, difficulty)

	// FoodData.tick, on every difficulty: a full bar with saturation heals
	// every 10 ticks (a sixth of the saturation, at most 6, given as health
	// and charged as exhaustion); a bar of 18 or more heals 1 every 80; an
	// empty one bites every 80 as far as the difficulty allows. The timer
	// resets when none applies. The rule gates the two heals only: a
	// starving body keeps biting with it off.
	hurt := v.Health > 0 && v.Health < bar
	switch {
	case regen && v.Saturation > 0 && hurt && v.Food >= MaxFood:
		v.FoodTicks++
		if v.FoodTicks >= saturatedRegenTicks {
			spent := math.Min(v.Saturation, maxSaturatedHeal)
			v.Health = math.Min(bar, v.Health+spent/maxSaturatedHeal)
			exhaust(v, spent)
			v.FoodTicks = 0
			healed = true
		}
	case regen && v.Food >= regenFood && hurt:
		v.FoodTicks++
		if v.FoodTicks >= vitalsStepTicks {
			v.Health = math.Min(bar, v.Health+1)
			exhaust(v, healExhaustion)
			v.FoodTicks = 0
			healed = true
		}
	case v.Food <= 0:
		v.FoodTicks++
		if v.FoodTicks >= vitalsStepTicks {
			if v.Health > starveFloors[difficulty] {
				starved = true
			}
			v.FoodTicks = 0
		}
	default:
		v.FoodTicks = 0
	}

	switch {
	case starved:
		return VitalsStarved, hurts, douse, changes
	case healed:
		return VitalsHealed, hurts, douse, changes
	}
	return VitalsSteady, hurts, douse, changes
}

// baseTickHurt reports whether a hurt of TickEnvironment's comes from
// Entity.baseTick (the burn, the wall, the water) rather than the blocks.
func baseTickHurt(damageType registry.DamageType) bool {
	return damageType == DamageTypeOnFire || damageType == DamageTypeInWall || damageType == DamageTypeDrown
}

// Water is how deep in water a step was taken, for what the step costs:
// vanilla's ServerPlayer.checkMovementStatistics charges a swim or a walk
// under water by the whole distance and a wade by the horizontal one.
type Water uint8

const (
	// Dry is a step out of the water.
	Dry Water = iota
	// Wading is a step with the body in water and the eyes above it,
	// isInWater without isEyeInFluid.
	Wading
	// Submerged is a step with the eyes under water, isEyeInFluid(WATER),
	// which a swim is too.
	Submerged
)

// FallDamage is the fall's whole blocks past the safe distance, scaled by
// the landing block's modifier and floored once (a hay bale's fifth of an
// eleven-block fall is 1, not 1.5). The epsilon is calculateFallPower's, so
// a fall of exactly the safe distance floors to 0. safeDistance is the
// safe_fall_distance attribute, which jump boost moves; mobs pass modifier 1.
// vanilla: LivingEntity.calculateFallDamage
func FallDamage(fallDistance, safeDistance, modifier float64) float64 {
	fall := (fallDistance + fallPowerEpsilon - safeDistance) * modifier
	if fall > 0 {
		return math.Floor(fall)
	}
	return 0
}

// LandingParticleCount is the number of block particles a landing bursts
// into, 0 when the fall does not pass the safe distance by a whole block.
// vanilla: LivingEntity.checkFallDamage
func LandingParticleCount(fallDistance, safeFallDistance float64) int32 {
	power := math.Max(0, math.Floor(fallDistance+fallPowerEpsilon-safeFallDistance))
	if power <= 0 {
		return 0
	}
	return int32(150 * math.Min(float64(float32(0.2))+power/15, 2.5))
}

// LandingParticleXZ pulls a landing at (x, z) onto the edge of the on-pos
// column (onX, onZ) when the body stands in another column.
// vanilla: LivingEntity.checkFallDamage
func LandingParticleXZ(x, z float64, onX, onZ int32) (float64, float64) {
	if int32(math.Floor(x)) == onX && int32(math.Floor(z)) == onZ {
		return x, z
	}
	xDiff := x - float64(onX) - 0.5
	zDiff := z - float64(onZ) - 0.5
	maxDiff := math.Max(math.Abs(xDiff), math.Abs(zDiff))
	return float64(onX) + 0.5 + xDiff/maxDiff*0.5, float64(onZ) + 0.5 + zDiff/maxDiff*0.5
}

// Landing is what a step lands on, gathered by the caller: the block under
// the feet whose fallOn scales the fall, the block the body's feet cell
// holds, and the block states the body's own box fills.
type Landing struct {
	// Ground is the block under the feet, the supporting block vanilla's
	// getOnPosLegacy reads a landing's fallOn from.
	Ground uint16
	// GroundX and GroundZ are the column of the Ground cell.
	GroundX, GroundZ int32
	// Feet is the block in the feet cell, which the powder snow's
	// entityInside checks alone: PowderSnowBlock's getInBlockState().is(this).
	Feet uint16
	// Overlapped are the block states of the cells the body's box fills
	// after the step, the scan vanilla's checkInsideBlocks walks: the web
	// and the sweet berry bush write the fall off whole from any of them.
	Overlapped []uint16
}

// FallEffect is a landing block's word on a fall: the distance becomes
// distance*Scale+Offset before the safe distance comes off, the damage is
// multiplied by Modifier, and DamageType names what hurt.
type FallEffect struct {
	Scale, Offset, Modifier float64
	DamageType              registry.DamageType
}

// FallOn is the landing block's say on a fall: a bed or shelf mushroom halves the distance
// before the safe three are subtracted; hay and honey make the damage a
// fifth; a slime block spares it, sneaking or not (zero, or no call when the
// bounce is suppressed); a dripstone tip adds 2.5 blocks and doubles it as
// stalagmite damage; anything else changes nothing.
// vanilla: Block.fallOn, BedBlock.fallOn, HayBlock.fallOn, HoneyBlock.fallOn,
// SlimeBlock.fallOn, PointedDripstoneBlock.fallOn, Blocks.SHELF_MUSHROOM's
// fallDistanceReduction
func FallOn(state uint16) FallEffect {
	plain := FallEffect{Scale: 1, Modifier: 1, DamageType: DamageTypeFall}
	switch b := registry.BlockOf(state); {
	case b == registry.BlockHayBlock, b == registry.BlockHoneyBlock:
		plain.Modifier = 0.2
	case bedBlocks[b], b == registry.BlockShelfMushroom:
		plain.Scale = 0.5
	case b == registry.BlockSlimeBlock:
		plain.Modifier = 0
	case b == registry.BlockPointedDripstone &&
		registry.StateValue(state, registry.PropThickness) == "tip" &&
		registry.StateValue(state, registry.PropVerticalDirection) == "up":
		plain.Offset, plain.Modifier, plain.DamageType = 2.5, 2, DamageTypeStalagmite
	}
	return plain
}

// IsHoneyBlock reports whether a block state is a honey block, whose landing
// plays the slide sound and the slide event beside the fall's own.
func IsHoneyBlock(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockHoneyBlock
}

// The honey block's slide numbers.
// vanilla: HoneyBlock.SLIDE_STARTS_WHEN_VERTICAL_SPEED_IS_AT_LEAST,
// MIN_FALL_SPEED_TO_BE_CONSIDERED_SLIDING, THROTTLE_SLIDE_SPEED_TO
const (
	honeySlideStart = 0.13
	honeyMinFall    = 0.08
	honeyThrottle   = 0.05
)

// honeyOldDeltaY undoes a living body's gravity and drag on a vertical
// velocity read after the travel.
// vanilla: HoneyBlock.getOldDeltaY
func honeyOldDeltaY(vy float64) float64 {
	return vy/float64(float32(0.98)) + 0.08
}

// HoneyFalling reports whether a body is airborne and falling fast enough
// to slide down a honey block's side, the cell-free part of HoneySliding.
func HoneyFalling(vy float64, onGround bool) bool {
	return !onGround && honeyOldDeltaY(vy) < -honeyMinFall
}

// HoneySliding reports whether a body with its feet at (x, y, z) and the
// given vertical velocity and box width slides down the side of the honey
// block in the cell: airborne, no higher than the block's top, falling and
// pressed against a side face.
// vanilla: HoneyBlock.isSlidingDown
func HoneySliding(cellX, cellY, cellZ int32, x, y, z, vy, width float64, onGround bool) bool {
	if !HoneyFalling(vy, onGround) || y > float64(cellY)+0.9375-1e-7 {
		return false
	}
	dx := math.Abs(float64(cellX) + 0.5 - x)
	dz := math.Abs(float64(cellZ) + 0.5 - z)
	overlap := 0.4375 + width/2
	return dx+1e-7 > overlap || dz+1e-7 > overlap
}

// HoneySlide is the velocity a sliding body leaves the honey block with: the
// fall throttled and, past the slide's start, the horizontal scaled down
// with it. The caller resets the fall distance.
// vanilla: HoneyBlock.doSlideMovement
func HoneySlide(vx, vy, vz float64) (float64, float64, float64) {
	if old := honeyOldDeltaY(vy); old < -honeySlideStart {
		f := -honeyThrottle / old
		vx, vz = vx*f, vz*f
	}
	return vx, (-honeyThrottle - 0.08) * float64(float32(0.98)), vz
}

// FallKind picks the pair of sounds a landing plays.
type FallKind int

const (
	FallGeneric FallKind = iota
	FallHostile
	FallPlayer
)

// FallSound is a landing's sound for the damage dealt: the big fall above 4
// (about eight blocks, read off the floored damage), the small one at 4 or
// less, from the pair of the kind.
// vanilla: LivingEntity.getFallDamageSound, getFallSounds, Player.getFallSounds
func FallSound(damage float64, kind FallKind) registry.Sound {
	small, big := registry.SoundEntityGenericSmallFall, registry.SoundEntityGenericBigFall
	switch kind {
	case FallHostile:
		small, big = registry.SoundEntityHostileSmallFall, registry.SoundEntityHostileBigFall
	case FallPlayer:
		small, big = registry.SoundEntityPlayerSmallFall, registry.SoundEntityPlayerBigFall
	}
	if damage > 4 {
		return big
	}
	return small
}

// FallResetSweep reports whether a step from was to now crosses, along its
// line, a block that writes off the fall: the fall_damage_resetting tag
// (climbables, web, berry bush), water, and for a player the end portal, end
// gateway and, when netherPortalInstant, the nether portal. Entity.move
// sweeps a falling step of a block or more, at most fallSweepReach ahead,
// so a fast fall through a one-thick layer is forgiven. A body with no
// fallDistance is not swept.
// vanilla: Entity.move, ClipContext.Block.FALLDAMAGE_RESETTING,
// ClipContext.Fluid.WATER
func FallResetSweep(w BlockAt, fallDistance float64, was, now player.Position, isPlayer, netherPortalInstant bool) bool {
	if fallDistance == 0 {
		return false
	}
	dx, dy, dz := now.X-was.X, now.Y-was.Y, now.Z-was.Z
	length := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if length < 1 {
		return false
	}
	if length > fallSweepReach {
		scale := fallSweepReach / length
		dx, dy, dz = dx*scale, dy*scale, dz*scale
	}
	return world.TraverseBlocks(was.X, was.Y, was.Z, dx, dy, dz, func(x, y, z, _ int32) bool {
		return writesFallOff(w.BlockAt(x, y, z), isPlayer, netherPortalInstant)
	})
}

// fallSweepReach is the farthest vanilla clips a fall sweep's ray, min of
// the step's length and this: Entity.move's Math.min(movement.length(), 8.0).
const fallSweepReach = 8.0

// writesFallOff reports whether a sweep writes the fall off on a state:
// water, the fall_damage_resetting tag, or for a player the end portal,
// end gateway and an instant nether portal.
func writesFallOff(state uint16, isPlayer, netherPortalInstant bool) bool {
	if IsWater(state) {
		return true
	}
	b := registry.BlockOf(state)
	if b.IsFallDamageResetting() {
		return true
	}
	if !isPlayer {
		return false
	}
	switch b {
	case registry.BlockEndPortal, registry.BlockEndGateway:
		return true
	case registry.BlockNetherPortal:
		return netherPortalInstant
	}
	return false
}

// impulseFallDamage is FallDamage on a landing at y, counting only the part
// of the fall below the impulse impact height when one is set.
// vanilla: LivingEntity.causeFallDamage
func impulseFallDamage(v *player.Vitals, fall, y, safeDistance, modifier float64) float64 {
	if v.ImpulseImpact != nil {
		fall = math.Min(fall, v.ImpulseImpact[1]-y)
		if fall <= 0 {
			ResetImpulse(v)
		} else {
			TryResetImpulse(v)
		}
	}
	damage := FallDamage(fall, safeDistance, modifier)
	if damage > 0 {
		ResetImpulse(v)
	}
	return damage
}

// impulseGraceTicks is how long a wind charge or mace smash impact height
// outlives the resets of landing. vanilla: LivingEntity.setIgnoreFallDamageFromCurrentImpulse
const impulseGraceTicks = 40

// SetImpulseImpact makes the next fall count only below pos.
// vanilla: LivingEntity.setIgnoreFallDamageFromCurrentImpulse(true, pos)
func SetImpulseImpact(v *player.Vitals, pos [3]float64) {
	ApplyImpulseGrace(v, impulseGraceTicks)
	v.ImpulseImpact = &pos
}

// ApplyImpulseGrace holds off the impulse reset for at least ticks.
// vanilla: LivingEntity.applyPostImpulseGraceTime
func ApplyImpulseGrace(v *player.Vitals, ticks int) {
	v.ImpulseGraceTicks = max(v.ImpulseGraceTicks, ticks)
}

// TryResetImpulse clears the impact height once its grace has run out.
// vanilla: LivingEntity.tryResetCurrentImpulseContext
func TryResetImpulse(v *player.Vitals) {
	if v.ImpulseGraceTicks == 0 {
		ResetImpulse(v)
	}
}

// ResetImpulse clears the impact height and its grace.
// vanilla: LivingEntity.resetCurrentImpulseContext
func ResetImpulse(v *player.Vitals) {
	v.ImpulseGraceTicks, v.ImpulseImpact = 0, nil
}

// TrackMovement follows one reported step's survival effects. A downward
// step in the air extends a fall; landing ends it and reports the damage
// past the safe distance and the fall the landing carried (what trampling
// reads). Only downward movement counts (Entity.checkFallDamage's
// `fallDistance -= y`), not a jump's arc or rise. Rising ends a fall; from
// the ground it is a jump, costing exhaustion like a sprint.
//
// Water banks nothing (Entity.baseTick writes the fall off on touch). Feet
// starting the step on a climbable (LivingEntity.onClimbable: the climbable
// tag, or an open trapdoor over a ladder of its facing) write the carried
// fall off before the step's descent banks, so catching a ladder forgives
// the fall and a slide down keeps one step's worth at most. Slow falling and
// levitation write it off every tick. Flying and spectator players never
// fall; creative players track falls but take no landing damage.
//
// The damage is FallDamage on the safe_fall_distance attribute (jump boost
// rides that map), scaled by the landing block (Landing, FallOn) and the
// fall_damage_multiplier attribute, on the part of the fall below an impulse
// impact height (SetImpulseImpact) when one is set.
//
// A box in a fall-writing block (web, berry bush, powder snow at the feet)
// banks nothing; so does a step of a block or more swept through one
// (swept, FallResetSweep), forgiving the carried fall before the step's
// descent banks.
//
// Distance costs exhaustion as checkMovementStatistics measures it, in whole
// centimetres: 0.01 per block in water (all swum, or walked under water, or
// the horizontal wade) and, out of it, 0.1 per block sprinted on the ground.
// Walking, sneaking and air cost nothing.
func TrackMovement(v *player.Vitals, effects []effect.Instance, was, now player.Position, flying, sprinting bool, water Water, swimming, climbing, swept bool, safeFallDistance, fallDamageMultiplier float64, landing Landing) (float64, float64) {
	dy := now.Y - was.Y
	stuck := IsPowderSnow(landing.Feet) || overlaps(landing.Overlapped, IsStuckIn)
	// vanilla: ServerGamePacketListenerImpl.handleMovePlayer (hasLandedInLiquid
	// for water; the caller adds lava, fall flying and the spin attack) and
	// Player.makeStuckInBlock
	defer func() {
		if now.OnGround || water != Dry && dy < 1e-5 || climbing || stuck || v.GameMode == protocol.GameModeSpectator {
			TryResetImpulse(v)
		}
	}()
	if v.Dead || v.GameMode == protocol.GameModeSpectator || flying {
		v.FallDistance = 0
		return 0, 0
	}
	// The sweep resets before the step banks, as in Entity.move, so a fast
	// drop through a one-thick layer lands on the short fall below alone.
	if swept {
		v.FallDistance = 0
	}
	if water != Dry || ResetsFall(effects) || stuck {
		v.FallDistance = 0
	} else {
		if climbing {
			v.FallDistance = 0
		}
		if dy < 0 {
			v.FallDistance -= dy
		}
	}
	var fallDamage float64
	var landedFall float64
	switch {
	case now.OnGround:
		// The landing's fall banks before the reset, so the caller sees the
		// descent this packet carried, the touchdown move included.
		landedFall = v.FallDistance
		// vanilla: Player.causeFallDamage returns before damage and sounds
		// when mayfly is set, without suppressing Entity.checkFallDamage.
		if v.FallDistance > 0 && !ModeInvulnerable(v.GameMode) {
			fall := FallOn(landing.Ground)
			fallDamage = impulseFallDamage(v, v.FallDistance*fall.Scale+fall.Offset, now.Y, safeFallDistance, fall.Modifier*fallDamageMultiplier)
		}
		v.FallDistance = 0
	case dy > 0:
		if was.OnGround {
			Exhaust(v, jumpExhaustion)
			if sprinting {
				Exhaust(v, 3*jumpExhaustion) // the rest of a sprint jump's 0.2
			}
		}
		v.FallDistance = 0
	}

	dx, dz := now.X-was.X, now.Z-was.Z
	centimetres := func(d float64) float64 { return float64(int32(math.Round(d * 100))) }
	switch {
	case swimming || water == Submerged:
		Exhaust(v, centimetres(math.Sqrt(dx*dx+dy*dy+dz*dz))*waterExhaustion*0.01)
	case water == Wading:
		Exhaust(v, centimetres(math.Hypot(dx, dz))*waterExhaustion*0.01)
	case now.OnGround && sprinting:
		Exhaust(v, centimetres(math.Hypot(dx, dz))*sprintExhaustion*0.01)
	}
	return fallDamage, landedFall
}
