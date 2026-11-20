package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

type preparedAttack struct {
	baseDamage, strength, damage, fall float64
	context                            rules.EnchContext
	feedback                           rules.AttackFeedback
	smash                              bool
}

// prepareAttack spends the cooldown only after the caller's target and reach checks.
// Enchantment damage scales with recovery, independently of the critical bonus.
// vanilla: Player.attack
func (c *client) prepareAttack(held protocol.ItemStack, targetType int32, living bool, victim func() *rules.LootEntity) (preparedAttack, bool) {
	b := preparedAttack{baseDamage: c.attackDamage()}
	b.strength = rules.AttackStrengthScale(int(c.attackStrengthTicks.Load()), c.attackSpeed())
	c.attackStrengthTicks.Store(0)
	b.context = c.attackEnchContext(meleeDamageType(held), targetType, victim)
	plain := rules.AttackDamage(b.baseDamage, false, 1)
	bonus := rules.EnchantedDamage(held, &b.context, plain) - plain
	critical := c.canCriticalAttack(living)
	b.damage = rules.AttackDamage(b.baseDamage, critical, b.strength)
	magic := bonus * b.strength
	// A zero-damage blow must not reach hurt or trigger its side effects.
	if b.damage <= 0 && magic <= 0 {
		return b, false
	}
	if living {
		b.fall, b.smash = c.smashFall(held)
		if b.smash {
			b.damage = rules.AttackDamageBonus(b.baseDamage, smashBonus(held, &b.context, b.fall), critical, b.strength)
		}
	}
	b.damage += magic
	b.feedback = c.blowFeedback(held, b.strength, critical, magic)
	return b, true
}

// bindLootBodies lets a data-pack effect's loot conditions read the blow's
// bodies: the victim, and the player as both attacker and direct attacker.
// The origin is the victim's position, the player's when the victim has none.
// vanilla: Enchantment.damageContext
func (c *client) bindLootBodies(ctx *rules.EnchContext, victim func() *rules.LootEntity) {
	ctx.BodyOf = victim
	if victim == nil {
		typed := &rules.LootEntity{Type: registry.EntityType(ctx.Victim).Name()}
		ctx.BodyOf = func() *rules.LootEntity { return typed }
	}
	self := func() *rules.LootEntity {
		e := c.LootEntity()
		return &e
	}
	ctx.AttackerOf, ctx.DirectAttackerOf = self, self
	ctx.LootWorld = func(loot *rules.LootContext) {
		pos := c.player.Position()
		c.srv.fillBlowWorld(c.theWorld(), loot, [3]float64{pos.X, pos.Y, pos.Z})
	}
}
