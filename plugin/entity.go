package plugin

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Entity is any entity. Assert it to LivingEntity, Mob, Player and the
// other interfaces for what its kind has. Bukkit: Entity
type Entity interface {
	// ID is the entity's network ID, unique while it is loaded; UUID is its
	// lasting identity.
	ID() int32
	UUID() UUID
	Type() registry.EntityType
	// IsValid reports whether the entity is alive and loaded.
	IsValid() bool

	World() World
	Location() Location
	Pos() Vec3
	// Teleport moves the entity, across worlds too; false when an event
	// cancelled it or the entity cannot move there.
	Teleport(to Location, cause TeleportCause) bool
	Velocity() Vec3
	SetVelocity(Vec3)
	OnGround() bool
	BoundingBox() Box

	Nameable
	// NameVisible shows the custom name without looking at the entity.
	NameVisible() bool
	SetNameVisible(bool)
	Glowing() bool
	SetGlowing(bool)
	Silent() bool
	SetSilent(bool)
	NoGravity() bool
	SetNoGravity(bool)
	Invulnerable() bool
	SetInvulnerable(bool)
	// FireTicks is how long it burns; FrozenTicks how long it has been in
	// powder snow.
	FireTicks() int
	SetFireTicks(int)
	FrozenTicks() int
	SetFrozenTicks(int)
	// Tags are the entity's scoreboard tags (/tag).
	Tags() []string
	AddTag(string) bool
	RemoveTag(string) bool

	Vehicle() (Entity, bool)
	Passengers() []Entity
	AddPassenger(Entity) bool
	RemovePassenger(Entity) bool
	// Dismount leaves the vehicle, if any.
	Dismount()

	// Nearby answers the entities whose boxes come within r of this one's.
	Nearby(r float64) []Entity
	// Remove removes the entity as /kill does without the death: no drops,
	// no death event. A player is not removed: kick them instead.
	Remove()
	// Data is the plugins' data saved with the entity.
	Data() Data
	// SpawnReason answers why the entity spawned, saved with it; SpawnDefault
	// for an entity whose reason was not recorded.
	// Paper: Entity.getEntitySpawnReason
	SpawnReason() SpawnReason
}

// LivingEntity is a mob, an armour stand or a player. Bukkit: LivingEntity
type LivingEntity interface {
	Entity
	Health() float32
	SetHealth(float32)
	// MaxHealth is the max_health attribute's value.
	MaxHealth() float32
	Absorption() float32
	SetAbsorption(float32)
	// Attribute answers one of the entity's attributes, false when its
	// kind has none.
	Attribute(id registry.AttributeID) (AttributeInstance, bool)
	// Damage hurts the entity as source would, with every rule vanilla's
	// hurt applies; false when nothing was taken.
	Damage(amount float32, source DamageSource) bool
	// Heal adds health, firing EntityRegainHealthEvent.
	Heal(amount float32, reason RegainReason)
	// Kill kills it as /kill does.
	Kill()
	// Killer answers the player credited with the last kill or hit.
	Killer() (Player, bool)

	Effects() []EffectInstance
	Effect(e Effect) (EffectInstance, bool)
	// AddEffect applies e as a potion would; false when the entity is
	// immune or has a stronger one.
	AddEffect(e EffectInstance) bool
	RemoveEffect(e Effect) bool
	ClearEffects()

	Equipment() Equipment
	EyePos() Vec3
	// HasLineOfSight reports whether the entity can see other, as mobs
	// check before attacking.
	HasLineOfSight(other Entity) bool
	// RayTrace answers the first block or entity within max blocks of the
	// eyes along the look direction.
	RayTrace(max float64, fluids bool) (RayHit, bool)
	// TargetBlock answers the block the eyes look at within max blocks,
	// fluids ignored. Bukkit: getTargetBlockExact
	TargetBlock(max int) (Block, bool)
	// LineOfSight answers the blocks from the eyes along the look direction,
	// up to max blocks (capped at 120), ending with the first block passes
	// rejects; nil passes lets air through only. Bukkit: getLineOfSight
	LineOfSight(max int, passes func(BlockState) bool) []Block
	// TargetBlockPassing answers the last block of LineOfSight: the one that
	// stopped it, or the last one reached. Bukkit: getTargetBlock
	TargetBlockPassing(max int, passes func(BlockState) bool) Block
	// Air is the breath left, in ticks.
	Air() int
	SetAir(int)
	// Invulnerable time after a hit, in ticks.
	NoDamageTicks() int
	SetNoDamageTicks(int)
	Swing(h Hand)
	Collidable() bool
	SetCollidable(bool)
	Gliding() bool
	SetGliding(bool)
	Sleeping() bool
	ArrowsInBody() int
	SetArrowsInBody(int)
	// Active item use: a drawn bow, a raised shield, food being eaten.
	ActiveItem() (ItemStack, Hand, bool)
	// LaunchProjectile launches a projectile of type t as the entity
	// shooting it, with velocity v (zero for the kind's own).
	LaunchProjectile(t registry.EntityType, v Vec3) (Projectile, error)
}

// AttributeInstance is an entity's attribute.
type AttributeInstance interface {
	Base() float64
	SetBase(float64)
	// Value answers the base with every modifier applied.
	Value() float64
	Modifiers() []AttributeModifier
	AddModifier(AttributeModifier)
	RemoveModifier(id Key)
}

// AttributeModifier is a modifier keyed by its ID.
type AttributeModifier struct {
	ID        Key
	Amount    float64
	Operation registry.AttributeOperation
}

// EffectInstance is an active status effect.
type EffectInstance struct {
	Effect Effect
	// Duration is in ticks, -1 for infinite; Amplifier 0 is level I.
	Duration, Amplifier      int
	Ambient, Particles, Icon bool
}

// DamageSource is what hurt an entity: its type and what caused it.
// The type is also the reason Bukkit calls a damage cause: Kill and
// SetHealth(0) hurt with the generic_kill type (Bukkit's SUICIDE, the
// "died" death message), and Damage with the generic type stands for
// CUSTOM; any other type may be passed to Damage. Look a type up with
// Registries().DamageType(MinecraftKey("generic_kill")).
// vanilla: DamageSource
type DamageSource struct {
	Type DamageType
	// Causing is the entity responsible (the shooter); Direct the one that
	// hit (the arrow). Either may be nil.
	Causing, Direct Entity
	// Pos is where the damage came from, for damage with no entity (a bed
	// exploding); nil when it has none.
	Pos *Vec3
}

// RegainReason is why an entity healed.
type RegainReason uint8

const (
	RegainNatural RegainReason = iota
	RegainSatiated
	RegainRegeneration
	RegainEating
	RegainMagic
	RegainWitherSpawn
	RegainEnderCrystal
	RegainPlugin
)

// RayHit is what a ray trace met: a block face or an entity.
type RayHit struct {
	Pos    Vec3
	Block  *Block
	Face   BlockFace
	Entity Entity
}

// Equipment is what an entity wears and holds. Bukkit: EntityEquipment
type Equipment interface {
	Get(s EquipmentSlot) ItemStack
	Set(s EquipmentSlot, item ItemStack)
	// DropChance is a mob's chance, 0 to 1, of dropping the slot's item
	// (above 1: always, undamaged).
	DropChance(s EquipmentSlot) float32
	SetDropChance(s EquipmentSlot, chance float32)
}

// Mob is a mob with AI. Bukkit: Mob
type Mob interface {
	LivingEntity
	Target() (LivingEntity, bool)
	// SetTarget sets the attack target, nil to clear it, firing
	// EntityTargetEvent.
	SetTarget(LivingEntity)
	// AI turns its goals and movement off (NoAI).
	AI() bool
	SetAI(bool)
	// SavedData is a copy of the mob's saved compound, as /data get entity
	// shows it; nil for a mob that is gone.
	SavedData() nbt.Compound
	// MergeData merges fields into the saved compound and loads it back, as
	// /data merge entity does; it reports whether the mob took it. Typed
	// setters cover the common fields; this one reaches the rest (shulker
	// colour, fox type, panda genes, tropical fish pattern, ...).
	MergeData(fields nbt.Compound) bool
	// Persistent keeps it from despawning (PersistenceRequired).
	Persistent() bool
	SetPersistent(bool)
	LeftHanded() bool
	SetLeftHanded(bool)
	// MoveTo paths to pos at speed times its movement speed; false when no
	// path is found.
	MoveTo(pos Vec3, speed float64) bool
	StopMoving()
	LookAt(pos Vec3)
	Leash() (Entity, bool)
	SetLeash(holder Entity)
	// Loot is the loot table the mob drops on death.
	Loot() (table Key, seed int64)
	SetLoot(table Key, seed int64)
}

// Ageable is a mob with a baby form.
type Ageable interface {
	Mob
	// Age is negative while a baby (ticks to grow up), positive while it
	// cannot breed again.
	Age() int
	SetAge(int)
	Baby() bool
	SetBaby(bool)
}

// Breedable is an animal.
type Breedable interface {
	Ageable
	// LoveTicks is the time left in love mode.
	LoveTicks() int
	SetLoveTicks(int)
	IsBreedingItem(s ItemStack) bool
}

// Tameable is a wolf, cat, parrot, horse or another tameable mob.
type Tameable interface {
	Breedable
	Tamed() bool
	Owner() (UUID, bool)
	// SetOwner tames it to owner; the zero UUID untames it.
	SetOwner(owner UUID)
	// SetTamed sets the tamed flag without touching the owner, so a mob can be
	// tame with no owner. The flag is not saved without an owner (vanilla:
	// TamableAnimal.readAdditionalSaveData). Bukkit: Tameable.setTamed
	SetTamed(bool)
	Sitting() bool
	SetSitting(bool)
}

// Villager is a villager or wandering trader's trading state.
type Villager interface {
	Ageable
	// Profession, Type and Level are the villager's data; a wandering
	// trader has none.
	Profession() Key
	SetProfession(Key)
	VillagerType() Key
	SetVillagerType(Key)
	Level() int
	SetLevel(int)
	Experience() int
	SetExperience(int)
	Trades() []Trade
	SetTrades([]Trade)
	// Trader answers the player trading with it.
	Trader() (Player, bool)
}

// Trade is a merchant offer.
type Trade struct {
	Cost, Cost2, Result  ItemStack
	Uses, MaxUses, XP    int
	PriceMultiplier      float32
	Demand, SpecialPrice int
	RewardsExperience    bool
}

// ItemEntity is a dropped item. Bukkit: Item
type ItemEntity interface {
	Entity
	Stack() ItemStack
	SetStack(ItemStack)
	// PickupDelay is the ticks before it can be picked up, 32767 for never.
	PickupDelay() int
	SetPickupDelay(int)
	// Owner may pick it up first; Thrower dropped it.
	Owner() (UUID, bool)
	SetOwner(UUID)
	Thrower() (UUID, bool)
	// Age is the ticks it has existed; it despawns at 6000.
	Age() int
	SetAge(int)
}

// Sheep is a sheep. Bukkit: Sheep
type Sheep interface {
	Breedable
	Color() DyeColor
	SetColor(DyeColor)
	Sheared() bool
	SetSheared(bool)
}

// VariantHolder is a mob with a registry variant: a cat, wolf, frog, pig,
// cow or chicken. Bukkit: Cat.getCatType, Wolf.getVariant, Frog.getVariant
type VariantHolder interface {
	Entity
	// Variant answers the variant's key, e.g. minecraft:black for a cat; the
	// zero Key when the mob has none saved.
	Variant() Key
	// SetVariant changes it; a key outside the variant's registry is ignored.
	SetVariant(Key)
}

// Collared is a wolf or cat. Bukkit: Wolf.getCollarColor, Cat.getCollarColor
type Collared interface {
	Tameable
	CollarColor() DyeColor
	SetCollarColor(DyeColor)
}

// Horse is a horse. Bukkit: Horse
type Horse interface {
	Breedable
	Color() HorseColor
	SetColor(HorseColor)
	Markings() HorseMarkings
	SetMarkings(HorseMarkings)
}

// HorseColor is a horse's coat colour, in vanilla's order.
type HorseColor uint8

const (
	HorseWhite HorseColor = iota
	HorseCreamy
	HorseChestnut
	HorseBrown
	HorseBlack
	HorseGray
	HorseDarkBrown
)

// HorseMarkings is a horse's marking pattern, in vanilla's order.
type HorseMarkings uint8

const (
	MarkingsNone HorseMarkings = iota
	MarkingsWhite
	MarkingsWhiteField
	MarkingsWhiteDots
	MarkingsBlackDots
)

// Creeper is a creeper. Bukkit: Creeper
type Creeper interface {
	Mob
	// Powered is whether lightning charged it.
	Powered() bool
	SetPowered(bool)
}

// Slime is a slime or magma cube. Bukkit: Slime
type Slime interface {
	Mob
	// Size is 1 for the smallest, 2 for the middle, 4 for the largest;
	// SetSize accepts 1 to 127 and keeps its health and attributes in step.
	Size() int
	SetSize(int)
}

// Firework is a firework rocket in flight. Bukkit: Firework
type Firework interface {
	Projectile
	// Item is the rocket item; its fireworks component gives the flight
	// duration and explosions.
	Item() ItemStack
	SetItem(ItemStack)
}

// ExperienceOrb is an experience orb.
type ExperienceOrb interface {
	Entity
	Value() int
	SetValue(int)
}

// Projectile is an arrow, trident, snowball, egg, pearl, potion, fireball
// or another projectile.
type Projectile interface {
	Entity
	Shooter() (Entity, bool)
	SetShooter(Entity)
}

// Arrow is an arrow, spectral arrow or trident.
type Arrow interface {
	Projectile
	BaseDamage() float64
	SetBaseDamage(float64)
	Critical() bool
	SetCritical(bool)
	PierceLevel() int
	SetPierceLevel(int)
	// Pickup is who may pick it up.
	Pickup() ArrowPickup
	SetPickup(ArrowPickup)
	InGround() bool
	// Potion is the tipped arrow's potion and the effects it applies on a
	// hit; the key is zero for an arrow with only custom effects, and ok is
	// false for a plain arrow, a spectral arrow or a trident.
	Potion() (Key, []EffectInstance, bool)
}

// ArrowPickup is who may pick up an arrow.
type ArrowPickup uint8

const (
	PickupDisallowed ArrowPickup = iota
	PickupAllowed
	PickupCreativeOnly
)

// FallingBlock is a falling block entity.
type FallingBlock interface {
	Entity
	State() BlockState
	DropItem() bool
	SetDropItem(bool)
	HurtEntities() bool
	SetHurtEntities(bool)
}

// TNT is primed TNT.
type TNT interface {
	Entity
	Fuse() int
	SetFuse(int)
	Source() (Entity, bool)
	// Yield is the explosion's power, 4 by default.
	Yield() float32
	SetYield(float32)
}

// ArmorStand is an armour stand.
type ArmorStand interface {
	LivingEntity
	Small() bool
	SetSmall(bool)
	Arms() bool
	SetArms(bool)
	BasePlate() bool
	SetBasePlate(bool)
	Marker() bool
	SetMarker(bool)
	Visible() bool
	SetVisible(bool)
	// Pose answers a part's rotation in degrees.
	Pose(part ArmorStandPart) Vec3
	SetPose(part ArmorStandPart, rot Vec3)
}

// ArmorStandPart is a posable part of an armour stand.
type ArmorStandPart uint8

const (
	PartHead ArmorStandPart = iota
	PartBody
	PartLeftArm
	PartRightArm
	PartLeftLeg
	PartRightLeg
)

// Hanging is an item frame, painting or leash knot.
type Hanging interface {
	Entity
	Facing() BlockFace
	SetFacing(f BlockFace, force bool) bool
}

// ItemFrame is an item frame or glow item frame.
type ItemFrame interface {
	Hanging
	Item() ItemStack
	SetItem(ItemStack)
	// Rotation is 0 to 7, in 45 degree steps.
	Rotation() int
	SetRotation(int)
	Fixed() bool
	SetFixed(bool)
	Visible() bool
	SetVisible(bool)
}

// Display is a block, item or text display entity: the entities plugins
// use for holograms and decorations. Bukkit: Display
type Display interface {
	Entity
	// Transform is the model's translation, rotations and scale.
	Transform() Transform
	SetTransform(Transform)
	// Interpolation runs the next transform change over duration ticks,
	// starting delay ticks from now.
	SetInterpolation(delay, duration int)
	Billboard() Billboard
	SetBillboard(Billboard)
	// Brightness overrides the light it is drawn with; nil for the world's.
	Brightness() *[2]int
	SetBrightness(*[2]int)
	ViewRange() float32
	SetViewRange(float32)
	Shadow() (radius, strength float32)
	SetShadow(radius, strength float32)
	GlowColor() int32
	SetGlowColor(int32)
}

// Transform is a display entity's transformation.
type Transform struct {
	Translation, Scale          [3]float32
	LeftRotation, RightRotation [4]float32 // quaternions x, y, z, w
}

// Billboard is how a display turns to face the camera.
type Billboard uint8

const (
	BillboardFixed Billboard = iota
	BillboardVertical
	BillboardHorizontal
	BillboardCenter
)

// BlockDisplay shows a block state.
type BlockDisplay interface {
	Display
	State() BlockState
	SetState(BlockState)
}

// ItemDisplay shows an item.
type ItemDisplay interface {
	Display
	Item() ItemStack
	SetItem(ItemStack)
}

// TextDisplay shows text.
type TextDisplay interface {
	Display
	Text() Text
	SetText(Text)
	LineWidth() int
	SetLineWidth(int)
	// Background is the ARGB background colour.
	Background() uint32
	SetBackground(uint32)
	SeeThrough() bool
	SetSeeThrough(bool)
}

// Interaction is an invisible box that reports clicks; plugins pair it with
// a display.
type Interaction interface {
	Entity
	Size() (width, height float32)
	SetSize(width, height float32)
	Responsive() bool
	SetResponsive(bool)
}

// Vehicle is a minecart or boat.
type Vehicle interface {
	Entity
	// DamageTaken is the damage the vehicle has taken; it breaks at 40.
	DamageTaken() float32
	SetDamageTaken(float32)
}

// TeleportCause is why an entity teleported.
type TeleportCause uint8

const (
	TeleportPlugin TeleportCause = iota
	TeleportCommand
	TeleportEnderPearl
	TeleportChorusFruit
	TeleportNetherPortal
	TeleportEndPortal
	TeleportEndGateway
	TeleportSpectate
	TeleportDismount
	TeleportExitBed
	TeleportUnknown
)

// SpawnReason is why an entity spawned. Bukkit: CreatureSpawnEvent.SpawnReason
type SpawnReason uint8

const (
	SpawnNatural SpawnReason = iota
	SpawnChunkGeneration
	SpawnSpawner
	SpawnTrialSpawner
	SpawnEgg
	SpawnBreeding
	SpawnCommand
	SpawnPlugin
	SpawnDispenser
	SpawnStructure
	SpawnPatrol
	SpawnRaid
	SpawnReinforcements
	SpawnConversion
	SpawnBucket
	SpawnLightning
	SpawnBuildIronGolem
	SpawnBuildSnowGolem
	SpawnBuildWither
	SpawnVillageDefense
	SpawnJockey
	SpawnTriggered
	SpawnDefault
	// SpawnSpawnerEgg is a spawn egg used on a block, liquid or mob.
	// Bukkit: SPAWNER_EGG
	SpawnSpawnerEgg
)
