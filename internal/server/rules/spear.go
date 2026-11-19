package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// StabKnockback is the shove every jab along its look deals before the
// enchantments' own. vanilla: Player.stabAttack
const StabKnockback = 0.4

// StabDamage is a jab's damage: the attack damage scaled by the swing's
// charge, with the enchantments' bonus scaled by the charge alone; and that
// bonus, for the magic critical. No critical multiplier rides a jab.
// vanilla: Player.stabAttack
func StabDamage(held protocol.ItemStack, ctx *EnchContext, attackDamage, strength float64) (damage, magic float64) {
	magic = (EnchantedDamage(held, ctx, attackDamage) - attackDamage) * strength
	return AttackDamage(attackDamage, false, strength) + magic, magic
}

// StabCountsAsHit reports whether a jab on a body counts as a hit, wearing
// the item and playing the hit sound: it does when the hurt landed or the
// weapon deals knockback. vanilla: Player.stabAttack
func StabCountsAsHit(hurtLanded, dealsKnockback bool) bool {
	return hurtLanded || dealsKnockback
}

// PiercingAftermath is what one post_piercing_attack effect does to the user.
type PiercingAftermath struct {
	// Wear is the item damage before Unbreaking, 0 for none.
	Wear int32
	// Exhaustion is the food exhaustion added.
	Exhaustion float64
	// Impulse is the velocity added; HasImpulse says it applies.
	Impulse    [3]float64
	HasImpulse bool
	// Sound is played at the user; ID -1 with no name means none.
	Sound protocol.SoundHolder
	// Level and Effect are the enchantment's level and whole effect, for the
	// data-pack-only parts.
	Level  int32
	Effect registry.EnchEntityEffect
}

// PostPiercingAttack lists the aftermath of every post_piercing_attack effect
// on the weapon whose requirements the user passes (Lunge's dash).
// vanilla: EnchantmentHelper.doPostPiercingAttackEffects
func PostPiercingAttack(weapon protocol.ItemStack, user EnchUser, yaw, pitch float32) []PiercingAftermath {
	var out []PiercingAftermath
	ctx := EnchContext{User: user}
	for _, ench := range enchantsOn(weapon) {
		for _, spawned := range ench.def.Effects.PostPiercingAttack {
			if !enchCondition(spawned.Requires, &ctx) {
				continue
			}
			e := spawned.Effect
			a := PiercingAftermath{Sound: protocol.SoundHolder{ID: -1}, Level: ench.level, Effect: e, Exhaustion: enchLevelValue(e.ApplyExhaustion, ench.level)}
			a.Wear = int32(enchLevelValue(e.ChangeItemDamage, ench.level))
			if i := e.ApplyImpulse; i != nil {
				a.Impulse = LookImpulse(yaw, pitch, i.CoordinateScale, enchLevelValue(i.Magnitude, ench.level))
				a.HasImpulse = true
			}
			if s := e.PlaySound; s != nil {
				sound := s.Sound
				if len(s.Sounds) > 0 {
					sound = s.Sounds[min(max(int(ench.level)-1, 0), len(s.Sounds)-1)]
				}
				a.Sound = sound
			}
			out = append(out, a)
		}
	}
	return out
}

// LookImpulse is the look vector scaled per axis by scale (not renormalised),
// then by magnitude.
// vanilla: ApplyEntityImpulse.apply
func LookImpulse(yaw, pitch float32, scale [3]float32, magnitude float64) [3]float64 {
	const rad = math.Pi / 180
	x := -math.Sin(float64(yaw)*rad) * math.Cos(float64(pitch)*rad)
	y := -math.Sin(float64(pitch) * rad)
	z := math.Cos(float64(yaw)*rad) * math.Cos(float64(pitch)*rad)
	return [3]float64{
		x * float64(scale[0]) * magnitude,
		y * float64(scale[1]) * magnitude,
		z * float64(scale[2]) * magnitude,
	}
}
