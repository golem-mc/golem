package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// putProjectileFields writes the inherited Projectile data before the subclass.
func putProjectileFields(tag nbt.CompoundTag, owner uuid.UUID, leftOwner, shot bool) {
	if owner != uuid.Nil {
		tag.Set("Owner", nbt.UUIDToIntArray(owner))
	}
	if leftOwner {
		tag.Set("LeftOwner", boolTag(true))
	}
	tag.Set("HasBeenShot", boolTag(shot))
}

// thrownSavesItem reports whether a thrown body's NBT includes an item stack.
// vanilla: Projectile.addAdditionalSaveData, Fireball.addAdditionalSaveData, ThrowableItemProjectile.addAdditionalSaveData
func thrownSavesItem(kind entity.ThrownKind) bool {
	switch kind {
	case entity.ThrownDragonFireball, entity.ThrownWindCharge, entity.ThrownBreezeWindCharge,
		entity.ThrownWitherSkull, entity.ThrownShulkerBullet, entity.ThrownLlamaSpit:
		return false
	}
	return true
}

// thrownDefaultItem is the item a saved body without an Item is drawn as;
// for throwable kinds the entity id names it.
// vanilla: Fireball.getDefaultItem, ThrowableItemProjectile.getDefaultItem
func thrownDefaultItem(kind entity.ThrownKind, id string) string {
	switch kind {
	case entity.ThrownWindCharge, entity.ThrownBreezeWindCharge:
		return "minecraft:wind_charge"
	case entity.ThrownDragonFireball, entity.ThrownSmallFireball, entity.ThrownLargeFireball:
		return "minecraft:fire_charge"
	case entity.ThrownWitherSkull:
		return "minecraft:wither_skeleton_skull"
	case entity.ThrownShulkerBullet:
		return "minecraft:shulker_shell"
	}
	return id
}

// putBulletFields writes a shulker bullet's flight state and target UUID.
// vanilla: ShulkerBullet.addAdditionalSaveData
func putBulletFields(tag nbt.CompoundTag, ent *thrownEntity) {
	t := ent.throwable
	if t.Kind != entity.ThrownShulkerBullet || t.Bullet == nil {
		return
	}
	steps, dir, dx, dy, dz := t.BulletSave()
	if ent.targetUUID != uuid.Nil {
		tag.Set("Target", nbt.UUIDToIntArray(ent.targetUUID))
	}
	if dir >= 0 {
		tag.Set("Dir", nbt.Byte(uint8(dir)))
	}
	tag.Set("Steps", nbt.Int(steps))
	tag.Set("TXD", nbt.Double(dx))
	tag.Set("TYD", nbt.Double(dy))
	tag.Set("TZD", nbt.Double(dz))
}

// readBulletFields restores a shulker bullet's flight state and returns its
// target UUID, resolved at registration; other kinds are untouched.
// vanilla: ShulkerBullet.readAdditionalSaveData
func readBulletFields(t *entity.Thrown, compound nbt.Compound) uuid.UUID {
	if t.Kind != entity.ThrownShulkerBullet {
		return uuid.Nil
	}
	dir := int8(-1)
	if b, ok := nbt.Get[nbt.Byte](compound, "Dir"); ok {
		dir = int8(b)
	}
	t.RestoreBullet(0, intFieldOr(compound, "Steps", 0), dir,
		nbt.DoubleFieldOr(compound, "TXD", 0), nbt.DoubleFieldOr(compound, "TYD", 0), nbt.DoubleFieldOr(compound, "TZD", 0))
	return entityUUIDFieldOf(compound, "Target")
}

// resolveBulletTarget resolves a woken bullet's target UUID to an entity
// ID. It takes the player and mob locks, so callers must hold neither.
func (s *Server) resolveBulletTarget(it *thrownEntity) {
	t := it.throwable
	if t.Kind != entity.ThrownShulkerBullet || t.Bullet == nil || it.targetUUID == uuid.Nil || t.BulletTargetID() != 0 {
		return
	}
	t.SetBulletTarget(s.resolveOwner(it.targetUUID))
}

// entityIdentityOf is the UUID of the player or mob with entity ID id, or
// the nil UUID. It takes the player and mob locks.
func (s *Server) entityIdentityOf(id int32) uuid.UUID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clients {
		if c.player.EntityID == id {
			return c.player.UUID
		}
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for _, ent := range dim.mobs.book {
			if ent.mob.ID == id {
				return ent.uuid
			}
		}
	}
	return uuid.Nil
}
