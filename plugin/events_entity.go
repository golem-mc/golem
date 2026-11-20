package plugin

import "github.com/golem-mc/golem/registry"

// EntitySpawnEvent fires when a mob, item, projectile, boat or minecart is
// added to the world, with why it spawned; a cancel discards it again in the
// same tick. Other kinds do not fire it.
// Bukkit: EntitySpawnEvent, CreatureSpawnEvent
type EntitySpawnEvent struct {
	Cancellable
	Entity Entity
	Reason SpawnReason
}

// EntityRemoveEvent fires when an entity leaves the world for good: it
// died, despawned or was discarded (not when its chunk unloads). The entity
// is already gone, so Entity answers only UUID. Paper: EntityRemoveFromWorldEvent
type EntityRemoveEvent struct {
	Base
	Entity Entity
	Cause  RemoveCause
}

// RemoveCause is why an entity left the world.
type RemoveCause uint8

const (
	RemoveDeath RemoveCause = iota
	RemoveDespawn
	RemovePickup
	RemoveMerge
	RemoveTransform
	RemoveDiscard
)

// EntityDamageEvent fires when a player or mob is hurt, and when an item
// entity, item frame or end crystal is: by a command, a player's blow, a
// projectile, a blast, and for an item fire, lava or a cactus. For those
// Entity is not a LivingEntity, Final is Amount, and only an item uses an
// edited Amount. For an item's burning the event fires after the tick's
// step, and the listeners' verdict then replaces the hurt the step made.
// After vanilla has
// worked out the amount from the source and before armour, enchantments and
// effects reduce it; Amount may be changed. Final is the health it would
// lose. A mob's hurt fires with no server lock held, on the tick goroutine.
// Bukkit: EntityDamageEvent, EntityDamageByEntityEvent
type EntityDamageEvent struct {
	Cancellable
	Entity Entity
	Source DamageSource
	Amount float32
	Final  float32
	// Critical is a player's critical hit.
	Critical bool
}

// EntityDeathEvent fires when a living entity dies, before its drops
// spawn; Drops and Experience may be changed. Bukkit: EntityDeathEvent
type EntityDeathEvent struct {
	Base
	Entity     LivingEntity
	Source     DamageSource
	Drops      []ItemStack
	Experience int
}

// PlayerDeathEvent is a player's death, fired in place of EntityDeathEvent:
// listen to both for every death. Message is the death message, nil for
// none; KeepInventory and KeepLevel override the game rule. Bukkit:
// PlayerDeathEvent
type PlayerDeathEvent struct {
	EntityDeathEvent
	Player        Player
	Message       *Text
	KeepInventory bool
	KeepLevel     bool
}

// EntityRegainHealthEvent fires when an entity heals; Amount may be
// changed. Bukkit: EntityRegainHealthEvent
type EntityRegainHealthEvent struct {
	Cancellable
	Entity LivingEntity
	Amount float32
	Reason RegainReason
}

// FoodLevelChangeEvent fires when a player's food level changes; Level may
// be changed. Bukkit: FoodLevelChangeEvent
type FoodLevelChangeEvent struct {
	Cancellable
	Player Player
	Level  int
	// Item is what was eaten, empty when hunger lowered it.
	Item ItemStack
}

// EntityResurrectEvent fires when a totem would save a dying entity;
// cancelling lets it die. Bukkit: EntityResurrectEvent
type EntityResurrectEvent struct {
	Cancellable
	Entity LivingEntity
	Hand   Hand
}

// EntityExplodeEvent fires when an entity explodes (TNT, a creeper, a
// fireball, the wither); Blocks may be trimmed and Yield is the share of
// them dropped. Bukkit: EntityExplodeEvent
type EntityExplodeEvent struct {
	Cancellable
	// Entity may already be removed (a creeper, a primed TNT); Type and
	// Location describe the exploder at the blast either way.
	Entity   Entity
	Type     registry.EntityType
	Location Location
	Pos      Vec3
	Blocks   []Block
	Yield    float32
}

// ExplosionPrimeEvent fires before an entity explodes; Radius and Fire may
// be changed. Cancelling stops the blast, but an exploder already removed
// (a creeper, a primed TNT) stays gone. Bukkit: ExplosionPrimeEvent
type ExplosionPrimeEvent struct {
	Cancellable
	Entity   Entity
	Type     registry.EntityType
	Location Location
	Radius   float32
	Fire     bool
}

// EntityTargetEvent fires when a mob picks or drops a target, with no lock
// held; Target may be changed (nil drops it) and a cancel puts the old one
// back. Reason is read off the change (TargetClosestPlayer,
// TargetClosestEntity, TargetAttacked for a retaliation when hurt, TargetForgot)
// or TargetPlugin for Mob.SetTarget. Bukkit: EntityTargetEvent
type EntityTargetEvent struct {
	Cancellable
	Entity Mob
	Target LivingEntity
	Reason TargetReason
}

// TargetReason is why a mob changed target.
type TargetReason uint8

const (
	TargetClosestPlayer TargetReason = iota
	TargetClosestEntity
	TargetAttacked
	TargetDefendOwner
	TargetOwnerAttacked
	TargetOwnerAttackedTarget
	TargetCollision
	TargetTemptation
	TargetForgot
	TargetDied
	TargetCommand
	TargetPlugin
	TargetOther
)

// EntityCombustEvent fires when a mob or player catches fire or has its
// burn raised; Duration (ticks) may be changed and 0 or a cancel leaves it
// as it was. A mob's fires with no lock held, on the tick goroutine. Bukkit: EntityCombustEvent
type EntityCombustEvent struct {
	Cancellable
	Entity   Entity
	Duration int
	// ByEntity is the entity that set it alight (a Fire Aspect attacker, a
	// burning arrow, a small fireball, a burning mob or lightning), nil when
	// none. Bukkit: EntityCombustByEntityEvent.getCombuster
	ByEntity Entity
	// ByBlock is the fire, lava or lava cauldron block that set it alight,
	// nil when none. Bukkit: EntityCombustByBlockEvent.getCombuster
	ByBlock *Block
}

// EntityChangeBlockEvent fires when an entity changes a block: an enderman
// taking or placing one, a falling block landing, a sheep eating grass, a
// ravager or a wither breaking blocks, a silverfish entering stone.
// Bukkit: EntityChangeBlockEvent
type EntityChangeBlockEvent struct {
	Cancellable
	Entity   Entity
	Block    Block
	NewState BlockState
}

// EntityBreakDoorEvent fires when a zombie finishes breaking a wooden door;
// a cancel leaves the door standing. Bukkit: EntityBreakDoorEvent
type EntityBreakDoorEvent struct {
	Cancellable
	Entity Entity
	Block  Block
}

// PlayerPickupArrowEvent fires when a player walks over a stuck arrow, spectral
// arrow or trident that they may take, with no lock held; a cancel leaves it
// where it is. Bukkit: PlayerPickupArrowEvent
type PlayerPickupArrowEvent struct {
	Cancellable
	Player Player
	Arrow  Entity
}

// EntityInteractEvent fires when a mob lands on farmland hard enough to
// trample it; a cancel keeps the farmland. Pressure plates and tripwires do
// not fire it. Bukkit: EntityInteractEvent
type EntityInteractEvent struct {
	Cancellable
	Entity Entity
	Block  Block
}

// EntityBreedEvent fires when two animals breed, with no lock held; Experience
// may be changed and a cancel leaves no child, hearts or experience (the
// parents' cooldown already stands). Child is nil: the baby does not exist
// until the event is done. Bukkit: EntityBreedEvent
type EntityBreedEvent struct {
	Cancellable
	Child          LivingEntity
	Mother, Father LivingEntity
	Breeder        Player // nil when none fed them
	Experience     int
}

// EntityTameEvent fires when a player tames an animal. Bukkit:
// EntityTameEvent
type EntityTameEvent struct {
	Cancellable
	Entity Tameable
	Owner  Player
}

// EntityTransformEvent fires when an entity turns into another: a zombie
// drowning, a villager struck by lightning, a piglin zombifying, a slime
// splitting. Bukkit: EntityTransformEvent
type EntityTransformEvent struct {
	Cancellable
	Entity Entity
	Into   []Entity
	Reason TransformReason
}

// TransformReason is why an entity transformed.
type TransformReason uint8

const (
	TransformDrowned TransformReason = iota
	TransformLightning
	TransformCured
	TransformInfection
	TransformFrozen
	TransformPiglinZombified
	TransformSplit
	TransformSheared
	TransformMetamorphosis
	TransformOther
)

// EntityPotionEffectEvent fires when an effect is added, changed or removed,
// with no lock held; New may be edited, and a cancel or a nil New on an
// addition refuses it. Cause is told for arrows, splash potions, beacons,
// conduits, melee enchantments, /effect and plugins, else EffectOther; an
// effect that runs out is not asked. Bukkit: EntityPotionEffectEvent
type EntityPotionEffectEvent struct {
	Cancellable
	Entity   LivingEntity
	Old, New *EffectInstance
	Cause    EffectCause
}

// EffectCause is what changed an effect.
type EffectCause uint8

const (
	EffectPotion EffectCause = iota
	EffectSplashPotion
	EffectAreaCloud
	EffectArrow
	EffectFood
	EffectMilk
	EffectBeacon
	EffectConduit
	EffectAttack
	EffectCommand
	EffectExpired
	EffectTotem
	EffectPlugin
	EffectOther
)

// EntityShootBowEvent fires when an entity shoots a bow or crossbow; the
// Force and Consume may be changed. Projectile is nil for a player's bow:
// the arrow does not exist until the event is done. Bukkit:
// EntityShootBowEvent
type EntityShootBowEvent struct {
	Cancellable
	Entity     LivingEntity
	Bow        ItemStack
	Projectile Entity
	Force      float32
	// Consume is whether the ammunition is used up.
	Consume bool
}

// ProjectileLaunchEvent fires when any projectile is launched. Bukkit:
// ProjectileLaunchEvent
type ProjectileLaunchEvent struct {
	Cancellable
	Projectile Projectile
}

// ProjectileHitEvent fires when an arrow, trident, thrown item or fireball
// hits a mob or a player, with no lock held, and when it ends on a block.
// Cancelling a body hit lets the flight pass that body (the hit is decided
// one tick later while a listener is registered); cancelling a block hit
// does nothing. Bukkit: ProjectileHitEvent
type ProjectileHitEvent struct {
	Cancellable
	Projectile Projectile
	Hit        RayHit
}

// EntityDropItemEvent fires when a mob drops an item by itself: a chicken
// laying an egg, a panda or copper golem dropping its item, a turtle's scute.
// Bukkit: EntityDropItemEvent
type EntityDropItemEvent struct {
	Cancellable
	Entity Entity
	Item   ItemEntity
}

// ItemDespawnEvent fires when an item entity is about to despawn; cancel
// to keep it another 6000 ticks. Bukkit: ItemDespawnEvent
type ItemDespawnEvent struct {
	Cancellable
	Item ItemEntity
}

// ItemMergeEvent fires when two item entities merge. Bukkit: ItemMergeEvent
type ItemMergeEvent struct {
	Cancellable
	Item, Target ItemEntity
}

// EntityMountEvent and EntityDismountEvent fire when an entity gets on or
// off another. Bukkit: EntityMountEvent, EntityDismountEvent
type EntityMountEvent struct {
	Cancellable
	Entity, Vehicle Entity
}

type EntityDismountEvent struct {
	Cancellable
	Entity, Vehicle Entity
}

// EntityAirChangeEvent fires when an entity's air changes. Bukkit:
// EntityAirChangeEvent
type EntityAirChangeEvent struct {
	Cancellable
	Entity LivingEntity
	Air    int
}

// EntityToggleGlideEvent fires when an entity starts or stops gliding.
type EntityToggleGlideEvent struct {
	Cancellable
	Entity LivingEntity
	On     bool
}

// EntityEnterLoveModeEvent fires when an animal is fed into love mode;
// Ticks may be changed. Bukkit: EntityEnterLoveModeEvent
type EntityEnterLoveModeEvent struct {
	Cancellable
	Entity Breedable
	Player Player
	Ticks  int
}

// VillagerAcquireTradeEvent fires when a villager gains a trade; Trade may
// be changed. Bukkit: VillagerAcquireTradeEvent
type VillagerAcquireTradeEvent struct {
	Cancellable
	Villager Villager
	Trade    Trade
}

// VillagerCareerChangeEvent fires when a villager takes or loses a
// profession. Bukkit: VillagerCareerChangeEvent
type VillagerCareerChangeEvent struct {
	Cancellable
	Villager   Villager
	Profession Key
}

// PlayerTradeEvent fires when a player trades with a merchant. Paper:
// PlayerTradeEvent
type PlayerTradeEvent struct {
	Cancellable
	Player   Player
	Merchant Entity
	Trade    Trade
}

// PiglinBarterEvent fires when a piglin finishes bartering; Outcome may be
// changed. Bukkit: PiglinBarterEvent
type PiglinBarterEvent struct {
	Cancellable
	Piglin  Mob
	Input   ItemStack
	Outcome []ItemStack
}

// SheepDyeWoolEvent fires when a player dyes a sheep. Bukkit:
// SheepDyeWoolEvent
type SheepDyeWoolEvent struct {
	Cancellable
	Sheep  Mob
	Player Player
	Color  DyeColor
}

// SlimeSplitEvent fires when a slime or magma cube dies and splits; Count
// may be changed. Bukkit: SlimeSplitEvent
type SlimeSplitEvent struct {
	Cancellable
	Slime Mob
	Count int
}

// CreeperPowerEvent fires when lightning charges a creeper. Bukkit:
// CreeperPowerEvent
type CreeperPowerEvent struct {
	Cancellable
	Creeper Mob
}

// ExpBottleEvent fires when a bottle o' enchanting lands; Experience may
// be changed. Bukkit: ExpBottleEvent
type ExpBottleEvent struct {
	Base
	Bottle     Projectile
	Experience int
}

// PlayerEggThrowEvent fires when a thrown egg breaks. Hatching says whether
// chicks hatch and Hatches how many (vanilla rolls one, or four on a rarer
// roll; Hatches is 1 when the roll hatched nothing); a listener may change
// both. Bukkit: PlayerEggThrowEvent
type PlayerEggThrowEvent struct {
	Base
	Player Player
	// Egg is already removed when the break fires, but answers Type.
	Egg      Entity
	Hatching bool
	Hatches  int
}

// HangingPlaceEvent and HangingBreakEvent fire when a painting, item frame
// or leash knot is placed or broken. Bukkit: HangingPlaceEvent,
// HangingBreakEvent
type HangingPlaceEvent struct {
	Cancellable
	Entity Hanging
	Player Player
	Block  Block
	Face   BlockFace
}

type HangingBreakEvent struct {
	Cancellable
	Entity Hanging
	// Remover is the entity that broke it, whatever its kind. For a blast
	// it is the cause when that differs from the exploder (the player who
	// lit the TNT), else the exploder, which may already be removed but
	// still answers Type. Nil for physics, or when the breaker is unknown.
	Remover Entity
	Cause   HangingBreakCause
}

// HangingBreakCause is why a hanging entity broke. Bukkit:
// HangingBreakEvent.RemoveCause
type HangingBreakCause uint8

const (
	// HangingBreakPhysics is a hanging entity whose support is gone.
	HangingBreakPhysics HangingBreakCause = iota
	// HangingBreakEntity is a blow or a projectile.
	HangingBreakEntity
	// HangingBreakExplosion is a blast.
	HangingBreakExplosion
)

// VehicleEvent fires when an entity enters, exits, damages or destroys a
// minecart or boat. Bukkit: VehicleEnterEvent, VehicleExitEvent,
// VehicleDamageEvent, VehicleDestroyEvent
type VehicleEvent struct {
	Cancellable
	Vehicle Vehicle
	Entity  Entity
	Action  VehicleAction
	// Damage may be changed for VehicleDamage.
	Damage float32
}

// VehicleMoveEvent fires after a tick in which a boat or minecart changed
// position or rotation, once per vehicle; a driver's moves between
// ticks are folded into the next one. Bukkit: VehicleMoveEvent
type VehicleMoveEvent struct {
	Base
	Vehicle  Vehicle
	From, To Location
}

// EntityMoveEvent fires after a tick in which a mob changed position or
// rotation, once per mob. Cancelling puts the mob back at From. Players
// fire PlayerMoveEvent instead; a teleport fires none. Paper: EntityMoveEvent
type EntityMoveEvent struct {
	Cancellable
	Entity   LivingEntity
	From, To Location
}

// VehicleAction is what happened to a vehicle.
type VehicleAction uint8

const (
	VehicleEnter VehicleAction = iota
	VehicleExit
	VehicleDamage
	VehicleDestroy
)

// RaidEvent fires as a raid starts, gains a wave, ends and stops. Bukkit:
// RaidTriggerEvent, RaidSpawnWaveEvent, RaidFinishEvent, RaidStopEvent
type RaidEvent struct {
	Cancellable // only RaidTrigger can be cancelled
	World       World
	Center      BlockPos
	Stage       RaidStage
	Wave        int
	Player      Player // the player who triggered it, at RaidTrigger
	Winners     []Player
}

// RaidStage is a stage of a raid.
type RaidStage uint8

const (
	RaidTrigger RaidStage = iota
	RaidWave
	RaidVictory
	RaidLoss
	RaidStopped
)
