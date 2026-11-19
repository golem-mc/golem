package rules

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Combat numbers: a hit's damage and knockback, what armour leaves, and which
// damage ignores armour, decided from plain values (held stack, positions,
// armour sums). Armour and weapon wear and the packets are internal/server/survival.go's.

// BowPower is a draw's power, (t²+2t)/3 for t the draw in seconds, capped at
// 1. An arrow leaves at three times it; a capped shot is critical.
// vanilla: BowItem.getPowerForTime
func BowPower(chargeTicks int) float64 {
	f := float64(chargeTicks) / 20.0
	f = (f*f + 2*f) / 3
	if f > 1 {
		f = 1
	}
	return f
}

// playerAttackSpeed is the base of the attack-speed attribute every player
// carries, Player.createAttributes' own four swings a second; the held
// item's modifier is added to it by AttackSpeed.
const playerAttackSpeedBase = 4.0

// criticalStrength is the swing recovery past which the critical, the sweep
// and the sprint-knockback bonus land.
// vanilla: Player.attack
const criticalStrength = 0.9

// KnockbackAttackBonus is the extra knockback a sprinting full-strength hit
// lands: vanilla's Player.attack adds it to the blow's knockback before
// causeExtraKnockback gets it, getKnockback + (knockbackAttack ? 0.5 : 0).
const KnockbackAttackBonus = 0.5

// AttackKnockback is a blow's extra shove: half the enchantment knockback
// (getKnockback halves it) plus, for a sprinting full-strength hit, a half.
func AttackKnockback(enchantedKnockback float64, knockbackAttack bool) float64 {
	extra := enchantedKnockback / 2
	if knockbackAttack {
		extra += KnockbackAttackBonus
	}
	return extra
}

// AttackSpeed is how fast the player swings, per second: the attribute's
// own base plus the held item's attack-speed modifier — a bare fist's four,
// a sword's one and six tenths.
func AttackSpeed(held protocol.ItemStack) float64 {
	return playerAttackSpeedBase + float64(registry.AttackSpeed(held.ItemID))
}

// AttackStrengthScale is the swing's recovery, read at the half tick: ticks
// since the reset over the attack-speed delay (20/speed ticks), clamped to
// 0..1. An attack, an arm swing or a main-hand item change resets it.
// vanilla: Player.getAttackStrengthScale
func AttackStrengthScale(ticks int, attackSpeed float64) float64 {
	s := (float64(ticks) + 0.5) / (1 / attackSpeed * 20)
	if s < 0 {
		return 0
	}
	if s > 1 {
		return 1
	}
	return s
}

// StabChargeTolerance is the ticks of charge a jab is credited past the
// swing's own count. vanilla: ServerGamePacketListenerImpl.handlePlayerAction (STAB)
const StabChargeTolerance = 5

// CannotAttackWithItem reports whether a swing wound ticks since its reset,
// plus tolerance ticks, is short of the held item's minimum_attack_charge at
// the attack_speed attribute's attackSpeed.
// vanilla: Player.cannotAttackWithItem
func CannotAttackWithItem(held protocol.ItemStack, ticks, tolerance int, attackSpeed float64) bool {
	required := float64(registry.StackMinimumAttackCharge(held))
	return required > 0 && float64(ticks+tolerance)/(1/attackSpeed*20) < required
}

// AttackDamage is a player's hit: attack_damage (fist base plus item and
// effect modifiers) scaled by recovery (0.2 + 0.8*strength²,
// baseDamageScaleFactor), times 1.5 for a falling player's critical past
// criticalStrength.
func AttackDamage(attackDamage float64, falling bool, strength float64) float64 {
	return AttackDamageBonus(attackDamage, 0, falling, strength)
}

// AttackDamageBonus is AttackDamage with the weapon's own bonus (a mace's
// smash) added after the recovery's scaling and before the critical's.
// vanilla: Player.attack, Item.getAttackDamageBonus
func AttackDamageBonus(attackDamage, bonus float64, falling bool, strength float64) float64 {
	damage := attackDamage*(0.2+strength*strength*0.8) + bonus
	if falling && strength > criticalStrength {
		damage *= criticalMultiplier
	}
	return damage
}

// Blow is what Player.attack reads off the attacker to choose a blow's
// feedback.
type Blow struct {
	Strength  float64 // AttackStrengthScale
	Critical  bool    // canCriticalAttack, before the strength is asked
	Sprinting bool
	OnGround  bool
	// KnownSpeedSq is the squared horizontal length of the last movement
	// the client reported, vanilla's getKnownMovement.
	KnownSpeedSq float64
	// Speed is the movement_speed attribute, vanilla's Player.getSpeed.
	Speed      float64
	Sword      bool    // the main hand holds a #minecraft:swords item
	MagicBoost float64 // the enchantments' bonus damage, already scaled
}

// AttackFeedback is the sounds and animations Player.attack adds to a blow.
type AttackFeedback struct {
	Knockback bool // the sprint's knockback sound, played before the hurt
	Critical  bool // the crit sound and the critical animation
	Sweep     bool // the sweep sound and particle
	Strong    bool // strong rather than weak, when neither crit nor sweep
	Magic     bool // the magic critical animation
}

// Feedback picks the blow's feedback. vanilla: Player.attack, isSweepAttack,
// attackVisualEffects
func (b Blow) Feedback() AttackFeedback {
	full := b.Strength > criticalStrength
	knockback := b.Sprinting && full
	critical := full && b.Critical
	return AttackFeedback{
		Knockback: knockback,
		Critical:  critical,
		Sweep: full && !critical && !knockback && b.OnGround &&
			// A sprint never counts: a sprinting full swing is a knockback one.
			b.KnownSpeedSq < math.Pow(float64(float32(b.Speed))*2.5, 2) && b.Sword,
		Strong: full,
		Magic:  b.MagicBoost > 0,
	}
}

// sweepPlainDamage is the hurt a swept body takes where no Sweeping Edge
// raised the share of the blow riding it: doSweepAttack's own 1.0f.
const sweepPlainDamage = 1.0

// SweepDamage is the hurt a sweep lands on one body it reaches: the plain
// one plus the sweeping share of the blow. vanilla: Player.doSweepAttack
func SweepDamage(sweepingRatio, baseDamage float64) float64 {
	return sweepPlainDamage + sweepingRatio*baseDamage
}

// SweepShove is the strength of the push every swept body takes along
// the attacker's facing. vanilla: Player.doSweepAttack, knockback(0.4, ...)
const SweepShove = 0.4

// The sweep's reach: the victim's box grown 1 sideways and 0.25 vertically,
// and the attacker's feet within 3 of a swept body.
// vanilla: Player.doSweepAttack, inflate(1.0, 0.25, 1.0), distanceToSqr < 9.0
const (
	sweepGrowSide     = 1.0
	sweepGrowVertical = 0.25
	sweepMaxFeetSq    = 9.0
)

// SweptVolume is the victim's box grown by the sweep's margins.
// vanilla: Player.doSweepAttack
func SweptVolume(minX, minY, minZ, maxX, maxY, maxZ float64) (min, max [3]float64) {
	return [3]float64{minX - sweepGrowSide, minY - sweepGrowVertical, minZ - sweepGrowSide},
		[3]float64{maxX + sweepGrowSide, maxY + sweepGrowVertical, maxZ + sweepGrowSide}
}

// SweepReaches reports whether a sweep from (ax, ay, az) reaches a body at
// (bx, by, bz): its box overlaps the grown victim box volMin..volMax and the
// attacker's feet are within 3.
// vanilla: Player.doSweepAttack
func SweepReaches(ax, ay, az, bx, by, bz float64, volMin, volMax, bodyMin, bodyMax [3]float64) bool {
	dx, dy, dz := ax-bx, ay-by, az-bz
	return dx*dx+dy*dy+dz*dz < sweepMaxFeetSq && BoxesIntersect(volMin, volMax, bodyMin, bodyMax)
}

// DamageIndicatorCount is how many damage-indicator particles a player's
// blow raises from the health it took: none for two or less.
// vanilla: Player.damageStatsAndHearts
func DamageIndicatorCount(dealt float64) int32 {
	if dealt <= 2 {
		return 0
	}
	return int32(dealt * 0.5)
}

// knockbackDirectionEpsilonSq is the squared horizontal length under which
// vanilla's knockback finds no direction to push along and draws one: the
// 1.0E-5 of LivingEntity.knockback.
const knockbackDirectionEpsilonSq = 1.0e-5

// knockbackDirectionJitter scales the random direction vanilla's knockback
// draws for a hit with no direction: the difference of two draws, times a
// hundredth, per axis.
const knockbackDirectionJitter = 0.01

// KnockbackDirection is the unit horizontal vector away from the attacker,
// or a small random direction from rng where the difference has no length.
// vanilla: LivingEntity.knockback
func KnockbackDirection(dx, dz float64, rng *rand.Rand) (ux, uz float64) {
	for dx*dx+dz*dz < knockbackDirectionEpsilonSq {
		dx = (rng.Float64() - rng.Float64()) * knockbackDirectionJitter
		dz = (rng.Float64() - rng.Float64()) * knockbackDirectionJitter
	}
	distance := math.Hypot(dx, dz)
	return dx / distance, dz / distance
}

// KnockbackFacing is the attacker's yaw as the horizontal unit the extra
// shove pushes along (causeExtraKnockback's sin and -cos, negated by
// knockback).
func KnockbackFacing(yaw float64) (ux, uz float64) {
	rad := yaw * math.Pi / 180
	return -math.Sin(rad), math.Cos(rad)
}

// ExtraKnockbackPush is the push causeExtraKnockback gives a body that is
// not living (a boat, a cart): k along the attacker's yaw (degrees) and a
// fixed tenth up, added to the body's motion whatever it resists.
// vanilla: Player.causeExtraKnockback, Entity.push
func ExtraKnockbackPush(yaw, k float64) (vx, vy, vz float64) {
	ux, uz := KnockbackFacing(yaw)
	return ux * k, 0.1, uz * k
}

// KnockbackExtra is a landed hit's push and lift (vy), reaching only the
// victim's client. It folds vanilla's two knockback calls from rest: the
// hurt's 0.4 along the attacker-to-victim offset (dx, dz; random when zero)
// and the attack's extra along the attacker's yaw, the second halving the
// first's horizontal and lift before adding its own. extra is already half
// the enchantment value; scale is what knockback resistance leaves, applied
// inside each call: a plain blow is 0.4*scale, an enchanted one horizontal
// (0.2*away + extra*facing)*scale and lift min(0.4, (0.2+extra)*scale). An
// airborne victim keeps its vertical velocity, held at rest, so vy is 0.
// vanilla: LivingEntity.knockback, Player.causeExtraKnockback
func KnockbackExtra(dx, dz, yaw, extra, scale float64, onGround bool, rng *rand.Rand) (vx, vy, vz float64) {
	ux, uz := KnockbackDirection(dx, dz, rng)
	if onGround {
		vy = knockbackStrength * scale
	}
	if extra <= 0 {
		return ux * knockbackStrength * scale, vy, uz * knockbackStrength * scale
	}
	ex, ez := KnockbackFacing(yaw)
	vx = (ux*knockbackStrength/2 + ex*extra) * scale
	vz = (uz*knockbackStrength/2 + ez*extra) * scale
	if onGround {
		vy = math.Min(knockbackStrength, (knockbackStrength/2+extra)*scale)
	}
	return vx, vy, vz
}

// KnockbackExtraOnly is the attacker's extra shove alone, for a hit landed
// through the invulnerability window that skipped the default knockback:
// along the yaw at extra's strength, lift the same capped at 0.4; none for
// an airborne victim. scale is what knockback resistance leaves.
// vanilla: Player.causeExtraKnockback, LivingEntity.knockback
func KnockbackExtraOnly(yaw, extra, scale float64, onGround bool) (vx, vy, vz float64) {
	ux, uz := KnockbackFacing(yaw)
	if onGround {
		vy = math.Min(knockbackStrength, extra*scale)
	}
	return ux * extra * scale, vy, uz * extra * scale
}

// arrowPunchPush is the horizontal push each level of Punch adds along the
// shaft's flight, and arrowPunchLift the upward push the same adds.
// vanilla: AbstractArrow.doKnockback
const (
	arrowPunchPush = 0.6
	arrowPunchLift = 0.1
)

// ArrowDefaultKnockback is a whole arrow hit's push along the shaft's
// horizontal flight (flightVX, flightVZ at the hit): 0.4 scaled by knockback
// resistance, lift 0.4 on the ground only; a vertical flight draws the
// random direction from rng.
// vanilla: LivingEntity.knockback, Projectile.calculateHorizontalHurtKnockbackDirection
func ArrowDefaultKnockback(flightVX, flightVZ, scale float64, onGround bool, rng *rand.Rand) (vx, vy, vz float64) {
	if scale <= 0 {
		return 0, 0, 0
	}
	ux, uz := KnockbackDirection(flightVX, flightVZ, rng)
	if onGround {
		vy = knockbackStrength * scale
	}
	return ux * knockbackStrength * scale, vy, uz * knockbackStrength * scale
}

// ArrowPunch is Punch's extra push along the shaft's horizontal flight: 0.6
// per level scaled by knockback resistance, plus 0.1 up unscaled; nothing for
// a vertical flight. punch is EnchantedKnockback's value, 0 without Punch.
// vanilla: AbstractArrow.doKnockback
func ArrowPunch(flightVX, flightVZ, punch, scale float64) (vx, vy, vz float64) {
	if punch <= 0 || scale <= 0 {
		return 0, 0, 0
	}
	horizontal := math.Hypot(flightVX, flightVZ)
	if horizontal <= 1e-9 {
		return 0, 0, 0
	}
	ux, uz := flightVX/horizontal, flightVZ/horizontal
	push := punch * arrowPunchPush * scale
	return ux * push, arrowPunchLift, uz * push
}

// DamageAfterArmour is the armour formula, evaluated in float32 like vanilla:
// the damage times 1 minus clamp(points - damage/(2 + toughness/4),
// points*0.2, 20)/25, so light blows meet the straight points and toughness
// pads heavy ones. points and toughness are the worn pieces' sums from their
// components; the armour's wear is the caller's.
//
// Zero points reduce nothing; the caller checks that before asking.
func DamageAfterArmour(damage, points, toughness float64) float64 {
	return DamageAfterArmourEffective(damage, points, toughness, 0)
}

// DamageAfterArmourEffective is DamageAfterArmour with the attacking weapon's
// armour effectiveness delta (ArmourEffectiveness) added to the armour
// fraction, which is then clamped to 0..1. A zero delta leaves it unclamped.
// vanilla: CombatRules.getDamageAfterAbsorb
func DamageAfterArmourEffective(damage, points, toughness, effectiveness float64) float64 {
	d, p := float32(damage), float32(points)
	tough := 2 + float32(toughness)/4
	effective := min(20, max(p*0.2, p-d/tough))
	fraction := effective / 25
	if effectiveness != 0 {
		fraction = min(1, max(0, fraction+float32(effectiveness)))
	}
	return float64(d * (1 - fraction))
}

// DamageAfterEnchantmentProtection cuts the post-armour damage by the
// protection points, capped at 20, 1/25 each (the armour formula's cap).
// points are DamageProtection's sum over the worn pieces; a blow they do not
// protect against loses nothing.
// vanilla: CombatRules.getDamageAfterMagicAbsorb
func DamageAfterEnchantmentProtection(damage, points float64) float64 {
	if points <= 0 {
		return damage
	}
	return float64(float32(damage) * (1 - min(20, float32(points))/25))
}

// ArmourWearPerPiece is the wear one piece takes from a blow the armour
// absorbed: vanilla's quarter of the damage, at least one.
func ArmourWearPerPiece(damage float64) int32 {
	return int32(math.Max(1, math.Floor(damage/4)))
}

// ArmourBypassed reports whether a damage type is in
// minecraft:bypasses_armor (fall, void, starvation, /kill), hitting at full
// strength. The tag table is the one sent at configuration, so server and
// client agree.
func ArmourBypassed(damageType registry.DamageType) bool {
	return damageType.InTag(registry.DamageTypeTagIDBypassesArmor)
}

// SnowballHitDamage is a snowball's hurt by the target's entity type ID: 3
// on a blaze, 0 otherwise; a zero hurt still lands with knockback and sound.
// vanilla: Snowball.onHitEntity
func SnowballHitDamage(entityType int32) float64 {
	if registry.EntityType(entityType) == registry.EntityTypeBlaze {
		return 3
	}
	return 0
}

// MobAttackAfterDifficulty scales a non-player living attacker's
// scaled_with_difficulty blow before armour: peaceful 0, easy
// min(damage/2+1, damage) so small blows survive, normal unchanged, hard
// 1.5 times.
// vanilla: Player.hurt
func MobAttackAfterDifficulty(damage float64, difficulty uint8) float64 {
	return ScaledWithDifficulty(damage, difficulty)
}

// ScaledWithDifficulty is the same scaling for damage types whose scaling is
// "always" rather than the mob attack's, such as minecraft:explosion: it
// applies whatever caused it. Peaceful 0, easy min(damage/2+1, damage),
// normal unchanged, hard 1.5 times.
// vanilla: Player.actuallyHurt
func ScaledWithDifficulty(damage float64, difficulty uint8) float64 {
	switch difficulty {
	case DifficultyPeaceful:
		return 0
	case DifficultyEasy:
		return math.Min(damage/2+1, damage)
	case DifficultyHard:
		return damage * 3 / 2
	}
	return damage
}

// Killer is who a death names: the display name (a player's, or a mob's
// translated kind) and its main hand, whose custom name switches the message
// to the ".item" form. A nil Killer is a blow with nobody behind it, which
// vanilla words as the victim's own doing.
type Killer struct {
	Name   protocol.Text
	Weapon protocol.ItemStack
}

// weaponText is the killer's named weapon as a death message shows it, the
// display name of the stack. ok is false for no killer or an unnamed weapon.
// vanilla: DamageSource.getLocalizedDeathMessage
func (k *Killer) weaponText() (protocol.Text, bool) {
	if k == nil || k.Weapon.Empty() || k.Weapon.CustomName == nil {
		return protocol.Text{}, false
	}
	return ItemText(k.Weapon, 1), true
}

// DeathMessage builds the death announcement for chat and the respawn
// screen, as vanilla's translation keys so each client localises it. killer
// is nil when there was nobody to name; victim is the player's display name.
func DeathMessage(damageType registry.DamageType, victim protocol.Text, killer *Killer) protocol.Text {
	if damageType.Info().DeathMessage == registry.DeathMessageIntentionalGameDesign {
		// vanilla: CombatTracker.getDeathMessage, intentional_game_design
		return protocol.Text{
			Translate: deathKey(damageType) + ".message",
			With:      []protocol.Text{victim, badRespawnLink(deathKey(damageType))},
		}
	}
	if killer == nil && damageType.Info().DeathMessage != registry.DeathMessageFallVariants {
		// With neither a causing entity nor a kill credit the key gets only
		// the victim, whatever the type.
		// vanilla: DamageSource.getLocalizedDeathMessage
		return protocol.Text{Translate: deathKey(damageType), With: []protocol.Text{victim}}
	}
	named := victim
	if killer != nil {
		named = killer.Name
	}
	switch damageType {
	case DamageTypeArrow, DamageTypeTrident,
		DamageTypePlayerAttack, DamageTypeMobAttack, DamageTypeMobAttackNoAggro, DamageTypeMaceSmash,
		DamageTypeThorns, DamageTypeSting:
		// The damage type's message_id (arrow, trident, player or mob) names
		// the killer; a custom-named held item swaps in the ".item" variant.
		// vanilla: DamageSource.getLocalizedDeathMessage
		key := deathKey(damageType)
		if item, ok := killer.weaponText(); ok {
			return protocol.Text{
				Translate: key + ".item",
				With:      []protocol.Text{victim, named, item},
			}
		}
		return protocol.Text{
			Translate: key,
			With:      []protocol.Text{victim, named},
		}
	case DamageTypeIndirectMagic:
		// Magic with no direct source: "was killed by ... using magic", the
		// indirect source named.
		return protocol.Text{
			Translate: deathKey(damageType),
			With:      []protocol.Text{victim, named},
		}
	case DamageTypeOutOfWorld:
		return protocol.Text{
			Translate: deathKey(damageType),
			With:      []protocol.Text{victim},
		}
	case DamageTypeExplosion, DamageTypePlayerExplosion:
		// "death.attack.explosion", or ".player" naming the killer (a creeper
		// among them). player_explosion's message_id is "explosion.player"
		// and answers the same way: an owned TNT names the owner. An explosion
		// with only a direct entity (an unowned TNT) keeps the plain message_id.
		// vanilla: DamageSource.getLocalizedDeathMessage
		if item, ok := killer.weaponText(); ok {
			return protocol.Text{
				Translate: "death.attack.explosion.player.item",
				With:      []protocol.Text{victim, named, item},
			}
		}
		if killer != nil && damageType == DamageTypeExplosion {
			return protocol.Text{
				Translate: "death.attack.explosion",
				With:      []protocol.Text{victim, named},
			}
		}
		if killer != nil {
			return protocol.Text{
				Translate: "death.attack.explosion.player",
				With:      []protocol.Text{victim, named},
			}
		}
		return protocol.Text{
			Translate: "death.attack.explosion",
			With:      []protocol.Text{victim},
		}
	case DamageTypeFireworks:
		// Fireworks: "went off with a bang", naming the player who fired it
		// where there is one.
		if item, ok := killer.weaponText(); ok {
			return protocol.Text{
				Translate: "death.attack.fireworks.item",
				With:      []protocol.Text{victim, named, item},
			}
		}
		if killer != nil {
			return protocol.Text{
				Translate: "death.attack.fireworks.player",
				With:      []protocol.Text{victim, named},
			}
		}
		return protocol.Text{
			Translate: "death.attack.fireworks",
			With:      []protocol.Text{victim},
		}
	case DamageTypeFallingAnvil, DamageTypeFallingBlock, DamageTypeFallingStalactite:
		// A falling block's message_id (anvil, plain or stalactite). Vanilla
		// names the victim and the falling body, but only the victim shows.
		key := deathKey(damageType)
		return protocol.Text{
			Translate: key,
			With:      []protocol.Text{victim, named},
		}
	case DamageTypeStarve:
		return protocol.Text{
			Translate: deathKey(damageType),
			With:      []protocol.Text{victim},
		}
	case DamageTypeGenericKill:
		return protocol.Text{
			Translate: deathKey(damageType),
			With:      []protocol.Text{victim},
		}
	case DamageTypeInFire, DamageTypeOnFire, DamageTypeLava,
		DamageTypeDrown, DamageTypeInWall, DamageTypeCactus,
		DamageTypeHotFloor, DamageTypeSweetBerryBush, DamageTypeCampfire,
		DamageTypeMagic, DamageTypeWither, DamageTypeFreeze, DamageTypeOutsideBorder,
		DamageTypeStalagmite, DamageTypeGeneric:
		// Environmental message_ids (burn, lava, drowning, wall, cactus,
		// magma, berry bush, poison, wither) name only the victim, unless
		// /damage put an entity behind it.
		key := deathKey(damageType)
		return namedDeath(key, victim, killer)
	default:
		if damageType.Info().DeathMessage == registry.DeathMessageFallVariants {
			return protocol.Text{
				Translate: "death.fell.accident.generic",
				With:      []protocol.Text{victim},
			}
		}
		return namedDeath(deathKey(damageType), victim, killer)
	}
}

// namedDeath is a death message for a type that names nobody on its own; an
// entity behind the source (only /damage by gives one) is named, with its
// named weapon.
// vanilla: DamageSource.getLocalizedDeathMessage
func namedDeath(key string, victim protocol.Text, killer *Killer) protocol.Text {
	if killer == nil {
		return protocol.Text{Translate: key, With: []protocol.Text{victim}}
	}
	if item, ok := killer.weaponText(); ok {
		return protocol.Text{Translate: key + ".item", With: []protocol.Text{victim, killer.Name, item}}
	}
	return protocol.Text{Translate: key, With: []protocol.Text{victim, killer.Name}}
}

// KillCreditMessage is the ".player" wording of a sourceless damage type's
// death message, naming the kill credit.
// vanilla: DamageSource.getLocalizedDeathMessage
func KillCreditMessage(damageType registry.DamageType, victim protocol.Text, credit *Killer) protocol.Text {
	key := DeathMessage(damageType, victim, nil).Translate
	if isFallVariantType(damageType) {
		// The type's message_id; the fall wordings are not its key.
		key = deathKey(damageType)
	}
	return protocol.Text{Translate: key + ".player", With: []protocol.Text{victim, credit.Name}}
}

// badRespawnLink is the bracketed bug-tracker link the bad-respawn-point
// death names. vanilla: CombatTracker.INTENTIONAL_GAME_DESIGN_STYLE
func badRespawnLink(msgKey string) protocol.Text {
	const issue = "MCPE-28723"
	return protocol.Text{
		Translate: "chat.square_brackets",
		With:      []protocol.Text{{Translate: msgKey + ".link"}},
		ClickURL:  "https://bugs.mojang.com/browse/" + issue,
		HoverText: &protocol.Text{Text: issue},
	}
}

// IronGolemLift is the extra vertical velocity after a golem blow hurts.
// vanilla: IronGolem.doHurtTarget
func IronGolemLift(resistance float64) float64 {
	return float64(float32(0.4)) * math.Max(0, 1-resistance)
}
