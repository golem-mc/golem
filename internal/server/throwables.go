package server

import (
	"log/slog"
	"maps"
	"math"
	"math/rand"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The thrown projectiles share the arrow lock; their sends run after it.
// The telling's rhythm was read off a vanilla server's traffic for a thrown
// egg: the throw's tick carries the sound, the Spawn Entity and the item;
// the next tick, a zero-delta position packet and the metadata again; every
// tenth tick, the motion and an absolute teleport. The throws are
// session-bound.

// thrownEntity is one thrown projectile in a world. The body itself is
// entity.Thrown; the server holds the dimension, the UUID, the stack the
// metadata carries and the tracking.
type thrownEntity struct {
	throwable *entity.Thrown
	w         *world.World
	uuid      uuid.UUID
	stack     protocol.ItemStack
	look      entityLook

	bodyTrack[struct{}]
	shotMark

	// tags are the scoreboard tags, what the tag selector option matches
	// and /tag edits. Guarded by the arrow lock the thrown list shares.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName

	// invulnerable is Entity.invulnerable, as loaded.
	invulnerable bool

	// variant is the chicken variant an egg's break hatches its chicks as —
	// the variant the egg item thrown was. "" on a body that is no egg.
	// vanilla: ThrownEgg.onHit
	variant string

	// firstPass is the tracked entity's own first sendChanges still owed:
	// the zero-delta position packet and the metadata sent again.
	firstPass bool

	ownerUUID uuid.UUID

	// targetUUID is the identity of the entity a shulker bullet homes on,
	// kept so a save can name it; the nil UUID on any other body.
	targetUUID uuid.UUID

	// portal is the thrown body's portal state — the cooldown of its last
	// crossing and the standing it has in the portal it flies through.
	portal rules.EntityPortal

	// holdWorld, holdPos and holdRenew are where and until when a pearl's
	// ender_pearl ticket was last placed (holding is false before the first);
	// an egg's or a snowball's never is. Only the tick loop touches them.
	holdWorld *world.World
	holdPos   world.ChunkPos
	holdRenew int64
	holding   bool
}

// thrownTracker tracks the thrown bodies for their pairing alone: their
// movement keeps its own schedule off the flight's tick count.
var thrownTracker = trackedKind[*thrownEntity, struct{}]{
	rangeChunks: entity.ThrownTracking,
	interval:    entity.ThrownInterval,
	rangeOf:     func(it *thrownEntity) int { return it.throwable.Kind.TrackingRange() },
	intervalOf:  func(it *thrownEntity) int { return it.throwable.Kind.UpdateInterval() },
	lock:        func(s *Server) *sync.Mutex { return &s.arrowsMu },
	list:        func(s *Server) *[]*thrownEntity { return &s.thrown },
	track:       func(it *thrownEntity) *bodyTrack[struct{}] { return &it.bodyTrack },
	world:       func(it *thrownEntity) *world.World { return it.w },
	id:          func(it *thrownEntity) int32 { return it.throwable.ID },
	dead:        func(it *thrownEntity) bool { return it.throwable.Dead },
	pose: func(it *thrownEntity) bodyPose {
		return bodyPose{x: it.throwable.X, y: it.throwable.Y, z: it.throwable.Z}
	},
	pairing: thrownPairing,
}

// The velocity a thrown projectile leaves the thrower with, vanilla's
// shootFromRotation: the power of one and a half, the inaccuracy of one.
// The potions leave at the power of a half, their use's own aim twenty
// degrees above the look the packet says.
// vanilla: ThrowablePotionItem.PROJECTILE_SHOOT_POWER and its use
const (
	thrownPower       = 1.5
	thrownInaccuracy  = 1.0
	thrownPotionPower = 0.5
	// thrownBottlePower is the bottle o' enchanting's throw power; it lifts
	// the aim by thrownPotionLift too. vanilla: ExperienceBottleItem.use
	thrownBottlePower = 0.7
	// thrownBottleColour is the splash colour of the bottle's world event.
	// vanilla: ThrownExperienceBottle.onHit
	thrownBottleColour = -13083194
	thrownPotionLift   = 20.0
)

// The throw sound's own numbers, vanilla's SnowballItem.use and
// EggItem.use: half a block of volume.
const (
	thrownSoundVolume = 0.5
)

const (
	// windChargeRadius is WindCharge's explosion radius; its damage
	// calculator multiplies every knockback by 1.22.
	// vanilla: WindCharge.explode
	windChargeRadius    = 1.2
	windChargeKnockback = 1.22
	// windChargeFaceOffset is how far a block hit's burst sits out of the
	// face. vanilla: AbstractWindCharge.onHitBlock
	windChargeFaceOffset = 0.25
	// windChargeCullAbove is how far above the build limit a charge flies
	// before it bursts. vanilla: AbstractWindCharge.tick
	windChargeCullAbove = 30
)

// windBurst is one wind charge's landing, bursting after the tick's locks.
type windBurst struct {
	w         *world.World
	id        int32
	uuid      uuid.UUID
	custom    customName
	ownerID   int32
	ownerUUID uuid.UUID
	rng       *rand.Rand
	radius    float64
	breeze    bool
	x, y, z   float64
}

// windPlayerHit is a wind charge striking a player directly.
type windPlayerHit struct {
	victim  *client
	ownerID int32
	// ownerMob is a non-player living owner (a breeze), whose blow scales
	// with difficulty.
	ownerMob bool
	id       int32
	uuid     uuid.UUID
	custom   customName
	kind     entity.ThrownKind
	x, y, z  float64
	damage   float64
	// vx, vz is the flight the knockback runs against.
	vx, vz float64
}

// windChargeResistanceOf is the burst's damage calculator for the blocks of
// w: only the tagged blocks stop a ray; every other cell, air included, is
// given the -0.3 that cancels the 0.3 each step adds to a resistance.
// vanilla: SimpleExplosionDamageCalculator.getBlockExplosionResistance
func windChargeResistanceOf(w *world.World) func(bx, by, bz int32) (float64, bool) {
	return func(bx, by, bz int32) (float64, bool) {
		if registry.BlockOf(w.BlockAt(bx, by, bz)).InTag(registry.TagIDBlocksWindChargeExplosions) {
			return 3600000, true
		}
		return -0.3, true
	}
}

// handleUseThrown answers a use of an egg, a snowball, an ender pearl or
// one of the two thrown potions with the throw the item's use puts into
// the world, and reports whether the item in the hand was one of them.
// The aim is the look the use's packet turned the player to. The sound
// plays first, before the projectile is added; the throw starts the
// stack's use_cooldown.
func (c *client) handleUseThrown(hand int32) bool {
	if c.player.Vitals().Dead {
		return false
	}
	mode := c.player.GameMode()
	if mode == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	item := registry.Item(held.ItemID)
	var kind entity.ThrownKind
	var sound registry.Sound
	var soundCategory int32
	switch item {
	case registry.ItemEgg, registry.ItemBrownEgg, registry.ItemBlueEgg:
		kind = entity.ThrownEgg
		sound = registry.SoundEntityEggThrow
		soundCategory = protocol.SoundCategoryPlayer
	case registry.ItemSnowball:
		kind = entity.ThrownSnowball
		sound = registry.SoundEntitySnowballThrow
		soundCategory = protocol.SoundCategoryNeutral
	case registry.ItemSplashPotion:
		kind = entity.ThrownSplashPotion
		sound = registry.SoundEntitySplashPotionThrow
		soundCategory = protocol.SoundCategoryPlayer
	case registry.ItemLingeringPotion:
		kind = entity.ThrownLingeringPotion
		sound = registry.SoundEntityLingeringPotionThrow
		soundCategory = protocol.SoundCategoryNeutral
	case registry.ItemExperienceBottle:
		kind = entity.ThrownExperienceBottle
		sound = registry.SoundEntityExperienceBottleThrow
		soundCategory = protocol.SoundCategoryNeutral
	case registry.ItemEnderPearl, registry.ItemWindCharge:
		kind = entity.ThrownEnderPearl
		sound = registry.SoundEntityEnderPearlThrow
		if item == registry.ItemWindCharge {
			kind = entity.ThrownWindCharge
			sound = registry.SoundEntityWindChargeThrow
		}
		soundCategory = protocol.SoundCategoryNeutral
	default:
		return false
	}
	pos := c.player.Position()
	seed := c.srv.levelRng.Int63()
	// The throw rides the thrower's own movement: vanilla
	// Projectile.shootFromRotation adds getKnownMovement, its vertical
	// part only off the ground.
	movement := c.player.KnownMovement()
	rideX, rideY, rideZ := movement.X, 0.0, movement.Z
	if !pos.OnGround {
		rideY = movement.Y
	}
	// The stack the flight carries is one of the held one, its components
	// riding along — the potion_contents a potion is brewed with above
	// all, both the metadata the body is drawn as and the shatter's own
	// reading.
	// The wind charge leaves from the eye itself, the others a tenth below.
	// vanilla: WindChargeItem.use, ThrowableItemProjectile
	spawnBelow := entity.ThrownSpawnBelow
	if kind == entity.ThrownWindCharge {
		spawnBelow = 0
	}
	id := c.srv.spawnThrown(kind, c.theWorld(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight-spawnBelow, pos.Z,
		pos.Yaw, pos.Pitch, rideX, rideY, rideZ,
		c.player.EntityID, c.player.UUID, seed,
		protocol.ItemStack{ItemID: held.ItemID, Count: 1, Raw: held.Raw})
	// The throw's own sound, played to the players within earshot of the
	// thrower's feet, at the pitch the item's own roll gives it — the
	// player's random in vanilla, and one of the server's own here.
	// vanilla: SnowballItem.use, EggItem.use, EnderpearlItem.use
	pitchRng := rand.New(rand.NewSource(seed))
	c.srv.broadcastSoundID(c.theWorld(), sound, soundCategory,
		pos.X, pos.Y, pos.Z, thrownSoundVolume,
		float32(0.4/(pitchRng.Float64()*0.4+0.8)))
	// vanilla: SnowballItem.use, EggItem.use, EnderpearlItem.use
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	if mode != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return false
		}
	}
	if id != 0 {
		// vanilla: SnowballItem.use, EggItem.use, EnderpearlItem.use
		c.useSwing = swingPredicted
	}
	return id != 0
}

// thrownLanding is the block a thrown flight ended on.
type thrownLanding struct {
	w         *world.World
	hit       entity.BlockHit
	ownerUUID uuid.UUID
}

// spawnThrown lays one thrown projectile in the world given, at the
// position given, thrown along the look given by the thrower whose entity
// ID and identity are given, with the thrower's movement given riding on
// the flight. The stack names the item the metadata carries — the plain
// egg, the brown one or the blue one, the snowball, the potion with its
// contents — and an egg's item is the variant its break will hatch. The
// velocity is the shootFromRotation the vanilla use runs, and the rolls
// the body will make later come out of the same source the spread's draws
// were spent from.
func (s *Server) spawnThrown(kind entity.ThrownKind, w *world.World,
	x, y, z float64, yaw, pitch float32, rideX, rideY, rideZ float64, ownerID int32, ownerUUID uuid.UUID, seed int64, stack protocol.ItemStack) int32 {
	if stack.ItemID == 0 {
		return 0
	}
	rolls := entity.NewRolls(seed)
	aimX, aimY, aimZ := entity.LookVector(yaw, pitch)
	power, inaccuracy := thrownPower, thrownInaccuracy
	if kind.IsPotion() {
		// The potion's use throws twenty degrees above its own look —
		// only the vertical part of the aim lifted, the horizontal kept
		// at the look's — at the half power the item throws with.
		// vanilla: ThrowablePotionItem.use -> spawnProjectileFromRotation
		power = thrownPotionPower
		aimY = liftedAimY(pitch)
	}
	if kind == entity.ThrownExperienceBottle {
		// vanilla: ExperienceBottleItem.use
		power = thrownBottlePower
		aimY = liftedAimY(pitch)
	}
	vx, vy, vz := entity.ShootVelocity(aimX, aimY, aimZ, power, inaccuracy, rolls.Rand)
	// The heading the spawn tells is the throw velocity's own: vanilla
	// Projectile.shoot sets yRot/xRot from the movement it was given,
	// before shootFromRotation adds getKnownMovement.
	spawnYaw, spawnPitch := entity.ArrowAngles(vx, vy, vz)
	return s.standThrown(kind, w, x, y, z, vx+rideX, vy+rideY, vz+rideZ, spawnYaw, spawnPitch, ownerID, ownerUUID, seed, rolls, stack)
}

// standThrown stands one thrown body in the world with the velocity and
// heading given.
func (s *Server) standThrown(kind entity.ThrownKind, w *world.World, x, y, z, vx, vy, vz float64,
	spawnYaw, spawnPitch float32, ownerID int32, ownerUUID uuid.UUID, seed int64, rolls *entity.Rolls, stack protocol.ItemStack) int32 {
	thrown := entity.NewThrown(s.nextEntity(), kind, x, y, z, vx, vy, vz, ownerID, seed)
	thrown.Yaw, thrown.Pitch = spawnYaw, spawnPitch
	// The rolls one thrown body makes all come out of the one source
	// vanilla's projectile carries.
	thrown.Rand, thrown.RandSrc = rolls.Rand, rolls.Src
	it := thrownShell(thrown, w, ownerUUID, stack)
	if it == nil {
		return 0
	}
	s.launchThrown(it)
	return it.throwable.ID
}

// spawnMobPotion stands up the splash potion a witch threw.
// vanilla: Witch.performRangedAttack
func (s *Server) spawnMobPotion(throw mobPotionThrow) {
	potion, known := registry.PotionID(throw.potion)
	if !known {
		return
	}
	stack := protocol.ItemStack{ItemID: int32(registry.ItemSplashPotion), Count: 1}
	stack.SetPotion(potion)
	seed := s.levelRng.Int63()
	yaw, pitch := entity.ArrowAngles(throw.vx, throw.vy, throw.vz)
	s.standThrown(entity.ThrownSplashPotion, throw.w, throw.x, throw.y, throw.z,
		throw.vx, throw.vy, throw.vz, yaw, pitch, throw.thrower, throw.throwerUUID, seed,
		entity.NewRolls(seed), stack)
}

// liftedAimY is the vertical aim of a look pitched thrownPotionLift degrees
// higher, which the potions and the bottle o' enchanting throw along.
func liftedAimY(pitch float32) float64 {
	return -math.Sin((float64(pitch) - thrownPotionLift) * math.Pi / 180)
}

// thrownShell wraps a thrown body the way the server holds it: drawn as the
// stack given — whose item name an egg's break also hatches the variant of
// — with blank tracking, and shown from the next tick's own first pass.
// nil for an item the registry does not know. The body's Rand is the
// caller's to set: the rolls it flies on come out of the same source the
// spread's draws were spent from.
func thrownShell(thrown *entity.Thrown, w *world.World, ownerUUID uuid.UUID, stack protocol.ItemStack) *thrownEntity {
	if _, ok := registry.ItemName(stack.ItemID); !ok {
		return nil
	}
	stack.Count = 1
	return &thrownEntity{
		throwable: thrown,
		w:         w,
		uuid:      uuid.New(),
		stack:     stack,
		variant:   eggVariantOfItem(registry.Item(stack.ItemID)),
		firstPass: true,
		ownerUUID: ownerUUID,
	}
}

// summonThrownKind is the kind and the item one of the summonable thrown
// entity types stands for: the egg where the type is unknown, the command
// having refused it already.
func summonThrownKind(name string) (entity.ThrownKind, string) {
	switch name {
	case "minecraft:breeze_wind_charge":
		return entity.ThrownBreezeWindCharge, "minecraft:wind_charge"
	case "minecraft:wind_charge":
		return entity.ThrownWindCharge, "minecraft:wind_charge"
	case "minecraft:shulker_bullet":
		return entity.ThrownShulkerBullet, "minecraft:shulker_shell"
	case "minecraft:llama_spit":
		return entity.ThrownLlamaSpit, "minecraft:shulker_shell"
	case "minecraft:snowball":
		return entity.ThrownSnowball, "minecraft:snowball"
	case "minecraft:ender_pearl":
		return entity.ThrownEnderPearl, "minecraft:ender_pearl"
	case "minecraft:splash_potion":
		return entity.ThrownSplashPotion, "minecraft:splash_potion"
	case "minecraft:lingering_potion":
		return entity.ThrownLingeringPotion, "minecraft:lingering_potion"
	case "minecraft:experience_bottle":
		return entity.ThrownExperienceBottle, "minecraft:experience_bottle"
	case "minecraft:small_fireball":
		return entity.ThrownSmallFireball, "minecraft:fire_charge"
	case "minecraft:fireball":
		return entity.ThrownLargeFireball, "minecraft:fire_charge"
	case "minecraft:dragon_fireball":
		return entity.ThrownDragonFireball, "minecraft:fire_charge"
	case "minecraft:wither_skull":
		return entity.ThrownWitherSkull, "minecraft:wither_skeleton_skull"
	}
	return entity.ThrownEgg, "minecraft:egg"
}

// summonThrown stands a /summon's thrown body of the kind named — an egg, a
// snowball, an ender pearl or a potion — up at the position given, read as
// Entity.load reads its base
// fields: the motion, the rotation, the NoGravity flag, the owner and its
// left-owner exclusion, the scoreboard tags and the UUID, a fresh one where
// the tag named none. The stack the body is
// drawn as is the Item its tag carried where it carried one — an Item no
// codec reads falls back to the kind's own item, getDefaultItem — and its
// kind's own item where it carried none.
// vanilla: SummonCommand.createEntity, ThrowableItemProjectile.readAdditionalSaveData
func (s *Server) summonThrown(w *world.World, name string, x, y, z float64, tag nbt.Compound) (*thrownEntity, error) {
	kind, item := summonThrownKind(name)
	stack, ok := registry.ItemID(item)
	if !ok {
		return nil, errNotSimulated
	}
	stackOf := protocol.ItemStack{ItemID: stack, Count: 1}
	tagged := maps.Clone(tag)
	if tagged == nil {
		tagged = nbt.Compound{}
	}
	if item, carried := nbt.Get[nbt.Compound](tagged, "Item"); carried {
		// The id is qualified the way a resource location parses, so an
		// Item written without a namespace still names the item it meant.
		if id, ok := nbt.Get[nbt.String](item, "id"); ok {
			item = maps.Clone(item)
			item["id"] = nbt.String(registry.Qualify(string(id)))
		}
		// An Item no codec reads is the kind's own item, vanilla's
		// getDefaultItem fallback, not a load that came to nothing.
		if read, reads := registry.StoredStack(item); reads {
			read.Count = 1
			stackOf = read
		}
	}
	identity, live := entityUUIDOfTag(tagged)
	if !live {
		identity = uuid.New()
	}
	if s.summonDuplicate(identity) {
		return nil, command.ErrDuplicateUUID
	}
	it, err := s.thrownOfFields(w, kind, stackOf, identity, x, y, z, s.resolveOwner(entityUUIDFieldOf(tagged, "Owner")), tagged)
	if err != nil {
		return nil, err
	}
	s.registerThrown(it)
	return it, nil
}

// savedThrownKind is the kind a chunk file keeps a thrown body of, by entity
// id; false for an id that is no thrown kind.
func savedThrownKind(id string) (entity.ThrownKind, bool) {
	switch id {
	case "minecraft:snowball":
		return entity.ThrownSnowball, true
	case "minecraft:egg":
		return entity.ThrownEgg, true
	case "minecraft:ender_pearl":
		return entity.ThrownEnderPearl, true
	case "minecraft:splash_potion":
		return entity.ThrownSplashPotion, true
	case "minecraft:lingering_potion":
		return entity.ThrownLingeringPotion, true
	case "minecraft:experience_bottle":
		return entity.ThrownExperienceBottle, true
	case "minecraft:breeze_wind_charge":
		return entity.ThrownBreezeWindCharge, true
	case "minecraft:wind_charge":
		return entity.ThrownWindCharge, true
	case "minecraft:small_fireball":
		return entity.ThrownSmallFireball, true
	case "minecraft:fireball":
		return entity.ThrownLargeFireball, true
	case "minecraft:dragon_fireball":
		return entity.ThrownDragonFireball, true
	case "minecraft:wither_skull":
		return entity.ThrownWitherSkull, true
	case "minecraft:shulker_bullet":
		return entity.ThrownShulkerBullet, true
	}
	return 0, false
}

// thrownIsSaved reports whether a thrown body is of a kind the chunk file
// keeps.
func thrownIsSaved(it *thrownEntity) bool {
	_, ok := savedThrownKind(registry.EntityTypes[it.throwable.Kind.EntityType()])
	return ok
}

// thrownOfTag builds the thrown body a saved compound describes, or nil for
// one with no place, identity or stack. The owner's entity ID is resolved
// when the wake registers it.
// vanilla: ThrowableItemProjectile.readAdditionalSaveData
func (s *Server) thrownOfTag(w *world.World, compound nbt.Compound) *thrownEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	kind, ok := savedThrownKind(string(id))
	if !ok {
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
	// A missing or empty Item loads the type's default item.
	// vanilla: ThrowableItemProjectile.getItem, getDefaultItem
	stack, ok := storedItemStackOf(compound)
	if !ok || stack.Empty() {
		item, found := registry.ItemID(thrownDefaultItem(kind, string(id)))
		if !found {
			return nil
		}
		stack = protocol.ItemStack{ItemID: item, Count: 1}
	}
	it, err := s.thrownOfFields(w, kind, stack, identity, x, y, z, 0, compound)
	if err != nil {
		return nil
	}
	it.portal = rules.EntityPortal{Cooldown: portalCooldownOf(compound)}
	return it
}

// thrownOfFields stands a thrown body of kind up from a compound's base
// fields, drawn as stack and thrown by the entity ownerID names. A wake
// passes 0 and resolves the owner after the mob lock is released.
func (s *Server) thrownOfFields(w *world.World, kind entity.ThrownKind, stackOf protocol.ItemStack,
	identity uuid.UUID, x, y, z float64, ownerID int32, tagged nbt.Compound) (*thrownEntity, error) {
	// The angles are the tag's own, zero where it named none: a summon
	// makes no shot for the flight to be turned to.
	vx, vy, vz := entityMotionOfTag(tagged)
	eid := s.nextEntity()
	// The thrower's exclusion reads as a saved projectile's does: its UUID
	// off the tag, resolved to the live body's entity ID, and the
	// LeftOwner flag the save took.
	ownerUUID := entityUUIDFieldOf(tagged, "Owner")
	thrown := entity.NewThrown(eid, kind, x, y, z, vx, vy, vz, ownerID, s.entitySeed(eid))
	thrown.Yaw, thrown.Pitch = entityRotationOfTag(tagged)
	thrown.NoGravity = nbt.BoolField(tagged, "NoGravity")
	thrown.Silent = nbt.BoolField(tagged, "Silent")
	thrown.RestoreLeftOwner(nbt.BoolField(tagged, "LeftOwner"))
	readFireballFields(thrown, tagged)
	targetUUID := readBulletFields(thrown, tagged)
	it := thrownShell(thrown, w, ownerUUID, stackOf)
	if it == nil {
		return nil, errNotSimulated
	}
	it.targetUUID = targetUUID
	it.shot = nbt.BoolField(tagged, "HasBeenShot")
	it.invulnerable = nbt.BoolField(tagged, "Invulnerable")
	it.uuid = identity
	it.tags.Reset(tagsOfTag(tagged))
	it.custom = customNameOfTag(tagged)
	it.look = entityLookOf(tagged).dataOnly()
	return it, nil
}

// registerThrown puts a built thrown body into the world and shows it to
// every player in range.
func (s *Server) registerThrown(it *thrownEntity) {
	s.resolveBulletTarget(it)
	thrownTracker.register(s, it)
	// The ticket goes down at once: a pearl in a chunk nothing ticks would
	// never step to place it.
	if it.throwable.Kind == entity.ThrownEnderPearl {
		s.followPearlHold(it)
	}
}

// thrownPairing is what shows a thrown body: the spawn, then the stack it
// is drawn as.
func thrownPairing(it *thrownEntity) []outbound {
	return spawnAndMetadata(thrownSpawnPayload(it), thrownMetadataPayload(it, false))
}

// thrownBreak is one thrown body's flight coming to its end: the body, the
// place it broke at, and the chicks an egg's roll granted the break.
type thrownBreak struct {
	w       *world.World
	id      int32
	uuid    uuid.UUID
	kind    entity.ThrownKind
	x, y, z float64
	chicks  int
	// owner is the thrower's identity.
	owner uuid.UUID
	// silent drops a potion's or bottle's break sound event.
	silent bool

	// blockHit is the block a potion's flight ended on and the face it
	// entered through, where a block stopped it at all: the cells a water
	// splash's douse walks from. A body's break carries none.
	blockHit entity.BlockHit
	onBlock  bool

	// variant is the chicken variant the chicks hatch as, "" where the
	// break was no egg's. vanilla: ThrownEgg.onHit
	variant string
	// yaw is the body's yaw at the break, the facing its chicks hatch with.
	yaw float32

	// potion is what a potion's shatter came to, nil on any other kind.
	potion *potionBreak

	// xp is the experience a bottle o' enchanting's burst awards, leaving
	// along (dirX, dirY, dirZ); zero on any other kind.
	xp               int
	dirX, dirY, dirZ float64
}

// potionBreak is what one thrown potion's shatter at the place its flight
// ended came to, gathered under the tick's own locks and told once they
// open: the colour the world event's glass and particles carry and whether
// the brewing's instants pick its other event, the mobs the spray reached
// with the tells ready to send, the players to be given their instances
// after the locks, and — a lingering one — the instances the cloud left
// behind is to rain. A water splash instead carries the burning bodies it
// put out.
type potionBreak struct {
	instant bool
	colour  int32

	// water is a plain water brewing, whose splash douses fire instead of
	// landing any effect.
	water bool

	// waterPlayers are the burning players the splash reached, put out
	// once the locks open; waterMobs the burning mobs, put out under the
	// lock with the fizz gathered to play after.
	waterPlayers []*client
	waterMobs    []waterDouse

	// waterHurts are the blows the splash landed on water-sensitive mobs,
	// told once the locks open.
	waterHurts []thrownMobHit

	// instantHurts are the blows the spray's instant harm landed on mobs,
	// told once the locks open.
	instantHurts []thrownMobHit

	// mobs are the mob tells the spray's effects turned, each the packets
	// for its watchers.
	mobs []splashMobTell

	// splashed are the IDs of the mobs the spray reached, which a dodge may
	// have teleported.
	splashed []int32

	// players are the players the spray reached, each with the instances
	// their bodies are to be given.
	players         []splashPlayerTell
	mannequins      []mannequinEffectTell
	waterMannequins []*mannequinEntity

	// cloud is the instances a lingering potion's shatter leaves a cloud
	// raining; nil on a splash.
	cloud []effect.Instance

	// contents is the stack's potion contents, kept whole so a lingering
	// shatter's cloud can report them back through its NBT.
	contents protocol.PotionContents

	// ownerUUID is the thrower's identity, the Owner a lingering potion's
	// cloud reports back.
	ownerUUID uuid.UUID

	// stack is the thrown potion's item, for the plugin events.
	stack protocol.ItemStack
	// splash is an effect splash; while a PotionSplashEvent listener is
	// registered its bodies wait in pending, the instances unlanded, for
	// the event to spare or weaken them. brewed and durationScale are what
	// each one's instances are made from.
	splash        bool
	pending       []pendingSplash
	brewed        []effect.Instance
	durationScale float32

	// cloudX, cloudY, cloudZ is where a lingering potion's cloud is left: the
	// struck body's position, or the break point off a block.
	// vanilla: ThrownLingeringPotion.onHitAsPotion
	cloudX, cloudY, cloudZ float64
}

// waterDouse is one burning mob a water splash put out, carried out of the
// locks: where the fizz plays from, and the kind whose bracket it plays in —
// a monster's own fires speak hostile.
type waterDouse struct {
	w       *world.World
	kind    entity.MobType
	x, y, z float64
}

// splashMobTell is one mob the spray reached: the metadata packets its
// effect changes turn for its watchers, and the watchers to send them to.
type splashMobTell struct {
	effects  []outbound
	watchers []uuid.UUID
	// riderEffects go to the players riding the mob.
	riderEffects []outbound
	riders       []*client
}

// mobEffectTell builds what a mob's effect changes tell the players tracking
// it; false when it is not in the book or nothing changed. The caller holds
// mobsMu.
func mobEffectTell(dim *dimension, m *entity.Mob, changes []effect.Change) (splashMobTell, bool) {
	ent := dim.mobs.book[m]
	if ent == nil || len(changes) == 0 {
		return splashMobTell{}, false
	}
	return mobEffectTellOf(ent, changes), true
}

// mobEffectTellOf builds what a mob's effect changes tell its watchers and
// riders. The caller holds mobsMu.
func mobEffectTellOf(ent *mobEntity, changes []effect.Change) splashMobTell {
	tell := splashMobTell{effects: mobEffectTells(ent, changes),
		riderEffects: riderEffectPackets(ent.mob.ID, changes), riders: ent.playersAboard()}
	for id := range ent.tracked.all() {
		tell.watchers = append(tell.watchers, id)
	}
	return tell
}

// splashPlayerTell is one player the spray reached, the instances their
// body is to be given.
type splashPlayerTell struct {
	c *client
	// effects are the effects in potion order: timed ones already scaled and
	// cloned for this body, instant ones as brewed, applied in place at scale.
	effects []effect.Instance
	scale   float64
	src     instantSource
}

// instantSource is the owner and the body (potion or cloud) an instant
// effect comes from. An ownerID of 0 is no owner.
type instantSource struct {
	ownerID, bodyID int32
	ownerIsPlayer   bool
	// ownerMob is a non-player living owner, whose harm scales with
	// difficulty on a player. vanilla: DamageSource.scalesWithDifficulty
	ownerMob      bool
	ownerCreative bool
	thrown        bool
	// bodyName is the body's entity type translation, which a death message
	// names when there is no owner.
	bodyName string
	// bodyX, bodyZ is where the body is, which a blow comes from.
	// vanilla: AbstractThrownPotion.calculateHorizontalHurtKnockbackDirection, DamageSource.getSourcePosition
	bodyX, bodyZ float64
}

// landMobEffects gives m the effects in order under the mob lock: an instant
// heal in place, an instant harm as indirect magic, a timed effect added. A
// witch's own harming spares her, and instant saturation does nothing to a
// mob. The blows and effect changes to tell come back.
// vanilla: HealOrHarmMobEffect.applyInstantaneousEffect, MobEffect.applyInstantaneousEffect
func landMobEffects(dim *dimension, w *world.World, m *entity.Mob, effects []effect.Instance, scale, fromX, fromZ float64, src instantSource) (hits []thrownMobHit, changes []effect.Change) {
	changes = landLivingEffects(mobEntityType(m.Type), effects, scale, m.AddEffect, m.Heal, func(in effect.Instance, amount float64) {
		if m.OwnsHarming(in, src.ownerID) {
			return
		}
		if landed, softened := m.HurtByInstantMagic(amount, fromX, fromZ, src.ownerID, src.ownerIsPlayer, src.ownerCreative, src.thrown); landed {
			hits = append(hits, bodyMobHitOf(dim, w, src.ownerID, src.bodyID, m, rules.DamageTypeIndirectMagic, softened, true))
		}
	})
	return hits, changes
}

// landLivingEffects applies a potion's ordered effects through the body's own hurts.
// vanilla: HealOrHarmMobEffect.applyInstantaneousEffect, MobEffect.applyInstantaneousEffect
func landLivingEffects(typ int32, effects []effect.Instance, scale float64, add func(effect.Instance) (effect.Change, bool), heal func(float64), harm func(effect.Instance, float64)) (changes []effect.Change) {
	for _, in := range effects {
		if !registry.MobEffectOf(in.ID).Instant {
			if ch, ok := add(in); ok {
				changes = append(changes, ch)
			}
			continue
		}
		if !rules.IsHealOrHarm(in.ID) {
			continue
		}
		amount, hurt := rules.InstantEffect(typ, in.ID, in.Amplifier, scale)
		if !hurt {
			heal(amount)
			continue
		}
		harm(in, amount)
	}
	return changes
}

// potionBreakOf reads one potion's shatter against the bodies the tick
// handed its flight, the mob lock held: the stack's contents to the colour
// and the instant pick it tells, the spray measured out to every body in
// reach under the margins the flight itself granted, and — a lingering
// one — the cloud's instances instead, their durations scaled by the
// lingering potion's own potion_duration_scale. A plain water brewing takes
// the water path: no effect lands, and the burning bodies its own reach
// finds are put out here with the mobs at hand. A brewing with
// no effects at all does neither: no spray, no cloud, the world event
// alone. What the spray left in a mob is applied here with the mob at hand,
// the tells gathered for the locks to open; a player's instances are only
// gathered, the bodies the players' own locks own.
// vanilla: AbstractThrownPotion.onHit, ThrownSplashPotion.onHitAsPotion,
// ThrownLingeringPotion.onHitAsPotion
func (s *Server) potionBreakOf(it *thrownEntity, dim *dimension, mobsByID map[int32]*entity.Mob,
	clientsByID map[int32]*client, targets []entity.ArrowTarget, mannequinsByID map[int32]*mannequinEntity) *potionBreak {
	contents, _ := it.stack.PotionContentsOf()
	shl := &potionBreak{
		stack:     it.stack,
		instant:   rules.InstantBrewing(contents),
		colour:    rules.PotionColour(contents),
		contents:  contents,
		ownerUUID: it.ownerUUID,
	}
	if rules.PotionIsWater(contents) {
		shl.water = true
		for _, target := range it.throwable.WaterHits(it.throwable.X, it.throwable.Y, it.throwable.Z, targets) {
			if target.Type == protocol.EntityTypeMannequin {
				shl.waterMannequins = append(shl.waterMannequins, mannequinsByID[target.EntityID])
				continue
			}
			if m := mobsByID[target.EntityID]; m != nil {
				shl.splashed = append(shl.splashed, m.ID)
				m.Rehydrate()
				owner := it.throwable.OwnerID
				if landed, softened := m.HurtByWaterSplash(it.throwable.X, it.throwable.Z, owner,
					shooterIsPlayer(owner, clientsByID), shooterCreative(owner, clientsByID)); landed {
					shl.waterHurts = append(shl.waterHurts, thrownMobHitOf(dim, it, m, rules.DamageTypeIndirectMagic, softened, true))
				}
				if !m.Dead && m.OnFire() {
					m.SetMetadataFireTicks(0)
					shl.waterMobs = append(shl.waterMobs, waterDouse{w: it.w, kind: m.Type, x: m.X, y: m.Y, z: m.Z})
				}
				continue
			}
			if c := clientsByID[target.EntityID]; c != nil && !c.player.Vitals().Dead &&
				c.player.Vitals().FireTicks > 0 {
				shl.waterPlayers = append(shl.waterPlayers, c)
			}
		}
		return shl
	}
	brewed := rules.PotionInstances(contents)
	if len(brewed) == 0 {
		return shl
	}
	if it.throwable.Kind == entity.ThrownLingeringPotion {
		cloud := make([]effect.Instance, 0, len(brewed))
		for _, in := range brewed {
			cloud = append(cloud, rules.LingeringInstance(effect.Clone(in), rules.LingeringDurationScale))
		}
		shl.cloud = cloud
		return shl
	}
	durationScale := it.stack.PotionDurationScaleOf()
	deferred := s.plugins != nil && s.plugins.Has(pluginruntime.PotionSplash)
	shl.splash, shl.brewed, shl.durationScale = true, brewed, durationScale
	for _, hit := range it.throwable.SplashHits(it.throwable.X, it.throwable.Y, it.throwable.Z, targets) {
		landing := splashLanding(brewed, hit.Scale, durationScale)
		if len(landing) == 0 {
			continue
		}
		owner := it.throwable.OwnerID
		src := instantSource{ownerID: owner, ownerMob: mobsByID[owner] != nil, bodyID: it.throwable.ID, bodyName: "entity.minecraft.splash_potion",
			bodyX: it.throwable.X, bodyZ: it.throwable.Z}
		src.ownerIsPlayer, src.ownerCreative, src.thrown = shooterIsPlayer(owner, clientsByID), shooterCreative(owner, clientsByID), true
		if hit.Target.Type == protocol.EntityTypeMannequin {
			shl.mannequins = append(shl.mannequins, mannequinEffectTell{w: it.w, ent: mannequinsByID[hit.Target.EntityID], effects: landing, scale: hit.Scale, src: src})
			continue
		}
		if m := mobsByID[hit.Target.EntityID]; m != nil {
			shl.splashed = append(shl.splashed, m.ID)
			if ent := dim.mobs.book[m]; deferred && ent != nil {
				shl.pending = append(shl.pending, pendingSplash{mob: m.ID, id: ent.uuid, scale: hit.Scale, src: src})
				continue
			}
			hits, changes := landMobEffects(dim, it.w, m, landing, hit.Scale, it.throwable.X, it.throwable.Z, src)
			shl.instantHurts = append(shl.instantHurts, hits...)
			if tell, ok := mobEffectTell(dim, m, changes); ok {
				shl.mobs = append(shl.mobs, tell)
			}
			continue
		}
		// The dying are passed up whole: the spray and the cloud both
		// only land on living bodies. A mob gone from the herd is out
		// of the lists already; the player's vitals answer for theirs.
		// vanilla: isAffectedByPotions, !isDeadOrDying
		if c := clientsByID[hit.Target.EntityID]; c != nil && !c.player.Vitals().Dead {
			if deferred {
				shl.pending = append(shl.pending, pendingSplash{c: c, scale: hit.Scale, src: src})
				continue
			}
			shl.players = append(shl.players, splashPlayerTell{c: c, effects: landing, scale: hit.Scale, src: src})
		}
	}
	return shl
}

// splashLanding is the potion's instances as a body at the given scale
// gets them: timed ones scaled and cloned, instant ones as brewed.
// vanilla: ThrownSplashPotion.onHitAsPotion
func splashLanding(brewed []effect.Instance, scale float64, durationScale float32) []effect.Instance {
	var landing []effect.Instance
	for _, in := range brewed {
		if registry.MobEffectOf(in.ID).Instant {
			landing = append(landing, in)
		} else if scaled, ok := rules.SplashInstance(in, scale, durationScale); ok {
			landing = append(landing, effect.Clone(scaled))
		}
	}
	return landing
}

// dowseWaterFire puts out the fire a water splash fell on, walking the
// cells vanilla's onHitBlock douses: the block one step out along the face
// the potion broke against, the hit block itself, and that first cell's four
// horizontal neighbours. A doused candle also plays block.candle.extinguish;
// a doused campfire's level event carries its own. A destroyed fire is air.
// vanilla: AbstractThrownPotion.onHitBlock, dowseFire
func (s *Server) dowseWaterFire(w *world.World, bx, by, bz, face int32) {
	for _, cell := range rules.WaterDowseCells(bx, by, bz, face) {
		newState, event, candle, ok := rules.DowseFire(w.BlockAt(cell[0], cell[1], cell[2]))
		if !ok {
			continue
		}
		s.setBlockAndTell(w, cell[0], cell[1], cell[2], newState)
		if candle {
			s.broadcastSoundPacketExcept(w, nil, candleExtinguishSound(float64(cell[0]), float64(cell[1]), float64(cell[2])))
			s.gameEvent(w, rules.GameEventBlockChange, float64(cell[0])+0.5, float64(cell[1])+0.5, float64(cell[2])+0.5, rules.GameEventContext{})
		}
		if event != 0 {
			s.broadcastLevelEvent(w, event, cell[0], cell[1], cell[2], 0)
		}
	}
}

// thrownMobHit is one mob a thrown blow landed on, carried out of the
// locks: the damage event's naming, the velocity the hurt's knockback
// left, the health where it left any, and where the voice plays from.
type thrownMobHit struct {
	damageType registry.DamageType
	w          *world.World
	victimID   int32
	kind       entity.MobType
	sound      protocol.SoundHolder
	volume     float32
	pitch      float32
	silent     bool
	causeID    int32
	directID   int32
	x, y, z    float64
	vx, vy, vz float64
	kb         *mobKnockback
	health     []byte
	// watchers are the players shown the mob, who alone are told of the hurt.
	watchers []uuid.UUID
	// softened is a blow that landed only its excess through the mob's
	// invulnerability window: its health alone is told of, none of a whole
	// hurt's damage event, shove or hurt sound.
	softened bool
}

// thrownSpawnPayload builds the Spawn Entity packet that puts a thrown body
// in the world for a client: the body's entity ID and UUID, the thrown
// entity type its kind is, where it is and how fast it flies. The data
// field is the thrower's entity ID, as vanilla's
// Projectile.getAddEntityPacket gives it — nought for a body nobody owns.
func thrownSpawnPayload(it *thrownEntity) []byte {
	t := it.throwable
	return spawnEntityPayload(spawnEntity{
		id: t.ID, uuid: it.uuid, typ: t.Kind.EntityType(),
		x: t.X, y: t.Y, z: t.Z,
		vx: t.VX, vy: t.VY, vz: t.VZ,
		pitch: encodeAngle(t.Pitch),
		yaw:   encodeAngle(t.Yaw),
		data:  t.OwnerID,
	})
}

// thrownMetadataPayload builds the Set Entity Metadata packet carrying the
// no-gravity flag, and the item of a body that is drawn as one. full sends
// the flag even when it is off, so a /data write that clears it tells the
// client; without it an off flag is the client's own default, which an
// absence of tells says.
// vanilla: SynchedEntityData.packDirty
func thrownMetadataPayload(it *thrownEntity, full bool) []byte {
	var b protocol.Buffer
	b.PutVarInt(it.throwable.ID)
	if full || it.throwable.NoGravity {
		b.PutMetadata(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean)
		b.PutBool(it.throwable.NoGravity)
	}
	// A wind charge is no ThrowableItemProjectile: it has no item slot, and
	// a client rejects an index past its synched data. A wither skull's
	// only datum is its dangerous flag, at the item slot's index.
	// vanilla: AbstractHurtingProjectile.defineSynchedData, WitherSkull.defineSynchedData
	if it.throwable.Kind == entity.ThrownWitherSkull {
		if full || it.throwable.Dangerous {
			b.PutMetadata(protocol.MetadataIndexItemSlot, protocol.MetadataBoolean)
			b.PutBool(it.throwable.Dangerous)
		}
	} else if !it.throwable.Kind.IsWindCharge() && it.throwable.Kind != entity.ThrownShulkerBullet &&
		it.throwable.Kind != entity.ThrownLlamaSpit && it.throwable.Kind != entity.ThrownDragonFireball {
		b.PutMetadata(protocol.MetadataIndexItemSlot, protocol.MetadataSlot)
		b.PutSlot(it.stack)
	}
	was := customName{}
	if full {
		// Unlike any name, so both fields go out.
		was = customName{wire: "\x00", visible: !it.custom.visible}
	}
	it.custom.putMetadata(&b, was)
	b.EndMetadata()
	return b.Bytes()
}

// thrownZeroDeltaPayload builds the move_entity_pos packet that carries no
// distance at all: the tracked entity's own first pass.
func thrownZeroDeltaPayload(it *thrownEntity) []byte {
	var b protocol.Buffer
	b.Grow(entityPositionRotLen)
	protocol.PutMoveEntityPos(&b, it.throwable.ID, 0, 0, 0, false)
	return b.Bytes()
}

// thrownTeleportPayload builds the entity_position_sync packet that puts a
// thrown body where its flight has carried it; none on the ground, flying.
func thrownTeleportPayload(t *entity.Thrown) []byte {
	var b protocol.Buffer
	b.Grow(teleportEntityLen)
	protocol.PutEntityPositionSync(&b, t.ID, t.X, t.Y, t.Z, t.Yaw, t.Pitch, false)
	return b.Bytes()
}

// killThrown discards a thrown body the way the /kill command and vanilla's
// Entity.kill do: out of the world in flight, nothing laid down behind it.
func (s *Server) killThrown(it *thrownEntity) {
	s.arrowsMu.Lock()
	it.throwable.Dead = true
	s.arrowsMu.Unlock()
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{it.throwable.ID}))
}

// teleportThrown moves a thrown body to the position given and stops it.
// vanilla: Entity.teleportTo
func (s *Server) teleportThrown(it *thrownEntity, to player.Position, by string) {
	thrownTracker.teleport(s, it, func() {
		it.throwable.X, it.throwable.Y, it.throwable.Z = to.X, to.Y, to.Z
		it.throwable.VX, it.throwable.VY, it.throwable.VZ = 0, 0, 0
	})
	logTeleported("thrown body", to, by)
}

// thrownTwinAt builds the thrown body that continues this one in another
// dimension, standing at pos: a new body, new ID but the same UUID,
// carrying the old one's state across with no file in between — kind,
// stack, age, velocity, rolls to come, tags. The caller holds the arrow
// lock or has taken the body out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) thrownTwinAt(it *thrownEntity, to *dimension, pos player.Position) *thrownEntity {
	t := it.throwable
	return &thrownEntity{
		throwable: &entity.Thrown{
			ID:   s.nextEntity(),
			Kind: t.Kind,
			X:    pos.X,
			Y:    pos.Y,
			Z:    pos.Z,
			VX:   t.VX, VY: t.VY, VZ: t.VZ,
			Yaw:   t.Yaw,
			Pitch: t.Pitch,
			// The age the flight has run rides along, the hit test's
			// margin and the tracked sends' rhythm both reading it.
			TickCount: t.TickCount,
			// The rolls the body has still to make come out of the one
			// source the throw seeded, carried across as they stand.
			Rand: t.Rand,
		},
		w:         to.w,
		uuid:      it.uuid,
		stack:     it.stack,
		variant:   it.variant,
		firstPass: true,
		ownerUUID: it.ownerUUID,
		// The tags are copied, not shared: the old body's death must not
		// leave the twin's list aliased to it.
		tags:   nbt.NewStringSet(it.tags.Values()),
		custom: it.custom,
	}
}

// portalTwinOfThrown is thrownTwinAt at a portal's arrival, the yaw turned
// by the angle between the two portals and the arrival's own cooldown
// armed.
func (s *Server) portalTwinOfThrown(it *thrownEntity, to *dimension, arrival world.SpawnPos, kind rules.PortalKind, exitAxis [2]int32) *thrownEntity {
	twin := s.thrownTwinAt(it, to, player.Position{
		X: float64(arrival.X) + 0.5,
		Y: float64(arrival.Y),
		Z: float64(arrival.Z) + 0.5,
	})
	// The arrival's own cooldown, the projectile's 2 ticks (vanilla:
	// Projectile.getDimensionChangingDelay), holding off a bounce back.
	twin.portal = rules.EntityPortal{Cooldown: projectileCooldownTicks}
	if turn := portalTurnAt(it.w, kind, player.Position{X: it.throwable.X, Y: it.throwable.Y, Z: it.throwable.Z, Yaw: twin.throwable.Yaw}, exitAxis); turn != 0 {
		// The turn is the yaw's alone: vanilla's transition carries the
		// rotation relative and the movement without ROTATE_DELTA, so the
		// flight keeps the direction it had however the portals run.
		twin.throwable.Yaw += turn
	}
	return twin
}

// finishThrownTravel sends one thrown body the portal pass caught across
// the dimension its portal goes to: the arrival found or built, the arrival
// chunk woken, the thrower's reference resolved against the world arrived
// in, the new body shown, and the one left behind taken out of every view.
func (s *Server) finishThrownTravel(it *thrownEntity, kind rules.PortalKind) {
	from := s.dimensionOfWorld(it.w)
	pos := player.Position{X: it.throwable.X, Y: it.throwable.Y, Z: it.throwable.Z}
	// An ender pearl stays in the end only while an online owner has yet
	// to see the credits: vanilla's canTeleport asks the credits of a
	// ServerPlayer owner, so an ownerless or offline-owner pearl crosses.
	// The body is put back to fly on where it is, the portal cooldown
	// already spent.
	// vanilla: ThrownEnderpearl.canTeleport
	if kind == rules.PortalEnd && from.w.DimensionTypeName() == world.EndType && it.throwable.Kind == entity.ThrownEnderPearl {
		if owner := s.clientByUUID(it.ownerUUID); owner != nil && !owner.player.SeenCredits() {
			s.arrowsMu.Lock()
			s.thrown = append(s.thrown, it)
			s.arrowsMu.Unlock()
			return
		}
	}
	s.crossPortal(it.w, it.throwable.ID, pos, kind, func(to *dimension, spot world.SpawnPos, exitAxis [2]int32) func() {
		twin := s.portalTwinOfThrown(it, to, spot, kind, exitAxis)
		return func() {
			twin.throwable.OwnerID = s.resolveOwner(twin.ownerUUID)
			s.registerThrown(twin)
			slog.Info("thrown body travelled through a portal", "to", to.name, "x", twin.throwable.X, "y", twin.throwable.Y, "z", twin.throwable.Z)
		}
	}, func() {
		s.arrowsMu.Lock()
		defer s.arrowsMu.Unlock()
		s.thrown = append(s.thrown, it)
	})
}

// witchThrewPotion queues a witch's splash potion and the throw's sound.
func (t *mobDimTick) witchThrewPotion(fx *mobEffects, ev entity.MobEvent) {
	h, w := t.h, t.w
	// A witch threw a splash potion: stood in the world outside
	// the locks, owned by the witch.
	fx.potions = append(fx.potions, mobPotionThrow{
		w: w, thrower: ev.Mob.ID, potion: ev.Potion,
		x: ev.X, y: ev.Y, z: ev.Z, vx: ev.VX, vy: ev.VY, vz: ev.VZ,
	})
	if ent, ok := h.book[ev.Mob]; ok {
		fx.potions[len(fx.potions)-1].throwerUUID = ent.uuid
	}
	fx.voiceID(w, ev.Mob, registry.SoundEntityWitchThrow, 1, ev.Pitch)
}
