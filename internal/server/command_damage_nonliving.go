package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// vanilla: DamageCommand.damage, Entity.hurtServer
func (e *entitySnapshot) damageNonliving(amount float32, damageType registry.DamageType, origin command.DamageOrigin, cause *damageCause) bool {
	s := e.srv
	causing := origin.Cause
	if causing == nil {
		causing = origin.Direct
	}
	creative := cause != nil && cause.creative
	bypass := creative || rules.InvulnerabilityBypassed(damageType)
	mob := false
	noBuild := false
	if target, ok := causing.(*entitySnapshot); ok {
		_, mob = target.ref.(*mobEntity)
	}
	if target, ok := causing.(command.Target); ok {
		noBuild = target.GameModeNumber() == protocol.GameModeAdventure || target.GameModeNumber() == protocol.GameModeSpectator
	}
	var causerID int32
	if causing != nil {
		causerID = causing.EntityID()
	}
	switch ref := e.ref.(type) {
	case *endCrystalEntity:
		if causing != nil && causing.EntityType() == "minecraft:ender_dragon" {
			return false
		}
		if _, ok := s.askNonlivingDamage(ref.w, ref.uuid, damageType, cause, amount); !ok {
			return false
		}
		s.endCrystalsMu.Lock()
		if ref.crystal.Dead || (ref.crystal.Invulnerable && !bypass) {
			s.endCrystalsMu.Unlock()
			return false
		}
		s.removeEndCrystalLocked(ref)
		s.endCrystalsMu.Unlock()
		var causeID int32
		var causeUUID uuid.UUID
		if causing != nil {
			causeID, causeUUID = causing.EntityID(), causing.UUID()
		}
		s.destroyEndCrystal(ref, causeID)
		if !damageType.InTag(registry.DamageTypeTagIDIsExplosion) {
			s.explodeEndCrystal(ref, causeID, "", causeUUID)
		}
		return true
	case *boatEntity:
		landed, metadata := s.boatHurt(ref, float64(amount), damageType, creative, causerID)
		if metadata != nil {
			s.broadcastWorld(ref.w, protocol.IDPlayEntityMetadata, metadata)
		}
		return landed
	case *minecartEntity:
		hit := entity.TNTCartHit{DamageType: damageType, OwnerID: causeEntityID(cause),
			Ignites: damageType.InTag(registry.DamageTypeTagIDIsFire) || damageType.InTag(registry.DamageTypeTagIDIsExplosion)}
		if direct, ok := origin.Direct.(*entitySnapshot); ok {
			s.arrowsMu.Lock()
			switch projectile := direct.ref.(type) {
			case *arrowEntity:
				a := projectile.arrow
				hit.Ignites, hit.FireArrow = a.FireTicks > 0, a.FireTicks > 0
				hit.FireArrowSpeedSq = a.VX*a.VX + a.VY*a.VY + a.VZ*a.VZ
			case *thrownEntity:
				hit.Ignites = projectile.throwable.Kind.Burns()
			}
			s.arrowsMu.Unlock()
		}
		return s.minecartHurtBy(ref, ref.w, float64(amount), creative, hit)
	case *itemEntity:
		if mob && !ref.w.BoolRule(world.RuleMobGriefing) {
			return false
		}
		var allowed bool
		if amount, allowed = s.askNonlivingDamage(ref.w, ref.uuid, damageType, cause, amount); !allowed || amount <= 0 && s.entityDamageListened() {
			return false
		}
		s.itemsMu.Lock()
		if mob && !ref.w.BoolRule(world.RuleMobGriefing) {
			s.itemsMu.Unlock()
			return false
		}
		landed := ref.item.Hurt(amount, damageType, creative)
		dead := ref.item.Dead
		spill := spillOf(ref)
		if landed && dead {
			ref.killed = true
			s.entityDestroyed(ref.uuid)
		}
		ix, iy, iz := ref.item.FeetPosition()
		s.itemsMu.Unlock()
		if landed {
			s.gameEvent(ref.w, rules.GameEventEntityDamage, ix, iy, iz, s.attackerEventContext(causerID))
		}
		if landed && dead {
			s.broadcastWorld(ref.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{ref.item.ID}))
			s.spillContents([]itemSpill{spill})
		}
		return landed
	case *armorStandEntity:
		return s.damageArmorStand(ref, damageType, creative, noBuild, mob, causerID)
	case *paintingEntity:
		return s.hitPainting(paintingHit{w: ref.w, paintingID: ref.id, dropless: creative, mob: mob, damageType: damageType})
	case *frameEntity:
		return s.damageFrame(ref, damageType, creative, mob, cause)
	case *knotEntity:
		tell, ok := s.knotTellByEntityID(ref.w, ref.id)
		if !ok || (tell.invulnerable && !bypass) || (mob && !ref.w.BoolRule(world.RuleMobGriefing)) {
			return false
		}
		s.downKnot(tell)
		return true
	case *fixtureEntity:
		if ref.kind != entity.FixtureCushion || noBuild || (s.cushionInvulnerable(ref) && !bypass) || (mob && !ref.w.BoolRule(world.RuleMobGriefing)) {
			return false
		}
		return s.breakCushion(ref, cushionBreakBy(creative))
	case *orbEntity:
		s.orbsMu.Lock()
		if ref.orb.Dead {
			s.orbsMu.Unlock()
			return false
		}
		landed, dead := ref.orb.Hurt(float64(amount), bypass)
		s.orbsMu.Unlock()
		if dead {
			s.killOrb(ref)
		}
		return landed
	case *thrownEntity:
		s.arrowsMu.Lock()
		if ref.throwable.Dead || ref.throwable.Kind != entity.ThrownShulkerBullet {
			s.arrowsMu.Unlock()
			return false
		}
		ref.throwable.Dead = true
		x, y, z := ref.throwable.X, ref.throwable.Y, ref.throwable.Z
		s.entityDestroyed(ref.uuid)
		s.arrowsMu.Unlock()
		s.bulletStruck(ref.w, ref.throwable.ID, x, y, z)
		return true
	}
	return false
}

// attacker is the hurting entity's ID, 0 for none; a shaking punch or a
// surviving hurt posts the entity_damage event.
// vanilla: ArmorStand.hurtServer, ArmorStand.causeDamage
func (s *Server) damageArmorStand(ref *armorStandEntity, damageType registry.DamageType, creative, noBuild, mob bool, attacker int32) bool {
	s.armorStandsMu.Lock()
	stand := ref.stand
	if stand.Dead {
		s.armorStandsMu.Unlock()
		return false
	}
	health, fire := stand.Health, stand.FireTicks
	hit := stand.HurtByCommand(int64(s.ticksRun.Load()), damageType, creative, noBuild, mob && !ref.w.BoolRule(world.RuleMobGriefing))
	dead := stand.Dead
	var metadata []byte
	if dead {
		s.removeArmorStandLocked(ref)
	} else if health != stand.Health || fire != stand.FireTicks {
		metadata = armorStandMetadataPayload(ref, true)
		markArmorStandMetadata(ref)
	}
	posted := hit.Shake || (!dead && health != stand.Health)
	sx, sy, sz := stand.X, stand.Y, stand.Z
	s.armorStandsMu.Unlock()
	if posted {
		s.gameEvent(ref.w, rules.GameEventEntityDamage, sx, sy, sz, s.attackerEventContext(attacker))
	}
	if dead {
		if hit.Particles || hit.Equipment {
			s.breakArmorStandTail(ref, armorStandBreak{item: hit.Item, equipment: hit.Equipment, particles: hit.Particles})
		} else {
			s.broadcastWorld(ref.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{stand.ID}))
		}
	}
	if hit.Shake {
		s.broadcastWorld(ref.w, protocol.IDPlayEntityEvent, entityEventPayload(stand.ID, protocol.EntityStatusArmorStandShake))
	}
	if metadata != nil {
		s.broadcastWorld(ref.w, protocol.IDPlayEntityMetadata, metadata)
	}
	return hit.Landed
}

// vanilla: ItemFrame.hurtServer, BlockAttachedEntity.hurtServer
func (s *Server) damageFrame(ref *frameEntity, damageType registry.DamageType, creative, mob bool, cause *damageCause) bool {
	return s.hurtFrame(arrowFrameHit{w: ref.w, frameID: ref.id, damageType: damageType, dropless: creative, mob: mob, cause: cause},
		damageType.InTag(registry.DamageTypeTagIDIsExplosion))
}
