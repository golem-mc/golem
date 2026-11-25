package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
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
		scaled = rules.ScaledWithDifficulty(scaled, c.theWorld().Difficulty())
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
	var hitFrom *entity.HitSource
	causing := origin.Cause
	if causing == nil {
		causing = origin.Direct
	}
	// Read player position before taking mobsMu to preserve lock order.
	if p, ok := causing.(*client); ok {
		pos := p.player.Position()
		hs := entity.PlayerHitSource(p.sized(entity.Player{
			X: pos.X, Y: pos.Y, Z: pos.Z,
			Yaw: float64(pos.Yaw), Pitch: float64(pos.Pitch),
		}))
		hitFrom = &hs
	}
	resolveHit := func() *entity.HitSource {
		if hitFrom != nil {
			return hitFrom
		}
		snap, ok := causing.(*entitySnapshot)
		if !ok {
			return nil
		}
		if mob, ok := snap.ref.(*mobEntity); ok {
			hs := mob.mob.HitSource()
			return &hs
		}
		pos := snap.pos
		eye := pos.Y + float64(float32(snap.box[4]-snap.box[1])*0.85)
		return &entity.HitSource{
			X: pos.X, Y: pos.Y, Z: pos.Z, Eye: eye,
			Yaw: float64(pos.Yaw), Pitch: float64(pos.Pitch),
		}
	}
	return e.srv.hurtMobTyped(ref.w, ref.mob, float64(amount), damageType, cause, directID, "", false, resolveHit)
}

// hurtMobTyped hurts m with a typed blow from cause (nil for none), then tells
// its watchers of the new health and the Damage Event (direct is an entity ID
// + 1). It reports whether the hurt landed. A landed blow notes directType,
// the entity type of its direct entity, for the kill criterion. With
// spareInvulnerable a mob with the invulnerable flag takes nothing even from a
// creative cause. hitFrom, if set, runs under the mob lock.
// vanilla: LivingEntity.hurtServer, broadcastDamageEvent
func (s *Server) hurtMobTyped(w *world.World, m *entity.Mob, amount float64, damageType registry.DamageType, cause *damageCause,
	direct int32, directType string, spareInvulnerable bool, hitFrom func() *entity.HitSource) bool {
	s.mobsMu.Lock()
	ent := s.herdOf(w).book[m]
	if ent == nil || m.Dead || spareInvulnerable && m.Invulnerable {
		s.mobsMu.Unlock()
		return false
	}
	var hs *entity.HitSource
	if hitFrom != nil {
		hs = hitFrom()
	}
	// With no source the shove's direction has no length, so knockback
	// picks a random one. vanilla: LivingEntity.dealDefaultKnockback
	src := damageCause{x: m.X, z: m.Z}
	if cause != nil {
		src = *cause
	}
	landed, softened := m.HurtByCommand(amount, damageType, src.entityID, src.player, src.creative, src.x, src.z, hs)
	var health []byte
	var sends []mobSend
	if landed {
		if directType != "" {
			m.NoteBlowDirect(directType)
		}
		health = ent.syncHealth()
		if !softened {
			sends = mobHurtToldBy(w, m, damageType, causeEntityID(cause), direct, damageEventPos(cause))
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
