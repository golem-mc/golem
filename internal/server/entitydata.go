package server

import (
	"github.com/golem-mc/golem/internal/server/effect"
	"maps"
	"math"
	"math/rand"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Entity-file compounds for items, arrows, orbs, primed TNT, falling
// blocks, paintings and item frames, saved beside the mobs. Shapes follow
// vanilla's Entity.saveWithoutId plus each class's addAdditionalSaveData, checked
// against real vanilla saves. Fields this server has no state for are
// omitted; vanilla's readers default them.
//
// The *OfTag readers return nil for an entity they cannot load faithfully;
// the caller then keeps its compound in the file untouched.

// Entity type ids, the "id" field a loader dispatches on. A boat's own id
// is the kind's name — minecraft:oak_boat and its kin — dispatched through
// entity.BoatKind.
const (
	itemEntityID          = "minecraft:item"
	arrowEntityID         = "minecraft:arrow"
	spectralArrowEntityID = "minecraft:spectral_arrow"
	tridentEntityID       = "minecraft:trident"
	orbEntityID           = "minecraft:experience_orb"
	tntEntityID           = "minecraft:tnt"
	fallingEntityID       = "minecraft:falling_block"
	paintingEntityID      = "minecraft:painting"
	frameEntityID         = "minecraft:item_frame"
	glowFrameEntityID     = "minecraft:glow_item_frame"
	armorStandEntityID    = "minecraft:armor_stand"
	cloudEntityID         = "minecraft:area_effect_cloud"
	fireworkEntityID      = "minecraft:firework_rocket"
	eyeEntityID           = "minecraft:eye_of_ender"
	bobberEntityID        = "minecraft:fishing_bobber"
	knotEntityID          = "minecraft:leash_knot"
)

// Defaults vanilla's readers use for absent fields, and fixed values for
// fields this server has no state for. Health is 5 for items and orbs; Air
// 300 is a body never underwater; Fire 0 is a body not burning.
const (
	defaultItemHealth = 5
	defaultOrbHealth  = 5
	defaultAirSupply  = 300
	restingFire       = 0
	// defaultArrowSound is getDefaultHitGroundSoundEvent; every arrow here
	// picks up as a plain arrow.
	defaultArrowSound      = registry.SoundEntityArrowHit
	crossbowHitSound       = registry.SoundItemCrossbowHit
	defaultArrowPickupItem = "minecraft:arrow"
	// defaultTridentSound is the trident's getDefaultHitGroundSoundEvent;
	// a thrown trident here always picks up as itself.
	defaultTridentSound = registry.SoundItemTridentHitGround
	// vanilla: FallingBlockEntity.readAdditionalSaveData
	defaultFallingFallHurtMax = 40
)

// entityBaseTag writes the common Entity.saveWithoutId fields this server
// has values for. fall_distance is a double and always 0 here
// (fallingTag overrides it). NoGravity is written only when set, the shape
// the entity files of a plain body carry.
func entityBaseTag(id string, x, y, z, vx, vy, vz float64, yaw, pitch float32,
	identity uuid.UUID, onGround bool, portalCooldown, fire int, noGravity bool) nbt.Compound {
	tag := nbt.Compound{}
	writeEntityBaseTag(tag, id, x, y, z, vx, vy, vz, yaw, pitch, identity, onGround, portalCooldown, fire, 0, noGravity)
	return tag
}

// writeEntityBaseTag writes Entity.saveAsPassenger and saveWithoutId in insertion order.
func writeEntityBaseTag(tag nbt.CompoundTag, id string, x, y, z, vx, vy, vz float64, yaw, pitch float32,
	identity uuid.UUID, onGround bool, portalCooldown, fire, invulnerableTime int, noGravity bool) {
	if id != "" {
		tag.Set("id", nbt.String(id))
	}
	tag.Set("Pos", vec3Tag(x, y, z))
	tag.Set("Motion", vec3Tag(vx, vy, vz))
	tag.Set("Rotation", nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(yaw), nbt.Float(pitch)}})
	tag.Set("fall_distance", nbt.Double(0))
	tag.Set("Fire", nbt.Short(clampToShort(fire)))
	tag.Set("Air", nbt.Short(defaultAirSupply))
	tag.Set("OnGround", boolTag(onGround))
	tag.Set("Invulnerable", boolTag(false))
	tag.Set("PortalCooldown", nbt.Int(portalCooldown))
	if invulnerableTime > 0 {
		tag.Set("invulnerable_time", nbt.Int(invulnerableTime))
	}
	tag.Set("UUID", nbt.UUIDToIntArray(identity))
	if noGravity {
		tag.Set("NoGravity", boolTag(true))
	}
}

// orderedEntityBaseTag starts a saveAsPassenger or saveWithoutId compound.
func orderedEntityBaseTag(id string, x, y, z, vx, vy, vz float64, yaw, pitch float32,
	identity uuid.UUID, onGround bool, portalCooldown, fire, invulnerableTime int) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(24)
	writeEntityBaseTag(tag, id, x, y, z, vx, vy, vz, yaw, pitch, identity, onGround, portalCooldown, fire, invulnerableTime, false)
	return tag
}

// putEntityOptions finishes Entity.saveWithoutId before the kind's additional data.
func putEntityOptions(tag nbt.CompoundTag, custom customName, silent, noGravity bool, look entityLook, tags nbt.StringSet) {
	custom.put(tag)
	if silent {
		tag.Set("Silent", boolTag(true))
	}
	if noGravity {
		tag.Set("NoGravity", boolTag(true))
	}
	look.put(tag)
	look.putBase(tag)
	putTags(tag, tags.Values())
	look.putData(tag)
}

// summonTag builds the compound a summon's kind reads: the tag's own
// entries, the entity id vanilla's command puts in itself, the command's
// position over the tag's, and a fresh UUID where the tag named none.
// vanilla: SummonCommand.createEntity, Entity.load
func summonTag(tag nbt.Compound, id string, x, y, z float64) nbt.Compound {
	compound := maps.Clone(tag)
	if compound == nil {
		compound = nbt.Compound{}
	}
	compound["id"] = nbt.String(id)
	compound["Pos"] = nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
		nbt.Double(x), nbt.Double(y), nbt.Double(z),
	}}
	if _, live := entityUUIDOfTag(compound); !live {
		compound["UUID"] = nbt.UUIDToIntArray(uuid.New())
	}
	return compound
}

// itemTag writes an item entity's compound. Files store the bottom of the
// box but the simulation holds the item at its centre, so Y is lowered by
// ItemHalfSize.
func itemTag(ent *itemEntity) *nbt.OrderedCompound {
	return itemTagAs(ent, itemEntityID)
}

func itemTagAs(ent *itemEntity, id string) *nbt.OrderedCompound {
	it := ent.item
	tag := orderedEntityBaseTag(id,
		it.X, it.Y-entity.ItemHalfSize, it.Z,
		it.VX, it.VY, it.VZ,
		0, 0, ent.uuid, it.OnGround, ent.portal.Cooldown, restingFire, it.InvulnerableTime)
	putEntityOptions(tag, ent.custom, ent.look.silent, it.NoGravity, ent.look, ent.tags)
	tag.Set("Invulnerable", boolTag(it.Invulnerable))
	tag.Set("Health", nbt.Short(clampToShort(it.Health)))
	tag.Set("Age", nbt.Short(clampToShort(it.Age)))
	tag.Set("PickupDelay", nbt.Short(clampToShort(it.PickupDelay)))
	if ent.thrower != uuid.Nil {
		tag.Set("Thrower", nbt.UUIDToIntArray(ent.thrower))
	}
	if it.Target != nil {
		tag.Set("Owner", nbt.UUIDToIntArray(*it.Target))
	}
	tag.Set("Item", registry.StackTag(it.Stack))
	putSpawnReason(tag, id, ent.spawnReason)
	putOrigin(tag, id, ent.origin)
	return tag
}

// arrowTypeID is the entity id an arrow saves under: the spectral one for a
// spectral shaft, the trident one for a thrown trident, whose flights the
// plain arrow's entity carries.
func arrowTypeID(ent *arrowEntity) string {
	switch ent.entityType {
	case protocol.EntityTypeSpectralArrow:
		return spectralArrowEntityID
	case protocol.EntityTypeTrident:
		return tridentEntityID
	}
	return arrowEntityID
}

// arrowTag writes an arrow's compound — a thrown trident's own for one of
// the trident kind, its stack under "item", the pickup item, and its blow's
// dealt mark kept. inBlockState is vanilla's lastState,
// the state the shaft hit, written from the arrow and omitted when it has
// none, as storeNullable omits null. Owner is written only when the
// shooter's UUID is known.
func arrowTag(ent *arrowEntity) *nbt.OrderedCompound {
	return arrowTagAs(ent, arrowTypeID(ent))
}

func arrowTagAs(ent *arrowEntity, id string) *nbt.OrderedCompound {
	a := ent.arrow
	var item nbt.CompoundTag = arrowPickupTag(a.Effect)
	if !a.Trident && !a.PickupStack.Empty() {
		item = registry.StackTag(a.PickupStack)
	}
	if a.Trident {
		item = registry.StackTag(a.Weapon)
	}
	tag := orderedEntityBaseTag(id,
		a.X, a.Y, a.Z, a.VX, a.VY, a.VZ,
		a.Yaw, a.Pitch, ent.uuid, false, ent.portal.Cooldown, a.FireTicks, 0)
	putEntityOptions(tag, ent.custom, ent.look.silent, a.NoGravity, ent.look, ent.tags)
	tag.Set("Invulnerable", boolTag(ent.look.invulnerable))
	putProjectileFields(tag, ent.ownerUUID, a.LeftOwner(), ent.shot)
	tag.Set("life", nbt.Short(clampToShort(a.Life)))
	if _, _, ok := registry.BlockStateName(a.LastState); ok {
		tag.Set("inBlockState", registry.BlockStateCodecTag(a.LastState))
	}
	tag.Set("shake", nbt.Byte(clampToByte(a.ShakeTicks)))
	tag.Set("inGround", boolTag(a.InGround))
	tag.Set("pickup", nbt.Byte(uint8(a.Pickup)))
	tag.Set("damage", nbt.Double(a.BaseDamage))
	tag.Set("crit", boolTag(a.Crit))
	tag.Set("PierceLevel", nbt.Byte(clampToByte(int(a.PierceLevel))))
	tag.Set("SoundEvent", nbt.String(a.HitSound.Name()))
	tag.Set("item", item)
	// Omitted when empty (a skeleton's arrow). A trident's own stack
	// rides "item"; vanilla's ThrownTrident carries no weapon beside it.
	if !a.Weapon.Empty() && !a.Trident {
		tag.Set("weapon", registry.StackTag(a.Weapon))
	}
	if ent.entityType == protocol.EntityTypeSpectralArrow {
		tag.Set("Duration", nbt.Int(a.GlowTicks))
	}
	if a.Trident {
		// vanilla: ThrownTrident.addAdditionalSaveData
		tag.Set("DealtDamage", boolTag(a.DealtDamage()))
	}
	return tag
}

// arrowStuckReach is how far from a stuck arrow its block can be: onHitBlock
// leaves it 0.05 out of the face, plus margin for rounding.
const arrowStuckReach = 0.06

// arrowLastBlock finds a stuck arrow's block (vanilla's lastState): the
// cell it is in if not air (fences, slabs), else the first non-air cell
// within arrowStuckReach.
func arrowLastBlock(w *world.World, a *entity.Arrow) (name, props string, ok bool) {
	offsets := [...]float64{0, -arrowStuckReach, arrowStuckReach}
	for _, dy := range offsets {
		for _, dx := range offsets {
			for _, dz := range offsets {
				state := w.BlockAt(int32(math.Floor(a.X+dx)), int32(math.Floor(a.Y+dy)), int32(math.Floor(a.Z+dz)))
				if n, p, known := registry.BlockStateName(state); known && registry.BlockOf(state) != registry.BlockAir {
					return n, p, true
				}
			}
		}
	}
	return "", "", false
}

// orbTag writes an experience orb's compound.
func orbTag(ent *orbEntity) *nbt.OrderedCompound {
	return orbTagAs(ent, orbEntityID)
}

func orbTagAs(ent *orbEntity, id string) *nbt.OrderedCompound {
	o := ent.orb
	tag := orderedEntityBaseTag(id,
		o.X, o.Y, o.Z, o.VX, o.VY, o.VZ,
		0, ent.look.pitch, ent.uuid, o.OnGround, ent.portal.Cooldown, restingFire, 0)
	putEntityOptions(tag, ent.custom, ent.look.silent, o.NoGravity, ent.look, ent.tags)
	tag.Set("Invulnerable", boolTag(ent.look.invulnerable))
	tag.Set("Health", nbt.Short(clampToShort(o.Health)))
	tag.Set("Age", nbt.Short(clampToShort(o.Age)))
	tag.Set("Value", nbt.Short(clampToShort(o.Value)))
	tag.Set("Count", nbt.Int(max(o.Count, 1)))
	return tag
}

// tntTag writes a primed TNT's compound; explosion_power only when it is not
// the default 4, as vanilla.
func tntTag(ent *tntEntity) *nbt.OrderedCompound {
	return tntTagAs(ent, tntEntityID)
}

func tntTagAs(ent *tntEntity, id string) *nbt.OrderedCompound {
	t := ent.tnt
	tag := orderedEntityBaseTag(id,
		t.X, t.Y, t.Z, t.VX, t.VY, t.VZ,
		0, 0, ent.uuid, t.OnGround, ent.portal.Cooldown, restingFire, 0)
	putEntityOptions(tag, ent.custom, t.Silent, t.NoGravity, ent.look, ent.tags)
	tag.Set("Invulnerable", boolTag(ent.look.invulnerable))
	tag.Set("fuse", nbt.Short(clampToShort(t.Fuse)))
	if _, _, ok := registry.BlockStateName(t.BlockState); ok {
		tag.Set("block_state", registry.BlockStateCodecTag(t.BlockState))
	}
	if t.Power != entity.TNTExplosionPower {
		tag.Set("explosion_power", nbt.Float(t.Power))
	}
	if ent.ownerUUID != uuid.Nil {
		tag.Set("owner", nbt.UUIDToIntArray(ent.ownerUUID))
	}
	putOrigin(tag, id, ent.origin)
	return tag
}

// fallingTag writes a falling block's compound. TileEntityData is omitted:
// this server keeps no block entity on a falling block.
func fallingTag(ent *fallingEntity) *nbt.OrderedCompound {
	return fallingTagAs(ent, fallingEntityID)
}

func fallingTagAs(ent *fallingEntity, id string) *nbt.OrderedCompound {
	f := ent.fb
	tag := orderedEntityBaseTag(id,
		f.X, f.Y, f.Z, f.VX, f.VY, f.VZ,
		0, 0, ent.uuid, f.OnGround, ent.portal.Cooldown, restingFire, 0)
	putEntityOptions(tag, ent.custom, ent.look.silent, f.NoGravity, ent.look, ent.tags)
	tag.Set("Invulnerable", boolTag(ent.look.invulnerable))
	tag.Set("BlockState", registry.BlockStateCodecTag(f.BlockState))
	tag.Set("Time", nbt.Int(f.Time))
	tag.Set("DropItem", boolTag(f.DropItem))
	tag.Set("HurtEntities", boolTag(f.HurtEntities))
	tag.Set("FallHurtAmount", nbt.Float(float32(f.FallHurtAmount)))
	tag.Set("FallHurtMax", nbt.Int(f.FallHurtMax))
	tag.Set("CancelDrop", boolTag(f.CancelDrop))
	tag.Set("fall_distance", nbt.Double(f.FallDistance))
	putOrigin(tag, id, ent.origin)
	return tag
}

// thrownTag writes a thrown projectile's compound: the base fields, the
// stack it flies as, and the throw's own flags and owner.
// vanilla: ThrowableItemProjectile.addAdditionalSaveData, Projectile.addAdditionalSaveData
func thrownTag(ent *thrownEntity) *nbt.OrderedCompound {
	return thrownTagAs(ent, registry.EntityTypes[ent.throwable.Kind.EntityType()])
}

func thrownTagAs(ent *thrownEntity, id string) *nbt.OrderedCompound {
	t := ent.throwable
	tag := orderedEntityBaseTag(id,
		t.X, t.Y, t.Z, t.VX, t.VY, t.VZ,
		t.Yaw, t.Pitch, ent.uuid, false, ent.portal.Cooldown, restingFire, 0)
	putEntityOptions(tag, ent.custom, t.Silent, t.NoGravity, ent.look, ent.tags)
	tag.Set("Invulnerable", boolTag(ent.invulnerable))
	putProjectileFields(tag, ent.ownerUUID, t.LeftOwner(), ent.shot)
	if t.Kind.IsFireball() || t.Kind.IsWindCharge() {
		// vanilla: AbstractHurtingProjectile.addAdditionalSaveData
		tag.Set("acceleration_power", nbt.Double(t.AccelerationPower))
	}
	if thrownSavesItem(t.Kind) {
		tag.Set("Item", registry.StackTag(ent.stack))
	}
	putFireballFields(tag, t)
	putBulletFields(tag, ent)
	return tag
}

// applyThrownTag reads a thrown projectile's compound onto a live body:
// the position, the motion, the angles, the stack, the throw's flags and
// the tags. A /data write runs it whole; a saved pearl's wake has read the
// place and speed it resumes from already and runs applyThrownRest for the
// rest. An absent position keeps the value already on the body.
// vanilla: ThrowableItemProjectile.readAdditionalSaveData, Projectile.readAdditionalSaveData
func applyThrownTag(it *thrownEntity, compound nbt.Compound) {
	t := it.throwable
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		t.X, t.Y, t.Z = x, y, z
	}
	t.VX, t.VY, t.VZ = entityMotionOfTag(compound)
	it.ownerUUID = entityUUIDFieldOf(compound, "Owner")
	applyThrownRest(it, compound)
}

// applyThrownRest reads the parts of a thrown body's compound that are not
// the place it stands or the speed it flies at: the angles, the stack, the
// no-gravity flag and the tags.
func applyThrownRest(it *thrownEntity, compound nbt.Compound) {
	t := it.throwable
	it.shot = nbt.BoolField(compound, "HasBeenShot")
	t.Yaw, t.Pitch = entityRotationOfTag(compound)
	if stack, ok := storedItemStackOf(compound); ok && !stack.Empty() {
		it.stack = stack
	}
	t.NoGravity = nbt.BoolField(compound, "NoGravity")
	t.Silent = nbt.BoolField(compound, "Silent")
	it.invulnerable = nbt.BoolField(compound, "Invulnerable")
	readFireballFields(t, compound)
	it.tags.Reset(tagsOfTag(compound))
	it.custom = customNameOfTag(compound)
	it.look = entityLookOf(compound).dataOnly()
}

// putFireballFields writes the flags after Fireball's Item in subclass order.
// vanilla: LargeFireball.addAdditionalSaveData, WitherSkull.addAdditionalSaveData
func putFireballFields(tag nbt.CompoundTag, t *entity.Thrown) {
	if !t.Kind.IsFireball() && !t.Kind.IsWindCharge() {
		return
	}
	switch t.Kind {
	case entity.ThrownLargeFireball:
		tag.Set("ExplosionPower", nbt.Byte(uint8(t.ExplosionPower)))
	case entity.ThrownWitherSkull:
		tag.Set("dangerous", boolTag(t.Dangerous))
	}
}

// readFireballFields reads putFireballFields back, the defaults standing
// for an absent field.
// vanilla: AbstractHurtingProjectile.readAdditionalSaveData, LargeFireball.readAdditionalSaveData, WitherSkull.readAdditionalSaveData
func readFireballFields(t *entity.Thrown, compound nbt.Compound) {
	if !t.Kind.IsFireball() && !t.Kind.IsWindCharge() {
		return
	}
	t.AccelerationPower = nbt.DoubleFieldOr(compound, "acceleration_power", entity.FireballAcceleration)
	switch t.Kind {
	case entity.ThrownLargeFireball:
		t.ExplosionPower = int(int8(intFieldOr(compound, "ExplosionPower", 1)))
	case entity.ThrownWitherSkull:
		t.Dangerous = nbt.BoolField(compound, "dangerous")
	}
}

// cloudTag writes an area effect cloud's compound: the shared saved fields,
// then the cloud's own — its age and its lifetime, the wait before its rain
// starts and the gap between its touches, what a use takes off its duration
// and its radius, its shrink per tick and its radius now — and the potion
// contents its shatter carried with the lingering scale, and the thrower the
// potion belonged to.
// vanilla: AreaEffectCloud.addAdditionalSaveData
func cloudTag(ent *cloudEntity) *nbt.OrderedCompound {
	return cloudTagAs(ent, cloudEntityID)
}

func cloudTagAs(ent *cloudEntity, id string) *nbt.OrderedCompound {
	c := ent.cloud
	tag := orderedEntityBaseTag(id,
		c.X, c.Y, c.Z, 0, 0, 0,
		0, 0, ent.uuid, false, 0, restingFire, 0)
	putEntityOptions(tag, ent.custom, ent.look.silent, ent.look.noGravity, ent.look, ent.tags)
	tag.Set("Invulnerable", boolTag(ent.look.invulnerable))
	tag.Set("Age", nbt.Int(c.TickCount))
	tag.Set("Duration", nbt.Int(c.Duration))
	tag.Set("WaitTime", nbt.Int(c.WaitTime))
	tag.Set("ReapplicationDelay", nbt.Int(c.Reapplication))
	tag.Set("DurationOnUse", nbt.Int(c.DurationOnUse))
	tag.Set("RadiusOnUse", nbt.Float(c.RadiusOnUse))
	tag.Set("RadiusPerTick", nbt.Float(c.RadiusPerTick()))
	tag.Set("Radius", nbt.Float(c.Radius()))
	if ent.customParticle != nil {
		tag.Set("custom_particle", ent.customParticle)
	}
	if ent.ownerUUID != uuid.Nil {
		tag.Set("Owner", nbt.UUIDToIntArray(ent.ownerUUID))
	}
	if contents, ok := registry.PotionContentsTag(ent.contents); ok && len(contents.Entries()) != 0 {
		tag.Set("potion_contents", contents)
	}
	if ent.durationScale != 1 {
		tag.Set("potion_duration_scale", nbt.Float(ent.durationScale))
	}
	return tag
}

// fireworkTag writes a firework rocket's compound: the shared saved fields,
// then the rocket's own — the ticks it has lived, its total life and the
// stack it flies as.
// vanilla: FireworkRocketEntity.addAdditionalSaveData, Projectile.addAdditionalSaveData
func fireworkTag(ent *fireworkEntity) *nbt.OrderedCompound {
	return fireworkTagAs(ent, fireworkEntityID)
}

func fireworkTagAs(ent *fireworkEntity, id string) *nbt.OrderedCompound {
	f := ent.rocket
	tag := orderedEntityBaseTag(id,
		f.X, f.Y, f.Z, f.VX, f.VY, f.VZ,
		f.Yaw, f.Pitch, ent.uuid, false, ent.portal.Cooldown, restingFire, 0)
	putEntityOptions(tag, ent.custom, false, false, entityLook{}, ent.tags)
	putProjectileFields(tag, ent.ownerUUID, f.LeftOwner(), ent.shot)
	tag.Set("Life", nbt.Int(f.Life))
	tag.Set("LifeTime", nbt.Int(f.Lifetime))
	tag.Set("FireworksItem", registry.StackTag(ent.stack))
	tag.Set("ShotAtAngle", boolTag(f.ShotAtAngle))
	return tag
}

// bobberTag writes a fishing bobber's compound: the shared saved fields,
// then the fall distance the hook's own physics accumulated. The body it is
// caught in and its water countdowns are left out: they have no key names in
// this server's references.
func bobberTag(ent *bobberEntity) *nbt.OrderedCompound {
	return bobberTagAs(ent, bobberEntityID)
}

func bobberTagAs(ent *bobberEntity, id string) *nbt.OrderedCompound {
	h := ent.hook
	tag := orderedEntityBaseTag(id,
		h.X, h.Y, h.Z, h.VX, h.VY, h.VZ,
		h.Yaw, h.Pitch, ent.uuid, false, 0, restingFire, 0)
	putEntityOptions(tag, ent.custom, false, false, entityLook{}, ent.tags)
	tag.Set("fall_distance", nbt.Double(h.FallDistance()))
	return tag
}

// knotTag writes a leash knot's compound: the shared saved fields alone. The
// knot's own held position is the base position already written, not a key
// of its own.
// vanilla: LeashFenceKnotEntity.addAdditionalSaveData
func knotTag(ent *knotEntity) *nbt.OrderedCompound {
	return knotTagAs(ent, knotEntityID)
}

func knotTagAs(ent *knotEntity, id string) *nbt.OrderedCompound {
	x, y, z := float64(knotX(ent)), float64(knotY(ent)), float64(knotZ(ent))
	tag := orderedEntityBaseTag(id,
		x, y, z, 0, 0, 0,
		0, 0, ent.uuid, false, 0, restingFire, 0)
	putEntityOptions(tag, ent.custom, false, false, entityLook{}, ent.tags)
	tag.Set("Invulnerable", boolTag(ent.invulnerable))
	return tag
}

// boatTag writes a boat's compound. The rocking a blow wound rides nowhere
// — vanilla's boat runs it in its synched data alone — and a chest boat's
// cargo rides as a container's Items, slots and all.
func boatTag(ent *boatEntity) *nbt.OrderedCompound {
	return boatTagAs(ent, ent.kind.Name())
}

func boatTagAs(ent *boatEntity, id string) *nbt.OrderedCompound {
	b := ent.boat
	tag := orderedEntityBaseTag(id,
		b.X, b.Y, b.Z, b.VX, b.VY, b.VZ,
		b.Yaw, ent.look.pitch, ent.uuid, b.OnGround, ent.portal.Cooldown, b.FireTicks, 0)
	putEntityOptions(tag, ent.custom, ent.look.silent, b.NoGravity, ent.look, ent.tags)
	tag.Set("Invulnerable", boolTag(b.Invulnerable))
	putLeash(tag, ent.leash)
	if ent.lootTable != "" {
		// An unrolled table stands for the contents.
		// vanilla: ContainerEntity.addChestVehicleSaveData
		tag.Set("LootTable", nbt.String(ent.lootTable))
		if ent.lootSeed != 0 {
			tag.Set("LootTableSeed", nbt.Long(ent.lootSeed))
		}
	} else if ent.cargo != nil {
		registry.PutSlottedItems(tag, ent.cargo, true)
	}
	if len(ent.carriedPassengers) > 0 {
		// vanilla: Entity.addSaveData
		tag.Set("Passengers", nbt.List{Elem: nbt.TypeCompound, Items: ent.carriedPassengers})
	}
	putSpawnReason(tag, id, ent.spawnReason)
	putOrigin(tag, id, ent.origin)
	return tag
}

// itemOfTag loads an item entity, or returns nil when the stack has a
// component this server does not model or an unknown item, or the position
// is out of the world. Absent fields keep a fresh spawn's values.
func itemOfTag(s *Server, w *world.World, compound nbt.Compound) *itemEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	if string(id) != itemEntityID {
		return nil
	}
	stack, ok := storedItemStackOf(compound)
	if !ok {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	vx, vy, vz := entityMotionOfTag(compound)
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil // without a UUID it cannot be told from others
	}
	// Files store the bottom of the box; the item runs on its centre.
	eid := s.nextEntity()
	it := &itemEntity{
		item:   entity.NewItem(eid, stack, x, y+entity.ItemHalfSize, z, vx, vy, vz, int(shortFieldOf(compound, "PickupDelay")), s.entitySeed(eid)),
		uuid:   identity,
		w:      w,
		sentVX: vx, sentVY: vy, sentVZ: vz,
	}
	applyItemTag(it, compound)
	return it
}

// applyItemTag reads an item entity's compound onto a live body — the
// stack, the position, the motion, the counters, the flags and the owner.
// A saved item's wake and a /data write share it; an absent Item — the
// stack a write removed — leaves the item empty, which the write discards.
// vanilla: ItemEntity.readAdditionalSaveData
func applyItemTag(it *itemEntity, compound nbt.Compound) {
	if stack, ok := storedItemStackOf(compound); ok {
		it.item.Stack = stack
	} else if _, hasItem := compound["Item"]; !hasItem {
		it.item.Stack = protocol.ItemStack{}
	}
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		// Files store the bottom of the box; the item runs on its centre.
		it.item.X, it.item.Y, it.item.Z = x, y+entity.ItemHalfSize, z
	}
	it.item.VX, it.item.VY, it.item.VZ = entityMotionOfTag(compound)
	it.item.PickupDelay = int(shortFieldOf(compound, "PickupDelay"))
	applyItemTagCounters(it.item, compound)
	if ground, ok := nbt.Get[nbt.Byte](compound, "OnGround"); ok {
		it.item.OnGround = ground != 0
	}
	it.item.NoGravity = nbt.BoolField(compound, "NoGravity")
	it.portal.Cooldown = portalCooldownOf(compound)
	it.tags.Reset(tagsOfTag(compound))
	it.custom = customNameOfTag(compound)
	it.look = entityLookOf(compound)
	it.thrower = entityUUIDFieldOf(compound, "Thrower")
	readSpawnReason(&it.spawnReason, compound)
	if it.w != nil {
		readOrigin(&it.origin, it.w.DimensionName(), compound)
	}
}

// applyItemTagCounters reads an item's age, health, invulnerable time and
// owner off its compound: an absent Health defaults to five, while an
// explicit zero or less is kept, so the item dies on its first hurt. A summon
// and a saved item's wake share it.
// vanilla: ItemEntity.readAdditionalSaveData, Entity.load
func applyItemTagCounters(it *entity.Item, compound nbt.Compound) {
	it.Invulnerable = nbt.BoolField(compound, "Invulnerable")
	it.Age = int(shortFieldOf(compound, "Age"))
	it.Health = shortFieldOr(compound, "Health", defaultItemHealth)
	invulnerable, _ := nbt.IntField(compound, "invulnerable_time")
	it.InvulnerableTime = int(invulnerable)
	if owner, ok := nbt.UUIDField(compound, "Owner"); ok {
		it.Target = &owner
	} else {
		it.Target = nil
	}
}

// arrowOfTag loads an arrow — or a thrown trident, the load a trident's
// own id takes — and returns nil when its pickup item cannot be read
// whole (a tipped arrow's components would be lost).
func arrowOfTag(s *Server, w *world.World, compound nbt.Compound) *arrowEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	entityType := int32(protocol.EntityTypeArrow)
	trident := string(id) == tridentEntityID
	switch string(id) {
	case arrowEntityID:
	case spectralArrowEntityID:
		entityType = protocol.EntityTypeSpectralArrow
	case tridentEntityID:
		entityType = protocol.EntityTypeTrident
	default:
		return nil
	}
	var stack protocol.ItemStack
	if trident {
		// The trident's own stack is its pickup item; a file with none
		// or one no codec reads is the plain item it defaults to.
		stack = protocol.ItemStack{ItemID: int32(registry.ItemTrident), Count: 1}
		if item, ok := nbt.Get[nbt.Compound](compound, "item"); ok {
			if read, ok := registry.StoredStack(item); ok {
				stack = read
			} else {
				return nil
			}
		}
		stack.Count = 1
	} else if _, _, ok := arrowPickupStackOf(compound); !ok {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	vx, vy, vz := entityMotionOfTag(compound)
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}

	// A trident's own baseDamage is the arrow default too: ThrownTrident
	// never sets it, so the file's 2.0 is read back, and the hit's eight
	// comes from the constant.
	damage := entity.ArrowBaseDamage
	if d, ok := nbt.Get[nbt.Double](compound, "damage"); ok && d >= 0 {
		damage = float64(d)
	}
	pickup := entity.PickupDisallowed
	if p, ok := nbt.Get[nbt.Byte](compound, "pickup"); ok && p <= 2 {
		pickup = entity.ArrowPickup(p)
	}
	// The owner's entity ID is resolved later by resolveArrowOwner.
	eid := s.nextEntity()
	var a *entity.Arrow
	if trident {
		a = entity.NewTrident(eid, x, y, z, vx, vy, vz, pickup, 0, stack, s.entitySeed(eid), entityItemEnchContext(w, x, y, z, protocol.EntityTypeTrident))
	} else {
		a = entity.NewArrow(eid, x, y, z, vx, vy, vz,
			nbt.BoolField(compound, "crit"), damage, pickup, 0, s.entitySeed(eid))
	}
	it := &arrowEntity{
		arrow:      a,
		uuid:       identity,
		w:          w,
		entityType: entityType,
	}
	applyArrowTag(it, compound)
	return it
}

// applyArrowTag reads an arrow's — or a thrown trident's — compound onto a
// live shaft: the position, the motion, the flight's counters and flags, the
// block it hit, and its pickup and weapon. A saved shaft's wake and a /data
// write share it.
func applyArrowTag(it *arrowEntity, compound nbt.Compound) {
	it.look = entityLookOf(compound)
	a := it.arrow
	it.needsSync = true
	it.shot = nbt.BoolField(compound, "HasBeenShot")
	a.RestoreLeftOwner(nbt.BoolField(compound, "LeftOwner"))
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		a.X, a.Y, a.Z = x, y, z
	}
	a.VX, a.VY, a.VZ = entityMotionOfTag(compound)
	a.Yaw, a.Pitch = entityRotationOfTag(compound)
	if d, ok := nbt.Get[nbt.Double](compound, "damage"); ok && d >= 0 {
		a.BaseDamage = float64(d)
	}
	if p, ok := nbt.Get[nbt.Byte](compound, "pickup"); ok && p <= 2 {
		a.Pickup = entity.ArrowPickup(p)
	}
	a.InGround = nbt.BoolField(compound, "inGround")
	a.Life = max(int(shortFieldOf(compound, "life")), 0)
	a.ShakeTicks = max(int(byteFieldOf(compound, "shake")), 0)
	a.FireTicks = fireOfTag(compound)
	a.NoGravity = nbt.BoolField(compound, "NoGravity")
	a.Crit = nbt.BoolField(compound, "crit")
	a.PierceLevel = int32(byteFieldOf(compound, "PierceLevel"))
	// vanilla: AbstractArrow.readAdditionalSaveData (registry byNameCodec)
	a.HitSound = defaultArrowSound
	if a.Trident {
		a.HitSound = defaultTridentSound
	}
	if sound, ok := nbt.Get[nbt.String](compound, "SoundEvent"); ok {
		if id, found := registry.SoundEventID(registry.Qualify(string(sound))); found {
			a.HitSound = registry.Sound(id + 1)
		}
	}
	a.RestoreDealtDamage(nbt.BoolField(compound, "DealtDamage"))
	// The block the shaft hit, saved as inBlockState: the arrow keeps it
	// so a later removal is seen, and the file having none leaves
	// ArrowNoLastState, vanilla's null.
	if block, ok := compound["inBlockState"]; ok {
		name, props := registry.BlockStateOf(block)
		if state, found := registry.FindState(name, props); found {
			a.LastState = state
		}
	}
	if a.InGround {
		// A stuck arrow has zero velocity, whatever the file says.
		a.VX, a.VY, a.VZ = 0, 0, 0
	}
	if it.entityType == protocol.EntityTypeSpectralArrow {
		a.GlowTicks = spectralArrowGlowTicks
		if duration, ok := nbt.IntField(compound, "Duration"); ok {
			a.GlowTicks = duration
		}
	}
	// An unreadable weapon stack is dropped; a trident's own weapon is
	// the stack its "item" is, no second copy beside it.
	if !a.Trident {
		if effect, pickup, ok := arrowPickupStackOf(compound); ok {
			// vanilla: AbstractArrow.readAdditionalSaveData, SpectralArrow.getDefaultPickupItem
			if _, hasItem := compound["item"]; !hasItem {
				item := registry.ItemArrow
				if it.entityType == protocol.EntityTypeSpectralArrow {
					item = registry.ItemSpectralArrow
				}
				pickup = protocol.ItemStack{ItemID: int32(item), Count: 1}
			}
			a.Effect = effect
			a.PickupStack = pickup
			// Only an Arrow reads its pickup's potion contents.
			if it.entityType != protocol.EntityTypeArrow {
				a.Effect = entity.ArrowEffect{}
			}
		}
		if weapon, ok := nbt.Get[nbt.Compound](compound, "weapon"); ok {
			if stack, ok := registry.StoredStack(weapon); ok {
				a.Weapon = stack
			}
		}
	}
	it.ownerUUID = entityUUIDFieldOf(compound, "Owner")
	it.portal.Cooldown = portalCooldownOf(compound)
	it.tags.Reset(tagsOfTag(compound))
	it.custom = customNameOfTag(compound)
}

// orbOfTag loads an experience orb, or returns nil for an out-of-world
// position or a negative value.
func orbOfTag(s *Server, w *world.World, compound nbt.Compound) *orbEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	if string(id) != orbEntityID {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}
	value, _ := nbt.ShortField(compound, "Value")
	if value < 0 {
		return nil
	}
	vx, vy, vz := entityMotionOfTag(compound)
	eid := s.nextEntity()
	it := &orbEntity{
		orb:    entity.NewOrb(eid, x, y, z, vx, vy, vz, int(value), s.entitySeed(eid)),
		uuid:   identity,
		w:      w,
		sentVX: vx, sentVY: vy, sentVZ: vz,
	}
	applyOrbTag(it, compound)
	return it
}

// applyOrbTag reads an experience orb's compound onto a live orb: the
// position, the motion, the value and count, the counters and flags, the
// portal cooldown and the tags. A saved orb's wake and a /data write share
// it.
func applyOrbTag(it *orbEntity, compound nbt.Compound) {
	it.look = entityLookOf(compound)
	o := it.orb
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		o.X, o.Y, o.Z = x, y, z
	}
	o.VX, o.VY, o.VZ = entityMotionOfTag(compound)
	if value, ok := nbt.ShortField(compound, "Value"); ok && value >= 0 {
		o.Value = int(value)
	}
	if c, ok := nbt.Get[nbt.Int](compound, "Count"); ok && c > 0 {
		o.Count = int(c)
	}
	o.Age = max(int(shortFieldOf(compound, "Age")), 0)
	o.Health = int(shortFieldOf(compound, "Health"))
	if o.Health <= 0 {
		o.Health = defaultOrbHealth
	}
	if ground, ok := nbt.Get[nbt.Byte](compound, "OnGround"); ok {
		o.OnGround = ground != 0
	}
	o.NoGravity = nbt.BoolField(compound, "NoGravity")
	o.Invulnerable = it.look.invulnerable
	it.portal.Cooldown = portalCooldownOf(compound)
	it.tags.Reset(tagsOfTag(compound))
	it.custom = customNameOfTag(compound)
}

// tntOfTag loads a primed TNT, or returns nil for a wrong id or missing position or UUID.
func tntOfTag(s *Server, w *world.World, compound nbt.Compound) *tntEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	if string(id) != tntEntityID {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}
	// A fuse at or under nought is kept: the body goes off on its first tick.
	// vanilla: PrimedTnt.readAdditionalSaveData
	fuse := shortFieldOr(compound, "fuse", entity.TNTDefaultFuse)
	state := registry.BlockTnt.DefaultState()
	vx, vy, vz := entityMotionOfTag(compound)
	eid := s.nextEntity()
	it := &tntEntity{
		tnt:  entity.NewTNT(eid, x, y, z, vx, vy, vz, fuse, state, 0),
		uuid: identity,
		w:    w,
		rand: rand.New(rand.NewSource(s.entitySeed(eid))),
	}
	applyTNTTag(it, compound)
	return it
}

// tntPowerOfTag reads explosion_power, clamped to 0..128.
// vanilla: PrimedTnt.readAdditionalSaveData
func tntPowerOfTag(compound nbt.Compound) float32 {
	return float32(min(max(floatFieldOr(compound, "explosion_power", entity.TNTExplosionPower), 0), 128))
}

// applyTNTTag reads a primed TNT's compound onto a live body: the position,
// the motion, the fuse, the block it is drawn as, the flags and the owner. A
// summon, saved TNT's wake and /data write share it.
func applyTNTTag(it *tntEntity, compound nbt.Compound) {
	t := it.tnt
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		t.X, t.Y, t.Z = x, y, z
	}
	t.VX, t.VY, t.VZ = entityMotionOfTag(compound)
	t.Fuse = shortFieldOr(compound, "fuse", entity.TNTDefaultFuse)
	t.Power = tntPowerOfTag(compound)
	t.Silent = nbt.BoolField(compound, "Silent")
	if block, ok := compound["block_state"]; ok {
		name, props := registry.BlockStateOf(block)
		if state, found := registry.FindState(name, props); found {
			t.BlockState = state
		}
	}
	if ground, ok := nbt.Get[nbt.Byte](compound, "OnGround"); ok {
		t.OnGround = ground != 0
	}
	t.NoGravity = nbt.BoolField(compound, "NoGravity")
	it.ownerUUID = entityUUIDFieldOf(compound, "owner")
	it.portal.Cooldown = portalCooldownOf(compound)
	it.tags.Reset(tagsOfTag(compound))
	it.custom = customNameOfTag(compound)
	it.look = entityLookOf(compound)
	if it.w != nil {
		readOrigin(&it.origin, it.w.DimensionName(), compound)
	}
}

// fallingOfTag loads a falling block, or returns nil for an out-of-world
// position. Absent fields take vanilla's reader defaults; a fall whose block
// state is air is loaded like any other and discarded by its first tick.
func fallingOfTag(s *Server, w *world.World, compound nbt.Compound) *fallingEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	if string(id) != fallingEntityID {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}
	name, props := fallingBlockStateOf(compound)
	state, found := registry.FindState(name, props)
	if !found {
		// A name no state answers to reads back as sand, vanilla's
		// FallingBlockEntity.DEFAULT_BLOCK_STATE, not a refusal.
		state = registry.BlockSand.DefaultState()
	}
	vx, vy, vz := entityMotionOfTag(compound)
	eid := s.nextEntity()
	fb := &fallingEntity{
		fb:   entity.NewFallingBlock(eid, x, y, z, vx, vy, vz, state, s.entitySeed(eid)),
		uuid: identity,
		w:    w,
	}
	applyFallingTag(fb, compound)
	return fb
}

// applyFallingTag reads a falling block's compound onto a live body: the
// block it is drawn as, the position, the motion, the fall's counters, the
// drop's flags, the hurt and the portal cooldown. A saved block's wake and
// a /data write share it.
func applyFallingTag(it *fallingEntity, compound nbt.Compound) {
	f := it.fb
	name, props := fallingBlockStateOf(compound)
	if state, ok := registry.FindState(name, props); ok && name != "minecraft:air" {
		f.BlockState = state
	}
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		f.X, f.Y, f.Z = x, y, z
	}
	f.VX, f.VY, f.VZ = entityMotionOfTag(compound)
	f.Time = max(intFieldOf(compound, "Time"), 0)
	f.DropItem = boolFieldOr(compound, "DropItem", true)
	f.CancelDrop = nbt.BoolField(compound, "CancelDrop")
	block, _ := registry.BlockByName(name)
	f.HurtEntities = boolFieldOr(compound, "HurtEntities", isAnvil(block))
	// vanilla: FallingBlockEntity.readAdditionalSaveData
	f.FallHurtAmount = floatFieldOr(compound, "FallHurtAmount", 0)
	f.FallHurtMax = intFieldOr(compound, "FallHurtMax", defaultFallingFallHurtMax)
	f.FallDistance = nbt.DoubleFieldOr(compound, "fall_distance", 0)
	if ground, ok := nbt.Get[nbt.Byte](compound, "OnGround"); ok {
		f.OnGround = ground != 0
	}
	f.NoGravity = nbt.BoolField(compound, "NoGravity")
	it.portal.Cooldown = portalCooldownOf(compound)
	it.tags.Reset(tagsOfTag(compound))
	it.custom = customNameOfTag(compound)
	it.look = entityLookOf(compound)
	if it.w != nil {
		readOrigin(&it.origin, it.w.DimensionName(), compound)
	}
}

// fallingBlockStateOf reads the codec's string or compound, defaulting to sand.
// vanilla: FallingBlockEntity.readAdditionalSaveData
func fallingBlockStateOf(compound nbt.Compound) (string, map[string]string) {
	tag, ok := compound["BlockState"]
	if !ok {
		return "minecraft:sand", nil
	}
	if block, ok := nbt.CompoundView(tag); ok {
		if _, ok := nbt.Get[nbt.String](block, "id"); !ok {
			// The legacy Name/Properties form is not part of the 26.3 codec.
			return "minecraft:sand", nil
		}
	}
	name, props := registry.BlockStateOf(tag)
	return registry.Qualify(name), props
}

// isAnvil reports whether a block is an anvil, whose falls hurt by
// default (vanilla's HurtEntities default).
func isAnvil(b registry.Block) bool {
	_, ok := b.FallingLandEvent()
	return ok
}

// paintingTag writes a painting's compound: the anchor cell as block_pos,
// the facing as the legacy 2D data value, and the variant by name. The
// position vanilla saves alongside is the box's centre, which its loader
// throws away for the anchor cell's sake.
// vanilla: Entity.saveWithoutId, BlockAttachedEntity.addAdditionalSaveData,
// Painting.addAdditionalSaveData, VariantUtils.writeVariant
func paintingTag(p *paintingEntity) *nbt.OrderedCompound {
	return paintingTagAs(p, paintingEntityID)
}

func paintingTagAs(p *paintingEntity, id string) *nbt.OrderedCompound {
	tag := orderedEntityBaseTag(id,
		paintingCentreX(p), paintingCentreY(p), paintingCentreZ(p),
		0, 0, 0,
		float32(painting2DOf(p.dir)*90), 0, p.uuid, false, 0, restingFire, 0)
	putEntityOptions(tag, p.custom, p.look.silent, p.look.noGravity, p.look, p.tags)
	tag.Set("facing", nbt.Byte(uint8(painting2DOf(p.dir))))
	tag.Set("block_pos", nbt.IntArray{p.x, p.y, p.z})
	tag.Set("Invulnerable", boolTag(p.invulnerable))
	if variants, ok := registry.Get("minecraft:painting_variant"); ok && int(p.variant) < len(variants.Entries) {
		tag.Set("variant", nbt.String(variants.Entries[p.variant]))
	}
	return tag
}

// paintingOfTag loads a painting, or returns nil when no anchor cell can be
// read. Absent fields keep vanilla's reader's defaults: south for the
// facing, the registry's own any — its first entry — for an unknown variant,
// and the anchor cell from Pos when block_pos is missing.
// vanilla: Painting.readAdditionalSaveData, VariantUtils.readVariant
func paintingOfTag(s *Server, w *world.World, compound nbt.Compound) *paintingEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	if string(id) != paintingEntityID {
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}
	_, known := registry.Get("minecraft:painting_variant")
	if !known {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	candidate, _ := paintingCandidate(0)
	p := newPainting(s.nextEntity(), identity, w, rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z), protocol.FaceSouth, 0, candidate.Width, candidate.Height)
	applyPaintingTag(p, compound)
	return p
}

// applyPaintingTag reloads the anchor and variant; an unreadable variant keeps the live one.
// vanilla: Painting.readAdditionalSaveData, BlockAttachedEntity.readAdditionalSaveData
func applyPaintingTag(p *paintingEntity, compound nbt.Compound) {
	p.look = entityLookOf(compound)
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		x, y, z = paintingCentreX(p), paintingCentreY(p), paintingCentreZ(p)
	}
	if pos, ok := nbt.Get[nbt.IntArray](compound, "block_pos"); ok && len(pos) == 3 && inWorld(float64(pos[0]), float64(pos[1]), float64(pos[2])) {
		// BlockAttachedEntity accepts an anchor less than 16 blocks from the loaded Pos.
		dx, dy, dz := float64(pos[0]-rules.BlockOf(x)), float64(pos[1]-rules.BlockOf(y)), float64(pos[2]-rules.BlockOf(z))
		if dx*dx+dy*dy+dz*dz < 256 {
			p.x, p.y, p.z = pos[0], pos[1], pos[2]
		}
	}
	if variants, ok := registry.Get("minecraft:painting_variant"); ok {
		if name, named := nbt.Get[nbt.String](compound, "variant"); named {
			if candidate, sized := paintingCandidate(variants.ID(registry.Qualify(string(name)))); sized {
				p.variant, p.width, p.height = candidate.Variant, candidate.Width, candidate.Height
			}
		}
	}
	p.dir = directionOfPainting2D(int32(codecByteOf(compound, "facing")))
	p.tags.Reset(tagsOfTag(compound))
	p.custom = customNameOfTag(compound)
	p.invulnerable = nbt.BoolField(compound, "Invulnerable")
	p.minX, p.minY, p.minZ, p.maxX, p.maxY, p.maxZ = rules.PaintingBox(p.x, p.y, p.z, p.dir, p.width, p.height)
}

// frameTag writes a frame's compound: the anchor cell as block_pos, the
// direction as the 3D data value Facing carries — the frame's own capital
// F, the paintings' 2D byte their own — the held item, its rotation, and
// the flags the vanilla frame writes alongside.
// vanilla: Entity.saveWithoutId, BlockAttachedEntity.addAdditionalSaveData,
// ItemFrame.addAdditionalSaveData
func frameTag(f *frameEntity) *nbt.OrderedCompound {
	id := frameEntityID
	if f.glow {
		id = glowFrameEntityID
	}
	return frameTagAs(f, id)
}

func frameTagAs(f *frameEntity, id string) *nbt.OrderedCompound {
	tag := orderedEntityBaseTag(id,
		frameCentreX(f), frameCentreY(f), frameCentreZ(f),
		0, 0, 0,
		float32(frameYaw(f.dir)), float32(framePitch(f.dir)), f.uuid, false, 0, restingFire, 0)
	putEntityOptions(tag, f.custom, f.look.silent, f.look.noGravity, f.look, f.tags)
	tag.Set("Invulnerable", boolTag(f.invulnerable))
	tag.Set("block_pos", nbt.IntArray{f.x, f.y, f.z})
	if !f.item.Empty() {
		tag.Set("Item", registry.StackTag(f.item))
	}
	tag.Set("ItemRotation", nbt.Byte(uint8(f.rotation)))
	tag.Set("ItemDropChance", nbt.Float(f.dropChance))
	tag.Set("Facing", nbt.Byte(uint8(f.dir)))
	tag.Set("Invisible", boolTag(f.invisible))
	tag.Set("Fixed", boolTag(f.fixed))
	return tag
}

// frameOfTag loads a frame, or returns nil when no anchor cell can be
// read. Absent fields keep vanilla's reader's defaults: an empty hand, a
// first-position item, the flags off, and Facing's fallback of down.
// vanilla: ItemFrame.readAdditionalSaveData, Direction.LEGACY_ID_CODEC
func frameOfTag(s *Server, w *world.World, compound nbt.Compound) *frameEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	var glow bool
	switch string(id) {
	case glowFrameEntityID:
		glow = true
	case frameEntityID:
	default:
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}
	var ax, ay, az int32
	if pos, ok := nbt.Get[nbt.IntArray](compound, "block_pos"); ok && len(pos) == 3 &&
		inWorld(float64(pos[0]), float64(pos[1]), float64(pos[2])) {
		ax, ay, az = pos[0], pos[1], pos[2]
	} else if x, y, z, ok := entityPositionOfTag(compound); ok {
		ax, ay, az = int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	} else {
		return nil
	}
	f := newFrame(s.nextEntity(), identity, w, ax, ay, az, frameFacingOf(compound), glow)
	applyFrameFields(f, compound)
	return f
}

// applyFrameFields sets a frame's item, rotation, drop chance, flags, tags
// and name from the compound; an absent Item empties the frame. Facing,
// which moves the frame's index entries, and the position are not read.
// vanilla: ItemFrame.readAdditionalSaveData
func applyFrameFields(f *frameEntity, compound nbt.Compound) {
	f.look = entityLookOf(compound)
	var item protocol.ItemStack
	if stack, ok := storedItemStackOf(compound); ok {
		item = stack
		item.Count = 1
	}
	f.setItem(item)
	f.rotation = int32(byteFieldOf(compound, "ItemRotation")) % 8
	f.dropChance = float32(nbt.DoubleFieldOr(compound, "ItemDropChance", 1))
	f.invisible = nbt.BoolField(compound, "Invisible")
	f.fixed = nbt.BoolField(compound, "Fixed")
	f.invulnerable = nbt.BoolField(compound, "Invulnerable")
	f.tags.Reset(tagsOfTag(compound))
	f.custom = customNameOfTag(compound)
}

// frameFacingOf reads Facing, down when absent. It wraps the way
// Direction.from3DDataValue does, NBT's byte being unsigned.
// vanilla: Direction.LEGACY_ID_CODEC
func frameFacingOf(compound nbt.Compound) int32 {
	return int32(codecByteOf(compound, "Facing")) % 6
}

// storedItemStackOf reads the Item stack, with registry.StoredStack's rules.
func storedItemStackOf(compound nbt.Compound) (protocol.ItemStack, bool) {
	item, ok := nbt.Get[nbt.Compound](compound, "Item")
	if !ok {
		return protocol.ItemStack{}, false
	}
	return registry.StoredStack(item)
}

// armorStandPoseList writes one pose part's three rotations as the float
// list the Pose compound keeps.
// vanilla: ArmorStand.addAdditionalSaveData's newFloatList
func armorStandPoseList(pose [3]float32) nbt.List {
	return nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{
		nbt.Float(pose[0]), nbt.Float(pose[1]), nbt.Float(pose[2]),
	}}
}

// armorStandTag writes a stand as the compound a vanilla entity file
// holds: the base fields, its own five flags and the six pose parts, its
// health and the equipment it carries, and the slot mask. The flags and
// the mask are written whatever they stand at; only the marker, which
// defaults on the client, is left out when clear.
// vanilla: ArmorStand.addAdditionalSaveData
func armorStandTag(ent *armorStandEntity) *nbt.OrderedCompound {
	return armorStandTagAs(ent, armorStandEntityID)
}

// armorStandDataTag skips id rather than deleting it, which could retain a grown table.
// vanilla: NbtPredicate.getEntityTagToCompare
func armorStandDataTag(ent *armorStandEntity) *nbt.OrderedCompound {
	return armorStandTagAs(ent, "")
}

func armorStandTagAs(ent *armorStandEntity, id string) *nbt.OrderedCompound {
	s := ent.stand
	tag := nbt.NewOrderedCompound(27)
	writeEntityBaseTag(tag, id, s.X, s.Y, s.Z, s.VX, s.VY, s.VZ, s.Yaw, ent.look.pitch,
		ent.uuid, s.OnGround, ent.portal.Cooldown, s.FireTicks, 0, s.NoGravity)
	tag.Set("Invulnerable", boolTag(s.Invulnerable))
	putEntityOptions(tag, ent.custom, ent.look.silent, s.NoGravity, ent.look, ent.tags)
	tag.Set("Health", nbt.Float(s.Health))
	// vanilla: LivingEntity.addAdditionalSaveData
	s.SaveWithAttributes(tag, func() {
		tag.Set("attributes", nbt.List{Elem: nbt.TypeEnd})
		if active := effect.Encode(ent.effects.All()); active != nil {
			tag.Set("active_effects", *active)
		}
	})
	equipment := &nbt.OrderedCompound{}
	writeHeldEquipment(equipment, s.MainHand, s.OffHand, s.Worn)
	if len(equipment.Entries()) > 0 {
		tag.Set("equipment", equipment)
	}
	tag.Set("Invisible", boolTag(s.Invisible))
	tag.Set("Small", boolTag(s.Small))
	tag.Set("ShowArms", boolTag(s.ShowArms))
	tag.Set("DisabledSlots", nbt.Int(s.DisabledSlots))
	tag.Set("NoBasePlate", boolTag(s.NoBasePlate))
	if s.Marker {
		tag.Set("Marker", boolTag(true))
	}
	tag.Set("Pose", armorStandPoseTag(s))
	putSpawnReason(tag, id, ent.spawnReason)
	putOrigin(tag, id, ent.origin)
	return tag
}

// armorStandPoseTag omits parts at their default rotation.
// vanilla: ArmorStand.ArmorStandPose.CODEC
func armorStandPoseTag(s *entity.ArmorStand) *nbt.OrderedCompound {
	pose := &nbt.OrderedCompound{}
	for part, name := range armorStandPoseParts {
		if s.Pose[part] != armorStandDefaultPose[part] {
			pose.Set(name, armorStandPoseList(s.Pose[part]))
		}
	}
	return pose
}

// heldEquipmentTag writes the six held and worn slots of a living body.
// vanilla: EntityEquipment.CODEC
func heldEquipmentTag(mainHand, offHand protocol.ItemStack, worn [4]protocol.ItemStack) nbt.Compound {
	equipment := nbt.Compound{}
	writeHeldEquipment(equipment, mainHand, offHand, worn)
	return equipment
}

func writeHeldEquipment(equipment nbt.CompoundTag, mainHand, offHand protocol.ItemStack, worn [4]protocol.ItemStack) {
	stacks := [6]protocol.ItemStack{mainHand, offHand, worn[0], worn[1], worn[2], worn[3]}
	for slot, name := range entity.StandSlotNames() {
		if stack := stacks[slot]; !stack.Empty() {
			equipment.Set(name, registry.StackTag(stack))
		}
	}
}

// heldEquipmentOf reads the six held and worn slots of a living body.
// vanilla: LivingEntity.readAdditionalSaveData, EntityEquipment.CODEC
func heldEquipmentOf(equipment nbt.Compound) (mainHand, offHand protocol.ItemStack, worn [4]protocol.ItemStack) {
	mainHand, offHand = entity.StoredSlot(equipment, "mainhand"), entity.StoredSlot(equipment, "offhand")
	names := entity.StandSlotNames()
	for i := range worn {
		worn[i] = entity.StoredSlot(equipment, names[i+2])
	}
	return
}

// armorStandOfTag loads an armor stand, or returns nil for one without a
// position. The five flags, the pose parts, the slot mask and the
// equipment read back through the same shapes armorStandTag writes, an
// absent field keeping a fresh stand's default.
// vanilla: ArmorStand.readAdditionalSaveData
func armorStandOfTag(s *Server, w *world.World, compound nbt.Compound) *armorStandEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	if string(id) != armorStandEntityID {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, hasUUID := entityUUIDOfTag(compound)
	if !hasUUID {
		identity = uuid.New()
	}
	eid := s.nextEntity()
	ent := &armorStandEntity{
		stand: entity.NewArmorStand(eid, x, y, z),
		w:     w,
		uuid:  identity,
	}
	applyArmorStandTag(ent, compound)
	return ent
}

// applyArmorStandFields reads the flags, pose, slot mask, health and equipment
// shared by a full stand load and an item's entity_data.
// vanilla: ArmorStand.readAdditionalSaveData
func applyArmorStandFields(ent *armorStandEntity, compound nbt.Compound) {
	stand := ent.stand
	stand.LoadFrom(compound)
	stand.Small = nbt.BoolField(compound, "Small")
	stand.ShowArms = nbt.BoolField(compound, "ShowArms")
	stand.NoBasePlate = nbt.BoolField(compound, "NoBasePlate")
	stand.Marker = nbt.BoolField(compound, "Marker")
	stand.Invisible = nbt.BoolField(compound, "Invisible")
	stand.Invulnerable = nbt.BoolField(compound, "Invulnerable")
	stand.NoGravity = nbt.BoolField(compound, "NoGravity")
	if slots, named := nbt.IntField(compound, "DisabledSlots"); named {
		stand.DisabledSlots = slots
	}
	if health, named := nbt.FloatField(compound, "Health"); named && health > 0 {
		stand.Health = min(float64(health), entity.ArmorStandHealth)
	}
	ent.look = entityLookOf(compound)
	list, _ := nbt.Get[nbt.List](compound, "active_effects")
	ent.effects = effect.Decode(list)
	stand.Pose = armorStandDefaultPose
	if pose, named := nbt.Get[nbt.Compound](compound, "Pose"); named {
		for part, name := range armorStandPoseParts {
			list, ok := nbt.Get[nbt.List](pose, name)
			if !ok || len(list.Items) != 3 {
				continue
			}
			var rotations [3]float32
			read := true
			for i, item := range list.Items {
				f, ok := item.(nbt.Float)
				if !ok {
					read = false
					break
				}
				rotations[i] = float32(f)
			}
			if read {
				stand.Pose[part] = rotations
			}
		}
	}
	if equipment, named := nbt.Get[nbt.Compound](compound, "equipment"); named {
		stand.MainHand, stand.OffHand, stand.Worn = heldEquipmentOf(equipment)
	}
}

// applyArmorStandTag loads a stand's whole compound onto the live stand a
// /data write names: the base fields every entity carries, then the stand's
// own, so a merge moves and turns it and carries its tags the way a saved
// stand's wake does. Both share applyArmorStandFields.
// vanilla: Entity.load, ArmorStand.readAdditionalSaveData
func applyArmorStandTag(ent *armorStandEntity, compound nbt.Compound) {
	ent.portal.Cooldown = portalCooldownOf(compound)
	s := ent.stand
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		s.X, s.Y, s.Z = x, y, z
	}
	s.VX, s.VY, s.VZ = entityMotionOfTag(compound)
	s.Yaw, _ = entityRotationOfTag(compound)
	s.OnGround = nbt.BoolField(compound, "OnGround")
	s.FireTicks = fireOfTag(compound)
	ent.tags.Reset(tagsOfTag(compound))
	ent.custom = customNameOfTag(compound)
	readSpawnReason(&ent.spawnReason, compound)
	if ent.w != nil {
		readOrigin(&ent.origin, ent.w.DimensionName(), compound)
	}
	applyArmorStandFields(ent, compound)
}

// boatOfTag loads a boat, or returns nil for an out-of-world position. The
// kind the id names the hull is; a chest boat's cargo comes back as its 27
// slots, an unreadable stack dropped. Absent fields keep a fresh laying's
// values, and a hurt state the synched data's own defaults.
func boatOfTag(s *Server, w *world.World, compound nbt.Compound) *boatEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	kind := entity.BoatKind(string(id))
	if kind == "" {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}
	yaw, _ := entityRotationOfTag(compound)
	eid := s.nextEntity()
	typ, _ := registry.EntityTypeByName(kind)
	it := &boatEntity{
		boat: entity.NewBoat(eid, x, y, z, yaw),
		uuid: identity,
		w:    w,
		kind: typ,
	}
	it.firstX, it.firstY, it.firstZ = x, y, z
	applyBoatTag(it, compound)
	return it
}

// applyBoatTag reads a boat's compound onto a live hull: the position, the
// motion and yaw, the flags, the custom name, a chest boat's cargo read
// back as its slots, and the lead. A saved boat's wake and a /data write share it.
func applyBoatTag(it *boatEntity, compound nbt.Compound) {
	it.portal.Cooldown = portalCooldownOf(compound)
	b := it.boat
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		b.X, b.Y, b.Z = x, y, z
	}
	b.VX, b.VY, b.VZ = entityMotionOfTag(compound)
	b.Yaw, _ = entityRotationOfTag(compound)
	if ground, ok := nbt.Get[nbt.Byte](compound, "OnGround"); ok {
		b.OnGround = ground != 0
	}
	b.FireTicks = int(shortFieldOf(compound, "Fire"))
	b.NoGravity = nbt.BoolField(compound, "NoGravity")
	b.Invulnerable = nbt.BoolField(compound, "Invulnerable")
	it.custom = customNameOfTag(compound)
	it.look = entityLookOf(compound)
	if entity.IsChestBoat(it.kind) {
		cargo := make([]protocol.ItemStack, entity.BoatCargoSlots)
		it.lootTable, it.lootSeed = "", 0
		if table, ok := nbt.Get[nbt.String](compound, "LootTable"); ok && table != "" {
			it.lootTable = string(table)
			if !strings.Contains(it.lootTable, ":") {
				it.lootTable = "minecraft:" + it.lootTable
			}
			it.lootSeed, _ = nbt.LongValue(compound["LootTableSeed"])
		}
		// A pending table stands for the contents, so the items are not read.
		// vanilla: ContainerEntity.readChestVehicleSaveData
		if it.lootTable == "" {
			registry.LoadSlottedItems(compound, cargo)
		}
		it.cargo = cargo
	}
	// Another lead read is one restored from a save, re-tied on the hull's
	// next tick; the one it replaces comes off without its item.
	// vanilla: Leashable.readLeashData
	if ref := leashOfTag(compound); ref != it.leash {
		it.untie()
		it.leash = ref
	}
	it.tags.Reset(tagsOfTag(compound))
	readSpawnReason(&it.spawnReason, compound)
	if it.w != nil {
		readOrigin(&it.origin, it.w.DimensionName(), compound)
	}
}

// minecartTag writes a cart's compound: the base fields, the tags, and the
// ride's own flipped and ticked flags, both always written. The rocking a
// blow wound and the activator's shake ride nowhere, being synched data
// vanilla reads off the cart itself.
// vanilla: AbstractMinecart.addAdditionalSaveData
func minecartTag(ent *minecartEntity) *nbt.OrderedCompound {
	return minecartTagAs(ent, ent.kind.Name())
}

func minecartTagAs(ent *minecartEntity, id string) *nbt.OrderedCompound {
	m := ent.cart
	tag := orderedEntityBaseTag(id,
		m.X, m.Y, m.Z, m.VX, m.VY, m.VZ,
		m.Yaw, ent.look.pitch, ent.uuid, m.OnGround, ent.portal.Cooldown, m.FireTicks, 0)
	putEntityOptions(tag, ent.custom, ent.look.silent, m.NoGravity, ent.look, ent.tags)
	tag.Set("Invulnerable", boolTag(m.Invulnerable))
	if _, _, ok := registry.BlockStateName(uint16(ent.displayState)); ok && ent.displayState != 0 {
		tag.Set("DisplayState", registry.BlockStateCodecTag(uint16(ent.displayState)))
	}
	if ent.displayOffset != ent.kindInfo().DisplayOffset {
		tag.Set("DisplayOffset", nbt.Int(ent.displayOffset))
	}
	tag.Set("FlippedRotation", boolTag(m.Flipped))
	tag.Set("HasTicked", boolTag(m.FirstTick))
	ent.cargo.putTag(tag)
	if ent.kind == registry.EntityTypeHopperMinecart {
		tag.Set("Enabled", boolTag(ent.enabled))
	}
	if ent.command != nil {
		ent.command.Save(tag)
	}
	if f := ent.cart.Furnace; f != nil {
		tag.Set("PushX", nbt.Double(f.PushX))
		tag.Set("PushZ", nbt.Double(f.PushZ))
		tag.Set("Fuel", nbt.Short(int16(f.Fuel)))
	}
	if ent.spawner != nil {
		ent.spawner.SaveSpawnerTo(tag)
	}
	if ent.kind == registry.EntityTypeTntMinecart {
		tag.Set("fuse", nbt.Int(ent.tnt.Fuse))
		if ent.tnt.Power != entity.TNTCartDefaultPower {
			tag.Set("explosion_power", nbt.Float(ent.tnt.Power))
		}
		if ent.tnt.SpeedFactor != entity.TNTCartDefaultSpeedFactor {
			tag.Set("explosion_speed_factor", nbt.Float(ent.tnt.SpeedFactor))
		}
	}
	if len(ent.carriedPassengers) > 0 {
		// vanilla: Entity.addSaveData
		tag.Set("Passengers", nbt.List{Elem: nbt.TypeCompound, Items: ent.carriedPassengers})
	}
	putSpawnReason(tag, id, ent.spawnReason)
	putOrigin(tag, id, ent.origin)
	return tag
}

// minecartOfTag loads a cart of the kind the compound's id names, or returns
// nil for an id that is no cart or a position outside the world.
func minecartOfTag(s *Server, w *world.World, compound nbt.Compound) *minecartEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	if _, ok := entity.MinecartKindOf(string(id)); !ok {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}
	it := s.newMinecart(w, string(id), identity, x, y, z)
	applyMinecartTag(it, compound)
	return it
}

// applyMinecartTag reads a cart's compound onto a live cart: the place, the
// motion and yaw, the flags, the tags and the ride's flipped and ticked
// flags, the fields a /data merge write names.
// vanilla: AbstractMinecart.readAdditionalSaveData
func applyMinecartTag(it *minecartEntity, compound nbt.Compound) {
	it.portal.Cooldown = portalCooldownOf(compound)
	m := it.cart
	it.needsSync = true
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		m.X, m.Y, m.Z = x, y, z
	}
	m.VX, m.VY, m.VZ = entityMotionOfTag(compound)
	m.Yaw, _ = entityRotationOfTag(compound)
	if ground, ok := nbt.Get[nbt.Byte](compound, "OnGround"); ok {
		m.OnGround = ground != 0
	}
	m.FireTicks = int(shortFieldOf(compound, "Fire"))
	m.NoGravity = nbt.BoolField(compound, "NoGravity")
	m.Invulnerable = nbt.BoolField(compound, "Invulnerable")
	it.custom = customNameOfTag(compound)
	it.look = entityLookOf(compound)
	readSpawnReason(&it.spawnReason, compound)
	if it.w != nil {
		readOrigin(&it.origin, it.w.DimensionName(), compound)
	}
	m.Flipped = nbt.BoolField(compound, "FlippedRotation")
	m.FirstTick = nbt.BoolField(compound, "HasTicked")
	it.displayState = 0
	if block, ok := compound["DisplayState"]; ok {
		name, props := registry.BlockStateOf(block)
		if state, found := registry.FindState(name, props); found {
			it.displayState = int32(state)
		}
	}
	it.displayOffset = it.kindInfo().DisplayOffset
	if offset, ok := nbt.Get[nbt.Int](compound, "DisplayOffset"); ok {
		it.displayOffset = int32(offset)
	}
	it.cargo = readVehicleCargo(compound, it.kindInfo().Slots)
	it.enabled = true
	if it.kind == registry.EntityTypeHopperMinecart {
		// vanilla: MinecartHopper.readAdditionalSaveData, getBooleanOr("Enabled", true)
		if v, ok := nbt.ByteField(compound, "Enabled"); ok {
			it.enabled = v != 0
		}
	}
	if f := it.cart.Furnace; f != nil {
		*f = entity.FurnaceCart{Lit: f.Lit}
		if v, ok := nbt.Get[nbt.Double](compound, "PushX"); ok {
			f.PushX = float64(v)
		}
		if v, ok := nbt.Get[nbt.Double](compound, "PushZ"); ok {
			f.PushZ = float64(v)
		}
		if v, ok := nbt.Get[nbt.Short](compound, "Fuel"); ok {
			f.Fuel = int32(v)
		}
	}
	if it.command != nil {
		it.command.Load(compound)
	}
	if it.spawner != nil {
		it.spawner.LoadSpawnerTag(compound)
	}
	if it.kind == registry.EntityTypeTntMinecart {
		it.tnt = entity.NewTNTCart()
		if v, ok := nbt.Get[nbt.Int](compound, "fuse"); ok {
			it.tnt.Fuse = int32(v)
		}
		if v, ok := nbt.Get[nbt.Float](compound, "explosion_power"); ok {
			it.tnt.Power = entity.ClampTNTCartSetting(float32(v))
		}
		if v, ok := nbt.Get[nbt.Float](compound, "explosion_speed_factor"); ok {
			it.tnt.SpeedFactor = entity.ClampTNTCartSetting(float32(v))
		}
	}
	it.tags.Reset(tagsOfTag(compound))
}

// arrowPickupTag is the pickup item a shot keeps: the arrow, carrying the
// effect a tipped shot gives as its potion_contents' one custom effect.
func arrowPickupTag(e entity.ArrowEffect) nbt.Compound {
	item := nbt.Compound{
		"id":    nbt.String(defaultArrowPickupItem),
		"count": nbt.Int(1),
	}
	if e.Name != "" {
		effect := nbt.Compound{"id": nbt.String(e.Name), "duration": nbt.Int(e.Ticks)}
		if e.Amplifier != 0 {
			effect["amplifier"] = nbt.Byte(int8(e.Amplifier))
		}
		item["components"] = nbt.Compound{
			"minecraft:potion_contents": nbt.Compound{
				"custom_effects": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{effect}},
			},
		}
	}
	return item
}

// arrowPickupStackOf reads an arrow's pickup item: a plain or one-effect
// arrow as arrowPickupOf does (no pickup stack), else any item stack the
// registry reads, kept whole as the pickup. Its potion contents give the
// shot its effect and colour, whatever item carries them. ok is false for
// an unreadable item.
// vanilla: AbstractArrow.readAdditionalSaveData, Arrow.getPotionContents
func arrowPickupStackOf(compound nbt.Compound) (entity.ArrowEffect, protocol.ItemStack, bool) {
	if effect, ok := arrowPickupOf(compound); ok {
		return effect, protocol.ItemStack{}, true
	}
	item, _ := nbt.Get[nbt.Compound](compound, "item")
	stack, ok := registry.StoredStack(item)
	if !ok {
		return entity.ArrowEffect{}, protocol.ItemStack{}, false
	}
	stack.Count = 1
	return pickupEffect(stack), stack, true
}

// arrowPickupOf reads the pickup item back: absent (vanilla defaults it),
// the plain arrow, or the arrow arrowPickupTag tips. ok is false for any
// other item, which is carried through as the tag it was.
func arrowPickupOf(compound nbt.Compound) (entity.ArrowEffect, bool) {
	item, ok := nbt.Get[nbt.Compound](compound, "item")
	if !ok {
		return entity.ArrowEffect{}, true
	}
	name, hasName := nbt.Get[nbt.String](item, "id")
	if !hasName || string(name) != defaultArrowPickupItem {
		return entity.ArrowEffect{}, false
	}
	components, hasComponents := item["components"]
	if !hasComponents {
		return entity.ArrowEffect{}, true
	}
	c, ok := nbt.CompoundView(components)
	if !ok || len(c) != 1 {
		return entity.ArrowEffect{}, false
	}
	potion, ok := nbt.Get[nbt.Compound](c, "minecraft:potion_contents")
	if !ok || len(potion) != 1 {
		return entity.ArrowEffect{}, false
	}
	effects, ok := nbt.Get[nbt.List](potion, "custom_effects")
	if !ok || len(effects.Items) != 1 {
		return entity.ArrowEffect{}, false
	}
	e, ok := nbt.CompoundView(effects.Items[0])
	if !ok {
		return entity.ArrowEffect{}, false
	}
	for key := range e {
		switch key {
		case "id", "duration", "amplifier":
		default:
			return entity.ArrowEffect{}, false
		}
	}
	id, okID := nbt.Get[nbt.String](e, "id")
	ticks, okTicks := nbt.Get[nbt.Int](e, "duration")
	if _, known := registry.MobEffectID(string(id)); !okID || !okTicks || !known {
		return entity.ArrowEffect{}, false
	}
	var amplifier int32
	if a, ok := nbt.Get[nbt.Byte](e, "amplifier"); ok {
		// The amplifier is saved as UNSIGNED_BYTE, so a value above 127
		// would read back negative from the signed byte nbt keeps.
		amplifier = int32(uint8(a))
	}
	return entity.ArrowEffect{Name: string(id), Ticks: int32(ticks), Amplifier: amplifier}, true
}

// entityPositionOfTag reads Pos. ok is false for a malformed or out-of-world
// position: other kinds then refuse the entity, and a mob stands at the origin.
func entityPositionOfTag(compound nbt.Compound) (x, y, z float64, ok bool) {
	pos, has := nbt.Get[nbt.List](compound, "Pos")
	if !has || len(pos.Items) != 3 {
		return 0, 0, 0, false
	}
	xyz := [3]float64{}
	for i, item := range pos.Items {
		value, isDouble := item.(nbt.Double)
		if !isDouble {
			return 0, 0, 0, false
		}
		xyz[i] = float64(value)
	}
	if !inWorld(xyz[0], xyz[1], xyz[2]) {
		return 0, 0, 0, false
	}
	return xyz[0], xyz[1], xyz[2], true
}

// entityMotionOfTag reads Motion, zero where absent and on any axis whose
// magnitude is over 10.
// vanilla: Entity.load
func entityMotionOfTag(compound nbt.Compound) (vx, vy, vz float64) {
	motion, ok := nbt.Get[nbt.List](compound, "Motion")
	if !ok || len(motion.Items) != 3 {
		return 0, 0, 0
	}
	for i, item := range motion.Items {
		value, ok := item.(nbt.Double)
		if !ok {
			continue
		}
		switch i {
		case 0:
			vx = float64(value)
		case 1:
			vy = float64(value)
		case 2:
			vz = float64(value)
		}
	}
	for _, v := range []*float64{&vx, &vy, &vz} {
		if math.Abs(*v) > 10 {
			*v = 0
		}
	}
	return vx, vy, vz
}

// entityRotationOfTag reads Rotation, zero where absent.
func entityRotationOfTag(compound nbt.Compound) (yaw, pitch float32) {
	rotation, ok := nbt.Get[nbt.List](compound, "Rotation")
	if !ok || len(rotation.Items) != 2 {
		return 0, 0
	}
	if value, ok := rotation.Items[0].(nbt.Float); ok {
		yaw = float32(value)
	}
	if value, ok := rotation.Items[1].(nbt.Float); ok {
		pitch = float32(value)
	}
	return yaw, pitch
}

// entityUUIDOfTag reads the UUID int array; ok is false if absent, not 4
// ints long, or nil.
func entityUUIDOfTag(compound nbt.CompoundTag) (identity uuid.UUID, ok bool) {
	if compound == nil {
		return uuid.Nil, false
	}
	ids, has := nbt.Get[nbt.IntArray](compound.Entries(), "UUID")
	if !has {
		return uuid.Nil, false
	}
	identity, ok = nbt.UUIDFromIntArray(ids)
	return identity, ok && identity != uuid.Nil
}

// entityUUIDFieldOf reads an owner UUID field, or uuid.Nil if absent or
// malformed. The zero UUID reads as uuid.Nil too, which is harmless for a
// projectile's owner: no body answers to it.
func entityUUIDFieldOf(compound nbt.Compound, key string) uuid.UUID {
	identity, _ := nbt.UUIDField(compound, key)
	return identity
}

// fireOfTag reads the ticks of fire a body was saved with, vanilla's
// remainingFireTicks; zero or below is none.
func fireOfTag(compound nbt.Compound) int {
	return max(int(shortFieldOf(compound, "Fire")), 0)
}

// shortFieldOf reads a short field, 0 if absent.
func shortFieldOf(compound nbt.Compound, key string) int16 {
	v, _ := nbt.ShortField(compound, key)
	return v
}

// byteFieldOf reads a byte field, 0 if absent.
func byteFieldOf(compound nbt.Compound, key string) uint8 {
	v, _ := nbt.ByteField(compound, key)
	return uint8(v)
}

// codecByteOf reads a byte field as a codec does (Codec.BYTE), a float
// truncating where byteFieldOf floors it; 0 if absent.
func codecByteOf(compound nbt.Compound, key string) uint8 {
	v, _ := nbt.IntValue(compound[key])
	return uint8(v)
}

// boolFieldOr reads a boolean field, fallback if absent.
func boolFieldOr(compound nbt.Compound, key string, fallback bool) bool {
	if _, ok := nbt.IntValue(compound[key]); !ok {
		return fallback
	}
	return nbt.BoolField(compound, key)
}

// intFieldOf reads an int field, 0 if absent.
func intFieldOf(compound nbt.Compound, key string) int {
	v, _ := nbt.IntField(compound, key)
	return int(v)
}

// intFieldOr reads an int field, fallback if absent.
func intFieldOr(compound nbt.Compound, key string, fallback int) int {
	if v, ok := nbt.IntField(compound, key); ok {
		return int(v)
	}
	return fallback
}

// shortFieldOr reads a short field, fallback if absent.
func shortFieldOr(compound nbt.Compound, key string, fallback int) int {
	if v, ok := nbt.ShortField(compound, key); ok {
		return int(v)
	}
	return fallback
}

// floatFieldOr reads a float field, fallback if absent.
func floatFieldOr(compound nbt.Compound, key string, fallback float64) float64 {
	if v, ok := nbt.FloatField(compound, key); ok {
		return float64(v)
	}
	return fallback
}

// portalCooldownOf reads PortalCooldown, 0 if absent.
func portalCooldownOf(compound nbt.Compound) int {
	if v, ok := nbt.Get[nbt.Int](compound, "PortalCooldown"); ok {
		return int(v)
	}
	return 0
}

// clampToShort clamps a counter into the short the file stores it as.
func clampToShort(v int) int16 {
	return int16(min(max(v, -32768), 32767))
}

// clampToByte clamps a counter into 0..255.
func clampToByte(v int) uint8 {
	return uint8(min(max(v, 0), 255))
}

// itemEntityType returns the type id an entity_data component names.
func itemEntityType(stack protocol.ItemStack) (int32, bool) {
	for _, raw := range stack.Raw {
		if raw.Type == protocol.DataComponentEntityData {
			r := protocol.NewReader(raw.Data)
			t := r.ReadVarInt()
			return t, r.Err() == nil
		}
	}
	return 0, false
}

// itemEntityData returns the compound of a stack's entity_data component, or
// nil when it has none, names a type other than typ, or does not decode.
// vanilla: EntityType.updateCustomEntityTag
func itemEntityData(stack protocol.ItemStack, typ int32) nbt.Compound {
	for _, raw := range stack.Raw {
		if raw.Type != protocol.DataComponentEntityData {
			continue
		}
		r := protocol.NewReader(raw.Data)
		if t := r.ReadVarInt(); r.Err() != nil || t != typ {
			return nil
		}
		tag, _, err := nbt.DecodeNetwork(r.Rest())
		if compound, ok := nbt.CompoundView(tag); ok && err == nil {
			return compound
		}
		return nil
	}
	return nil
}

// itemEntityTag lays a stack's custom_name and entity_data over a body's
// saved tag, the identity (UUID, id) kept, and returns it. It returns nil
// when the stack carries neither, so the body stays as built.
// vanilla: EntityType.createDefaultStackConfig, TypedEntityData.loadInto
func itemEntityTag(tag nbt.Compound, stack protocol.ItemStack, typ int32) nbt.Compound {
	data := itemEntityData(stack, typ)
	if data == nil && stack.CustomName == nil {
		return nil
	}
	if stack.CustomName != nil {
		tag["CustomName"] = protocol.TextTag(*stack.CustomName)
	}
	forEntityDataKeys(data, func(k string, v nbt.Tag) { tag[k] = v })
	return tag
}

// forEntityDataKeys calls set for every key of an entity_data compound but
// the identity (UUID, id).
// vanilla: TypedEntityData.loadInto
func forEntityDataKeys(data nbt.Compound, set func(k string, v nbt.Tag)) {
	for k, v := range data {
		if k != "UUID" && k != "id" {
			set(k, v)
		}
	}
}
