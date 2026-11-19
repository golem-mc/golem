package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// damageCauseOf is the cause for a /damage source entity or position, nil
// for neither: the at clause's position, else the carrying entity.
// vanilla: DamageSource.getSourcePosition, DamageSource.getLocalizedDeathMessage
func damageCauseOf(origin command.DamageOrigin, damageType registry.DamageType) *damageCause {
	direct, causing := origin.Direct, origin.Cause
	if causing == nil {
		causing = direct
	}
	if origin.At == nil && causing == nil {
		return nil
	}
	cause := &damageCause{posOnly: causing == nil, eventPos: origin.At != nil}
	if causing != nil {
		cause.entityID = causing.EntityID()
		cause.directID = direct.EntityID() + 1
		cause.killer = &rules.Killer{Name: causing.DisplayName()}
		if p, ok := causing.(command.Target); ok {
			cause.player, cause.creative = true, p.GameModeNumber() == protocol.GameModeCreative
		}
		// The main-hand item of a living cause names the weapon.
		if held, ok := causing.ItemIn(command.ItemSlotMainHand); ok {
			cause.killer.Weapon = held
		}
	}
	if direct != nil {
		cause.weapon, _ = direct.ItemIn(command.ItemSlotMainHand)
		holder, living := direct.(command.AttributeTarget)
		kind, _ := registry.EntityTypeByName(direct.EntityType())
		cause.shove = living && holder.HasAttributes() && shovesOnBlock(kind) &&
			!damageType.InTag(registry.DamageTypeTagIDIsProjectile)
		if living && !damageType.InTag(registry.DamageTypeTagIDIsProjectile) {
			cause.disableSeconds = livingDisableSeconds(kind, cause.weapon)
		}
	}
	at := origin.At
	if at == nil && direct != nil {
		p := direct.Position()
		at = &p
	}
	if at != nil {
		cause.x, cause.y, cause.z = at.X, at.Y, at.Z
	}
	return cause
}

// Damage applies /damage to the player through the normal hurt, so armour,
// absorption, the window and the type's death message apply. Difficulty-scaling
// types are scaled first; an amount scaled to nothing
// hurts nothing. vanilla: DamageCommand.damage, Player.hurtServer
func (c *client) Damage(amount float32, damageType registry.DamageType, origin command.DamageOrigin) bool {
	scaled := float64(amount)
	causing := origin.Cause
	if causing == nil {
		causing = origin.Direct
	}
	livingNonPlayer := false
	if causing != nil {
		_, isPlayer := causing.(command.Target)
		holder, living := causing.(command.AttributeTarget)
		livingNonPlayer = !isPlayer && living && holder.HasAttributes()
	}
	if rules.DamageScales(damageType, livingNonPlayer) {
		scaled = rules.ScaledWithDifficulty(scaled, c.srv.world.Difficulty())
	}
	if scaled == 0 {
		return false
	}
	cause := damageCauseOf(origin, damageType)
	landed, softened := c.hurt(scaled, damageType, cause)
	// A whole hit shoves the victim away from the source unless no_knockback,
	// in a random direction when there is no source; a blocked hit's
	// knockback stays inside hurt. vanilla: LivingEntity.dealDefaultKnockback
	if landed && !softened && !damageType.InTag(registry.DamageTypeTagIDNoKnockback) {
		from := c.player.Position()
		if cause != nil {
			from.X, from.Z = cause.x, cause.z
		}
		c.knockbackFromPoint(c.theWorld(), from.X, from.Y, from.Z, syncVelocityNow)
	}
	return landed
}

// Damage routes /damage through the target kind's hurt behavior.
// vanilla: DamageCommand.damage, DamageSource.getSourcePosition
func (e *entitySnapshot) Damage(amount float32, damageType registry.DamageType, origin command.DamageOrigin) bool {
	cause := damageCauseOf(origin, damageType)
	var directID int32
	if cause != nil {
		directID = cause.directID
	}
	if ref, ok := e.ref.(*mannequinEntity); ok {
		creative := false
		if cause != nil {
			creative = cause.creative
		}
		from := e.Position()
		if cause != nil {
			from.X, from.Z = cause.x, cause.z
		}
		return e.srv.hurtMannequinBy(ref, float64(amount), damageType, causeEntityID(cause), directID, damageEventPos(cause), creative,
			&mannequinKnockback{x: from.X, z: from.Z})
	}
	ref, ok := e.ref.(*mobEntity)
	if !ok {
		return e.damageNonliving(amount, damageType, origin, cause)
	}
	s, w := e.srv, ref.w
	s.mobsMu.Lock()
	m := ref.mob
	if s.herdOf(w).book[m] == nil || m.Dead {
		s.mobsMu.Unlock()
		return false
	}
	// With no source the shove's direction has no length, so knockback
	// picks a random one. vanilla: LivingEntity.dealDefaultKnockback
	src := damageCause{x: m.X, z: m.Z}
	if cause != nil {
		src = *cause
	}
	landed, softened := m.HurtByCommand(float64(amount), damageType, src.entityID, src.player, src.creative, src.x, src.z)
	var health []byte
	var sends []mobSend
	if landed {
		health = ref.syncHealth()
		if !softened {
			// vanilla: LivingEntity.hurtServer broadcastDamageEvent
			sends = mobHurtToldBy(w, m, damageType, causeEntityID(cause), directID, damageEventPos(cause))
		}
	}
	s.mobsMu.Unlock()
	if !landed {
		return false
	}
	s.broadcastWorld(w, protocol.IDPlayEntityMetadata, health)
	s.sendMobSends(sends)
	return true
}
