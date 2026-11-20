package server

import (
	"github.com/golem-mc/golem/registry"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// The sounds of a mace's smash.
const (
	soundMaceSmashAir         = registry.SoundItemMaceSmashAir
	soundMaceSmashGround      = registry.SoundItemMaceSmashGround
	soundMaceSmashGroundHeavy = registry.SoundItemMaceSmashGroundHeavy
)

// smashFall is the fall this player's blow with held smashes with, and
// whether it smashes. vanilla: MaceItem.canSmashAttack
func (c *client) smashFall(held protocol.ItemStack) (float64, bool) {
	fall := c.player.Vitals().FallDistance
	return fall, rules.CanSmashAttack(held, fall, c.fallFlying.Load())
}

// smashBonus is a smash's added damage after fall, density read against
// ctx's victim. vanilla: MaceItem.getAttackDamageBonus
func smashBonus(held protocol.ItemStack, ctx *rules.EnchContext, fall float64) float64 {
	return rules.SmashDamageBonus(fall, rules.SmashDamagePerFallenBlock(held, ctx))
}

// smashVictim is the body a landed smash struck.
type smashVictim struct {
	id        int32
	x, y, z   float64
	onGround  bool
	halfWidth float64
	// damage is the blow's damage before armor; 0 for a stand.
	damage float64
	// player is a struck player's UUID, uuid.Nil for a mob or stand.
	player uuid.UUID
}

// landSmash is a landed smash's aftermath: attacker bounce, smash sound,
// dust at the victim and push on nearby bodies.
// vanilla: MaceItem.hurtEnemy
func (c *client) landSmash(w *world.World, fall float64, v smashVictim) {
	// The bounce resets the horizontal motion along with the lift.
	c.setVelocity(w, 0, rules.SmashAttackerLift, 0)
	// An impact height at or below the attacker's stays; else it is here.
	// vanilla: MaceItem.calculateImpactPosition
	at := c.player.Position()
	c.player.UpdateVitals(func(vt *player.Vitals) {
		impact := [3]float64{at.X, at.Y, at.Z}
		if vt.ImpulseImpact != nil && vt.ImpulseImpact[1] <= at.Y {
			impact = *vt.ImpulseImpact
		}
		rules.SetImpulseImpact(vt, impact)
		if v.onGround {
			vt.SpawnExtraParticlesOnFall = true
		}
	})
	sound := soundMaceSmashAir
	if v.onGround {
		sound = soundMaceSmashGround
		if rules.HeavySmash(fall) {
			sound = soundMaceSmashGroundHeavy
		}
	}
	c.attackSound(sound)
	bx, by, bz := rules.StandingBlock(w, v.x, v.y, v.z, v.halfWidth)
	c.srv.broadcastLevelEvent(w,
		protocol.WorldEventMaceSmash, bx, by, bz, rules.SmashParticles)

	if progress := c.advancements.Load(); progress != nil {
		progress.PlayerHurtSmash(c.heldItem(protocol.HandMain).ItemID, v.damage, time.Now())
	}
	c.smashKnockback(w, fall, v)
}

// finishMeleeItemEffects runs post-attack explosions before a smash clears the fall.
// vanilla: Player.itemAttackInteraction
func (c *client) finishMeleeItemEffects(held protocol.ItemStack, ctx rules.EnchContext, fall float64, smash bool) {
	ctx.DirectAttackerFallDistance = fall
	ctx.DirectAttackerFlying = c.fallFlying.Load() || c.flying.Load()
	w := c.theWorld()
	for _, e := range rules.PostAttackExplosions(held, &ctx) {
		at := c.player.Position()
		c.srv.runExplosion(blast{w: w, x: at.X, y: at.Y, z: at.Z, radius: e.Radius,
			interaction: e.Interaction, harmless: true, knockbackScale: e.Knockback,
			burst: true, blockResist: windChargeResistanceOf(w), rng: c.srv.levelRng})
	}
	c.resetSmashFall(smash)
}

func (c *client) resetSmashFall(smash bool) {
	if smash {
		c.player.UpdateVitals(func(vt *player.Vitals) { vt.FallDistance = 0 })
	}
}

// smashKnockback pushes every living body near the victim away, except the
// attacker, victim, the attacker's allies, the struck player's tamed pets,
// spectators, creative flyers and marker armor stands.
// vanilla: MaceItem.knockback, knockbackPredicate
func (c *client) smashKnockback(w *world.World, fall float64, v smashVictim) {
	c.srv.mu.RLock()
	var inWorld []*client
	for _, o := range c.srv.clients {
		if o != c && o.theWorld() == w {
			inWorld = append(inWorld, o)
		}
	}
	c.srv.mu.RUnlock()
	myTeam, allies := c.alliesAmong(inWorld)
	var players []*client
	for _, o := range inWorld {
		if o.player.EntityID == v.id || allies[o.player.UUID] {
			continue
		}
		mode := o.player.GameMode()
		if mode == protocol.GameModeSpectator ||
			mode == protocol.GameModeCreative && o.flying.Load() {
			continue
		}
		players = append(players, o)
	}
	for _, o := range players {
		at := o.player.Position()
		var scale float64
		o.player.Attributes(func(a *player.Attributes) { scale = rules.KnockbackScale(a) })
		kx, ky, kz, ok := rules.SmashKnockback(at.X-v.x, at.Y-v.y, at.Z-v.z, fall, 1-scale)
		if !ok {
			continue
		}
		kx, ky, kz, ok = c.srv.knockbackEvent(o.pluginSelf(), c.player.UUID, kx, ky, kz)
		if !ok {
			continue
		}
		o.addVelocity(w, kx, ky, kz)
	}

	var mobPushes []*mobKnockback
	c.srv.mobsMu.Lock()
	for m, ent := range c.srv.herdOf(w).book {
		if m.ID == v.id || m.Dead || alliedTo(c.player.UUID, myTeam, allies, m) ||
			v.player != uuid.Nil && m.Tamable.Tame && m.Tamable.Owner == v.player {
			continue
		}
		if kx, ky, kz, ok := rules.SmashKnockback(m.X-v.x, m.Y-v.y, m.Z-v.z, fall, m.KnockbackResistance()); ok {
			oldVX, oldVY, oldVZ, oldImpulse := m.VX, m.VY, m.VZ, m.Impulse
			m.Push(kx, ky, kz)
			if kb := c.srv.holdMobKnockback(ent, c.player.UUID, oldVX, oldVY, oldVZ, oldImpulse); kb != nil {
				mobPushes = append(mobPushes, kb)
			}
		}
	}
	c.srv.mobsMu.Unlock()
	for _, kb := range mobPushes {
		c.srv.applyMobKnockback(kb)
	}

	c.srv.armorStandsMu.Lock()
	for _, ent := range c.srv.armorStands {
		st := ent.stand
		if ent.w != w || st.ID == v.id || st.Dead || st.Marker {
			continue
		}
		if kx, ky, kz, ok := rules.SmashKnockback(st.X-v.x, st.Y-v.y, st.Z-v.z, fall, 0); ok {
			st.Push(kx, ky, kz)
		}
	}
	c.srv.armorStandsMu.Unlock()
}
