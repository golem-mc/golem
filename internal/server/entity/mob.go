package entity

import (
	"math"
	"math/rand"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// A mob is an entity the server simulates with a mind of its own. This file
// holds its type, state and Tick; the body's movement is in mobphysics.go,
// paths in pathfind.go, goals in goals.go, the world's mobs in mobs.go. The
// world reaches a mob only through this package's interface.

// MobType is a kind of mob, by its minecraft:entity_type registry ID.
type MobType = registry.EntityType

// NoMobType is the zero MobType, read as none: ID 0 is minecraft:acacia_boat,
// never a mob kind.
const NoMobType MobType = 0

// MobTypeOf returns the type of a registry name, or NoMobType when unknown.
// A name without a namespace is a minecraft one.
// vanilla: EntityType.by
func MobTypeOf(name string) MobType {
	if t, ok := registry.EntityTypeByName(registry.Qualify(name)); ok {
		return t
	}
	return NoMobType
}

// The mob types this package simulates.
const (
	TypeCow            = registry.EntityTypeCow
	TypeMooshroom      = registry.EntityTypeMooshroom
	TypePig            = registry.EntityTypePig
	TypeSheep          = registry.EntityTypeSheep
	TypeChicken        = registry.EntityTypeChicken
	TypeTurtle         = registry.EntityTypeTurtle
	TypeSquid          = registry.EntityTypeSquid
	TypeGlowSquid      = registry.EntityTypeGlowSquid
	TypeBat            = registry.EntityTypeBat
	TypeHappyGhast     = registry.EntityTypeHappyGhast
	TypeHorse          = registry.EntityTypeHorse
	TypeDonkey         = registry.EntityTypeDonkey
	TypeMule           = registry.EntityTypeMule
	TypeLlama          = registry.EntityTypeLlama
	TypeTraderLlama    = registry.EntityTypeTraderLlama
	TypeSkeletonHorse  = registry.EntityTypeSkeletonHorse
	TypeZombieHorse    = registry.EntityTypeZombieHorse
	TypeZombie         = registry.EntityTypeZombie
	TypeSpider         = registry.EntityTypeSpider
	TypeCreeper        = registry.EntityTypeCreeper
	TypeEnderman       = registry.EntityTypeEnderman
	TypeWitch          = registry.EntityTypeWitch
	TypeSkeleton       = registry.EntityTypeSkeleton
	TypeHusk           = registry.EntityTypeHusk
	TypeStray          = registry.EntityTypeStray
	TypeBogged         = registry.EntityTypeBogged
	TypeParched        = registry.EntityTypeParched
	TypeWitherSkeleton = registry.EntityTypeWitherSkeleton
	TypeCaveSpider     = registry.EntityTypeCaveSpider
	TypeDrowned        = registry.EntityTypeDrowned
	TypeZombieVillager = registry.EntityTypeZombieVillager
	TypeVillager       = registry.EntityTypeVillager
	TypeSlime          = registry.EntityTypeSlime
	TypeMagmaCube      = registry.EntityTypeMagmaCube
	TypeSnowGolem      = registry.EntityTypeSnowGolem
	TypeIronGolem      = registry.EntityTypeIronGolem
	TypePhantom        = registry.EntityTypePhantom
	TypeWolf           = registry.EntityTypeWolf
	TypeEndermite      = registry.EntityTypeEndermite
	TypeSilverfish     = registry.EntityTypeSilverfish
	TypeRabbit         = registry.EntityTypeRabbit
	TypeCat            = registry.EntityTypeCat
	TypeOcelot         = registry.EntityTypeOcelot
	TypeFox            = registry.EntityTypeFox
)

// MobSounds are a kind's registered voices and step event. Squirt is only the squids'.
type MobSounds struct {
	Ambient, Hurt, Death, Eat, Squirt, Step registry.Sound
	// StepVolume is the step sound's volume; 0 is the usual 0.15.
	StepVolume float32
}

// kind is what a mob is by its type alone. The box is vanilla's, width
// centred on X/Z and height rising from the feet; eyeHeight (where look,
// line of sight and the daylight burn start) defaults to four fifths of the
// height. speed is the movement_speed attribute of the type's
// createAttributes. tempting lists the kind's food, or temptTag names an
// item tag holding it. volume overrides getSoundVolume where the kind
// departs from LivingEntity's 1.
type kind struct {
	// typeID, typeIDOK and updateInterval are resolved once by resolveKinds:
	// the wire entity type ID (false when the registry has no such type) and
	// the tracker's updateInterval.
	typeID         int32
	typeIDOK       bool
	updateInterval int

	width, height    float64
	eyeHeight        float64
	maxHealth, speed float64
	tempting         []registry.Item
	// temptTag is the item tag holding the kind's food; nil when tempting
	// stands alone.
	// vanilla: ItemTags
	temptTag *registry.ItemTagID

	// babyWidth, babyHeight and babyEye are the kind's BABY_DIMENSIONS; zero
	// for kinds that are never babies.
	babyWidth, babyHeight, babyEye float64

	// family is the kind whose machinery a variant runs (the zombie's for the
	// husk); empty for a kind that is its own.
	family MobType
	// name is the type's en_us display name.
	name string

	hostile bool
	// livesInPeaceful marks a hostile kind whose type lacks notInPeaceful, so
	// peaceful neither despawns nor refuses it.
	// vanilla: EntityType.canSpawn, Monster.shouldDespawnInPeaceful
	livesInPeaceful           bool
	followRange, attackDamage float64
	// pathLength is the path search range where it exceeds the follow range.
	// vanilla: PathNavigation.setRequiredPathLength
	pathLength float64
	armor      float64
	// stepHeight is the step_height base where the kind's createAttributes
	// raises it over the living 0.6; zero otherwise.
	stepHeight float64
	// headRotSpeed, maxHeadXRot and maxHeadYRot are Mob.getHeadRotSpeed,
	// getMaxHeadXRot and getMaxHeadYRot; zero is vanilla's 10, 40 and 75.
	headRotSpeed, maxHeadXRot, maxHeadYRot int
	// knockbackResist is the knockback_resistance attribute the kind's
	// createAttributes gives, 0 to 1.
	knockbackResist float64
	// attackKnockback is the attack_knockback attribute the kind's
	// createAttributes gives.
	attackKnockback float64
	sounds          MobSounds

	// water marks the squids: their spawn rules ask for water, not grass, the
	// passive cap never counts them, and they run only their own swim.
	water bool

	// volume is getSoundVolume; zero reads as LivingEntity's 1, which every
	// kind but the cow keeps.
	volume float64

	// ambientInterval is getAmbientSoundInterval in ticks; zero reads as Mob's
	// 80.
	ambientInterval int

	// burnsInDaylight is membership in the minecraft:burn_in_daylight entity
	// tag, which Mob.aiStep's burnUndead reads.
	burnsInDaylight bool

	// bornWithBow is the bow AbstractSkeleton.populateDefaultEquipmentSlots
	// puts in the main hand at finalizeSpawn.
	bornWithBow bool

	// arrowEffect is what the kind's getArrow override adds to its arrows.
	arrowEffect ArrowEffect
	// cube marks the AbstractCubeMob kinds, whose box, eyes, health, pace and
	// blow scale with size; spawnScale is the builder's spawnDimensionsScale
	// (the natural spawn's box check over the body's). fireImmune is the
	// builder's fireImmune.
	cube       bool
	spawnScale float64
	fireImmune bool
	// walksOnLava marks the kinds that stand on a lava surface.
	// vanilla: LivingEntity.canStandOnFluid
	walksOnLava bool

	// fireAverse marks the kinds whose constructor sets the fire malus to
	// -1 and the near-fire malus to 16: the animals, the villagers and the
	// piglins. vanilla: Animal, AbstractVillager, AbstractPiglin constructors
	fireAverse bool

	// waterSensitive marks the kinds hurt by water or rain.
	// vanilla: LivingEntity.isSensitiveToWater
	waterSensitive bool

	// rangedInterval and rangedIntervalHard override the ranged attack's
	// getAttackInterval and getHardAttackInterval; zero keeps the
	// skeleton's.
	rangedInterval, rangedIntervalHard int

	// followPace is the pace a baby keeps the grown body of its kind
	// company at, vanilla's own FollowParentGoal speed modifier.
	followPace float64

	// panicPace, temptPace and strollPace are the goals' speed modifiers; zero
	// keeps the package default.
	panicPace, temptPace, strollPace float64

	// babyable is whether the kind can be a baby (vanilla's AgeableMob sorts and
	// the zombie family), filled in at init so IsBaby needs no table lookup.
	babyable bool

	// ambient marks the MobCategory.AMBIENT kinds (the bat): not creatures,
	// despawned far from every player, and without goals.
	ambient bool

	// flying marks the kinds driven by the flying move control (no gravity)
	// instead of the walk physics.
	flying bool

	// flyPath is the path rules of a flyer whose navigation plans a path
	// (flypath.go); nil for one that flies straight at the point.
	// vanilla: FlyingPathNavigation
	flyPath *flyMaluses
}

// kinds holds every type this package can simulate. A type with no entry is
// an upstream bug, and NewMob panics rather than inventing an animal. Entries
// are shared by pointer so a tick reads them without a map lookup.
var kinds = [registry.EntityTypeCount]*kind{
	TypeEnderDragon: dragonKind,
	TypeSilverfish:  silverfishKind,
	TypeCat:         catKind,
	TypeOcelot:      ocelotKind,
	TypeFox:         foxKind,
	// vanilla: Cow. Box 0.9 by 1.4, ten of health, movement speed
	// 0.20000000298023224, tempted by wheat.
	TypeCow: &kind{
		name:       "Cow",
		fireAverse: true,
		width:      0.9,
		height:     1.4,
		maxHealth:  10,
		speed:      0.20000000298023224,
		// The calf's box, Cow's BABY_DIMENSIONS: scalable(0.45, 0.7),
		// eyes 0.69 above its feet.
		babyWidth:  0.45,
		babyHeight: 0.7,
		babyEye:    0.69,
		tempting:   []registry.Item{registry.ItemWheat},
		// The classic sound set's ambient, at AbstractCow.getSoundVolume's quiet
		// voice.
		sounds: MobSounds{Ambient: registry.SoundEntityCowAmbient, Step: registry.SoundEntityCowStep},
		volume: 0.4,

		followPace:      1.25, // FollowParentGoal(this, 1.25)
		panicPace:       2.0,  // PanicGoal(this, 2.0)
		temptPace:       1.25, // TemptGoal(this, 1.25)
		ambientInterval: 120,  // Animal.getAmbientSoundInterval's own
	},
	// vanilla: MushroomCow, an AbstractCow: the cow's box, health, pace,
	// cow_food tag and sound set (MushroomCow registers no attributes or sound
	// set of its own).
	TypeMooshroom: {
		name:       "Mooshroom",
		fireAverse: true,
		width:      0.9,
		height:     1.4,
		maxHealth:  10,
		speed:      0.20000000298023224,
		// The calf's box, MushroomCow's BABY_DIMENSIONS: scalable(0.45, 0.7), eyes
		// 0.69.
		babyWidth:  0.45,
		babyHeight: 0.7,
		babyEye:    0.69,
		tempting:   []registry.Item{registry.ItemWheat},
		sounds: MobSounds{
			Ambient: registry.SoundEntityCowAmbient,
			Hurt:    registry.SoundEntityCowHurt,
			Death:   registry.SoundEntityCowDeath,
			Step:    registry.SoundEntityCowStep,
		},
		volume: 0.4,

		followPace:      1.25, // FollowParentGoal(this, 1.25)
		panicPace:       2.0,  // PanicGoal(this, 2.0)
		temptPace:       1.25, // TemptGoal(this, 1.25)
		ambientInterval: 120,  // Animal.getAmbientSoundInterval's own
	},
	// vanilla: Pig. Box 0.9 by 0.9, ten of health, movement speed 0.25,
	// tempted by the root vegetables.
	TypePig: &kind{
		name:       "Pig",
		fireAverse: true,
		width:      0.9,
		height:     0.9,
		maxHealth:  10,
		speed:      0.25,
		// The piglet's box, Pig's BABY_DIMENSIONS: scalable(0.45, 0.45),
		// eyes 0.40625 above its feet.
		babyWidth:  0.45,
		babyHeight: 0.45,
		babyEye:    0.40625,
		tempting:   []registry.Item{registry.ItemCarrot, registry.ItemPotato, registry.ItemBeetroot, registry.ItemCarrotOnAStick},
		sounds:     MobSounds{Ambient: registry.SoundEntityPigAmbient, Eat: registry.SoundEntityPigEat, Step: registry.SoundEntityPigStep},

		followPace:      1.1,  // FollowParentGoal(this, 1.1)
		panicPace:       1.25, // PanicGoal(this, 1.25)
		temptPace:       1.2,  // TemptGoal(this, 1.2)
		ambientInterval: 120,  // Animal.getAmbientSoundInterval's own
	},
	// vanilla: Sheep. Box 0.9 by 1.3, eight of health, movement speed
	// 0.23000000417232513, tempted by wheat.
	TypeSheep: &kind{
		name:       "Sheep",
		fireAverse: true,
		width:      0.9,
		height:     1.3,
		maxHealth:  8,
		speed:      0.23000000417232513,
		// The lamb's box, Sheep's BABY_DIMENSIONS: scalable(0.45, 0.65), eyes
		// 0.65625 (above the box's top, as vanilla has it).
		babyWidth:  0.45,
		babyHeight: 0.65,
		babyEye:    0.65625,
		tempting:   []registry.Item{registry.ItemWheat},
		sounds:     MobSounds{Ambient: registry.SoundEntitySheepAmbient, Step: registry.SoundEntitySheepStep},

		followPace:      1.1,  // FollowParentGoal(this, 1.1)
		panicPace:       1.25, // PanicGoal(this, 1.25)
		temptPace:       1.1,  // TemptGoal(this, 1.1)
		ambientInterval: 120,  // Animal.getAmbientSoundInterval's own
	},
	// vanilla: EntityTypes.POLAR_BEAR's builder (sized 1.4 by 1.4, tracked at
	// 10 chunks) and PolarBear.createAttributes. The cub is scalable(0.7,
	// 0.7) with eyes at 0.34375. No food: a polar bear never breeds.
	TypePolarBear: &kind{
		name:         "Polar Bear",
		fireAverse:   true,
		width:        1.4,
		height:       1.4,
		maxHealth:    30,
		speed:        0.25,
		followRange:  20,
		attackDamage: 6,
		babyWidth:    0.7,
		babyHeight:   0.7,
		babyEye:      0.34375,
		sounds: MobSounds{
			Ambient: registry.SoundEntityPolarBearAmbient,
			Hurt:    registry.SoundEntityPolarBearHurt,
			Death:   registry.SoundEntityPolarBearDeath,
			Step:    registry.SoundEntityPolarBearStep,
		},

		followPace:      1.25, // FollowParentGoal(this, 1.25)
		panicPace:       2.0,  // PanicGoal(this, 2.0, ...)
		strollPace:      1.0,  // RandomStrollGoal(this, 1.0)
		ambientInterval: 120,  // Animal.getAmbientSoundInterval's own
	},
	// vanilla: EntityTypes.RABBIT's builder and Rabbit.createAttributes.
	// Box 0.49 by 0.6, three of health, movement speed 0.3, tempted by the
	// rabbit_food tag.
	TypeRabbit: &kind{
		name:         "Rabbit",
		fireAverse:   true,
		width:        0.49,
		height:       0.6,
		eyeHeight:    0.59,
		maxHealth:    3,
		speed:        float64(float32(0.3)),
		attackDamage: 3,
		// Rabbit's BABY_DIMENSIONS: scalable(0.24, 0.4), eyes 0.39.
		babyWidth:  0.24,
		babyHeight: 0.4,
		babyEye:    0.39,
		temptTag:   &registry.ItemTagIDRabbitFood,
		sounds: MobSounds{
			Ambient: registry.SoundEntityRabbitAmbient,
			Hurt:    registry.SoundEntityRabbitHurt,
			Death:   registry.SoundEntityRabbitDeath,
		},

		followPace:      1.0, // Rabbit.FOLLOW_SPEED_MOD
		panicPace:       2.2, // RabbitPanicGoal(this, 2.2)
		temptPace:       1.0, // TemptGoal(this, 1.0)
		strollPace:      0.6, // Rabbit.STROLL_SPEED_MOD
		ambientInterval: 120, // Animal.getAmbientSoundInterval's own
	},
	// vanilla: Chicken. Box 0.4 by 0.7, four of health, movement speed
	// 0.25, tempted by the seeds.
	TypeChicken: &kind{
		name:       "Chicken",
		fireAverse: true,
		width:      0.4,
		height:     0.7,
		maxHealth:  4,
		speed:      0.25,
		// The chick's box, Chicken's BABY_DIMENSIONS: scalable(0.3, 0.4),
		// eyes 0.28125 above its feet.
		babyWidth:  0.3,
		babyHeight: 0.4,
		babyEye:    0.28125,
		// The chicken_food tag's seed items.
		// vanilla: ItemTags.CHICKEN_FOOD
		temptTag: &registry.ItemTagIDChickenFood,
		sounds:   MobSounds{Ambient: registry.SoundEntityChickenAmbient, Step: registry.SoundEntityChickenStep},

		followPace:      1.1, // FollowParentGoal(this, 1.1)
		panicPace:       1.4, // PanicGoal(this, 1.4)
		temptPace:       1.0, // TemptGoal(this, 1.0)
		ambientInterval: 120, // Animal.getAmbientSoundInterval's own
	},
	// vanilla: Turtle, EntityTypes.TURTLE. Box 1.2 by 0.4, thirty of health,
	// movement speed 0.25, tempted by the turtle_food tag.
	TypeTurtle: &kind{
		name:       "Turtle",
		fireAverse: true,
		width:      1.2,
		height:     0.4,
		maxHealth:  30,
		speed:      0.25,
		stepHeight: 1,
		// The hatchling's box, Turtle's BABY_DIMENSIONS: the grown box
		// scaled by 0.3, its eyes at the 0.85 share of the height.
		babyWidth:  0.36,
		babyHeight: 0.12,
		babyEye:    0.102,
		temptTag:   &registry.ItemTagIDTurtleFood,
		// The ambient land sound is chosen by Sounds.
		sounds: MobSounds{Ambient: registry.SoundEntityTurtleAmbientLand},

		ambientInterval: 200, // Turtle.getAmbientSoundInterval
	},
	// vanilla: EntityTypes.SQUID's builder and Squid.createAttributes. Box 0.8
	// by 0.8, eyes 0.4, ten of health; the movement speed is
	// Mob.createMobAttributes' 0.7, which the tentacle swim never reads.
	TypeSquid: &kind{
		name:      "Squid",
		width:     0.8,
		height:    0.8,
		eyeHeight: 0.4,
		maxHealth: 10,
		speed:     0.7,
		// The baby's box, Squid's BABY_DIMENSIONS: scalable(0.5, 0.5),
		// eyes 0.37 above its feet.
		babyWidth:  0.5,
		babyHeight: 0.5,
		babyEye:    0.37,
		water:      true,
		sounds: MobSounds{
			Ambient: registry.SoundEntitySquidAmbient,
			Hurt:    registry.SoundEntitySquidHurt,
			Death:   registry.SoundEntitySquidDeath,
			Squirt:  registry.SoundEntitySquidSquirt,
		},
		// Squid.getSoundVolume's own quiet voice, and
		// AgeableWaterCreature.getAmbientSoundInterval's 120.
		volume:          0.4,
		ambientInterval: 120,
	},
	// vanilla: EntityTypes.GLOW_SQUID's builder; GlowSquid keeps the squid's
	// attributes and machinery (its family) with its own voice and ink.
	TypeGlowSquid: &kind{
		family:     TypeSquid,
		name:       "Glow Squid",
		width:      0.8,
		height:     0.8,
		eyeHeight:  0.4,
		maxHealth:  10,
		speed:      0.7,
		babyWidth:  0.5,
		babyHeight: 0.5,
		babyEye:    0.37,
		water:      true,
		sounds: MobSounds{
			Ambient: registry.SoundEntityGlowSquidAmbient,
			Hurt:    registry.SoundEntityGlowSquidHurt,
			Death:   registry.SoundEntityGlowSquidDeath,
			Squirt:  registry.SoundEntityGlowSquidSquirt,
		},
		volume:          0.4,
		ambientInterval: 120,
	},
	// vanilla: EntityTypes.BAT's builder and Bat.createAttributes. Box 0.5 by
	// 0.9, eyes 0.45, six of health, an unread 0.7 base speed, getSoundVolume
	// 0.1.
	TypeBat: &kind{
		name:      "Bat",
		width:     0.5,
		height:    0.9,
		eyeHeight: 0.45,
		maxHealth: 6,
		speed:     0.7,
		ambient:   true,
		volume:    0.1,
		sounds: MobSounds{
			Ambient: registry.SoundEntityBatAmbient,
			Hurt:    registry.SoundEntityBatHurt,
			Death:   registry.SoundEntityBatDeath,
		},
	},
	// vanilla: EntityTypes.HAPPY_GHAST's builder and
	// HappyGhast.createAttributes. Box 4.0 by 4.0, eyes 2.6, twenty of health,
	// 0.05 walking and flying speed. The ghastling
	// is BABY_SCALE's quarter box (0.95 by 0.95), eyes 0.46875.
	TypeHappyGhast: &kind{
		name:       "Happy Ghast",
		fireAverse: true,
		width:      4.0,
		height:     4.0,
		eyeHeight:  2.6,
		maxHealth:  20,
		speed:      0.05,
		temptTag:   &registry.ItemTagIDHappyGhastTemptItems,
		babyWidth:  0.95,
		babyHeight: 0.95,
		babyEye:    0.46875,
		sounds: MobSounds{
			Ambient: registry.SoundEntityHappyGhastAmbient,
			Hurt:    registry.SoundEntityHappyGhastHurt,
			Death:   registry.SoundEntityHappyGhastDeath,
		},
		// HappyGhast.getSoundVolume's own: only a grown body is fourfold.
		volume:          4.0,
		ambientInterval: 120, // Animal.getAmbientSoundInterval's own
		flying:          true,
	},
	// vanilla: EntityTypes.HORSE's builder and
	// AbstractHorse.createBaseHorseAttributes. Box 1.3964844 by 1.6, eyes 1.52;
	// the 53 health and 0.225 pace are rerolled by a finalize. The foal is
	// BABY_SCALE's 0.7.
	TypeHorse: &kind{
		name:       "Horse",
		stepHeight: 1,
		fireAverse: true,
		width:      1.3964844,
		height:     1.6,
		eyeHeight:  1.52,
		maxHealth:  53,
		speed:      float64(float32(0.225)),
		babyWidth:  0.97753908,
		babyHeight: 1.12,
		babyEye:    1.064,
		sounds: MobSounds{
			Ambient: registry.SoundEntityHorseAmbient,
			Hurt:    registry.SoundEntityHorseHurt,
			Death:   registry.SoundEntityHorseDeath,
		},
		volume:          0.8, // AbstractHorse.getSoundVolume
		followPace:      1.0, // FollowParentGoal(this, 1.0)
		panicPace:       1.2, // MountPanicGoal(this, 1.2)
		ambientInterval: 400, // AbstractHorse.getAmbientSoundInterval
		temptTag:        &registry.ItemTagIDHorseTemptItems,
		temptPace:       1.25, // TemptGoal(this, 1.25, ...)
	},
	// vanilla: EntityTypes.DONKEY's builder and
	// AbstractChestedHorse.createBaseChestedHorseAttributes. Box 1.3964844 by
	// 1.5, eyes 1.425; the foal is the type's dimensions halved.
	TypeDonkey: &kind{
		name:       "Donkey",
		stepHeight: 1,
		fireAverse: true,
		width:      1.3964844,
		height:     1.5,
		eyeHeight:  1.425,
		maxHealth:  53,
		speed:      float64(float32(0.175)),
		babyWidth:  0.6982422,
		babyHeight: 0.75,
		babyEye:    0.7125,
		sounds: MobSounds{
			Ambient: registry.SoundEntityDonkeyAmbient,
			Hurt:    registry.SoundEntityDonkeyHurt,
			Death:   registry.SoundEntityDonkeyDeath,
		},
		volume:          0.8, // AbstractHorse.getSoundVolume
		followPace:      1.0, // FollowParentGoal(this, 1.0)
		panicPace:       1.2, // MountPanicGoal(this, 1.2)
		ambientInterval: 400, // AbstractHorse.getAmbientSoundInterval
		temptTag:        &registry.ItemTagIDHorseTemptItems,
		temptPace:       1.25, // TemptGoal(this, 1.25, ...)
	},
	// vanilla: EntityTypes.MULE's builder; Donkey's numbers in a taller box
	// of 1.6, eyes 1.52.
	TypeMule: &kind{
		name:       "Mule",
		stepHeight: 1,
		fireAverse: true,
		width:      1.3964844,
		height:     1.6,
		eyeHeight:  1.52,
		maxHealth:  53,
		speed:      float64(float32(0.175)),
		babyWidth:  0.6982422,
		babyHeight: 0.8,
		babyEye:    0.76,
		sounds: MobSounds{
			Ambient: registry.SoundEntityMuleAmbient,
			Hurt:    registry.SoundEntityMuleHurt,
			Death:   registry.SoundEntityMuleDeath,
		},
		volume:          0.8, // AbstractHorse.getSoundVolume
		followPace:      1.0, // FollowParentGoal(this, 1.0)
		panicPace:       1.2, // MountPanicGoal(this, 1.2)
		ambientInterval: 400, // AbstractHorse.getAmbientSoundInterval
		temptTag:        &registry.ItemTagIDHorseTemptItems,
		temptPace:       1.25, // TemptGoal(this, 1.25, ...)
	},
	// vanilla: EntityTypes.SKELETON_HORSE's builder and
	// SkeletonHorse.createAttributes. The horse's box; 15 health and a 0.2
	// pace that no finalize rerolls. The foal is BABY_DIMENSIONS' 0.7.
	TypeSkeletonHorse: &kind{
		name:       "Skeleton Horse",
		stepHeight: 1,
		fireAverse: true,
		width:      1.3964844,
		height:     1.6,
		eyeHeight:  1.52,
		maxHealth:  15,
		speed:      float64(float32(0.2)),
		babyWidth:  0.97753908,
		babyHeight: 1.12,
		babyEye:    1.064,
		sounds: MobSounds{
			Ambient: registry.SoundEntitySkeletonHorseAmbient,
			Hurt:    registry.SoundEntitySkeletonHorseHurt,
			Death:   registry.SoundEntitySkeletonHorseDeath,
		},
		volume:          0.8, // AbstractHorse.getSoundVolume
		followPace:      1.0, // FollowParentGoal(this, 1.0)
		ambientInterval: 400, // AbstractHorse.getAmbientSoundInterval
	},
	// vanilla: EntityTypes.ZOMBIE_HORSE's builder and ZombieHorse.createAttributes.
	// A monster-category kind that is no Monster: it lives in peaceful.
	// Its speed and jump are rolled at spawn.
	TypeZombieHorse: &kind{
		name:            "Zombie Horse",
		stepHeight:      1,
		hostile:         true,
		livesInPeaceful: true,
		burnsInDaylight: true,
		fireAverse:      true,
		width:           1.3964844,
		height:          1.6,
		eyeHeight:       1.52,
		maxHealth:       25,
		speed:           float64(float32(0.225)), // vanilla: AbstractHorse.createBaseHorseAttributes
		babyWidth:       0.97753908,
		babyHeight:      1.12,
		babyEye:         1.064,
		sounds: MobSounds{
			Ambient: registry.SoundEntityZombieHorseAmbient,
			Hurt:    registry.SoundEntityZombieHorseHurt,
			Death:   registry.SoundEntityZombieHorseDeath,
		},
		volume:          0.8, // AbstractHorse.getSoundVolume
		followPace:      1.0, // FollowParentGoal(this, 1.0)
		panicPace:       1.2,
		ambientInterval: 400, // AbstractHorse.getAmbientSoundInterval
		temptTag:        &registry.ItemTagIDZombieHorseFood,
		temptPace:       1.25, // TemptGoal(this, 1.25, ...)
	},
	// vanilla: EntityTypes.LLAMA's builder and
	// AbstractChestedHorse.createBaseChestedHorseAttributes. Box 0.9 by 1.87,
	// eyes 1.7765; the foal is Llama.BABY_DIMENSIONS, half of it.
	TypeLlama: &kind{
		name:       "Llama",
		pathLength: 40, // vanilla: Llama.<init> setRequiredPathLength(40)
		stepHeight: 1,
		fireAverse: true,
		width:      0.9,
		height:     1.87,
		eyeHeight:  1.7765,
		maxHealth:  53,
		speed:      float64(float32(0.175)),
		babyWidth:  0.45,
		babyHeight: 0.935,
		babyEye:    0.88825,
		sounds: MobSounds{
			Ambient: registry.SoundEntityLlamaAmbient,
			Step:    registry.SoundEntityLlamaStep,
			Hurt:    registry.SoundEntityLlamaHurt,
			Death:   registry.SoundEntityLlamaDeath,
		},
		volume:          0.8, // AbstractHorse.getSoundVolume
		followPace:      1.0, // FollowParentGoal(this, 1.0)
		panicPace:       1.2, // PanicGoal(this, 1.2)
		ambientInterval: 400, // AbstractHorse.getAmbientSoundInterval
		temptTag:        &registry.ItemTagIDLlamaTemptItems,
		temptPace:       1.25, // TemptGoal(this, 1.25, ...)
	},
	// vanilla: EntityTypes.TRADER_LLAMA's builder, Llama's in the same box.
	TypeTraderLlama: &kind{
		name:       "Trader Llama",
		pathLength: 40, // vanilla: Llama.<init> setRequiredPathLength(40)
		stepHeight: 1,
		fireAverse: true,
		width:      0.9,
		height:     1.87,
		eyeHeight:  1.7765,
		maxHealth:  53,
		speed:      float64(float32(0.175)),
		babyWidth:  0.45,
		babyHeight: 0.935,
		babyEye:    0.88825,
		sounds: MobSounds{
			Ambient: registry.SoundEntityLlamaAmbient,
			Step:    registry.SoundEntityLlamaStep,
			Hurt:    registry.SoundEntityLlamaHurt,
			Death:   registry.SoundEntityLlamaDeath,
		},
		volume:          0.8, // AbstractHorse.getSoundVolume
		followPace:      1.0, // FollowParentGoal(this, 1.0)
		panicPace:       1.2, // Llama's PanicGoal(this, 1.2); the trader's own 2.0 one is a separate goal
		ambientInterval: 400, // AbstractHorse.getAmbientSoundInterval
		temptTag:        &registry.ItemTagIDLlamaTemptItems,
		temptPace:       1.25, // TemptGoal(this, 1.25, ...)
	},
	// vanilla: EntityTypes.PHANTOM, Mob.createMobAttributes, Monster.createMonsterAttributes
	TypePhantom: &kind{
		name:         "Phantom",
		width:        0.9,
		height:       0.5,
		eyeHeight:    0.175,
		maxHealth:    20,
		speed:        0.7,
		hostile:      true,
		followRange:  16,
		attackDamage: 2,
		sounds: MobSounds{
			Ambient: registry.SoundEntityPhantomAmbient,
			Hurt:    registry.SoundEntityPhantomHurt,
			Death:   registry.SoundEntityPhantomDeath,
		},
		burnsInDaylight: true,
	},
	// vanilla: EntityTypes.ZOMBIE's builder, Monster.createMonsterAttributes
	// and Zombie.createAttributes. Box 0.6 by 1.95, eyes 1.74, twenty of
	// health, 0.23 pace, a blow of three, two armour, follow range 35 (Zombie's;
	// the monster base has Mob.createMobAttributes' 16). The step sound is
	// declared but unplayed. On burn_in_daylight.
	TypeZombie: &kind{
		name:         "Zombie",
		width:        0.6,
		height:       1.95,
		eyeHeight:    1.74,
		maxHealth:    20,
		speed:        float64(float32(0.23)),
		hostile:      true,
		followRange:  35,
		attackDamage: 3,
		armor:        2,
		// The baby zombie's box, Zombie's BABY_DIMENSIONS:
		// scalable(0.49, 0.98), eyes 0.775 above its feet.
		babyWidth:  0.49,
		babyHeight: 0.98,
		babyEye:    0.775,
		sounds: MobSounds{
			Ambient: registry.SoundEntityZombieAmbient,
			Hurt:    registry.SoundEntityZombieHurt,
			Death:   registry.SoundEntityZombieDeath,
			Step:    registry.SoundEntityZombieStep,
		},
		burnsInDaylight: true,
	},
	// vanilla: EntityTypes.SPIDER's builder and Spider.createAttributes. Box 1.4
	// by 0.9, eyes 0.65, sixteen of health, 0.3 pace (0.30000001192092896 as a
	// float), the monster base's damage 2 and follow range 16, no armour. Not on
	// burn_in_daylight.
	TypeSpider: &kind{
		name:         "Spider",
		strollPace:   0.8, // WaterAvoidingRandomStrollGoal(this, 0.8)
		width:        1.4,
		height:       0.9,
		eyeHeight:    0.65,
		maxHealth:    16,
		speed:        0.30000001192092896,
		hostile:      true,
		followRange:  16,
		attackDamage: 2,
		sounds: MobSounds{
			Ambient: registry.SoundEntitySpiderAmbient,
			Hurt:    registry.SoundEntitySpiderHurt,
			Death:   registry.SoundEntitySpiderDeath,
			Step:    registry.SoundEntitySpiderStep,
		},
	},
	// vanilla: EntityTypes.CREEPER's builder and Creeper.createAttributes. Box
	// 0.6 by 1.7, eyes the four-fifths default (1.445), twenty of health, 0.25
	// pace, follow range 16, no armour. Creeper.doHurtTarget hurts no one, so
	// the attack damage of 2 (Monster.createMonsterAttributes) is never read.
	// No ambient sound: the class overrides no getAmbientSound.
	TypeCreeper: &kind{
		name:         "Creeper",
		strollPace:   0.8, // WaterAvoidingRandomStrollGoal(this, 0.8)
		width:        0.6,
		height:       1.7,
		maxHealth:    20,
		speed:        0.25,
		hostile:      true,
		followRange:  16,
		attackDamage: 2,
		sounds: MobSounds{
			Hurt:  registry.SoundEntityCreeperHurt,
			Death: registry.SoundEntityCreeperDeath,
		},
	},
	// vanilla: EntityTypes.ENDERMAN's builder and EnderMan.createAttributes. Box
	// 0.6 by 2.9, eyes 2.55, forty health, 0.3f pace (pace adds an attacking
	// one's 0.15), seven damage, follow range 64, step height 1. Not on
	// burn_in_daylight: daylight teleports it instead.
	TypeEnderman: &kind{
		waterSensitive: true,
		name:           "Enderman",
		width:          0.6,
		height:         2.9,
		eyeHeight:      2.55,
		maxHealth:      40,
		speed:          float64(float32(0.3)),
		hostile:        true,
		followRange:    64,
		attackDamage:   7,
		stepHeight:     1,
		sounds: MobSounds{
			Ambient: registry.SoundEntityEndermanAmbient,
			Hurt:    registry.SoundEntityEndermanHurt,
			Death:   registry.SoundEntityEndermanDeath,
		},
	},
	// vanilla: EntityTypes.WITCH's builder and Witch.createAttributes. Box 0.6
	// by 1.95, eyes 1.62, twenty-six health, 0.25 pace, follow range 16.
	TypeWitch: &kind{
		name:         "Witch",
		width:        0.6,
		height:       1.95,
		eyeHeight:    1.62,
		maxHealth:    26,
		speed:        0.25,
		hostile:      true,
		followRange:  16,
		attackDamage: 2,
		sounds: MobSounds{
			Ambient: registry.SoundEntityWitchAmbient,
			Hurt:    registry.SoundEntityWitchHurt,
			Death:   registry.SoundEntityWitchDeath,
		},
	},
	// vanilla: EntityTypes.SKELETON's builder and
	// AbstractSkeleton.createAttributes. Box 0.6 by 1.99, eyes 1.74, 0.25 pace
	// over Monster.createMonsterAttributes (health 20, damage 2, follow range
	// 16), no armour. On burn_in_daylight. Its attack is ranged.go's
	// RangedBowAttackGoal instead of the melee one.
	TypeSkeleton: &kind{
		name:         "Skeleton",
		width:        0.6,
		height:       1.99,
		eyeHeight:    1.74,
		maxHealth:    20,
		speed:        0.25,
		hostile:      true,
		followRange:  16,
		attackDamage: 2,
		sounds: MobSounds{
			Ambient: registry.SoundEntitySkeletonAmbient,
			Hurt:    registry.SoundEntitySkeletonHurt,
			Death:   registry.SoundEntitySkeletonDeath,
			Step:    registry.SoundEntitySkeletonStep,
		},
		burnsInDaylight: true,
		// AbstractSkeleton.populateDefaultEquipmentSlots gives every skeleton a
		// plain bow, whatever its spawn.
		bornWithBow: true,
	},
	// vanilla: EntityTypes.HUSK's builder; the zombie's attributes (Husk
	// registers none). Off burn_in_daylight; baby eyes 0.825
	// (Husk.BABY_DIMENSIONS).
	TypeHusk: &kind{
		family:       TypeZombie,
		name:         "Husk",
		width:        0.6,
		height:       1.95,
		eyeHeight:    1.74,
		maxHealth:    20,
		speed:        float64(float32(0.23)),
		hostile:      true,
		followRange:  35,
		attackDamage: 3,
		armor:        2,
		babyWidth:    0.49,
		babyHeight:   0.98,
		babyEye:      0.825,
		sounds: MobSounds{
			Ambient: registry.SoundEntityHuskAmbient,
			Hurt:    registry.SoundEntityHuskHurt,
			Death:   registry.SoundEntityHuskDeath,
			Step:    registry.SoundEntityHuskStep,
		},
	},
	// vanilla: EntityTypes.STRAY's builder; AbstractSkeleton.createAttributes,
	// and Stray.getArrow's slowness of 600 ticks on every arrow.
	TypeStray: &kind{
		family:       TypeSkeleton,
		name:         "Stray",
		width:        0.6,
		height:       1.99,
		eyeHeight:    1.74,
		maxHealth:    20,
		speed:        0.25,
		hostile:      true,
		followRange:  16,
		attackDamage: 2,
		sounds: MobSounds{
			Ambient: registry.SoundEntityStrayAmbient,
			Hurt:    registry.SoundEntityStrayHurt,
			Death:   registry.SoundEntityStrayDeath,
			Step:    registry.SoundEntityStrayStep,
		},
		burnsInDaylight: true,
		bornWithBow:     true,
		arrowEffect:     ArrowEffect{Name: "minecraft:slowness", Ticks: 600},
	},
	// vanilla: EntityTypes.BOGGED's builder and Bogged.createAttributes'
	// sixteen of health; Bogged.getArrow's poison of 100 ticks, and its
	// attack intervals of 70, or 50 on hard.
	TypeBogged: &kind{
		family:       TypeSkeleton,
		name:         "Bogged",
		width:        0.6,
		height:       1.99,
		eyeHeight:    1.74,
		maxHealth:    16,
		speed:        0.25,
		hostile:      true,
		followRange:  16,
		attackDamage: 2,
		sounds: MobSounds{
			Ambient: registry.SoundEntityBoggedAmbient,
			Hurt:    registry.SoundEntityBoggedHurt,
			Death:   registry.SoundEntityBoggedDeath,
			Step:    registry.SoundEntityBoggedStep,
		},
		burnsInDaylight:    true,
		bornWithBow:        true,
		arrowEffect:        ArrowEffect{Name: "minecraft:poison", Ticks: 100},
		rangedInterval:     70,
		rangedIntervalHard: 50,
	},
	// vanilla: EntityTypes.PARCHED's builder and Parched.createAttributes'
	// sixteen of health; Parched.getArrow's weakness of 600 ticks, attack
	// intervals 70, or 50 on hard. Parched.canBeAffected refuses weakness. Not
	// on burn_in_daylight, unlike the other skeletons.
	TypeParched: &kind{
		family:       TypeSkeleton,
		name:         "Parched",
		width:        0.6,
		height:       1.99,
		eyeHeight:    1.74,
		maxHealth:    16,
		speed:        0.25,
		hostile:      true,
		followRange:  16,
		attackDamage: 2,
		sounds: MobSounds{
			Ambient: registry.SoundEntityParchedAmbient,
			Hurt:    registry.SoundEntityParchedHurt,
			Death:   registry.SoundEntityParchedDeath,
			Step:    registry.SoundEntityParchedStep,
		},
		bornWithBow:        true,
		arrowEffect:        ArrowEffect{Name: "minecraft:weakness", Ticks: 600},
		rangedInterval:     70,
		rangedIntervalHard: 50,
	},
	// vanilla: EntityTypes.WITHER_SKELETON's builder, fire immune, and
	// WitherSkeleton.finalizeSpawn's attack damage 4. A stone sword makes it
	// fight in melee.
	TypeWitherSkeleton: &kind{
		family:       TypeSkeleton,
		name:         "Wither Skeleton",
		width:        0.7,
		height:       2.4,
		eyeHeight:    2.1,
		maxHealth:    20,
		speed:        0.25,
		hostile:      true,
		followRange:  16,
		attackDamage: 4,
		sounds: MobSounds{
			Ambient: registry.SoundEntityWitherSkeletonAmbient,
			Hurt:    registry.SoundEntityWitherSkeletonHurt,
			Death:   registry.SoundEntityWitherSkeletonDeath,
			Step:    registry.SoundEntityWitherSkeletonStep,
		},
		fireImmune: true,
	},
	// vanilla: EntityTypes.CAVE_SPIDER's builder and
	// CaveSpider.createCaveSpider's twelve of health over the spider's
	// attributes and sounds.
	TypeCaveSpider: &kind{
		family:       TypeSpider,
		name:         "Cave Spider",
		strollPace:   0.8, // WaterAvoidingRandomStrollGoal(this, 0.8)
		width:        0.7,
		height:       0.5,
		eyeHeight:    0.45,
		maxHealth:    12,
		speed:        0.30000001192092896,
		hostile:      true,
		followRange:  16,
		attackDamage: 2,
		sounds: MobSounds{
			Ambient: registry.SoundEntitySpiderAmbient,
			Hurt:    registry.SoundEntitySpiderHurt,
			Death:   registry.SoundEntitySpiderDeath,
			Step:    registry.SoundEntitySpiderStep,
		},
	},
	// vanilla: EntityTypes.ENDERMITE's builder and Endermite.createAttributes.
	// Box 0.4 by 0.3, eyes 0.13 (set by the builder; the default would be
	// 0.24), eight of health, 0.25 pace, attack damage 2.
	TypeEndermite: &kind{
		name:         "Endermite",
		width:        0.4,
		height:       0.3,
		eyeHeight:    0.13,
		maxHealth:    8,
		speed:        0.25,
		hostile:      true,
		followRange:  16,
		attackDamage: 2,
		sounds: MobSounds{
			Ambient: registry.SoundEntityEndermiteAmbient,
			Hurt:    registry.SoundEntityEndermiteHurt,
			Death:   registry.SoundEntityEndermiteDeath,
			Step:    registry.SoundEntityEndermiteStep,
		},
	},
	// vanilla: EntityTypes.DROWNED's builder and Drowned.createAttributes,
	// the zombie's own; on the burn_in_daylight tag.
	TypeDrowned: &kind{
		family:       TypeZombie,
		name:         "Drowned",
		width:        0.6,
		height:       1.95,
		eyeHeight:    1.74,
		maxHealth:    20,
		speed:        float64(float32(0.23)),
		hostile:      true,
		followRange:  35,
		attackDamage: 3,
		armor:        2,
		stepHeight:   1,
		babyWidth:    0.49,
		babyHeight:   0.98,
		babyEye:      0.775,
		sounds: MobSounds{
			Ambient: registry.SoundEntityDrownedAmbient,
			Hurt:    registry.SoundEntityDrownedHurt,
			Death:   registry.SoundEntityDrownedDeath,
			Step:    registry.SoundEntityDrownedStep,
		},
		burnsInDaylight: true,
	},
	// vanilla: EntityTypes.VILLAGER, Villager.createAttributes: 0.6 by 1.95,
	// twenty of health, speed 0.5; baby box Villager.BABY_DIMENSIONS.
	TypeVillager: &kind{
		name:       "Villager",
		pathLength: 48, // vanilla: Villager.<init> setRequiredPathLength(48)
		fireAverse: true,
		width:      0.6,
		height:     1.95,
		eyeHeight:  1.62,
		maxHealth:  20,
		speed:      0.5,
		babyWidth:  0.49,
		babyHeight: 0.98,
		babyEye:    0.63,
		sounds: MobSounds{
			Ambient: registry.SoundEntityVillagerAmbient,
			Hurt:    registry.SoundEntityVillagerHurt,
			Death:   registry.SoundEntityVillagerDeath,
		},
	},
	// vanilla: EntityTypes.WANDERING_TRADER, Mob.createMobAttributes: 0.6
	// by 1.95, twenty of health, speed 0.7.
	TypeWanderingTrader: &kind{
		fireAverse: true,
		name:       "Wandering Trader",
		width:      0.6,
		height:     1.95,
		eyeHeight:  1.62,
		maxHealth:  20,
		speed:      0.7,
		sounds: MobSounds{
			Ambient: registry.SoundEntityWanderingTraderAmbient,
			Hurt:    registry.SoundEntityWanderingTraderHurt,
			Death:   registry.SoundEntityWanderingTraderDeath,
		},
	},
	// vanilla: EntityTypes.ZOMBIE_VILLAGER's builder over the zombie's
	// attributes; its baby's eyes sit at 0.67 (ZombieVillager.
	// BABY_DIMENSIONS).
	TypeZombieVillager: &kind{
		family:       TypeZombie,
		name:         "Zombie Villager",
		width:        0.6,
		height:       1.95,
		eyeHeight:    1.74,
		maxHealth:    20,
		speed:        float64(float32(0.23)),
		hostile:      true,
		followRange:  35,
		attackDamage: 3,
		armor:        2,
		babyWidth:    0.49,
		babyHeight:   0.98,
		babyEye:      0.67,
		sounds: MobSounds{
			Ambient: registry.SoundEntityZombieVillagerAmbient,
			Hurt:    registry.SoundEntityZombieVillagerHurt,
			Death:   registry.SoundEntityZombieVillagerDeath,
			Step:    registry.SoundEntityZombieVillagerStep,
		},
		burnsInDaylight: true,
	},
	// vanilla: EntityTypes.ZOMBIFIED_PIGLIN's builder (fireImmune) over
	// ZombifiedPiglin.createAttributes: the zombie's with five attack damage
	// and no reinforcements. Neutral: it hunts only what angered it.
	TypeZombifiedPiglin: &kind{
		family:       TypeZombie,
		name:         "Zombified Piglin",
		width:        0.6,
		height:       1.95,
		eyeHeight:    1.79,
		maxHealth:    20,
		speed:        float64(float32(0.23)),
		hostile:      true,
		followRange:  35,
		attackDamage: 5,
		armor:        2,
		babyWidth:    0.49,
		babyHeight:   0.98,
		babyEye:      0.78,
		fireImmune:   true,
		sounds: MobSounds{
			Ambient: registry.SoundEntityZombifiedPiglinAmbient,
			Hurt:    registry.SoundEntityZombifiedPiglinHurt,
			Death:   registry.SoundEntityZombifiedPiglinDeath,
			Step:    registry.SoundEntityZombieStep,
		},
	},
	// vanilla: EntityTypes.SLIME's builder, a size-one box of 0.52 scaled by
	// size; attributes by size from AbstractCubeMob.setSize and Slime.setSize,
	// over Mob's follow range.
	TypeSlime: &kind{
		name:        "Slime",
		width:       0.52,
		height:      0.52,
		eyeHeight:   0.325,
		maxHealth:   1,
		hostile:     true,
		followRange: 16,
		cube:        true,
		spawnScale:  4,
	},
	// vanilla: EntityTypes.MAGMA_CUBE's builder; fire immune, and
	// MagmaCube.setSize's three points of armour a size.
	TypeMagmaCube: &kind{
		name:        "Magma Cube",
		width:       0.52,
		height:      0.52,
		eyeHeight:   0.325,
		maxHealth:   1,
		hostile:     true,
		followRange: 16,
		cube:        true,
		spawnScale:  4,
		fireImmune:  true,
	},
	// vanilla: EntityTypes.SNOW_GOLEM's builder and SnowGolem.createAttributes.
	// Box 0.7 by 1.9, eyes 1.7, four of health, speed 0.2. On the
	// freeze_immune_entity_types tag. Shearing is not modelled.
	TypeSnowGolem: &kind{
		waterSensitive: true,
		name:           "Snow Golem",
		width:          0.7,
		height:         1.9,
		eyeHeight:      1.7,
		maxHealth:      4,
		speed:          float64(float32(0.2)),
		sounds: MobSounds{
			Ambient: registry.SoundEntitySnowGolemAmbient,
			Hurt:    registry.SoundEntitySnowGolemHurt,
			Death:   registry.SoundEntitySnowGolemDeath,
		},
		ambientInterval: 120, // AbstractGolem.getAmbientSoundInterval's own
	},
	// vanilla: EntityType.WOLF's builder and Wolf.createAttributes. Box 0.6 by
	// 0.85, eyes 0.68, eight of health, speed 0.3. Variants are not simulated.
	TypeWolf: &kind{
		name:       "Wolf",
		fireAverse: true,
		width:      0.6,
		height:     0.85,
		eyeHeight:  0.68,
		maxHealth:  8,
		speed:      float64(float32(0.3)),
		volume:     0.4, // Wolf.getSoundVolume
		// The classic set; Sounds picks the variant's.
		sounds: MobSounds{Ambient: registry.SoundEntityWolfAmbient,
			Hurt: registry.SoundEntityWolfHurt, Death: registry.SoundEntityWolfDeath},
		// Wolf.createAttributes' ATTACK_DAMAGE
		attackDamage:    4,
		ambientInterval: 120, // vanilla: Animal.getAmbientSoundInterval
	},
	// vanilla: EntityTypes.IRON_GOLEM's builder and IronGolem.createAttributes.
	// Box 1.4 by 2.7, eyes the four-fifths default, a hundred of health, speed
	// 0.25, knockback resistance 1.0 and base attack damage 15.
	// AbstractGolem has no ambient sound.
	TypeIronGolem: &kind{
		name:            "Iron Golem",
		width:           1.4,
		height:          2.7,
		maxHealth:       100,
		speed:           0.25,
		knockbackResist: 1,
		attackDamage:    15,
		stepHeight:      1,
		sounds: MobSounds{
			Hurt:       registry.SoundEntityIronGolemHurt,
			Death:      registry.SoundEntityIronGolemDeath,
			Step:       registry.SoundEntityIronGolemStep,
			StepVolume: 1,
		},
	},
}

// init derives each kind's babyable from its family and type; Frog and
// ZombieNautilus override canBeABaby to false.
// vanilla: AgeableMob.canBeABaby
func init() {
	for i, k := range kinds {
		if k == nil {
			continue
		}
		t := MobType(i)
		k.babyable = t != TypeFrog && t != TypeZombieNautilus && (Family(t) == TypeZombie || ageableAnimal(t) || t == TypeVillager || t == TypeWanderingTrader || t == TypeZoglin)
	}
}

// The chicken's egg: one every five to ten minutes, rerolled from the mob's
// own dice after each egg.
const (
	eggIntervalMin = 6000
	eggIntervalMax = 12000
)

// Player is what a mob knows about a player; the server builds the list
// fresh each tick. Huntable folds vanilla's targeting conditions: alive,
// and neither creative nor spectator. Pushable is EntitySelector.pushableBy:
// alive and not a spectator, creative included. Sneaking, Invisible and
// Worn shorten the hunt's reach (a worn head of a mob's own kind hides the
// player from it).
type Player struct {
	// Age is the body tick count used by dragon contact cooldowns.
	Age     int
	X, Y, Z float64
	// Width and Height are the target's box; zero means a player's 0.6 by 1.8.
	Width, Height float64
	// EyeHeight is the eyes' height above Y; zero means 0.9 of Height, or a
	// standing player's when Width is unset.
	EyeHeight float64
	// VX, VY, VZ is the server-side delta movement in blocks per tick, which a
	// ranged mob leads its aim by.
	VX, VY, VZ float64
	// KnownVX, KnownVY, KnownVZ is the movement the client last reported, in
	// blocks per tick.
	// vanilla: ServerPlayer.getKnownMovement
	KnownVX, KnownVY, KnownVZ float64
	Yaw, Pitch                float64
	HeldItem                  protocol.ItemStack
	// ArmourEffectiveness is the held weapon's armor_effectiveness delta
	// (Breach) for a blow this attacker lands.
	ArmourEffectiveness float64
	OffHandItem         protocol.ItemStack
	EntityID            int32
	// UUID is the player's identity, which persistent anger and a tamed mob's
	// owner are saved by.
	UUID     uuid.UUID
	Huntable bool
	Pushable bool
	// Team is the name of the player's scoreboard team, "" for none.
	Team string
	// TeamCollision is that team's collision rule.
	TeamCollision CollisionRule
	// Passenger says the body rides a vehicle; cramming leaves it out.
	// vanilla: Entity.isPassenger
	Passenger bool
	// OnHappyGhast says the player is seated on a happy ghast.
	// vanilla: HappyGhast.scanPlayerAboveGhast
	OnHappyGhast bool
	// BoatDriver and NautilusDriver identify controlling passengers for dolphin following.
	BoatDriver, NautilusDriver bool
	// RideBox is the immediate vehicle's box, or zero on foot.
	RideBox AABB
	// RideRoot is the root vehicle's feet and the top of its dimensions.
	RideRoot struct{ X, Y, Z, Top float64 }

	// Spectator is a spectator, left out of the despawn's nearest-player
	// distance.
	// vanilla: EntitySelector.NO_SPECTATORS
	Spectator bool
	// Dead is a body awaiting respawn, which no mob notices.
	// vanilla: LivingEntity.canBeSeenByAnyone
	Dead bool

	// Flying, InWater, InPowderSnow and OnGround are the facts a parrot
	// lands on a shoulder by; ShoulderSlots is how many shoulders are free.
	// vanilla: Abilities.flying, Entity.isInWater, isInPowderSnow,
	// ServerPlayer.setEntityOnShoulder
	Flying, InWater, InPowderSnow, OnGround bool
	ShoulderSlots                           int

	// Creative is a creative player, whose blow reaches an invulnerable mob.
	// vanilla: DamageSource.isCreativePlayer
	Creative bool

	// Sneaking is the sneak key's flag. vanilla: Entity.isDiscrete
	Sneaking bool
	// Sprinting is the sprint key flag, independent of movement speed.
	Sprinting bool
	// Swimming is Entity.isSwimming, independent of the current pose.
	Swimming bool
	// Sleeping is a body lying in a bed. vanilla: LivingEntity.isSleeping
	Sleeping bool
	// SleepTimer is the sleep counter: 100 once a wake completed the sleep, 0
	// after a forced one.
	// vanilla: Player.getSleepTimer, stopSleepInBed
	SleepTimer int
	// Invisible says the body carries the Invisibility effect.
	// vanilla: LivingEntity.updateInvisibilityStatus
	Invisible bool
	// Worn is the worn armour, indexed like Mob.Worn: feet, legs, chest, head.
	Worn [4]protocol.ItemStack

	// Health is the body's health, which a witch's choice of potion reads.
	Health float64
	// Slowed, Poisoned and Weakened say the body carries the effect, for the
	// witch's choice of potion.
	Slowed, Poisoned, Weakened bool
	// Hero says the body carries Hero of the Village, which villagers give gifts for.
	Hero bool
	// HurtBy is the entity ID that last struck this body within vanilla's 100
	// ticks, 0 for none; HurtByStamp is the body's age then.
	// vanilla: LivingEntity.getLastHurtByMob, getLastHurtByMobTimestamp
	HurtBy      int32
	HurtByStamp int
	// HurtNoWolfRetaliation says the body's last landed hurt is in
	// #no_wolf_retaliation, which a tamed animal does not avenge.
	// vanilla: OwnerHurtByTargetGoal.canUse
	HurtNoWolfRetaliation bool
	// HurtMob is the entity ID of the mob this body last hit, 0 for none;
	// HurtMobStamp is the body's age then.
	// vanilla: LivingEntity.getLastHurtMob, getLastHurtMobTimestamp
	HurtMob      int32
	HurtMobStamp int
}

// playerEyeHeight is a standing player's eye height, the same 1.62 the
// packages above use.
const playerEyeHeight = 1.62

// box is the target's width, height and eye height, a standing player's
// 0.6, 1.8 and 1.62 when the snapshot carries none.
func (p *Player) box() (w, h, eye float64) {
	w, h, eye = p.Width, p.Height, p.EyeHeight
	if w == 0 {
		w = playerWidth
	}
	if h == 0 {
		h = playerHeight
		if p.Width == 0 && eye == 0 {
			eye = playerEyeHeight
		}
	}
	if eye == 0 {
		eye = h * 0.9
	}
	return w, h, eye
}

// width is the target's box width.
func (p *Player) width() float64 {
	w, _, _ := p.box()
	return w
}

// height is the target's box height.
func (p *Player) height() float64 {
	_, h, _ := p.box()
	return h
}

// eyeHeight is the target's eye height above its feet.
func (p *Player) eyeHeight() float64 {
	_, _, eye := p.box()
	return eye
}

// Hurt numbers: invulnerableTime is the window a landed hurt opens; in its
// first half (above hurtCooldown) a new hurt lands only its excess over the
// last amount, without knockback. Knockback is 0.4 blocks a tick, and as
// much upward for a body on the ground. LivingEntity.getLastDamageSource
// keeps a hurt fresh 40 ticks; resolvePlayerResponsibleForDamage remembers a
// player's hurt 100 ticks for the death's experience.
const (
	hurtCooldown      = 10
	invulnerableTime  = 20
	knockbackStrength = 0.4
	lastDamageExpiry  = 40
	playerMemoryTicks = 100
)

// Mob is one simulated mob. X, Y, Z are its feet, with the box around them;
// Yaw is the body's, Pitch and HeadYaw the head's. A mob at zero Health is
// Dead until the tick that reports the death removes it. Rand drives every
// choice the mob makes and is seeded by the caller, so a seed reproduces a
// walk. The unexported machinery is driven by ticking, not by callers.
type Mob struct {
	LivingState
	ID   int32
	Type MobType

	Place
	VX, VY, VZ          float64
	Yaw, Pitch, HeadYaw float32
	OnGround            bool
	// GroundGuess marks OnGround as the spawn's stand-in, not a saved fact.
	GroundGuess bool
	look        lookControl

	Health, MaxHealth float64
	// damagedEvent marks a hurt not yet told as entity_damage.
	damagedEvent bool
	// recordSlot fits the padding before LivingEffects, preserving the hot field offsets.
	recordSlot int32
	LivingEffects
	// AirSupply is the breath left in ticks: rules.MaxAirSupply out of water,
	// spent a tick at a time with the eyes under it. Metadata index 1.
	AirSupply int
	// TicksFrozen is Entity.ticksFrozen: +1 a tick in powder snow, -2 outside
	// it. Metadata index 7.
	TicksFrozen int
	// ArrowCount is the arrows stuck in the body, metadata index 12;
	// removeArrowTime counts down to the next falling out.
	ArrowCount, removeArrowTime int
	// wasInPowderSnow is last tick's powder-snow read, which keeps the sun off
	// for a tick after leaving it; lastInPowderSnow is this tick's.
	// vanilla: Entity.baseTick
	wasInPowderSnow, lastInPowderSnow bool

	// wolf is a wolf's wet-and-shake state.
	wolf wolfShake
	// attrs is the body's attribute map, installed on a command's first
	// read or edit or from a saved list the map owns; nil until then.
	// pace and the health cap read it under the server's mob lock.
	attrs *player.Attributes
	// followSpawnBonus is the add_multiplied_base amount of follow_range's
	// minecraft:random_spawn_bonus, in [-0.11485, 0.11485]; 0 for none.
	// vanilla: Mob.RANDOM_SPAWN_BONUS_ID
	followSpawnBonus float64
	// attrWorn is the equipment whose modifiers the map holds, in
	// equipmentSlots order.
	attrWorn      [7]protocol.ItemStack
	attrWornRead  uint64
	attrWornValid bool
	// Tamable is the owner and sit state of a tameable mob.
	Tamable Tamable
	// underfootAir is whether the block under the body is air, refreshed by
	// freezeStep while frozen: tryAddFrost withholds the slow from a body with
	// nothing under it.
	underfootAir bool
	// FallDistance is the fall in progress, banked a block at a time until
	// a landing spends it.
	FallDistance float64
	// locCell and locRevision are the cell and equipment revision the last
	// tick's pack location_changed check saw.
	locCell     [3]int32
	locSeen     bool
	locRevision uint64
	// locPieces is each EnchEquipment piece as of the last revision move.
	locPieces [6]protocol.ItemStack
	// locChecked and locEnchanted cache whether any worn or held stack
	// carries an enchantment with a location_changed or tick entry, as of
	// locRevision and the enchantment definitions' locGen.
	locChecked, locEnchanted bool
	locGen                   uint64
	// loc is the set of location_changed entries active per slot.
	loc rules.LocatedState
	// moveOrder is a plugin's walk in progress, which the goals leave alone.
	moveOrder bool
	// NoPush makes the body neither push nor be pushed, set by a plugin
	// (LivingEntity.setCollidable); it is not saved.
	NoPush bool
	// InvulnTicks counts down the invulnerability window of the last hit.
	InvulnTicks int
	// lastHurt is the pre-armour amount of the last landed hurt, which the
	// window measures the next one's excess against.
	// vanilla: LivingEntity.lastHurt
	lastHurt float64
	// Gate is asked about each hurt that lands; nil when the server has none.
	Gate HurtGate
	// EffectGate is asked before an effect is added or removed; EffectCause
	// is what the caller names for the next change, spent by it.
	EffectGate  EffectGate
	EffectCause effect.Cause
	// Barter is asked what a bartering piglin throws; nil when the server has none.
	Barter BarterGate
	// Career is asked before a villager takes a profession and reports whether
	// it may; nil when the server has none.
	Career func(m *Mob, profession string) bool
	// Convert is asked before the body turns into another kind and reports
	// whether it may; nil when the server has none.
	Convert func(m *Mob, into MobType) bool
	// Combust is asked before the body catches fire for ticks and answers the
	// ticks it burns, 0 for none; nil when the server has none.
	Combust func(m *Mob, ticks int, src CombustSource) int
	// GameTime is the world's age, stamped by the server ahead of each tick;
	// anger end times are measured on it.
	GameTime int64
	// Effective is the effective difficulty at the mob, which the illusioner's
	// blindness spell reads; the server refreshes it each tick.
	Effective float32
	// Age is the ticks the body has lived.
	Age int
	// BabyAge is AgeableMob.age: negative while a baby, counting up to zero
	// (24000 ticks of growing), and positive while a bred adult's cooldown
	// counts down. A baby's death drops nothing and gives no experience.
	BabyAge int
	// InLove is Animal.inLove: 600 ticks counting down after a feed, while the
	// body seeks a mate. Only a grown body at age zero holds it (the tick clears
	// it otherwise), and a hurt clears it.
	InLove int
	// LoveCause is the player whose feeding caused love, credited with
	// the birth; HasLoveCause tells a stored all-zero UUID from none.
	// vanilla: Animal.loveCause
	LoveCause    uuid.UUID
	HasLoveCause bool
	// Persistence is PersistenceRequired, which spares a mob the distance and
	// idleness despawns.
	Persistence bool
	// Life is the endermite's age in ticks, the Lifetime tag; it is
	// discarded at endermiteMaxLife unless persistent.
	// vanilla: Endermite.life
	Life int
	// Invulnerable is the Invulnerable tag: every hurt is refused but a
	// creative player's blow and the bypasses-invulnerability damage types.
	// vanilla: LivingEntity.isInvulnerableTo
	Invulnerable bool
	// CanPickUpLoot is the CanPickUpLoot tag: whether the body takes items
	// within reach.
	CanPickUpLoot bool
	// AgeLocked is AgeableMob's AGE_LOCKED: a locked baby does not grow.
	AgeLocked bool
	// ageLockCooldown counts down the ticks until the golden dandelion can be
	// used again, a pause or reset icon on every even one.
	ageLockCooldown int
	// forcedAge is the ticks feeding has grown the baby by, which become its
	// breeding cooldown when an unforced growth makes it an adult.
	// vanilla: AgeableMob.forcedAge
	forcedAge int
	// forcedAgeTimer counts down the ticks of happy_villager sparkles after a
	// feeding grew the baby. vanilla: AgeableMob.forcedAgeTimer
	forcedAgeTimer int
	// ZombificationImmune is Hoglin's IsImmuneToZombification: it never
	// turns into a zoglin.
	ZombificationImmune bool
	// NoAI skips goals, navigation and travel, gravity included.
	// vanilla: Mob.isEffectiveAi, LivingEntity.aiStep
	NoAI bool
	// vanilla: Entity.isNoGravity
	NoGravity bool
	// Team is the name of the scoreboard team the body is on, "" for none,
	// read from the board each tick. vanilla: Entity.getTeam
	Team string
	// TeamCollision is that team's collision rule, read with Team.
	TeamCollision CollisionRule
	// Silent is the Silent tag: the body's own sounds are suppressed.
	Silent bool
	// Glowing is the Glowing tag: shared flag bit 0x40 stays set while it is on,
	// effect or not.
	// vanilla: Entity.setGlowingTag, Entity.isCurrentlyGlowing
	Glowing bool
	// VisualFire is HasVisualFire: the flames are drawn whether or not it burns.
	VisualFire bool
	// LeftHanded is Mob.leftHanded: Mob metadata bit 0x02 and the saved
	// LeftHanded tag. A finalize rolls it 5 in 100.
	LeftHanded bool
	// RidesVehicle marks a body whose vehicle places it instead of its own
	// travel. Its tick still runs, including goals and pushing.
	// vanilla: Entity.rideTick
	RidesVehicle bool
	// BoardingCooldown is the ticks left before a minecart or boat may pick the
	// body up again, 60 on leaving a vehicle; not saved.
	// vanilla: Entity.boardingCooldown, Entity.removePassenger
	BoardingCooldown int32
	// Vehicle is the mob this body rides, nil when walking or seated in a boat
	// (boats are not mobs), so a seat has one holder. Guarded by the mob lock.
	Vehicle *Mob
	// Passengers are the riders in mounting order, as Set Passengers lists
	// them.
	Passengers []*Mob

	// CanBreakDoors is Zombie's canBreakDoors, rolled at the finalize: it adds
	// the door-breaking goal and lets paths run through doors.
	CanBreakDoors bool
	// zombie is the attribute state of the zombie family.
	zombie zombieState
	// enderman is the enderman's own state.
	enderman endermanState
	// queued is the events a call outside the tick raised (the silverfish a
	// landed hurt let out, a resize's splash), told on the next step.
	queued []MobEvent
	// infestRng is the infested effect's dice, drawn from Rand at first use so
	// the other rolls keep their order.
	infestRng *rand.Rand
	// friends is a silverfish's wake-up goal, which a landed hurt arms.
	friends *wakeFriendsGoal
	// anger is NeutralMob's persistent anger (enderman, zombified piglin, polar
	// bear).
	anger angerState
	// polar is the polar bear's warning-sound counter.
	polar polarState
	// Standing is a polar bear's warning pose.
	// vanilla: PolarBear.DATA_STANDING_ID
	Standing bool
	// piglin is the zombified piglin's own state.
	piglin piglinState
	bee    beeState
	// barter is the piglin and the brute's bartering and conversion state.
	barter barterState

	// ChickenJockey marks a hen a baby zombie rode: no eggs, and the monster
	// despawn.
	// vanilla: Chicken.setChickenJockey, Chicken.removeWhenFarAway
	ChickenJockey bool

	// Horse is the horse's own state; zero for every other kind.
	Horse HorseState
	// stillTimeout counts the ticks a happy ghast still holds still.
	// vanilla: HappyGhast.serverStillTimeout
	stillTimeout int

	// CamelHuskFits is set by the spawner before finalizing: the camel husk's
	// box is free at the husk's block.
	CamelHuskFits bool
	// MonsterJockeysDisabled is set by the spawner before finalizing when
	// spawn_mobs or spawn_monsters is off.
	MonsterJockeysDisabled bool
	// Jockey is the ride finalizeSpawn rolled, resolved once the mob is in the
	// world; RiderKind names what a JockeyRider carries, Rider is a
	// JockeyBuiltRider's passenger.
	Jockey    JockeyKind
	RiderKind MobType
	Rider     *Mob
	Rand      *rand.Rand
	// lootSeed seeds the equipment drops, split apart from Rand so wandering
	// draws never move it; lootRand is built on first use.
	lootSeed int64
	lootRand *rand.Rand
	// DeathLootTable names the table the death rolls instead of the kind's
	// own ("" for none); DeathLootTableSeed, when not 0, seeds that roll.
	// vanilla: Mob.lootTable, lootTableSeed
	DeathLootTable     string
	DeathLootTableSeed int64

	// EggTimer is the chicken's countdown to its next egg.
	EggTimer int

	// ScuteTimer is the armadillo countdown to its next shed.
	ScuteTimer                                                    int
	armadilloState                                                int
	armadilloTicks, armadilloDanger, armadilloPeek, armadilloScan int
	armadilloDangerAround, armadilloPendingRoll                   bool
	copper                                                        copperGolemState
	// Antenna is the copper golem's antenna slot, the saved saddle slot.
	Antenna protocol.ItemStack

	sniffer snifferBrain
	panda   pandaState

	// Home is the turtle's home block; HasEgg and LayingEgg are its synched egg
	// flags; layEggCounter and goingHome are shared goal state.
	// vanilla: Turtle.homePos, HAS_EGG, LAYING_EGG, layEggCounter, goingHome
	Home          Waypoint
	HasEgg        bool
	LayingEgg     bool
	layEggCounter int
	goingHome     bool

	// Squirt says a hurt squid is to play its squirt sound and ink cloud;
	// whoever reads it clears it.
	// vanilla: Squid.hurtServer, Squid.spawnInk
	Squirt bool

	// DarkTicks is DATA_DARK_TICKS_REMAINING (metadata 18): the 100 ticks a hurt
	// dims a glow squid, counted down each tick.
	// vanilla: GlowSquid.hurtServer's setDarkTicks
	DarkTicks int

	// Unhappy is a villager's DATA_UNHAPPY_COUNTER (metadata 18): 40 ticks of
	// head-shaking after a refusal, counted down each tick.
	// vanilla: Villager.setUnhappy
	Unhappy int

	// squid is the tentacle swim's state: the swim vector, the cycle and its
	// pace.
	// vanilla: Squid.movementVector, tentacleMovement, tentacleSpeed
	squid struct {
		moveX, moveY, moveZ float64
		tentacle            float64
		pace                float64
	}

	// phantom is the phantom's flight mind.
	phantom phantomState

	// Resting is the bat's hanging flag, bit 0x01 of its metadata byte.
	Resting bool
	bat     batState

	// FromBucket marks a bucket-released mob, which never despawns.
	FromBucket bool
	// Named is whether the mob has a custom name; a named fish never despawns.
	Named bool
	// Johnny is a vindicator that was ever named "Johnny": it attacks every
	// living body.
	Johnny bool
	// InRaid is a raider counted by a raid, which never despawns.
	// vanilla: Raider.requiresCustomPersistence
	InRaid bool
	// PatrolLeader is Raider.patrolLeader, set when the raider takes the
	// captain's post.
	PatrolLeader bool
	// Patrolling preserves the patrol against random despawning within 128 blocks.
	Patrolling bool
	// PatrolTarget is the nullable long-distance patrol destination.
	PatrolTarget *Waypoint
	banner       bannerSeek
	raidWalk     raidWalk
	// Celebrating is Raider.IS_CELEBRATING, set while the raid is lost.
	Celebrating     bool
	SalmonSize      int
	TropicalVariant int32
	axolotl         axolotlState
	parrot          parrotState
	allay           allayState
	tadpole         tadpoleState
	nautilus        nautilusState
	camel           camelState
	dolphin         dolphinState
	fish            fishState
	puffer          pufferState
	guardian        guardianState
	shulker         shulkerState
	hoglin          hoglinState
	blaze           blazeState
	breeze          breezeState
	spell           spellState
	ravager         ravagerState
	vex             vexState
	ghast           ghastState
	wither          witherState
	// reflected is set while a player's large fireball hurts the ghast, which
	// fire immunity does not stop.
	reflected    bool
	strider      striderState
	itemSteering itemSteeringState
	home         homeState
	// WardenIdentities resolves mob identities while the caller holds the herd lock.
	WardenIdentities MobIdentities
	// NautilusIdentities resolves the saved anger target under the herd lock.
	NautilusIdentities MobIdentities
	// DolphinItems is the tick's pickable item snapshot for the play goal.
	DolphinItems []FoxItem

	warden   wardenState
	creaking creakingState
	// dragon is set for an ender dragon alone.
	dragon *dragonState

	// CustomData is the saved "data" compound, nil when empty.
	// vanilla: Entity.customData
	CustomData nbt.Compound
	// Colour is a sheep's wool colour name ("white", "orange", ...), from
	// vanilla's mostly-white distribution.
	Colour string
	// Collar is a wolf's collar colour word, red until a dye changes it.
	Collar string
	// Body is the body-slot stack; write it through SetEquipment.
	Body   protocol.ItemStack
	sulfur sulfurState
	// armorCracked and armorBroke are the armour damage since the last tick's
	// event: a crack threshold crossed, or the piece gone.
	armorCracked, armorBroke bool
	// golemCracked is a crack level crossed by a blow since the last tick.
	golemCracked bool
	// Begging is a wolf's interested flag, which tilts its head.
	Begging bool

	// PlayerCreated is an iron golem's 0x01 metadata bit, set when a player
	// builds it and saved with it.
	// vanilla: IronGolem.isPlayerCreated
	PlayerCreated bool

	// Mushroom is a mooshroom's variant, "red" (MushroomCow.Variant.DEFAULT) or
	// "brown".
	Mushroom string

	// StewEffects is nil until a flower is fed; a non-nil empty list still fills suspicious stew.
	StewEffects []protocol.StewEffect

	// RabbitType is the Rabbit.Variant id (0 brown, 1 white, 2 black, 3 white
	// splotched, 4 gold, 5 salt, 99 evil), metadata index 18 and the RabbitType
	// save field.
	RabbitType int32

	// Goat is a goat's variant, horns and ram state; no other type reads it.
	Goat GoatData
	frog frogTongue
	// pregnant is the frog's saved IS_PREGNANT memory.
	pregnant bool
	// Fox is the state only a fox keeps.
	Fox FoxState
	// MoreCarrotTicks counts down the rest after a rabbit's meal; it eats
	// again at 0.
	// vanilla: Rabbit.moreCarrotTicks
	MoreCarrotTicks int
	rabbit          rabbitState

	// Trusting is an ocelot that no longer flees players.
	// vanilla: Ocelot.DATA_TRUSTING
	Trusting bool

	// Lying and RelaxStateOne are a cat's bed poses: curled up, and settling
	// beside its sleeping owner.
	// vanilla: Cat.IS_LYING, RELAX_STATE_ONE
	Lying, RelaxStateOne bool
	// catTempt is the cat's fish tempt, which a stray begs through.
	catTempt *temptGoal
	// ChestOpen says whether anyone has the chest at a cell open, set each tick
	// for a cat.
	ChestOpen func(x, y, z int32) bool
	// Chests is the copper golem's reach into chests, set each tick.
	Chests ChestAccess

	// Variant and SoundVariant are a cow's, pig's or chicken's variant registry
	// entries ("minecraft:cold"); "" for other kinds.
	Variant, SoundVariant string

	// Sheared says a sheep has lost its fleece (grazing regrows it) or a bogged
	// its mushrooms.
	Sheared bool

	// Impulse requests a movement update off cadence, such as for a shove
	// or a cube's takeoff. Whoever reads it clears it.
	Impulse bool

	// TargetID is the entity ID of the player the mob targets (Mob.setTarget),
	// 0 for none. Target goals revalidate it against the tick's player list.
	TargetID int32

	// LeashHolderID is the entity ID of the player or leash knot holding the
	// lead, 0 when unleashed. The tick drops it when the holder is gone or out
	// of reach.
	// vanilla: Leashable.LeashData.leashHolder
	LeashHolderID int32

	// alerted is the attacker another mob's HurtByTargetGoal.alertOthers named.
	// The mob's own goal adopts it on its next selection and drops it when the
	// attacker is gone; the marker also stops the alert spreading further.
	alerted int32

	// Aggressive is Mob.setAggressive's metadata bit (a zombie's raised arms),
	// held by the melee attack goal while it runs.
	Aggressive bool

	// Climbing is the spider's 0x01 metadata bit, set each tick from horizontal
	// collision; while set, the physics clamps the walk.
	// vanilla: handleOnClimbable
	Climbing bool

	// MainHand and OffHand are the held stacks; Worn is the armour indexed by
	// ArmorSlotNames (feet, legs, chest, head). DropChances is
	// DropChances.byEquipment, indexed hands first, then armour feet to head;
	// above 1 the piece always drops undamaged, and a picked-up piece is at 2.
	// Equipment stacks are read-only; write them through SetEquipment.
	MainHand, OffHand protocol.ItemStack
	equipmentRevision uint64
	// totemSaved and totemChanges are a death-protection save the next tick
	// reports: the animation and the effects it turned.
	totemSaved   bool
	totemChanges []effect.Change
	Worn         [4]protocol.ItemStack
	DropChances  [6]float32
	// BodyDropChance and SaddleDropChance are the chances of the body and
	// saddle slots, which have no loot slot but are saved in drop_chances.
	BodyDropChance, SaddleDropChance float32
	dropChancesOrder                 *DropChancesOrder

	// Size is a cube mob's size, 0 for every other kind; cube is its move
	// control.
	Size int
	cube cubeMove

	// Offers is a villager's or wandering trader's saved Offers compound, the
	// trades it holds (vanilla: MerchantOffers.CODEC); nil for one with none
	// yet.
	Offers nbt.Compound
	// OfferUpdater supplies the saved recipe codec for restocking.
	OfferUpdater OfferUpdater
	// DespawnDelay is the wandering trader's ticks left before it leaves; zero
	// stays forever.
	DespawnDelay int32
	// WanderTarget is the wandering trader's block to walk to, nil for none;
	// useLeft is its potion or milk's use ticks left.
	// vanilla: WanderingTrader.wanderTarget, LivingEntity.useItemRemaining
	WanderTarget *Waypoint
	useLeft      int32
	// VillagerXp is Villager.villagerXp, the trade experience toward the next
	// level; Trading is whether a player has its merchant window open. Both are
	// guarded by the mob lock.
	VillagerXp int32
	cure       zombieCure
	Trading    bool
	// TradingPlayer is the UUID of the player whose window is open; valid
	// while Trading.
	TradingPlayer uuid.UUID
	// vb is the villager's memories, schedule and restock counters; Village is
	// its dimension's POIs (also an allay's dimension) and DayTime the time of
	// day, both set before each tick.
	vb villagerMemory
	// vg is the villager's gossip and reputation.
	vg villagerGossip
	// FoodLevel is Villager.foodLevel, the food points the villager has eaten
	// and not yet spent on breeding.
	FoodLevel int32
	// Sleeping is a villager lying in its bed. vanilla: LivingEntity.isSleeping
	Sleeping bool
	// DrawTrades requests lazy Offers population after the mob lock.
	DrawTrades func(*Mob)
	Village    VillageView
	DayTime    int64
	// OfferFlowerTicks is IronGolem.offerFlowerTick, transient and counted every tick.
	OfferFlowerTicks int
	forceFlowerOffer bool
	// Villager is the VillagerData and whether its type was finalized from the
	// birth biome.
	Villager VillagerData

	// InWaterTime and the drowned conversion are Zombie's inWaterTime,
	// isUnderWaterConverting and conversionTime.
	InWaterTime           int
	DrownedConverting     bool
	DrownedConversionTime int

	// StrayConverting and StrayConversionTime are Skeleton's
	// isFreezeConverting and conversionTime; inPowderSnowTime is the ticks it
	// has spent in powder snow.
	StrayConverting     bool
	StrayConversionTime int
	inPowderSnowTime    int

	// Swimming is Drowned.updateSwimming: under water with a target in water.
	// swimSpeed is the speed its move control eases toward the pace.
	// searchingForLand is Drowned.searchingForLand, set while DrownedSwimUpGoal runs.
	Swimming         bool
	swimSpeed        float64
	searchingForLand bool

	// LootExtra adds what only the server knows (scoreboard tags, team,
	// related bodies) to the snapshot Mob.LootEntity builds; nil adds none.
	// It runs, and the relation closures it sets are valid, only under mobsMu.
	LootExtra func(*rules.LootEntity)

	// Crouching and Sprinting are a cat's or ocelot's stance, set from the walk
	// pace: crouched at the tempt pace, sprinting at the flee pace.
	Crouching, Sprinting bool

	// UsingItem is Mob.isUsingItem: a skeleton holds it from the draw's first
	// tick to the arrow's release.
	UsingItem bool
	// UseOffHand is bit 0x02 of LivingEntity.DATA_LIVING_ENTITY_FLAGS: the
	// last use started in the off hand. A stop leaves it, as vanilla's does.
	UseOffHand bool
	// ChargingCrossbow is Pillager.IS_CHARGING_CROSSBOW.
	ChargingCrossbow bool
	// witch is the witch's own state.
	witch witchState

	// SwellDir is the creeper's swell direction metadata: 1 while the fuse
	// runs, -1 when idle. The swell goal sets it; the server sends it when it
	// changes.
	SwellDir int

	// Ignited is the creeper's ignited flag: a fuse lit by a creeper_igniters
	// item runs to the blast whatever the swell goal asks. It is saved, so a
	// load relights it.
	Ignited bool

	// Powered is the creeper's DATA_IS_POWERED, loaded from the powered tag; it
	// doubles the blast radius.
	Powered bool

	// MaxSwell and ExplosionRadius are the creeper's maxSwell and
	// explosionRadius, 30 and 3 from its constructor unless the save says
	// otherwise.
	MaxSwell        int
	ExplosionRadius int

	// FireTicks is remainingFireTicks: while positive the on-fire flag shows,
	// one fire damage lands every 20th tick, and water or rain puts it out.
	FireTicks int

	// NoActionTicks is noActionTime, the idleness counter the despawn reads.
	NoActionTicks int

	// Dead marks a corpse; deathTold gates kill effects independently of the
	// corpse's ticks, including zero health loaded without a killing blow.
	Dead      bool
	deathTold bool
	// deathSoftened: the killing blow was window-softened.
	deathSoftened bool

	// gone says the mob was discarded: removed with no death animation, sound
	// or drops.
	gone bool
	// discarded asks a tick to report the vanishing (a leaving rider's boat's
	// passengers).
	discarded bool
	// herdTicked marks a body Mobs.tickBuf stepped this pass, which a rider
	// set down mid-pass must not repeat; herdKept says the collection keeps
	// it, decided right after its own tick.
	herdTicked, herdKept bool
	// Dormant holds the mob out of the herd's tick: its chunk is loaded but
	// not entity-ticking. The server sets it each tick.
	// vanilla: ServerLevel.tick (entityTickList, ChunkLevel.isEntityTicking)
	Dormant bool
	// ticked marks a body that has run a tick, which instantiates movement_speed.
	ticked bool

	// The last hurt. hurtByPlayer is true for a player's swing, for the loot
	// tables' killed_by_player. retaliate is that player's entity ID, for the
	// hostile goal that turns on its attacker. playerMemoryTicks is
	// lastHurtByPlayerMemoryTime, counted down a tick at a time and read by the
	// death's experience. lastDamageType and lastDamageStamp are
	// lastDamageSource and lastDamageStamp, so panic can tell a fresh hurt from
	// a stale one.
	hurtByPlayer    bool
	hurtByMelee     bool
	lastAttackerID  int32
	lastAttackerAge int
	retaliate       int32
	// TargetWhy is the goal behind the last target a retaliation or owner goal
	// set; it counts only while its ID is the mob's target.
	TargetWhy         TargetWhy
	retaliateStamp    int
	playerMemoryTicks int
	// playerCreditID is the player whose hurt set playerMemoryTicks; a later
	// mob's blow overwrites retaliate but not this.
	playerCreditID  int32
	lastDamageType  registry.DamageType
	lastDamageStamp int
	// blowAttacker is the entity ID behind lastDamageType, 0 for none; unlike
	// retaliate, a later sourceless hurt clears it.
	blowAttacker int32
	// blowDirect is the entity type of the projectile behind the last landed
	// hurt, "" for none; a later hurt clears it.
	blowDirect string
	// combat is the body's combat tracker; hurt is the landed hurt not yet
	// recorded in it, and climbedFrom where the body last climbed (see
	// mobcombat.go).
	combat      rules.CombatTracker
	hurt        rules.CombatEntry
	climbedFrom rules.FallLocation

	ai  goalSelector
	nav mobNav
	// motion is displacement between tick starts, before travel applies drag.
	// vanilla: Entity.computeSpeed, Entity.getKnownSpeed
	motion struct {
		last, speed [3]float64
		set         bool
	}
	walk struct {
		dirX, dirZ, speed float64
	}
	// riderWalk is the strafe a rider's bow goal asks of this mount: the rider
	// ticks apart from the mount, so the request waits here for the mount's
	// move.
	// vanilla: MoveControl.Operation.STRAFE
	riderWalk struct {
		dirX, dirZ, speed float64
		set               bool
	}
	jump bool
	// stuck is the slow factor (cobweb, bush, snow) the last move ended in,
	// applied to the next.
	stuck      stuckFactor
	justLanded bool
	// bounced is the last move's landing bounce off a block; the physics step
	// turns it into a MobBounced.
	bounced bool
	// bouncedOff is the last move reversing a velocity off anything; the
	// physics step turns it into a MobBouncedOff.
	bouncedOff bool
	// flap is Entity.flyDist and the flapping kinds' nextFlap.
	flap flapState
	// stride is Entity.moveDist and nextStep.
	stride stepState
	// crystal is the unsaved amethyst chime intensity and last body tick.
	crystal rules.CrystalStep

	// waterFluid and lavaFluid are EntityFluidInteraction, refilled at the top
	// of the tick so goals and movement see the water as the tick began.
	// canFloat is Navigation.canFloat, raised by FloatGoal's constructor; it
	// tells the water's own kick from a non-floating mob's leap. noJumpDelay is
	// vanilla's ten-tick pause between ground jumps.
	waterFluid  fluidTracker
	lavaFluid   fluidTracker
	canFloat    bool
	noJumpDelay int

	// ghastBaby records which of the happy ghast's two minds is wired, swapped
	// at ageBoundaryReached. Meaningful only on that kind.
	ghastBaby bool

	// leash is the holder resolved for this tick, nil while unleashed or when
	// the holder is not in the tick's list. leashStep reads and clears it.
	leash *LeashHolder

	// leashDelayed marks a lead that travelled dimensions with its mob and has
	// no holder there yet; leashDelayedTicks counts the wait, and the lead
	// drops after the leash's grace.
	// vanilla: Leashable.restoreLeashFromSave
	leashDelayed      bool
	leashDelayedTicks int

	// LandedFall is the fall distance of the tick's landing, for the server's
	// farmland trample, which clears it after reading. 0 when nothing landed.
	LandedFall float64

	// horizontalCollision is Entity.horizontalCollision: the last move was
	// stopped sideways. The spider's climbing reads it.
	horizontalCollision bool

	// swell is Creeper.swell, wound by SwellDir each tick; it explodes at the
	// max.
	swell int

	// ambientTimer is ambientSoundTime, the idle sound's countdown.
	ambientTimer int

	// kin is the mob's kinds entry, resolved by NewMob so per-tick reads skip
	// the map. A zero Mob built without NewMob has none, and kind() falls back
	// to the table.
	kin *kind

	// push is the tick's spatial grid for pushes and patrol companions; pushCell is the
	// mob's cell and pushIdx its place in the snapshot, by which candidates are
	// reordered so pushes land in the tick's walk order. Set for one tick and
	// cleared after.
	push *pushGrid
	// tickPaths belongs to the herd and is lent to this body for its tick.
	tickPaths *MobPathScratch
	// prey is the tick's villagers, traders, iron golems, turtles and axolotls
	// in herd order, which the zombie's mob targets scan instead of the herd;
	// preyKnown is false where no tick built it. Set for one tick.
	prey      []*Mob
	preyKnown bool
	// stands is the tick's visible armor stands of the world, which a goat's
	// ram may target. Set for one tick.
	stands   []StandTarget
	pushCell pushCellKey
	pushIdx  int
	// pushSlot is the mob's index in its cell's entries.
	pushSlot int

	// BlowResisted and BlowAbsorbed are what Resistance and absorption took off
	// a player's last blow, for the attacker's statistics.
	BlowResisted, BlowAbsorbed float64

	metadataRevision uint64
	metadataDirty    MobMetadataMask
	metadataExternal mobMetadataExternal
	memoryMapOrder   *MemoryMapOrder
}

// KindDims reports a type's collision box width and height. A type with no
// entry is an upstream bug.
func KindDims(t MobType) (width, height float64) {
	k := lookupKind(t)
	if k == nil {
		panic("entity: unknown mob type " + t.Name())
	}
	return k.width, k.height
}

// NewMob creates a live mob of the type at the position, with its entity ID
// and the seed for every choice it makes. A sheep starts white until
// FinalizeSpawn colours it. Hostile kinds get the hostile goals; water kinds
// get none, since Squid's only goals run as the squid step.
func NewMob(t MobType, id int32, x, y, z float64, seed int64) *Mob {
	resolveKinds()
	k := lookupKind(t)
	if k == nil {
		panic("entity: unknown mob type " + t.Name())
	}
	rng := NewDice(seed)
	m := &Mob{
		ID:        id,
		Type:      t,
		kin:       k,
		X:         x,
		Y:         y,
		Z:         z,
		MaxHealth: k.maxHealth,
		Health:    k.maxHealth,
		AirSupply: rules.MaxAirSupply,
		Rand:      rng,
		lootSeed:  lootSeed(seed),
	}
	m.ResetDropChances()
	m.anger.neverAngered = startsNeverAngered(t)
	if t == TypeWarden {
		m.warden.digCooldown, m.warden.vibrationOfferedAge = wardenDigCooldown, -1
		m.ai.wire(wardenGoals(m)...)
	} else if golemKind(t) {
		m.ai.wire(golemGoals(m)...)
	} else if t == TypeEnderDragon {
		m.dragon = newDragonState()
	} else if t == TypePhantom {
		// The phantom runs its own goals, in phantomMoveStep.
	} else if m.IsShulker() {
		m.ai.wire(shulkerGoals(m)...)
		m.snapShulker()
	} else if t == TypeBreeze {
		// BreezeAi is stepped directly, without goal scheduling.
	} else if m.IsBlaze() {
		m.ai.wire(blazeGoals(m)...)
	} else if m.IsGhast() {
		m.ai.wire(ghastGoals(m)...)
	} else if m.IsWither() {
		m.ai.wire(witherGoals(m)...)
	} else if m.IsCreaking() {
		m.ai.wire(creakingGoals(m)...)
	} else if m.IsGuardian() {
		m.ai.wire(guardianGoals(m)...)
		// vanilla: ElderGuardian's constructor
		m.Persistence = m.Type == TypeElderGuardian
	} else if t == TypeVindicator {
		m.ai.wire(vindicatorGoals(m)...)
	} else if t == TypeHoglin {
		m.ai.wire(hoglinGoals(m)...)
	} else if t == TypeZoglin {
		m.ai.wire(zoglinGoals(m)...)
	} else if t == TypePillager {
		m.ai.wire(pillagerGoals(m)...)
	} else if t == TypeEvoker {
		m.ai.wire(evokerGoals(m)...)
	} else if t == TypeIllusioner {
		m.ai.wire(illusionerGoals(m)...)
	} else if t == TypeRavager {
		m.ai.wire(ravagerGoals(m)...)
	} else if t == TypeVex {
		m.ai.wire(vexGoals(m)...)
	} else if k.hostile && t != TypeZombieHorse && t != TypeZombieNautilus && t != TypeCamelHusk && t != TypeGiant {
		m.ai.wire(hostileGoals(m)...)
	} else if t == TypeBee {
		m.ai.wire(beeGoals(m)...)
	} else if t == TypeAllay {
		m.ai.wire(allayGoals(m)...)
	} else if k.flying && t != TypeParrot {
		m.ai.wire(flyingGoals(m)...)
	} else if t == TypeTurtle {
		m.ai.wire(turtleGoals(m)...)
	} else if t == TypeWolf {
		m.ai.wire(wolfGoals(m)...)
	} else if t == TypeCat {
		m.ai.wire(catGoals(m)...)
	} else if t == TypeOcelot {
		m.ai.wire(ocelotGoals(m)...)
	} else if t == TypeParrot {
		// Standalone simulations have a separate stream until a level binds its own.
		m.SetParrotSoundRandom(NewDice(seed ^ spawnDiceSalt))
		m.ai.wire(parrotGoals(m)...)
	} else if IsEquine(t) {
		m.ai.wire(horseGoals(m)...)
	} else if t == TypeStrider {
		m.ai.wire(striderGoals(m)...)
		m.Horse.NeedsStick = true
	} else if t == TypePig {
		m.Horse.NeedsStick = true
		m.ai.wire(passiveGoals(m)...)
	} else if t == TypeVillager {
		// vanilla: the Villager constructor; a load reads CanPickUpLoot over it
		m.CanPickUpLoot = true
		m.ai.wire(villagerGoals(m)...)
	} else if t == TypeWanderingTrader {
		m.ai.wire(traderGoals(m)...)
	} else if t == TypeRabbit {
		m.ai.wire(rabbitGoals(m)...)
	} else if t == TypeSniffer {
		m.ai.everyTick = true
		m.ai.wire(snifferGoals(m)...)
	} else if t == TypeArmadillo {
		m.ScuteTimer = 6000 + m.Rand.Intn(6000)
		m.ai.wire(armadilloGoals(m)...)
	} else if t == TypeCopperGolem {
		m.Persistence = true
		m.copper.nextWeathering = copperNextUnset
		// vanilla: the CopperGolem constructor; a load with a Brain replaces the
		// brain, and the unsaved memory with it (copperGolemFields)
		m.copper.cooldown = 60 + m.Rand.Intn(40)
		m.ai.wire(copperGolemGoals(m)...)
	} else if t == TypePanda {
		// vanilla: the Panda constructor, before any age is set
		m.CanPickUpLoot = true
		m.ai.wire(pandaGoals(m)...)
	} else if t == TypePolarBear {
		m.ai.wire(polarBearGoals(m)...)
	} else if t == TypeFox {
		m.CanPickUpLoot = true
		m.ai.wire(foxGoals(m)...)
	} else if t == TypeDolphin {
		m.CanPickUpLoot = true
		m.AirSupply = dolphinMaxAir
		m.dolphin.moistness = dolphinMaxMoistness
		m.ai.wire(dolphinGoals(m)...)
	} else if IsNautilus(t) {
		m.nautilus.attackCooldown = 2400 + m.Rand.Intn(1201)
		m.ai.wire(nautilusGoals(m)...)
	} else if t == TypeFrog {
		m.ai.everyTick = true
		m.frog.scan = m.Rand.Intn(20)
		m.frog.jump.cooldown = rollJumpGap(m, GoDice{m.Rand})
		tempt := &temptGoal{m: m}
		m.ai.wire(&panicGoal{m: m, pace: 2, brain: true}, &frogTongueGoal{m: m}, &breedGoal{m: m}, &frogLaySpawnGoal{m: m}, &goatJumpGoal{m: m, tempt: tempt}, tempt, &frogSwimGoal{m: m}, &frogCroakGoal{m: m}, &randomStroll{m: m, blocked: m.InWater}, &lookAtPlayer{m: m}, &randomLookAround{m: m})
	} else if t == TypeTadpole {
		m.FromBucket = true
		m.ai.everyTick = true
		m.ai.wire(tadpoleGoals(m)...)
	} else if t == TypeAxolotl {
		m.AirSupply = axolotlMaxAir
		m.ai.everyTick = true
		m.ai.wire(axolotlGoals(m)...)
	} else if t == TypePufferfish {
		m.initFish()
		m.ai.wire(pufferfishGoals(m)...)
	} else if FishKind(t) {
		m.initFish()
		m.ai.wire(fishGoals(m)...)
	} else if t == TypeGoat {
		m.ai.wire(goatGoals(m)...)
		m.initGoatMemories()
	} else if !k.water && !k.ambient {
		m.ai.wire(passiveGoals(m)...)
	}
	if t == TypeTraderLlama {
		// vanilla: TraderLlama.despawnDelay
		m.DespawnDelay = traderLlamaDespawn
	}
	if t == TypeTurtle {
		m.Home = m.cell()
	}
	if m.is(TypeSquid) {
		// vanilla: Squid's constructor's own dice
		m.squid.pace = squidSwimArm / (1 + rng.Float64())
	}
	if t == TypeBat {
		// vanilla: Bat's constructor
		m.Resting = true
	}
	if t == TypeChicken {
		m.EggTimer = eggIntervalMin + rng.Intn(eggIntervalMax-eggIntervalMin+1)
	}
	if t == TypeSheep {
		m.Colour = "white"
	}
	if t == TypeMooshroom {
		// vanilla: MushroomCow.Variant.DEFAULT
		m.Mushroom = "red"
	}
	if _, _, ok := VariantRegistries(t); ok {
		m.Variant, m.SoundVariant = DefaultVariants(t)
	}
	if t == TypeWolf || t == TypeCat {
		m.Collar = WolfDefaultCollar
	}
	if IsEquine(t) {
		m.Horse.Speed, m.Horse.Jump = k.speed, horseBaseJump
		if IsCamel(t) {
			// vanilla: Camel.isTamed is always true
			m.Horse.Tame, m.Horse.Jump = true, camelJump
		}
		if m.Chested() {
			m.Horse.Jump = chestedBaseJump
		}
		if IsLlama(t) {
			m.Horse.Strength = 1
		}
	}
	if t == TypeCreeper {
		m.SwellDir = -1
		m.MaxSwell = creeperMaxSwell
		m.ExplosionRadius = creeperExplosionRadius
	}
	if t == TypeZombieVillager {
		m.Villager = newZombieVillagerData(rng)
	}
	if t == TypeVillager {
		m.Villager = VillagerData{Type: "minecraft:plains", Profession: "minecraft:none", Level: 1}
	}
	if k.cube {
		size := 1
		if t == TypeSulfurCube {
			size = 2
		}
		m.SetSize(size, true)
	}
	return m
}

// SetLoveCause records the player whose feeding caused love.
func (m *Mob) SetLoveCause(id uuid.UUID) { m.LoveCause, m.HasLoveCause = id, true }

// RecordSlot is the server's unsaved record index, or zero when unfiled.
// The server's mob lock guards it.
func (m *Mob) RecordSlot() int { return int(m.recordSlot) }

// SetRecordSlot sets the server's record index under its mob lock.
func (m *Mob) SetRecordSlot(slot int) { m.recordSlot = int32(slot) }

// zeroKind is the entry for a type the kinds table does not name.
var zeroKind kind

// lookupKind returns nil for an unmodelled or out-of-range type.
func lookupKind(t MobType) *kind {
	if uint32(t) < uint32(len(kinds)) {
		return kinds[t]
	}
	return nil
}

// kindOf is the kinds entry for a type, or zeroKind.
func kindOf(t MobType) *kind {
	if k := lookupKind(t); k != nil {
		return k
	}
	return &zeroKind
}

// kind is the mob's kinds entry: NewMob's resolved pointer, else the
// table's entry for the type, else zeroKind.
func (m *Mob) kind() *kind {
	if m.kin != nil {
		return m.kin
	}
	return kindOf(m.Type)
}

// BabyAgeStart is AgeableMob.BABY_START_AGE, -24000 ticks (twenty
// minutes). A zombie's babyhood is a metadata boolean with no growing, but
// it is still born at this age.
const BabyAgeStart = -24000

// SetBaby makes the body a baby (age BabyAgeStart) or grown (age 0).
// vanilla: AgeableMob.setAge
func (m *Mob) SetBaby(baby bool) {
	wasBaby := m.IsBaby()
	if baby {
		m.SetMetadataBabyAge(BabyAgeStart)
		if m.Type == TypeSniffer {
			m.SetMetadataBabyAge(snifferBabyAge) // vanilla: Sniffer.getBabyStartAge
		}
	} else {
		m.SetMetadataBabyAge(0)
	}
	if wasBaby != m.IsBaby() {
		m.syncSulfurSize()
	}
	m.syncFlyingGoals()
}

// syncFlyingGoals wires the happy ghast's goal set for its age when it
// crosses the age line: flyingBabyGoals for a ghastling, flyingGoals grown.
// vanilla: HappyGhast.ageBoundaryReached, Mob.removeAllGoals
func (m *Mob) syncFlyingGoals() {
	if !m.is(TypeHappyGhast) {
		return
	}
	baby := m.IsBaby()
	if m.ghastBaby == baby {
		return
	}
	m.ghastBaby = baby
	m.ai.clear()
	if baby {
		m.ai.wire(flyingBabyGoals(m)...)
	} else {
		m.ai.wire(flyingGoals(m)...)
	}
}

// IsBaby is AgeableMob.isBaby: age below 0, and always false for kinds that
// are never babies.
func (m *Mob) IsBaby() bool { return m.BabyAge < 0 && m.kind().babyable }

// SetAge sets the age; crossing the baby line redraws the box and moves the
// body clear of what the new size overlaps. A loaded body starts grown, so
// an age read back only shrinks the box and the body stays put.
// vanilla: AgeableMob.setAge, Entity.refreshDimensions
func (m *Mob) SetAge(w World, age int) {
	wasBaby := m.IsBaby()
	oldWidth, oldHeight := m.width(), m.height()
	m.SetMetadataBabyAge(age)
	if wasBaby != m.IsBaby() {
		m.syncSulfurSize()
	}
	m.refreshDimensions(w, oldWidth, oldHeight)
	m.syncFlyingGoals()
}

// refreshDimensions moves a body whose box just grew to the nearest clear
// spot, so a baby that outgrows a fence gap is not left overlapping it
// (the next move would carry it through). Boxes that did not grow, or are
// over four blocks a side, are left alone.
// vanilla: Entity.refreshDimensions, Entity.fudgePositionAfterSizeChange
func (m *Mob) refreshDimensions(w World, oldWidth, oldHeight float64) {
	newWidth, newHeight := m.width(), m.height()
	if newWidth > 4 || newHeight > 4 {
		return
	}
	if newWidth <= oldWidth && newHeight <= oldHeight {
		return
	}
	m.fudgePositionAfterSizeChange(w, oldWidth, oldHeight, newWidth, newHeight)
}

// fudgePositionAfterSizeChange looks within half the growth either way for
// the clear centre nearest the old one. If that fails and both width and
// height grew, it retries ignoring the vertical growth.
// vanilla: Entity.fudgePositionAfterSizeChange
func (m *Mob) fudgePositionAfterSizeChange(w World, oldWidth, oldHeight, newWidth, newHeight float64) {
	oldCenter := [3]float64{m.X, m.Y + oldHeight/2, m.Z}
	widthDelta := max(0, newWidth-oldWidth) + 1e-6
	heightDelta := max(0, newHeight-oldHeight) + 1e-6
	allowed := boxOfSize(oldCenter, widthDelta, heightDelta, widthDelta)
	if c, ok := findFreePosition(w, allowed, oldCenter, newWidth, newHeight, newWidth); ok {
		m.SetPos(c[0], c[1]-newHeight/2, c[2])
		return
	}
	if newWidth > oldWidth && newHeight > oldHeight {
		allowed = boxOfSize(oldCenter, widthDelta, 1e-6, widthDelta)
		if c, ok := findFreePosition(w, allowed, oldCenter, newWidth, oldHeight, newWidth); ok {
			m.SetPos(c[0], c[1]-oldHeight/2+1e-6, c[2])
		}
	}
}

// loveTicks is Animal.setInLove's 600: thirty seconds to find a mate.
const loveTicks = 600

// breedCooldownTicks is Animal's PARENT_AGE_AFTER_BREEDING, the five minutes
// both parents wait before they may love again.
const breedCooldownTicks = 6000

// IsInLove is Animal.isInLove.
func (m *Mob) IsInLove() bool { return m.InLove > 0 }

// Feed applies a right-click with the kind's food: a grown body with no
// love or cooldown falls in love, and a baby grows a tenth of the age it
// still owes. Otherwise nothing happens and the caller keeps the food. loved
// is the caller's cue for the hearts.
// vanilla: Animal.mobInteract, AgeableMob.getSpeedUpSecondsWhenFeeding
func (m *Mob) Feed() (loved, grew bool) {
	loved, grew = m.feed()
	if m.Type == TypeHoglin && (loved || grew) {
		m.Persistence = true // vanilla: Hoglin.mobInteract
	}
	return loved, grew
}

func (m *Mob) feed() (loved, grew bool) {
	if m.ArmadilloScared() {
		return false, false
	}
	if m.Type == TypePanda {
		// vanilla: Panda.mobInteract
		if m.panda.scared {
			return false, false
		}
		m.panda.gotBamboo = m.panda.gotBamboo || m.TargetID != 0
	}
	if m.Type == TypeTadpole {
		if m.AgeLocked {
			return false, false
		}
		m.tadpole.age += int(float64(max(0, tadpoleGrowTicks-m.tadpole.age)/20)*0.1) * 20
		return false, true
	}
	if m.Type == TypeSulfurCube {
		if !m.IsBaby() || m.AgeLocked {
			return false, false
		}
		m.forceAgeUp()
		return false, true
	}
	if !ageableAnimal(m.Type) {
		return false, false
	}
	switch {
	case m.BabyAge == 0 && m.InLove <= 0 && !m.HasEgg && m.hoglin.pacified == 0 && m.Type != TypeHappyGhast:
		// vanilla: Turtle.canFallInLove, Hoglin.canFallInLove, HappyGhast.canFallInLove
		m.InLove = loveTicks
		return true, false
	case m.IsBaby() && !m.AgeLocked:
		// vanilla: ageUp(getSpeedUpSecondsWhenFeeding(-age), true); a tenth of
		// the owed seconds never reaches grown.
		m.forceAgeUp()
		return false, true
	}
	return false, false
}

// forceAgeUp grows a fed baby by a tenth of the seconds it still owes, adds
// the growth to forcedAge and starts the sparkles. A tenth never reaches
// grown, so vanilla's setAge(forcedAge) on reaching 0 happens only in ageUp.
// vanilla: AgeableMob.ageUp(getSpeedUpSecondsWhenFeeding(-age), true)
func (m *Mob) forceAgeUp() {
	delta := int(float64(-m.BabyAge/20)*0.1) * 20
	m.SetMetadataBabyAge(m.BabyAge + delta)
	m.forcedAge += delta
	if m.forcedAgeTimer == 0 {
		m.forcedAgeTimer = 40
	}
}

// forcedAgeStep counts the sparkles down, one on every fourth tick.
// vanilla: AgeableMob.aiStep
func (m *Mob) forcedAgeStep(out []MobEvent) []MobEvent {
	if m.forcedAgeTimer <= 0 {
		return out
	}
	if m.forcedAgeTimer%4 == 0 {
		w := m.width()
		x := m.X + (2*m.Rand.Float64()-1)*w
		y := m.Y + m.height()*m.Rand.Float64() + 0.5
		z := m.Z + (2*m.Rand.Float64()-1)*w
		out = append(out, MobEvent{Mob: m, Kind: MobSparkled, Status: protocol.ParticleHappyVillager, X: x, Y: y, Z: z})
	}
	m.forcedAgeTimer--
	return out
}

// ageLockStep counts the golden dandelion cooldown down, with a pause (locked)
// or reset icon above the head on every even tick.
// vanilla: AgeableMob.makeAgeLockedParticle
func (m *Mob) ageLockStep(out []MobEvent) []MobEvent {
	if m.ageLockCooldown <= 0 {
		return out
	}
	if m.ageLockCooldown%2 == 0 {
		particle, lift := int32(protocol.ParticleResetMobGrowth), 0.0
		if m.AgeLocked {
			particle, lift = protocol.ParticlePauseMobGrowth, 0.2
		}
		w, h := m.width(), m.height()
		x := m.X + (2*m.Rand.Float64()-1)*w
		y := m.Y + h*(2*m.Rand.Float64()-1)*0.2 + h + lift
		z := m.Z + (2*m.Rand.Float64()-1)*w
		out = append(out, MobEvent{Mob: m, Kind: MobSparkled, Status: particle, X: x, Y: y, Z: z})
	}
	m.ageLockCooldown--
	return out
}

// BredWith ends both parents' love and starts their cooldown; the child,
// hearts and experience are the world's.
// vanilla: Animal.finalizeSpawnChildFromBreeding
func (m *Mob) BredWith(partner *Mob) {
	m.InLove, partner.InLove = 0, 0
	m.SetMetadataBabyAge(breedCooldownTicks)
	partner.SetMetadataBabyAge(breedCooldownTicks)
}

// Dye recolours a live, unsheared sheep's wool and reports whether it took;
// anything else, or the colour it already is, keeps the dye.
// vanilla: DyeItem.interactLivingEntity
func (m *Mob) Dye(colour string) bool {
	if m.Type != TypeSheep || !m.Alive() || m.Sheared || m.Colour == colour {
		return false
	}
	m.SetMetadataColour(colour)
	return true
}

// TemptedBy reports whether the stack is one of the kind's foods.
// vanilla: Animal.isFood
func (m *Mob) TemptedBy(stack protocol.ItemStack) bool { return m.tempts(stack) }

// FedBy reports whether right-clicking the mob with the stack feeds it: a food
// of the kind, except the flowers a bee takes an effect from instead of love.
// vanilla: Bee.mobInteract, FlowerBlock.getBeeInteractionEffect
func (m *Mob) FedBy(stack protocol.ItemStack) bool {
	if m.Type == TypePig {
		return !stack.Empty() && registry.Item(stack.ItemID).InTag(registry.ItemTagIDPigFood)
	}
	if m.Type == TypeSulfurCube {
		return m.IsBaby() && !stack.Empty() && registry.Item(stack.ItemID).InTag(registry.ItemTagIDSulfurCubeFood)
	}
	if m.Type == TypeBee {
		if _, ok := registry.Item(stack.ItemID).BeeInteractionEffect(); ok {
			return false
		}
	}
	return m.tempts(stack)
}

// FinalizeSpawn is Mob.finalizeSpawn for a mob stood up from nothing (a
// natural spawn or an untagged summon). dice is the level's random: the
// WorldGenRegion's during chunk generation, SpawnDice elsewhere. The
// sheep's colour comes from the birth biome (SheepColorSpawnRules), babies
// are decided by the pack's SpawnGroupData or alone, and equipment rolls
// read the local difficulty.
func (m *Mob) FinalizeSpawn(dice Dice, biome registry.Biome, group *SpawnGroup, local rules.LocalDifficulty) {
	m.finalizeSpawn(dice, rules.SpawnContext{Biome: biome, MoonBrightness: local.Moon}, group, local, false)
}

// FinalizeSpawnAt is FinalizeSpawn with the full spawn context (including
// structure membership at the spawn block).
func (m *Mob) FinalizeSpawnAt(dice Dice, ctx rules.SpawnContext, group *SpawnGroup, local rules.LocalDifficulty) {
	m.finalizeSpawn(dice, ctx, group, local, false)
}

// FinalizeStructureSpawn finalizes a template mob with the STRUCTURE spawn
// reason on dice, the WorldGenRegion's random at generation. blackCat is the
// swamp hut's cat. vanilla: CatVariants, StructureCheck on #cats_spawn_as_black
func (m *Mob) FinalizeStructureSpawn(dice Dice, biome registry.Biome, blackCat bool, local rules.LocalDifficulty) {
	ctx := rules.SpawnContext{Biome: biome, MoonBrightness: local.Moon}
	if blackCat {
		ctx.InStructure = func(id string) bool { return id == "minecraft:swamp_hut" }
	}
	m.finalizeSpawn(dice, ctx, nil, local, true)
}

// FinalizeStructureSpawnAt is FinalizeStructureSpawn with the full spawn context.
func (m *Mob) FinalizeStructureSpawnAt(dice Dice, ctx rules.SpawnContext, local rules.LocalDifficulty) {
	m.finalizeSpawn(dice, ctx, nil, local, true)
}

// SpawnDice stands in for the level's random a finalize outside chunk
// generation rolls: vanilla's is not the mob's own, so it is a stream salted
// off the loot seed that moves neither the mob's dice nor its loot.
func (m *Mob) SpawnDice() Dice {
	return GoDice{NewDice(m.lootSeed ^ spawnDiceSalt)}
}

// spawnDiceSalt gives SpawnDice its own seed.
const spawnDiceSalt = 0x2545F4914F6CDD1D

// finalizeSpawn runs each kind's own finalize, then AgeableMob's, then
// Mob's. The overworld creatures, the zombie family and the vindicator draw
// from dice; the other kinds keep their own streams.
func (m *Mob) finalizeSpawn(dice Dice, ctx rules.SpawnContext, group *SpawnGroup, local rules.LocalDifficulty, structure bool) {
	biome := ctx.Biome
	if !structure && isRaiderType(m.Type) {
		// vanilla: PatrollingMonster.finalizeSpawn, canBeLeader; the float is
		// drawn for the witch and ravager too
		if dice.NextFloat() < patrolLeaderOdds && IsIllagerType(m.Type) {
			m.PatrolLeader = true
		}
	}
	if m.Type == TypeCat {
		// vanilla: Cat.finalizeSpawn runs super first, so the variant draws
		// after Mob's; deferred before finalizeMob, it runs after it. Only a
		// structure cat rolls on dice; others keep their own stream.
		catDice := Dice(GoDice{m.Rand})
		if structure {
			catDice = dice
		}
		defer m.finalizeCat(catDice, ctx)
	}
	if m.Type == TypeVindicator {
		// vanilla: Vindicator.finalizeSpawn rolls after Mob's, on the same dice
		m.finalizeMob(dice)
	} else if m.is(TypeZombie) {
		// vanilla: Zombie.finalizeSpawn rolls after Mob's, on the same dice
		m.finalizeMob(dice)
		m.finalizeZombie(dice, group, local)
		if m.Type == TypeHusk {
			m.finalizeHusk(dice, group, local)
		}
		if m.Type == TypeDrowned {
			defer m.finalizeDrowned(dice, biome, group, structure)
		}
	} else {
		defer m.finalizeMob(dice)
	}
	if m.Type == TypeAxolotl {
		m.finalizeAxolotl(group)
		return
	}
	if m.Type == TypeParrot {
		// vanilla: Parrot.finalizeSpawn
		m.SetParrotVariant(int(dice.NextIntN(parrotVariants)))
	}
	if m.IsFish() {
		m.finalizeFish(group)
		return
	}
	m.rollJockey(group)
	if m.Type == TypeStrider {
		m.finalizeStrider(dice, ctx, local)
	}
	m.finalizeSpiderEffect(group, local)
	if m.Type == TypePhantom {
		m.finalizePhantom()
	}
	if m.Type == TypeSheep {
		m.SetMetadataColour(rules.SheepSpawnColour(biome, dice))
	}
	if IsEquine(m.Type) {
		m.finalizeHorse(dice, group)
	}
	if m.Type == TypeWitherSkeleton {
		// vanilla: WitherSkeleton.populateDefaultEquipmentSlots (no armour roll)
		m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemStoneSword), Count: 1})
	}
	if m.Type == TypeVindicator {
		m.finalizeVindicator(dice, local)
	}
	if m.hoglinKind() {
		// vanilla: Hoglin.finalizeSpawn, Zoglin.finalizeSpawn roll with or without
		// a group; only a structure's rolls on dice
		roll := m.Rand.Float32
		if structure {
			roll = dice.NextFloat
		}
		if roll() < hoglinBabyOdds {
			m.SetBaby(true)
		}
	}
	if m.Type == TypePillager {
		m.finalizePillager(local)
	}
	if m.Type == TypeVex {
		m.finalizeVex(local)
	}
	if m.Type == TypeRabbit {
		m.finalizeRabbit(dice, biome, group)
	}
	if m.Type == TypeGoat {
		m.finalizeGoat(dice)
	}
	if m.Type == TypePanda {
		m.finalizePanda(dice)
	}
	if m.Type == TypeFox {
		m.finalizeFox(dice, biome, group)
	}
	if m.Type == TypeIllusioner {
		// vanilla: Illusioner.finalizeSpawn
		m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemBow), Count: 1})
	}
	if m.kind().bornWithBow {
		// vanilla: AbstractSkeleton.populateDefaultEquipmentSlots; armour before
		// the bow
		dice := m.equipmentDice()
		m.rollSpawnArmour(dice, local)
		m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemBow), Count: 1})
		m.rollSpawnEnchantments(dice, local)
	}
	if m.is(TypeSkeleton) {
		m.rollHalloweenHead(m.halloweenDice(), local)
	}
	// vanilla: Cow, Pig and Chicken.finalizeSpawn, the variant before the
	// sound variant.
	if m.Type == TypeFrog {
		m.finalizeFrog(dice, ctx)
	} else if IsNautilus(m.Type) {
		if m.Type == TypeZombieNautilus {
			if v, ok := rules.SelectVariantToSpawn("minecraft:zombie_nautilus_variant", ctx, dice); ok {
				m.Variant = v
			}
		}
		// vanilla: AbstractNautilus.finalizeSpawn, NautilusAi.initMemories (2400..3600)
		m.nautilus.attackCooldown = 2400 + int(dice.NextIntN(1201))
	} else if variants, sounds, ok := VariantRegistries(m.Type); ok && m.Type != TypeCat { // the cat's is deferred above
		if m.Type == TypeWolf {
			m.finalizeWolfVariant(dice, ctx, group)
		} else if v, ok := rules.SelectVariantToSpawn(variants, ctx, dice); ok {
			m.Variant = v
		}
		m.SoundVariant = rules.RandomRegistryEntry(sounds, dice)
	}
	if (m.Type == TypeZombieVillager || m.Type == TypeVillager) && !m.Villager.Finalized {
		// vanilla: VillagerDataHolder.finalizeVillagerType
		m.SetMetadataVillagerType(VillagerTypeByBiome(biome))
		m.SetMetadataVillagerFinalized(true)
	}
	// vanilla: AbstractSkeleton.finalizeSpawn rolls loot pickup scaled by the
	// special multiplier.
	if m.is(TypeSkeleton) {
		m.CanPickUpLoot = NewDice(m.lootSeed^lootPickupSalt).Float64() < lootPickupOdds*float64(local.Special)
	}
	if m.isPiglin() {
		m.finalizePiglin(dice, local, structure)
	}
	if group == nil {
		// A vanilla summon passes a null group; an animal is grown.
		return
	}
	if m.hoglinKind() {
		return
	}
	// AgeableMob.finalizeSpawn: shouldSpawnBaby keeps the first member grown and
	// rolls 5 in 100 for each later one. A group a zombie claimed is refused,
	// and never-baby kinds roll nothing. A strider swaps in its own data with
	// size 0.
	// vanilla: Strider.finalizeSpawn
	if !ageableAnimal(m.Type) || m.Type == TypeStrider || (group.claimed && (group.zombie || group.spider)) {
		return
	}
	group.claimed = true
	odds := float32(babyOdds)
	if group.rabbitSet || m.Type == TypeOcelot {
		odds = rabbitPackBabyOdds
	}
	if IsEquine(m.Type) && !IsLlama(m.Type) && m.Type != TypeHorse {
		// vanilla: Horse's HorseGroupData and Llama's LlamaGroupData come
		// first, so AbstractHorse's 0.2 never applies to them
		odds = equinePackBabyOdds
	}
	if m.Type == TypeDolphin {
		odds = 0.1 // vanilla: Dolphin.finalizeSpawn
	}
	if m.Type == TypePanda {
		odds = equinePackBabyOdds // vanilla: Panda.finalizeSpawn's AgeableMobGroupData(0.2f)
	}
	if m.Type == TypePolarBear {
		// vanilla: PolarBear.finalizeSpawn's AgeableMobGroupData(1.0f)
		odds = 1
	}
	if m.Type == TypeFox {
		// vanilla: Fox.finalizeSpawn, a pack's third member on is a kit
		if group.size >= 2 {
			m.SetBaby(true)
		}
	} else if m.Type != TypeWolf && group.size > 0 && group.rng.NextFloat() <= odds {
		// vanilla: Wolf.WolfPackData's super(false) rolls no wolf baby
		m.SetBaby(true)
	}
	group.size++
}

// equinePackBabyOdds is the 0.2f of AbstractHorse.finalizeSpawn's
// AgeableMobGroupData.
const equinePackBabyOdds = 0.2

// babyOdds is AgeableMobGroupData's 0.05, the same odds as ZombieGroupData's
// getSpawnAsBabyOdds.
const babyOdds = 0.05

// rabbitPackBabyOdds makes every rabbit after the pack's first a baby.
// vanilla: Rabbit.RabbitGroupData's super(1.0f)
const rabbitPackBabyOdds = 1.0

// leftHandedOdds is Mob.finalizeSpawn's 0.05.
const leftHandedOdds = 0.05

// followSpawnSpread is the spread of Mob.finalizeSpawn's triangle roll of
// the follow_range bonus.
const followSpawnSpread = 0.11485000000000001

// finalizeMob is Mob.finalizeSpawn, the last step of every kind's: the
// follow_range bonus (rolled only while the mob has none), then left-handedness.
// vanilla: Mob.finalizeSpawn, RandomSource.triangle
func (m *Mob) finalizeMob(dice Dice) {
	if m.followSpawnBonus == 0 {
		m.followSpawnBonus = followSpawnSpread * (dice.NextDouble() - dice.NextDouble())
		if m.attrs != nil {
			m.syncFollowSpawnBonus(m.attrs)
		}
	}
	m.SetMetadataLeftHanded(dice.NextFloat() < leftHandedOdds)
}

// finalizeZombie is Zombie.finalizeSpawn after Mob's, every draw on dice in
// vanilla's order. A group an animal claimed first is refused (vanilla's
// instanceof check): only the Halloween head and the attributes remain.
// handleAttributes stays on the mob's own stream, as vanilla rolls it on
// this.random.
// vanilla: Zombie.finalizeSpawn
func (m *Mob) finalizeZombie(dice Dice, group *SpawnGroup, local rules.LocalDifficulty) {
	m.CanPickUpLoot = dice.NextFloat() < lootPickupOdds*local.Special
	if group != nil && group.claimed && !group.zombie {
		m.rollHalloweenHead(dice, local)
		m.handleAttributes(local.Special, false)
		return
	}
	baby := false
	switch {
	case group == nil:
		baby = dice.NextFloat() < babyOdds
	case !group.claimed:
		group.claimed, group.zombie = true, true
		group.baby = group.rng.NextFloat() < babyOdds
		baby = group.baby
	default:
		baby = group.baby
	}
	if baby {
		m.SetBaby(true)
	}
	if group == nil || !group.noChicken {
		m.rollChickenJockey(dice)
	}
	m.rollBreakDoors(dice, local.Special)
	m.populateEquipment(dice, local)
	m.rollHalloweenHead(dice, local)
	m.handleAttributes(local.Special, false)
}

// lootPickupOdds is the 0.55 loot-pickup chance, scaled by the special
// multiplier.
// vanilla: Zombie.finalizeSpawn, AbstractSkeleton.finalizeSpawn
const lootPickupOdds = 0.55

// lootPickupSalt gives the loot pickup roll its own seed.
const lootPickupSalt = 0x6A09E667F3BCC909

// SpawnGroup is SpawnGroupData for one natural spawn pack: the shared dice
// and which sort of mob claimed the group first. A zombie pack decides its
// babies once at the first finalize; an animal pack keeps the first member
// grown and rolls 5 in 100 for each later one.
type SpawnGroup struct {
	axolotlVariants [2]int
	axolotlCount    int
	axolotlSet      bool
	// rng is the level's random the pack's finalizes roll.
	rng Dice

	// school is the first fish of a pack, whom the rest follow.
	// vanilla: AbstractSchoolingFish.SchoolSpawnGroupData
	school                        *Mob
	tropicalVariant               int32
	tropicalSet, tropicalSolitary bool

	claimed, zombie bool

	// natural marks a pack from the natural spawn pass.
	// vanilla: EntitySpawnReason.NATURAL
	natural bool

	// Whether the one roll made the whole of a zombie pack baby.
	baby bool
	// noChicken keeps a baby zombie of the pack from rolling a chicken.
	// vanilla: ZombieGroupData.canSpawnJockey
	noChicken bool

	// A spider's pack: claimed by a spider, and the effect its roll gave every
	// member (spiderEffect only when hasEffect).
	spider, hasEffect bool
	spiderEffect      int32

	// A horse pack's one colour.
	// vanilla: Horse.HorseGroupData
	horseSet     bool
	llamaSet     bool
	llamaVariant int32
	horseColour  int32

	// A wolf pack's one variant.
	// vanilla: Wolf.WolfPackData
	wolfVariant string

	// A rabbit pack's one variant.
	// vanilla: Rabbit.RabbitGroupData
	rabbitSet  bool
	rabbitType int32

	// A fox pack's one variant.
	// vanilla: Fox.FoxGroupData
	foxSet     bool
	foxVariant int32

	// An animal pack's members finalized so far; the first is grown.
	size int
}

// NewSpawnGroup makes the pack data for one spawnCategoryForPosition call.
func NewSpawnGroup(rng Dice) *SpawnGroup { return &SpawnGroup{rng: rng} }

// NewNaturalSpawnGroup is NewSpawnGroup for the natural spawn pass.
func NewNaturalSpawnGroup(rng Dice) *SpawnGroup {
	return &SpawnGroup{rng: rng, natural: true}
}

// Natural reports whether this pack comes from the NATURAL spawn pass.
func (g *SpawnGroup) Natural() bool { return g != nil && g.natural }

// ageableAnimal reports the AgeableMob animals (farm animals, squids, happy
// ghast), whose packs roll babies per member after the first. The zombie's
// pack is decided by one roll instead.
func ageableAnimal(t MobType) bool {
	switch t {
	case TypeChicken, TypeCow, TypeMooshroom, TypePig, TypeSheep, TypeTurtle, TypeSquid, TypeGlowSquid, TypeDolphin, TypeHappyGhast, TypeCamel, TypeHorse, TypeDonkey, TypeMule, TypeLlama, TypeTraderLlama, TypeSkeletonHorse, TypeZombieHorse, TypeRabbit, TypeWolf,
		TypeArmadillo, TypeSniffer, TypeCat, TypeOcelot, TypePanda, TypePolarBear, TypeGoat, TypeFox, TypeHoglin, TypeStrider, TypeBee, TypeAxolotl, TypeNautilus, TypeZombieNautilus, TypeFrog:
		return true
	}
	return false
}

// IsAnimal reports vanilla's Animal class, which a boat seats 0.2 further
// along the hull. The zombie villager is an AgeableMob, not an Animal.
// vanilla: AbstractBoat.getPassengerAttachmentPoint's Animal
func IsAnimal(t MobType) bool {
	switch t {
	case TypeChicken, TypeCow, TypeMooshroom, TypePig, TypeSheep, TypeTurtle, TypeHappyGhast, TypeCamel, TypeCamelHusk, TypeHorse, TypeDonkey, TypeMule, TypeLlama, TypeTraderLlama, TypeSkeletonHorse, TypeZombieHorse, TypeRabbit,
		TypeArmadillo, TypeSniffer, TypeCat, TypeOcelot, TypePanda, TypePolarBear, TypeGoat, TypeHoglin, TypeStrider, TypeBee, TypeAxolotl, TypeNautilus, TypeZombieNautilus, TypeFrog, TypeParrot, TypeFox, TypeWolf:
		return true
	}
	return false
}

// VariantRegistries names the registries a kind's Variant and SoundVariant
// are entries of, or false for a kind without variants.
func VariantRegistries(t MobType) (variants, sounds string, ok bool) {
	switch t {
	case TypeZombieNautilus:
		return "minecraft:zombie_nautilus_variant", "", true
	case TypeFrog:
		return "minecraft:frog_variant", "", true
	case TypeCow:
		return "minecraft:cow_variant", "minecraft:cow_sound_variant", true
	case TypePig:
		return "minecraft:pig_variant", "minecraft:pig_sound_variant", true
	case TypeChicken:
		return "minecraft:chicken_variant", "minecraft:chicken_sound_variant", true
	case TypeWolf:
		return "minecraft:wolf_variant", "minecraft:wolf_sound_variant", true
	case TypeCat:
		return "minecraft:cat_variant", "minecraft:cat_sound_variant", true
	}
	return "", "", false
}

// DefaultVariants are the variants the synched data starts at, which the
// client assumes when the field is never sent.
// vanilla: Cow, Pig, Chicken, Wolf and Cat.defineSynchedData
func DefaultVariants(t MobType) (variant, sound string) {
	switch t {
	case TypeFrog, TypeZombieNautilus:
		return "minecraft:temperate", ""
	case TypeWolf:
		return "minecraft:pale", "minecraft:classic"
	case TypeCat:
		return "minecraft:black", "minecraft:classic"
	}
	return "minecraft:temperate", "minecraft:classic"
}

// Family is the kind whose machinery the type runs: the zombie for a husk,
// otherwise the type itself.
func Family(t MobType) MobType {
	if f := kindOf(t).family; f != NoMobType {
		return f
	}
	return t
}

// family is Family read off the resolved kind.
func (m *Mob) family() MobType {
	if f := m.kind().family; f != NoMobType {
		return f
	}
	return m.Type
}

// is reports whether the mob belongs to the family given.
func (m *Mob) is(family MobType) bool { return m.family() == family }

// MobName is the type's en_us display name, or false for a type this
// package does not simulate.
func MobName(t MobType) (string, bool) {
	k := lookupKind(t)
	if k == nil {
		return "", false
	}
	return k.name, true
}

// MobSoundsOf gives a kind's sound events.
func MobSoundsOf(t MobType) MobSounds { return kindOf(t).sounds }

// MobSoundVolume is the kind's getSoundVolume, LivingEntity's 1 by default.
func MobSoundVolume(t MobType) float32 {
	if v := kindOf(t).volume; v > 0 {
		return float32(v)
	}
	return 1.0
}

// Sounds is the mob's current set: the kind's, a farm animal's sound
// variant's, a pig or chick's baby set, a drowned's water set in water.
// vanilla: Mob.getSoundSet, Drowned.getAmbientSound
func (m *Mob) Sounds() MobSounds {
	s := m.kind().sounds
	if m.Type == TypeBreeze {
		if !m.OnGround {
			s.Ambient = registry.SoundEntityBreezeIdleAir
		} else if m.TargetID == 0 {
			s.Ambient = registry.SoundEntityBreezeIdleGround
		}
	}
	if IsNautilus(m.Type) {
		return MobSounds{Ambient: m.NautilusSound(NautilusAmbient), Hurt: m.NautilusSound(NautilusHurt), Death: m.NautilusSound(NautilusDeath), Eat: m.NautilusSound(NautilusEat)}
	}
	if m.Type == TypeDolphin && m.InWater() {
		s.Ambient = registry.SoundEntityDolphinAmbientWater
	}
	if m.Type == TypeAxolotl && m.InWater() {
		s.Ambient = registry.SoundEntityAxolotlIdleWater
	}
	if m.Type == TypeSniffer && (m.sniffer.state == SnifferDigging || m.sniffer.state == SnifferSearching) {
		s.Ambient = registry.SoundNone // vanilla: Sniffer.getAmbientSound
	}
	if m.Type == TypePanda {
		// vanilla: Panda.getAmbientSound
		switch m.pandaVariant() {
		case GeneAggressive:
			s.Ambient = registry.SoundEntityPandaAggressiveAmbient
		case GeneWorried:
			s.Ambient = registry.SoundEntityPandaWorriedAmbient
		}
	}
	if m.Type == TypeWarden {
		s.Ambient = m.wardenAngerSound()
		if m.wardenUnderground() || m.WardenPose() == protocol.PoseRoaring {
			s.Ambient = registry.SoundNone
		}
	}
	if m.Type == TypeCow || m.Type == TypePig || m.Type == TypeChicken {
		s = m.farmSounds(s)
	}
	if m.Type == TypeGoat && m.Goat.Screaming {
		s = screamingGoatSounds
	}
	if m.IsBaby() {
		if baby, ok := babySounds(m.Type); ok {
			s = baby
		}
	}
	if m.Type == TypeWolf {
		s = m.wolfSounds()
	}
	if m.Type == TypeCat {
		s = m.catSounds()
	}
	if m.Type == TypePanda {
		// vanilla: Panda.getAmbientSound
		switch m.pandaVariant() {
		case GeneAggressive:
			s.Ambient = registry.SoundEntityPandaAggressiveAmbient
		case GeneWorried:
			s.Ambient = registry.SoundEntityPandaWorriedAmbient
		}
	}
	if m.Type == TypeFox && m.Fox.Sleeping {
		// vanilla: Fox.getAmbientSound
		s.Ambient = registry.SoundEntityFoxSleep
	}
	if m.Type == TypeTurtle && (m.InWater() || !m.OnGround || m.IsBaby()) {
		// vanilla: Turtle.getAmbientSound
		s.Ambient = registry.SoundNone
	}
	if m.Type == TypeStrider && (m.panicking() || m.striderTempted()) {
		// vanilla: Strider.getAmbientSound
		s.Ambient = registry.SoundNone
	}
	if m.Creepy() {
		// vanilla: EnderMan.getAmbientSound
		s.Ambient = registry.SoundEntityEndermanScream
	}
	if m.Type == TypeZombifiedPiglin && m.Angry() {
		// vanilla: ZombifiedPiglin.getAmbientSound
		s.Ambient = registry.SoundEntityZombifiedPiglinAngry
	}
	if m.Type == TypeZoglin && m.TargetID != 0 {
		// vanilla: Zoglin.getAmbientSound
		s.Ambient = registry.SoundEntityZoglinAngry
	}
	if m.Type == TypeHoglin {
		// vanilla: HoglinAi.getSoundForActivity; conversion overrides FIGHT.
		switch {
		case m.hoglin.retreat > 0 || !m.NoAI && !m.ZombificationImmune && m.hoglin.inOverworld > 0:
			s.Ambient = registry.SoundEntityHoglinRetreat
		case m.TargetID != 0:
			s.Ambient = registry.SoundEntityHoglinAngry
		case m.hoglin.seen:
			s.Ambient = registry.SoundEntityHoglinRetreat
		}
	}
	if m.Type == TypePiglin && m.barter.ambient != registry.SoundNone {
		s.Ambient = m.barter.ambient
	}
	if m.IsCreaking() {
		s = m.creakingSounds(s)
	}
	if m.wardenUnderground() {
		s.Ambient = registry.SoundNone
	}
	if m.ArmadilloScared() {
		s.Ambient = registry.SoundNone
		s.Hurt = registry.SoundEntityArmadilloHurtReduced
	}
	if m.ShulkerClosed() {
		// vanilla: Shulker.getHurtSound, playAmbientSound
		s.Ambient = registry.SoundNone
		s.Hurt = registry.SoundEntityShulkerHurtClosed
	}
	if m.IsGuardian() && !m.InWater() {
		// vanilla: Guardian.getAmbientSound, getHurtSound, getDeathSound
		if m.Type == TypeElderGuardian {
			s.Ambient, s.Hurt, s.Death = registry.SoundEntityElderGuardianAmbientLand, registry.SoundEntityElderGuardianHurtLand, registry.SoundEntityElderGuardianDeathLand
		} else {
			s.Ambient, s.Hurt, s.Death = registry.SoundEntityGuardianAmbientLand, registry.SoundEntityGuardianHurtLand, registry.SoundEntityGuardianDeathLand
		}
	}
	if m.Type == TypeDrowned && m.InWater() {
		s.Ambient = registry.SoundEntityDrownedAmbientWater
		s.Hurt = registry.SoundEntityDrownedHurtWater
		s.Death = registry.SoundEntityDrownedDeathWater
	}
	if m.Type == TypeCopperGolem {
		s = m.copperSounds(s)
	}
	if m.isCube() {
		// vanilla: Slime and MagmaCube.getHurtSound and getDeathSound
		sounds := cubeVoiceSounds[m.Type]
		if m.tiny() {
			s.Hurt, s.Death = sounds.smallHurt, sounds.smallDeath
		} else {
			s.Hurt, s.Death = sounds.hurt, sounds.death
		}
	}
	return s
}

var cubeVoiceSounds = [registry.EntityTypeCount]struct{ hurt, death, smallHurt, smallDeath registry.Sound }{
	TypeSlime:      {registry.SoundEntitySlimeHurt, registry.SoundEntitySlimeDeath, registry.SoundEntitySlimeHurtSmall, registry.SoundEntitySlimeDeathSmall},
	TypeMagmaCube:  {registry.SoundEntityMagmaCubeHurt, registry.SoundEntityMagmaCubeDeath, registry.SoundEntityMagmaCubeHurtSmall, registry.SoundEntityMagmaCubeDeathSmall},
	TypeSulfurCube: {registry.SoundEntitySulfurCubeHurt, registry.SoundEntitySulfurCubeDeath, registry.SoundEntitySmallSulfurCubeHurt, registry.SoundEntitySmallSulfurCubeDeath},
}

// variantSounds is the *_sound_variant set the mob speaks: its variant's
// adult sounds, or the baby ones while it is a baby. A variant the registry
// lacks speaks as classic.
// vanilla: Wolf.getSoundSet, Cat.getSoundSet, Pig.getSoundSet
func (m *Mob) variantSounds() registry.SoundVariantSet {
	_, regName, _ := VariantRegistries(m.Type)
	if set, ok := registry.SoundVariantOf(regName, m.SoundVariant, m.IsBaby()); ok {
		return set
	}
	set, _ := registry.SoundVariantOf(regName, "minecraft:classic", m.IsBaby())
	return set
}

// farmSounds is a cow's, pig's or chicken's variant set as the mob's sounds,
// each left at the kind's own where the set has none.
func (m *Mob) farmSounds(s MobSounds) MobSounds {
	p := m.variantSounds()
	for _, f := range [...]struct {
		dst *registry.Sound
		src registry.Sound
	}{
		{&s.Ambient, p.Ambient}, {&s.Hurt, p.Hurt}, {&s.Death, p.Death}, {&s.Eat, p.Eat}, {&s.Step, p.Step},
	} {
		if f.src != registry.SoundNone {
			*f.dst = f.src
		}
	}
	return s
}

// babySounds is a kind's own baby set (getSoundSet's babySounds while
// isBaby); false for kinds whose babies use the grown or variant set.
func babySounds(t MobType) (MobSounds, bool) {
	switch t {
	case TypeTurtle:
		return MobSounds{
			Hurt:  registry.SoundEntityTurtleHurtBaby,
			Death: registry.SoundEntityTurtleDeathBaby,
			Step:  registry.SoundEntityTurtleShambleBaby,
		}, true
	case TypePolarBear:
		bear := kinds[TypePolarBear].sounds
		bear.Ambient = registry.SoundEntityPolarBearAmbientBaby
		return bear, true
	case TypeHorse:
		// vanilla: Horse.getAmbientSound, getHurtSound, getDeathSound
		horse := kinds[TypeHorse].sounds
		horse.Ambient = registry.SoundEntityBabyHorseAmbient
		horse.Hurt = registry.SoundEntityBabyHorseHurt
		horse.Death = registry.SoundEntityBabyHorseDeath
		return horse, true
	case TypeHappyGhast:
		return MobSounds{
			Ambient: registry.SoundEntityGhastlingAmbient,
			Hurt:    registry.SoundEntityGhastlingHurt,
			Death:   registry.SoundEntityGhastlingDeath,
		}, true
	}
	return MobSounds{}, false
}

// OnFire is Entity.isOnFire: fire ticks left, and never for a fire-immune
// kind.
func (m *Mob) OnFire() bool { return m.FireTicks > 0 && !m.kind().fireImmune }

// fallDamageImmune is membership in the minecraft:fall_damage_immune entity
// tag (the chicken among them), checked first by
// LivingEntity.calculateFallDamage.
func (m *Mob) fallDamageImmune() bool {
	return m.inTag(registry.EntityTypeTagIDFallDamageImmune)
}

// SoundVolume is getSoundVolume: AbstractCubeMob's 0.4 per size for cubes,
// the kind's own (the cow's 0.4), else 1. A grown happy ghast speaks four
// times louder than a ghastling.
// vanilla: HappyGhast.getSoundVolume
func (m *Mob) SoundVolume() float32 {
	if m.isCube() {
		return 0.4 * float32(m.Size)
	}
	if m.is(TypeHappyGhast) && m.IsBaby() {
		return 1.0
	}
	return MobSoundVolume(m.Type)
}

// width, height, pace and eyeHeight are the mob's measures from its type;
// eye height defaults to four fifths of the height. A baby uses its kind's
// BABY_DIMENSIONS, and a baby zombie's pace has SPEED_MODIFIER_BABY. The
// scale attribute multiplies every measure but the unscaled sleeping box.
// vanilla: LivingEntity.getDimensions
func (m *Mob) width() float64 {
	if m.Sleeping && m.Type == TypeVillager {
		return sleepingBox
	}
	return m.poseWidth() * m.attrScale()
}

func (m *Mob) poseWidth() float64 {
	if m.IsBaby() && m.kind().babyWidth > 0 {
		return m.kind().babyWidth * m.poseScale()
	}
	return m.kind().width * m.scale() * m.poseScale()
}

// scale is a cube mob's size (AbstractCubeMob.getDefaultDimensions), 1
// otherwise.
func (m *Mob) scale() float64 {
	if m.isCube() {
		return float64(m.Size)
	}
	if m.Type == TypePhantom {
		return m.phantomScale()
	}
	if m.Type == TypeSalmon {
		// vanilla: Salmon.getDefaultDimensions
		return salmonScale[m.SalmonSize]
	}
	if m.Type == TypePufferfish {
		// vanilla: Pufferfish.getDefaultDimensions
		return puffScale[m.puffer.state]
	}
	return 1
}

// BbWidth is the current box width (a baby's when one), which the farmland
// trample weighs.
func (m *Mob) BbWidth() float64 { return m.width() }

// BbHeight is the current box height.
func (m *Mob) BbHeight() float64 { return m.height() }

func (m *Mob) height() float64 {
	if m.Sleeping && m.Type == TypeVillager {
		return sleepingBox
	}
	return m.poseHeight() * m.attrScale()
}

func (m *Mob) poseHeight() float64 {
	if m.wardenUnderground() {
		return 1
	}
	if IsCamel(m.Type) && m.CamelSitting() {
		// vanilla: Camel.ADULT_SITTING_DIMENSIONS, BABY_SITTING_DIMENSIONS
		if m.IsBaby() {
			return camelBabySittingH
		}
		return m.kind().height - camelSitHeightDrop
	}
	if m.IsBaby() && m.kind().babyHeight > 0 {
		return m.kind().babyHeight * m.poseScale()
	}
	if m.snifferDigging() {
		return snifferDigHeight
	}
	return m.kind().height * m.scale() * m.poseScale()
}

// Pace is the mob's movement pace, for tests.
func (m *Mob) Pace() float64 { return m.pace() }

func (m *Mob) pace() float64 {
	var speed, mapValue float64
	mapPaced := m.attrs != nil && m.attrPaced()
	if mapPaced {
		// The map holds the pace: the kind's base, a command's edits, the
		// frost slow, the drinker's, the baby's. SyncSpeedModifiers,
		// once a tick before the dirty scan, keeps those current; this
		// read carries them.
		// speed then collects only the addends the map does not hold.
		mapValue = m.attrs.Value(registry.AttributeMovementSpeed)
	} else {
		speed = m.paceBase()
		// Powder snow's transient ADD_VALUE movement_speed slow, on a body with
		// frozen ticks and only air under it. It lands before the baby zombie's
		// ADD_MULTIPLIED_BASE haste because value modifiers add first.
		// vanilla: LivingEntity.tryAddFrost, AttributeInstance.calculateValue
		if m.TicksFrozen > 0 && !m.underfootAir {
			speed += rules.FreezeSpeedAmount(m.TicksFrozen)
		}
		if m.DrinkingPotion() {
			// vanilla: Witch.SPEED_MODIFIER_DRINKING
			speed += witchDrinkSpeed
		}
		if !m.isCube() && m.is(TypeZombie) && m.IsBaby() {
			speed *= 1.5
		}
		if m.Type == TypePiglin && m.IsBaby() {
			// vanilla: Piglin.SPEED_MODIFIER_BABY, ADD_MULTIPLIED_BASE
			speed *= 1.2
		}
	}
	if m.Type == TypeRavager {
		// The eased walk-to-hunt step, a goal's speed factor in vanilla
		// rather than an attribute.
		speed += m.ravager.pace - ravagerWalkPace
	}
	if m.Type == TypeZombifiedPiglin && !m.IsBaby() && m.anger.ticks > 0 {
		// vanilla: ZombifiedPiglin.customServerAiStep SPEED_MODIFIER_ATTACKING
		speed += 0.05
	}
	if m.Type == TypeEnderman && m.TargetID != 0 && m.attrs == nil {
		// vanilla: EnderMan.setTarget's attacking modifier, ADD_VALUE 0.15f;
		// a body with its map carries it in SyncSpeedModifiers instead.
		speed += float64(float32(0.15))
	}
	speed += m.striderColdPace()
	if mapPaced {
		// Speed and Slowness are already modifiers on the map; the addends
		// stand in for modifiers it lacks, so the effects scale them too.
		return math.Max(0, mapValue+speed*m.effectFactor())
	}
	return m.effectSpeed(speed)
}

// pathSpeed is the path walk's speed: pace times the walk's factor, or a
// land turtle's move control speed.
func (m *Mob) pathSpeed() float64 {
	speed := m.pace() * m.nav.speedFactor
	if m.Type == TypeTurtle {
		speed = m.turtleMoveSpeed(speed)
	}
	return speed
}

// stepHeight is the ledge height a walk climbs without a jump: the
// step_height attribute, read as a float.
// vanilla: LivingEntity.maxUpStep, SulfurCube.maxUpStep
func (m *Mob) stepHeight() float64 {
	if m.SulfurCubeFilled() {
		return 0
	}
	if m.attrs != nil {
		return float64(float32(m.attrs.Value(registry.AttributeStepHeight)))
	}
	if h := m.kind().stepHeight; h != 0 {
		return h
	}
	return stepHeight
}

// Hostile reports vanilla's Monster test, which a bed's sleep check asks of
// nearby mobs.
func (m *Mob) Hostile() bool { return m.kind().hostile && m.Type != TypeCamelHusk }

func (m *Mob) eyeHeight() float64 { return m.poseEyeHeight() * m.attrScale() }

// EyeHeight is the eyes' height above the feet in the current pose and scale.
// vanilla: Entity.getEyeHeight
func (m *Mob) EyeHeight() float64 { return m.eyeHeight() }

func (m *Mob) poseEyeHeight() float64 {
	if IsCamel(m.Type) && m.CamelSitting() {
		if m.IsBaby() {
			return camelBabySittingEye
		}
		return camelSittingEye
	}
	if m.IsBaby() && m.kind().babyEye > 0 {
		return m.kind().babyEye * m.poseScale()
	}
	if m.snifferDigging() {
		return snifferDigEye
	}
	if e := m.kind().eyeHeight; e > 0 {
		return e * m.scale() * m.poseScale()
	}
	return m.kind().height * 0.85
}

// BlastBox is the entity's AABB, which ServerExplosion.getSeenPercent
// samples.
func (m *Mob) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	if m.Type == TypeShulker {
		return m.shulkerBox().Corners()
	}
	hw := m.width() / 2
	return m.X - hw, m.Y, m.Z - hw, m.X + hw, m.Y + m.height(), m.Z + hw
}

// BlastBoxHits reports whether BlastBox intersects the box. It tests X and Z
// first so a body that misses horizontally never computes its height.
func (m *Mob) BlastBoxHits(minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	if m.Type == TypeShulker {
		bMinX, bMinY, bMinZ, bMaxX, bMaxY, bMaxZ := m.shulkerBox().Corners()
		return minX < bMaxX && maxX > bMinX && minY < bMaxY && maxY > bMinY && minZ < bMaxZ && maxZ > bMinZ
	}
	hw := m.width() / 2
	if !(minX < m.X+hw && maxX > m.X-hw && minZ < m.Z+hw && maxZ > m.Z-hw) {
		return false
	}
	return minY < m.Y+m.height() && maxY > m.Y
}

// EyePosition is getEyePosition, where an explosion's knockback is directed
// from.
func (m *Mob) EyePosition() (x, y, z float64) {
	return m.X, m.Y + m.eyeHeight(), m.Z
}

// armoured is the blow after armour: LivingEntity.getDamageAfterArmorAbsorb
// on the kind's armour points plus the worn pieces' (and a horse's body
// armour's) modifiers, with their toughness. Bypasses-armor blows take the plain hurt and kill paths and
// never reach here.
func (m *Mob) armoured(amount float64) float64 {
	return m.armouredBy(amount, 0)
}

// armouredBy is armoured under a weapon's armour effectiveness delta.
func (m *Mob) armouredBy(amount, effectiveness float64) float64 {
	if m.attrs != nil {
		// The map holds the kind's base, the worn pieces' and a command's.
		points := m.attrs.Value(registry.AttributeArmor)
		if points <= 0 {
			return amount
		}
		return rules.DamageAfterArmourEffective(amount, points, m.attrs.Value(registry.AttributeArmorToughness), effectiveness)
	}
	points := m.baseArmor()
	if m.Type == TypeMagmaCube {
		// vanilla: MagmaCube.setSize
		points = float64(3 * m.Size)
	}
	var toughness float64
	piece := func(stack protocol.ItemStack, slot string) {
		for _, mod := range rules.EquipmentModifiers(stack, slot) {
			switch mod.Attribute {
			case registry.AttributeArmor:
				points += mod.Amount
			case registry.AttributeArmorToughness:
				toughness += mod.Amount
			}
		}
	}
	for i, slot := range ArmorSlotNames {
		piece(m.Worn[i], slot)
	}
	// Wolf armour soaks a blow whole before armour, in wolfArmorTakes.
	if IsEquine(m.Type) || IsNautilus(m.Type) {
		piece(m.Body, "body")
	}
	if points <= 0 {
		return amount
	}
	return rules.DamageAfterArmourEffective(amount, points, toughness, effectiveness)
}

// afterResistance scales a blow by the mob's Resistance, then by the
// protection enchantments on its equipment, both after armour.
// vanilla: LivingEntity.getDamageAfterMagicAbsorb
func (m *Mob) afterResistance(amount float64, damageType registry.DamageType) float64 {
	amount = m.afterProtection(amount, damageType)
	if m.Type == TypeWitch && damageType.InTag(registry.DamageTypeTagIDWitchResistantTo) {
		amount *= witchMagicFactor
	}
	return amount
}

// enchPieces is the worn armour and both hands with their slot names.
func (m *Mob) enchPieces() []rules.EnchPiece {
	pieces := make([]rules.EnchPiece, 0, len(m.Worn)+2)
	for i, name := range ArmorSlotNames {
		pieces = append(pieces, rules.EnchPiece{Stack: m.Worn[i], Slot: name})
	}
	return append(pieces,
		rules.EnchPiece{Stack: m.MainHand, Slot: "mainhand"},
		rules.EnchPiece{Stack: m.OffHand, Slot: "offhand"})
}

// afterProtection is LivingEntity.getDamageAfterMagicAbsorb.
func (m *Mob) afterProtection(amount float64, damageType registry.DamageType) float64 {
	amount = rules.Resist(m.effects.All(), damageType, amount)
	if amount <= 0 || rules.ProtectionBypassed(damageType) {
		return amount
	}
	ctx := &rules.EnchContext{DamageType: damageType, Direct: true, Victim: m.typeID()}
	if protection := rules.DamageProtection(m.enchPieces(), ctx, nil); protection > 0 {
		amount = rules.DamageAfterEnchantmentProtection(amount, protection)
	}
	return amount
}

// HurtByBlast is an explosion's hurt and push. damageType is kept so panic
// answers explosion or player_explosion; the push lands whatever the hurt
// did, and creative lets the blast reach an invulnerable body.
// vanilla: LivingEntity.hurtServer, ServerExplosion.hurtEntities, Entity.push
func (m *Mob) HurtByBlast(amount float64, kx, ky, kz float64, damageType registry.DamageType, creative bool) {
	m.HurtByCausedBlast(amount, kx, ky, kz, damageType, creative, false, 0)
}

// HurtByCausedBlast is HurtByBlast with livingCause saying whether the blast's
// source is a living entity, and causeID naming the blame source.
func (m *Mob) HurtByCausedBlast(amount float64, kx, ky, kz float64, damageType registry.DamageType, creative, livingCause bool, causeID int32) {
	m.noteWardenHurt(causeID, false)
	if landed, _ := m.plainHurtBy(amount, damageType, creative, livingCause, hurtContext{causeID: causeID}); landed {
		m.noteLastDamage(damageType)
	}
	m.PushByBlast(kx, ky, kz)
}

// PushByBlast is the push of an explosion that hurts nothing.
// vanilla: ServerExplosion.hurtEntities, Entity.push
func (m *Mob) PushByBlast(kx, ky, kz float64) {
	if m.wardenUnderground() {
		return
	}
	m.VX += kx
	m.VY += ky
	m.VZ += kz
	m.Impulse = true
}

// Push adds (x, y, z) to the body's velocity, told on its next tick.
// vanilla: Entity.push
func (m *Mob) Push(x, y, z float64) {
	// vanilla: Creaking.push
	if m.creaking.frozen {
		return
	}
	m.VX += x
	m.VY += y
	m.VZ += z
	m.Impulse = true
}

// HurtByWorld is a hurt with nobody behind it (a conduit's magic), through
// the usual window and natural armour. landed says it took hold (zero
// amount still opens the window); softened says the window cut it to its
// excess, told by health alone.
// vanilla: LivingEntity.hurtServer, actuallyHurt
func (m *Mob) HurtByWorld(amount float64, damageType registry.DamageType) (landed, softened bool) {
	if damageType.InTag(registry.DamageTypeTagIDIsProjectile) {
		if dodges, _ := m.dodgesProjectile(damageType, false); dodges {
			return false, false
		}
	}
	if amount == 0 {
		return m.openWindow(damageType), false
	}
	landed, softened = m.plainHurt(amount, damageType, false)
	if landed {
		m.noteLastDamage(damageType)
	}
	return landed, softened
}

// openWindow starts the invulnerability window with a hurt of nothing; false
// when the body is dead, immune to the type or already inside a window.
// vanilla: LivingEntity.hurtServer
func (m *Mob) openWindow(damageType registry.DamageType) bool {
	if m.Dead || m.InvulnerableTo(damageType, false) ||
		rules.FireImmune(m.effects.All(), damageType) || m.InvulnTicks > hurtCooldown {
		return false
	}
	m.lastHurt = 0
	m.InvulnTicks = invulnerableTime
	m.HurtTime = 10 // vanilla: LivingEntity.hurtServer
	m.InLove = 0
	return true
}

// HurtByPlayerThorns is a player's thorns damage on a mob that struck them,
// credited to the player for XP and player-kill loot; a whole hit shoves the
// mob away from the wearer at (fromX, fromZ). Results are as for HurtByWorld.
// vanilla: LivingEntity.hurtServer, dealDefaultKnockback, resolvePlayerResponsibleForDamage
func (m *Mob) HurtByPlayerThorns(amount float64, damageType registry.DamageType, playerID int32, creative bool, fromX, fromZ float64) (landed, softened bool) {
	m.noteWardenHurt(playerID, true)
	landed, softened = m.plainHurtBy(amount, damageType, creative, true, hurtContext{})
	if landed && !softened {
		m.knockback(knockbackStrength, fromX-m.X, fromZ-m.Z)
	}
	if landed {
		m.hurtByPlayer = true
		m.notePlayerHit()
		m.playerMemoryTicks = playerMemoryTicks
		m.playerCreditID = playerID
		m.setRetaliate(playerID)
		m.noteLastDamage(damageType)
		m.blowAttacker = playerID
	}
	return landed, softened
}

// knockback is LivingEntity.knockback along (xd, zd), which points from the
// body toward the source. The old velocity is halved and the push taken off
// it; the upward share lifts only a grounded body and is capped at 0.4; a
// near-zero direction gets vanilla's random nudge. A rider's shove stays on
// the rider, whose seat clears it; its vehicle does not move.
// vanilla: LivingEntity.knockback
func (m *Mob) knockback(power, xd, zd float64) {
	if m.Type == TypeEnderDragon {
		return
	}
	// vanilla: Creaking.knockback
	if m.creaking.frozen {
		return
	}
	power *= 1 - m.KnockbackResistance()
	if power <= 0 {
		return
	}
	for xd*xd+zd*zd < 1.0e-5 {
		xd = (m.Rand.Float64() - m.Rand.Float64()) * 0.01
		zd = (m.Rand.Float64() - m.Rand.Float64()) * 0.01
	}
	horizontal := math.Hypot(xd, zd)
	m.VX = m.VX/2 - xd/horizontal*power
	m.VZ = m.VZ/2 - zd/horizontal*power
	if m.OnGround {
		m.VY = math.Min(0.4, m.VY/2+power)
	}
	m.Impulse = true
}

// HurtByFall is a gravity block landing on a mob: armour and window apply,
// and the shove points away from where the block rests (no falling type
// bypasses armour or has no_knockback). The result says a whole hurt landed;
// a softened one has no shove and nothing to broadcast.
// vanilla: LivingEntity.hurtServer
func (m *Mob) HurtByFall(amount float64, damageType registry.DamageType, fromX, fromZ float64) bool {
	return m.HurtByFallFrom(amount, damageType, fromX, fromZ, nil)
}

// HurtByFallFrom includes the falling entity's stance for a filled cube's shove.
func (m *Mob) HurtByFallFrom(amount float64, damageType registry.DamageType, fromX, fromZ float64, from *HitSource) bool {
	if m.Dead || amount <= 0 {
		return false
	}
	if m.sulfurBlocked(damageType) {
		m.hitKnockback(from, fromX-m.X, fromZ-m.Z, amount)
		return false // The impulse is synced without a damage animation.
	}

	landed, soft := m.plainHurt(amount, damageType, false)
	if !landed {
		return false
	}
	m.noteLastDamage(damageType)
	if soft {
		return false
	}
	m.knockback(knockbackStrength, fromX-m.X, fromZ-m.Z)
	return true
}

// HurtByFirework is a rocket burst's hurt: window and armour apply, the
// launcher is the cause, and the shove points away from the rocket. Results
// are landed and softened.
// vanilla: LivingEntity.hurtServer,
// FireworkRocketEntity.calculateHorizontalHurtKnockbackDirection
func (m *Mob) HurtByFirework(amount float64, damageType registry.DamageType, causeID int32, causeIsPlayer, causeCreative bool, fromX, fromZ float64, from ...*HitSource) (landed, softened bool) {
	return m.hurtByDamage(amount, damageType, causeID, causeIsPlayer, causeCreative, fromX-m.X, fromZ-m.Z, from...)
}

// HurtByCommand is /damage: causeID (0 for none) is kept for
// retaliation and kill credit, and a whole hit shoves away from (fromX, fromZ)
// unless the type is no_knockback. When from is non-nil (a by/from entity),
// a filled sulfur cube bends its shove by the attacker's aim. Results are
// landed and softened.
// vanilla: DamageCommand.damage, LivingEntity.hurtServer
func (m *Mob) HurtByCommand(amount float64, damageType registry.DamageType, causeID int32, causeIsPlayer, causeCreative bool, fromX, fromZ float64, from ...*HitSource) (landed, softened bool) {
	if causeID != 0 {
		return m.hurtByDamage(amount, damageType, causeID, causeIsPlayer, causeCreative, fromX-m.X, fromZ-m.Z, from...)
	}
	landed, softened = m.HurtByWorld(amount, damageType)
	if landed && !softened && !damageType.InTag(registry.DamageTypeTagIDNoKnockback) {
		m.knockback(knockbackStrength, fromX-m.X, fromZ-m.Z)
	}
	return landed, softened
}

// hurtByDamage is the projectile blow shared by thrown projectiles and
// firework bursts: health off through the window, the cause kept for
// retaliation and kill credit, the type noted, and the shove only when the
// whole blow landed.
// vanilla: LivingEntity.hurtServer, calculateHorizontalHurtKnockbackDirection
func (m *Mob) hurtByDamage(amount float64, damageType registry.DamageType, causeID int32, causeIsPlayer, creative bool, knX, knZ float64, from ...*HitSource) (landed, softened bool) {
	if damageType.InTag(registry.DamageTypeTagIDIsProjectile) {
		if dodges, _ := m.dodgesProjectile(damageType, creative); dodges {
			return false, false
		}
	}
	if m.dragon != nil {
		before := m.Health
		defer func() { m.dragonSittingDamage(before) }()
		defer dragonHurtResult(m.dragonAccepts(amount), &landed, &softened)
	}

	m.noteWardenHurt(causeID, false)
	if m.Dead {
		return false, false
	}
	// vanilla: Allay.hurtServer
	if m.Type == TypeAllay && causeIsPlayer && m.allayLikes(causeID) {
		return false, false
	}
	var source *HitSource
	if len(from) > 0 {
		source = from[0]
	}
	if m.Type == TypeBreeze && source != nil && source.Type == TypeBreeze {
		return false, false
	}
	if source != nil && m.witherSpares(source.Type) {
		return false, false
	}
	if m.sulfurBlocked(damageType) {
		m.sulfurHurtHook(damageType)
		if !damageType.InTag(registry.DamageTypeTagIDNoKnockback) {
			m.hitKnockback(source, knX, knZ, amount)
		}
		return true, true
	}
	mayLand, soft := m.throughWindow(amount, damageType, creative, true, causeIsPlayer, hurtContext{causeID: causeID})
	if mayLand == 0 && soft {
		return false, false
	}
	if mayLand > 0 && !m.wolfArmorTakes(mayLand, damageType) {
		if !rules.ArmourBypassed(damageType) {
			mayLand = m.armoured(mayLand)
		}
		m.loseHealth(m.absorb(m.afterResistance(mayLand, damageType)))
	}
	m.NoActionTicks = 0
	m.resetAmbientSoundTime()
	if causeIsPlayer {
		m.hurtByPlayer = true
		m.notePlayerHit()
		m.playerMemoryTicks = playerMemoryTicks
		m.playerCreditID = causeID
	} else {
		m.hurtByPlayer = false
	}
	m.hurtByMelee = false
	// vanilla: LivingEntity.resolveMobResponsibleForDamage; a breeze's brain answers HURT_BY instead
	if damageType != rules.DamageTypeWindCharge || m.Type == TypeBreeze || !m.inTag(registry.EntityTypeTagIDNoAngerFromWindCharge) {
		m.setRetaliate(causeID)
	}
	m.noteReinforcement(causeID)
	m.noteLastDamage(damageType)
	m.blowAttacker = causeID
	m.noteSquidHurt()
	if m.Health == 0 {
		m.dies(damageType, soft)
	}
	if soft {
		return true, true
	}
	if !damageType.InTag(registry.DamageTypeTagIDNoKnockback) {
		m.hitKnockback(source, knX, knZ, amount)
	}
	return true, false
}

// cell is the block cell the mob's feet are in.
func (m *Mob) cell() Waypoint {
	return Waypoint{int32(math.Floor(m.X)), int32(math.Floor(m.Y)), int32(math.Floor(m.Z))}
}

// tempts reports whether a held stack is the kind's food, by its list or
// item tag. An empty hand tempts nobody.
func (m *Mob) tempts(stack protocol.ItemStack) bool {
	if stack.Empty() {
		return false
	}
	if m.Type == TypeSulfurCube && m.IsBaby() {
		return registry.Item(stack.ItemID).InTag(registry.ItemTagIDSulfurCubeFood)
	}
	if tag := m.kind().temptTag; tag != nil {
		// A ghastling or a harnessed body follows food only, not harnesses
		// (HappyGhast.registerGoals picks the food tag for a baby or a body in
		// armor).
		if tag == &registry.ItemTagIDHappyGhastTemptItems && m.is(TypeHappyGhast) && (m.IsBaby() || !m.Body.Empty()) {
			tag = &registry.ItemTagIDHappyGhastFood
		}
		return registry.Item(stack.ItemID).InTag(*tag)
	}
	return slices.Contains(m.kind().tempting, registry.Item(stack.ItemID))
}

// nearestPlayerWithin is the nearest noticed player within the distance
// (line of sight required when seen), or nil.
// vanilla: TargetingConditions.test (forNonCombat)
func (m *Mob) nearestPlayerWithin(t *tickView, within float64, seen bool) *Player {
	return m.nearestPlayerMatching(t, within, seen, nil)
}

// nearestPlayerMatching filters before choosing the nearest noticed player.
func (m *Mob) nearestPlayerMatching(t *tickView, within float64, seen bool, accept func(*Player) bool) *Player {
	var nearest *Player
	best := math.Inf(1)
	for i := range t.players {
		p := &t.players[i]
		if accept != nil && !accept(p) {
			continue
		}
		d := Dist2(m.X, m.Y+m.height()*0.85, m.Z, p.X, p.Y, p.Z)
		if d >= best || !m.noticesPlayer(t, p, within, seen) {
			continue
		}
		best = d
		nearest = p
	}
	return nearest
}

// noticesPlayer applies the non-combat targeting conditions: reach cut by
// the player's visibility, feet to feet, and line of sight when asked.
// Spectators and the dead are never noticed.
// vanilla: LivingEntity.canBeSeenByAnyone
func (m *Mob) noticesPlayer(t *tickView, p *Player, within float64, seen bool) bool {
	if p.Spectator || p.Dead {
		return false
	}
	if m.distanceToPlayer(p) > max(within*visibilityPercent(m, p), minVisibilityDistance) {
		return false
	}
	return !seen || m.seesTarget(t.w, p)
}

// nearestTemptingPlayerWithin is the nearest player within reach holding the
// mob's food in either hand. Tempting ignores line of sight.
func (m *Mob) nearestTemptingPlayerWithin(t *tickView, within float64) *Player {
	var nearest *Player
	best := math.Inf(1)
	for i := range t.players {
		p := &t.players[i]
		if !m.tempts(p.HeldItem) && !m.tempts(p.OffHandItem) {
			continue
		}
		d := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
		if d < best && m.noticesPlayer(t, p, within, false) {
			best = d
			nearest = p
		}
	}
	return nearest
}

// Dist2 is the squared distance between two points.
func Dist2(x0, y0, z0, x1, y1, z1 float64) float64 {
	dx, dy, dz := x1-x0, y1-y0, z1-z0
	return dx*dx + dy*dy + dz*dz
}

// yawToward is the body yaw facing a horizontal direction, in client
// degrees: 0 south (+Z), 90 west (-X).
func yawToward(dx, dz float64) float64 {
	return math.Atan2(-dx, dz) * 180 / math.Pi
}

// pitchToward is the pitch looking from one point at another; negative is
// up.
func pitchToward(dx, dy, dz float64) float64 {
	return -math.Atan2(dy, math.Hypot(dx, dz)) * 180 / math.Pi
}

// turnToward moves an angle toward target by at most step the short way
// round, landing on it exactly so a head returning to the body stops dead.
func turnToward(current, target float32, step float64) float32 {
	delta := float64(target - current)
	delta = math.Mod(delta+540, 360) - 180
	if math.Abs(delta) <= step {
		return target
	}
	return current + float32(math.Copysign(step, delta))
}

// InvulnerableTo reports immunity to a damage type before the window and armour.
// vanilla: LivingEntity.isInvulnerableTo, Entity.isInvulnerableToBase
func (m *Mob) InvulnerableTo(damageType registry.DamageType, creative bool) bool {
	if m.wardenImmune(damageType) {
		return true
	}
	if damageType.InTag(registry.DamageTypeTagIDIsFire) && m.kind().fireImmune && !m.reflected {
		return true
	}
	if damageType.InTag(registry.DamageTypeTagIDIsFall) && m.fallDamageImmune() {
		return true
	}
	if m.creaking.bound && !rules.InvulnerabilityBypassed(damageType) {
		return true
	}
	if m.Type == TypeWither && m.witherRefuses(damageType) {
		return true
	}
	// vanilla: SulfurCube.hurtServer, SULFUR_CUBE_WITH_BLOCK_IMMUNE_TO
	if m.SulfurCubeFilled() && damageType.InTag(registry.DamageTypeTagIDSulfurCubeWithBlockImmuneTo) {
		return true
	}
	return m.Invulnerable && !creative && !rules.InvulnerabilityBypassed(damageType)
}

// throughWindow reports how much of a hurt may land now and whether it lands
// through the last hurt's window: nothing while the window is above
// hurtCooldown and the blow is no larger than the last; only the excess for
// a larger one; or the whole blow once past the gate, which reopens the
// window with this amount as the basis. An immune body lands nothing. Any
// hurt that lands ends love mode (Animal.actuallyHurt).
// vanilla: LivingEntity.hurtServer
func (m *Mob) throughWindow(amount float64, damageType registry.DamageType, creative, entityBehind, byPlayer bool, ctx hurtContext) (mayLand float64, soft bool) {
	if m.ArmadilloScared() {
		amount = max(0, (amount-1)/2)
	}
	if m.Type == TypeArmadillo {
		defer func() { m.armadilloHurt(mayLand, damageType, entityBehind) }()
	}
	if m.Type == TypePanda {
		// vanilla: Panda.hurtServer
		m.pandaSetFlag(pandaFlagSit, false)
	}

	if m.dragon != nil {
		if amount = m.dragonHurt(amount, damageType, byPlayer); amount == 0 {
			return 0, true
		}
	}
	m.sulfurHurtHook(damageType)
	if m.creakingShields(damageType, entityBehind, byPlayer) || m.InvulnerableTo(damageType, creative) {
		return 0, true
	}
	m.noteWitherHurt()
	// Armed before the window decides, so a hit it absorbs still calls.
	// vanilla: Silverfish.hurtServer
	m.noteSilverfishHurt(entityBehind, damageType)
	m.noteAxolotlHurt(amount, entityBehind)
	// vanilla: Bat.hurtServer, which wakes the bat before any other check
	m.SetMetadataResting(false)
	// vanilla: Wolf.hurtServer
	if m.Type == TypeWolf || m.Type == TypeParrot {
		m.Tamable.OrderedToSit = false
	}
	// Fire Resistance drops a fire-typed blow before the window opens.
	// vanilla: LivingEntity.hurtServer
	if rules.FireImmune(m.effects.All(), damageType) {
		return 0, true
	}
	// vanilla: LivingEntity.hurtServer, before the cooldown and armour
	if damageType.InTag(registry.DamageTypeTagIDIsFreezing) && m.inTag(registry.EntityTypeTagIDFreezeHurtsExtraTypes) {
		amount *= 5
	}
	// Any worn head item cuts a falling block's blow, before the window.
	// vanilla: LivingEntity.hurtServer (hurtHelmet wears nothing on a mob)
	if damageType.InTag(registry.DamageTypeTagIDDamagesHelmet) && !m.Worn[wornHead].Empty() {
		amount *= 0.75
	}
	inWindow := m.InvulnTicks > hurtCooldown && !damageType.InTag(registry.DamageTypeTagIDBypassesCooldown)
	var window float64
	if inWindow {
		if amount <= m.lastHurt {
			return 0, true
		}
		window = m.lastHurt
	}
	mayLand = amount - window
	// Asked after the window decided and before it is touched, so a refused
	// hurt leaves the window as it was.
	if m.Gate != nil && m.Gate.Listening() {
		var ok bool
		if mayLand, ok = m.askHurt(mayLand, damageType, ctx); !ok {
			return 0, true
		}
		amount = mayLand + window
	}
	m.lastHurt = amount
	m.InLove = 0
	if inWindow {
		return mayLand, true
	}
	m.InvulnTicks = invulnerableTime
	m.HurtTime = 10 // vanilla: LivingEntity.hurtServer
	return mayLand, false
}

// TakeDamage is one player hit on a mob, and reports whether it landed.
// Inside the window a blow no larger than the last lands nothing, and a
// larger one only its excess without knockback. Outside it the whole blow
// lands, its type is kept for panic, the mob is knocked back 0.4 along the
// ground and 0.4 up, and the mind reacts: an animal panics, a hostile mob
// turns on the attacker (HurtByTargetGoal's last-hurt-by). Death happens
// here; the tick reports it.
func (m *Mob) TakeDamage(amount float64, source Player) bool {
	landed, _ := m.TakeDamageExtra(amount, source, 0)
	return landed
}

// TakeDamageExtra is TakeDamage plus an enchantment's extra knockback, dealt
// as a second knockback along the attacker's facing (each call halves what
// the last left). The second result says the blow landed only through the
// window: the default shove, flinch and sound stay back, but Player.attack
// still deals the enchantment's shove.
func (m *Mob) TakeDamageExtra(amount float64, source Player, extraKnockback float64) (landed, softened bool) {
	return m.TakeDamageTypedExtra(amount, rules.DamageTypePlayerAttack, source, extraKnockback)
}

// TakeDamageTypedExtra is TakeDamageExtra for a blow of the stack's own
// damage type, which keeps the default shove.
// vanilla: ItemStack.getDamageSource
func (m *Mob) TakeDamageTypedExtra(amount float64, damageType registry.DamageType, source Player, extraKnockback float64) (landed, softened bool) {
	return m.takeAttack(amount, damageType, source, extraKnockback, true)
}

// TakeStabDamage is a player's jab under the weapon's damage type. The type
// is in no_knockback, so there is no default shove; KnockbackAlong deals the
// jab's own.
// vanilla: LivingEntity.hurtServer
func (m *Mob) TakeStabDamage(amount float64, damageType registry.DamageType, source Player) (landed, softened bool) {
	return m.takeAttack(amount, damageType, source, 0, false)
}

// takeAttack lands a player's blow of the damage type given; defaultKnockback
// is false for a type in the no_knockback tag.
func (m *Mob) takeAttack(amount float64, damageType registry.DamageType, source Player, extraKnockback float64,
	defaultKnockback bool) (landed, softened bool) {
	if m.dragon != nil {
		before := m.Health
		defer func() { m.dragonSittingDamage(before) }()
		defer dragonHurtResult(m.dragonAccepts(amount), &landed, &softened)
	}

	m.noteWardenHurt(source.EntityID, true)
	if m.Dead || amount <= 0 {
		return false, false
	}
	// A cube holding a block shrugs the blow off but still takes its shove.
	// vanilla: SulfurCube.hurtServer
	from := PlayerHitSource(source)
	if m.sulfurBlocked(damageType) {
		if defaultKnockback {
			m.hitKnockback(&from, source.X-m.X, source.Z-m.Z, amount)
		}
		return true, true
	}
	mayLand, softened := m.throughWindow(amount, damageType, source.Creative, true, true, hurtContext{causeID: source.EntityID, effectiveness: source.ArmourEffectiveness})
	if mayLand == 0 {
		return false, false
	}
	if !m.wolfArmorTakes(mayLand, damageType) {
		armoured := mayLand
		if !rules.ArmourBypassed(damageType) {
			armoured = m.armouredBy(mayLand, source.ArmourEffectiveness)
		}
		m.BlowResisted = armoured - rules.Resist(m.effects.All(), damageType, armoured)
		reduced := m.afterResistance(armoured, damageType)
		left := m.absorb(reduced)
		m.BlowAbsorbed = reduced - left
		m.loseHealth(left)
	}
	m.hurtByPlayer = true
	m.notePlayerHit()
	m.hurtByMelee = true
	m.playerMemoryTicks = playerMemoryTicks
	m.playerCreditID = source.EntityID
	m.resetAmbientSoundTime()
	// The striker is kept for every kind (only hostiles act on it), and the
	// hurt resets the despawn's idleness.
	// vanilla: lastHurtByMob, HurtByTargetGoal
	m.setRetaliate(source.EntityID)
	m.noteReinforcement(source.EntityID)
	m.NoActionTicks = 0
	m.noteLastDamage(damageType)
	m.blowAttacker = source.EntityID
	m.noteSquidHurt()
	if m.Health == 0 {
		m.dies(damageType, softened)
	}
	if softened {
		// A softened blow runs only the enchantment's second shove; the
		// default one stands behind tookFullDamage.
		// vanilla: Player.attack
		if extraKnockback > 0 {
			m.knockbackFacing(source.Yaw, extraKnockback)
		}
		return true, true
	}
	if defaultKnockback {
		m.hitKnockback(&from, source.X-m.X, source.Z-m.Z, amount)
	}
	if extraKnockback > 0 {
		m.knockbackFacing(source.Yaw, extraKnockback)
	}
	return true, false
}

// knockbackFacing is the attack's extra shove along the attacker's look,
// through LivingEntity.knockback.
// vanilla: Player.causeExtraKnockback
func (m *Mob) knockbackFacing(yaw, power float64) {
	ux, uz := rules.KnockbackFacing(yaw)
	m.knockback(power, -ux, -uz)
}

// KnockbackAlong shoves the mob along the attacker's yaw at the given
// strength, whether or not a hurt landed.
// vanilla: Player.causeExtraKnockback
func (m *Mob) KnockbackAlong(yaw, power float64) {
	m.knockbackFacing(yaw, power)
}

// killDamage is LivingEntity.kill's damage (as /kill deals it), beyond any
// health.
const killDamage = 3.4e38

// Kill deals LivingEntity.kill's generic-kill damage, or removes a dragon
// directly (EnderDragon.kill). No entity is behind it: no shove or credit.
func (m *Mob) Kill() {
	if m.Type == TypeEnderDragon {
		m.gone, m.discarded = true, true
		return
	}
	if m.Dead {
		return
	}
	m.InvulnTicks = 0
	if landed, _ := m.plainHurt(killDamage, rules.DamageTypeGenericKill, false); landed {
		m.noteLastDamage(rules.DamageTypeGenericKill)
	}
}

// KillAndReport kills and collects the death effects without advancing a tick.
// vanilla: LivingEntity.kill, LivingEntity.die
func (m *Mob) KillAndReport(w World) []MobEvent {
	m.Kill()
	if !m.Dead || m.deathTold {
		return nil
	}
	events := m.queuedStep(m.reinforcementStep(w, nil))
	// Generic-kill has no attacker for a villager's witnesses.
	events = m.reportDeath(events, w, nil, nil, false)
	return append(events, m.endSeats()...)
}

// Alive is LivingEntity.isAlive: not removed and not out of health. gone is
// set by a despawn or conversion before the tick that reports it.
func (m *Mob) Alive() bool { return !m.Dead && !m.gone && m.Health > 0 }

// Discard asks the next tick to report the mob gone without dying (a
// leaving rider's boat's passengers): no death animation, sound or drops.
// vanilla: PlayerList.remove
func (m *Mob) Discard() { m.discarded = true }

// HurtByPlayer reports whether the last hurt was a player's swing, for the
// loot tables' killed_by_player. A plain hurt (a fall) resets it.
func (m *Mob) HurtByPlayer() bool { return m.hurtByPlayer }

// HurtByMelee reports whether the last hurt was a player's hand weapon
// rather than an arrow, for the loot tables' fire-aspect condition.
func (m *Mob) HurtByMelee() bool { return m.hurtByMelee }

// AttackerID is the entity ID of the last player or mob to hurt the mob,
// whose looting and fire aspect the loot tables read.
func (m *Mob) AttackerID() int32 { return m.retaliate }

// LastAttackerID is the last living attacker, remembered for 100 ticks independently of AI.
// vanilla: LivingEntity.getLastHurtByMob, LivingEntity.tick
func (m *Mob) LastAttackerID() int32 {
	if m.Age-m.lastAttackerAge > 100 {
		return 0
	}
	return m.lastAttackerID
}

// BlowAttackerID is the entity ID behind the last landed hurt, 0 for none;
// the loot context's attacker comes from it, not AttackerID.
// vanilla: LivingEntity.dropFromLootTable, DamageSource.getEntity
func (m *Mob) BlowAttackerID() int32 { return m.blowAttacker }

// NoteBlowDirect records the projectile entity type behind the hurt that just
// landed. vanilla: DamageSource.getDirectEntity
func (m *Mob) NoteBlowDirect(entityType string) { m.blowDirect = entityType }

// BlowDirectType is the projectile entity type behind the last landed hurt,
// "" for none. vanilla: DamageSource.getDirectEntity
func (m *Mob) BlowDirectType() string { return m.blowDirect }

// PlayerKillCredit reports whether a player hurt this mob within the
// 100-tick memory, which LivingEntity.dropExperience asks before dropping
// experience.
func (m *Mob) PlayerKillCredit() bool { return m.playerMemoryTicks > 0 }

// LastHurtByPlayer reports whether the last attacker is the player holding
// the kill credit (a later mob's blow takes it away).
// vanilla: SculkCatalystBlockEntity.CatalystListener.tryAwardItSpreadsAdvancement
func (m *Mob) LastHurtByPlayer() bool {
	return m.playerMemoryTicks > 0 && m.retaliate != 0 && m.retaliate == m.playerCreditID
}

// PlayerCreditID is the entity ID of the player behind PlayerKillCredit.
// vanilla: LivingEntity.lastHurtByPlayer
func (m *Mob) PlayerCreditID() int32 { return m.playerCreditID }

// plainHurt is a hurt with nobody behind it (a fall, standing fire). The
// window weighs the raw amount before armour, and armour takes its share
// only of the part that lands (none for bypasses-armor). It resets the
// despawn's idleness but shoves and scares no one: a cow that walks off a
// cliff does not run from it. creative lets the blow reach an invulnerable
// body. Results are landed and softened (the window cut it to its excess).
// vanilla: LivingEntity.hurtServer, actuallyHurt
func (m *Mob) plainHurt(amount float64, damageType registry.DamageType, creative bool) (landed, softened bool) {
	return m.plainHurtBy(amount, damageType, creative, false, hurtContext{})
}

// plainHurtBy is plainHurt with livingCause, which skips an enderman's
// teleport roll.
// vanilla: EnderMan.hurtServer
func (m *Mob) plainHurtBy(amount float64, damageType registry.DamageType, creative, livingCause bool, ctx hurtContext) (landed, softened bool) {
	if m.dragon != nil {
		before := m.Health
		defer func() { m.dragonSittingDamage(before) }()
		defer dragonHurtResult(m.dragonAccepts(amount), &landed, &softened)
	}

	if m.Dead || amount <= 0 {
		return false, false
	}
	if m.sulfurBlocked(damageType) {
		m.sulfurHurtHook(damageType)
		if !damageType.InTag(registry.DamageTypeTagIDNoKnockback) {
			m.hitKnockback(nil, 0, 0, amount)
		}
		return true, true
	}
	if !livingCause {
		m.endermanHurtRoll(damageType, creative)
	}
	mayLand, soft := m.throughWindow(amount, damageType, creative, livingCause, false, ctx)
	if mayLand == 0 {
		return false, false
	}
	if !rules.ArmourBypassed(damageType) {
		mayLand = m.armoured(mayLand)
	}
	mayLand = m.afterResistance(mayLand, damageType)
	crack := m.golemCrackLevel()
	if !m.wolfArmorTakes(mayLand, damageType) {
		m.loseHealth(m.absorb(mayLand))
	}
	m.golemCracked = m.golemCracked || m.golemCrackLevel() != crack
	m.hurtByPlayer = false
	m.hurtByMelee = false
	m.NoActionTicks = 0
	m.resetAmbientSoundTime()
	m.noteSquidHurt()
	if m.Health == 0 {
		m.dies(damageType, soft)
	}
	return true, soft
}

// noteLastDamage records a landed hurt's type and tick for panic.
// vanilla: LivingEntity's lastDamageSource
func (m *Mob) noteLastDamage(damageType registry.DamageType) {
	// vanilla: LivingEntity.hurtServer stops the sleep
	m.WakeSleeper()
	m.noteHurtType(damageType)
	m.lastDamageType = damageType
	m.blowDirect = ""
	m.lastDamageStamp = m.Age
	m.blowAttacker = 0
	m.noteReinforcement(0)
	m.rollInfested()
}

// lastDamageSource is the last landed hurt's type while fresh; zero after 40
// ticks or when none is recorded.
// vanilla: LivingEntity.getLastDamageSource
func (m *Mob) lastDamageSource() registry.DamageType {
	if m.Age-m.lastDamageStamp > lastDamageExpiry {
		return 0
	}
	return m.lastDamageType
}

// LastDamageType is the last landed hurt's damage type while fresh, zero for
// none. vanilla: LivingEntity.getLastDamageSource
func (m *Mob) LastDamageType() registry.DamageType { return m.lastDamageSource() }

// HurtByMob is another mob's blow (Mob.doHurtTarget's mob_attack): window
// applies, knockback is away from the attacker's feet, and the attacker is
// recorded for the victim's HurtByTargetGoal. It is no player's, so
// killed_by_player reads false. The result says the hurt landed.
func (m *Mob) HurtByMob(amount float64, source *Mob) bool {
	return m.HurtByMobOwned(amount, source, 0)
}

// HurtByMobOwned is HurtByMob with ownerID, the entity ID of a tame wolf
// source's owner in this level (0 when absent), who takes the player credit.
func (m *Mob) HurtByMobOwned(amount float64, source *Mob, ownerID int32) bool {
	return m.hurtByMob(amount, rules.DamageTypeMobAttack, source, ownerID, nil)
}

// HurtByRam is HurtByMob for a goat's ram: mob_attack_no_aggro, so the victim
// does not retaliate or call reinforcements.
// vanilla: DamageSources.noAggroMobAttack, DamageTypeTags.NO_ANGER
func (m *Mob) HurtByRam(amount float64, source *Mob) bool {
	return m.HurtByMobAs(amount, rules.DamageTypeMobAttackNoAggro, source)
}

// HurtByMobAs is HurtByMob under another damage type (a guardian's thorns
// or beam), still knocked back by the direct entity.
// vanilla: LivingEntity.hurtServer
func (m *Mob) HurtByMobAs(amount float64, damageType registry.DamageType, source *Mob) bool {
	return m.hurtByMob(amount, damageType, source, 0, nil)
}

// HurtByMobVia is HurtByMobAs for a blow whose direct entity is not source
// (an evoker's fangs): source is the causing entity, and the shove points
// away from (fromX, fromZ), the direct entity's position.
// vanilla: LivingEntity.hurtServer, DamageSource.getSourcePosition
func (m *Mob) HurtByMobVia(amount float64, damageType registry.DamageType, source *Mob, fromX, fromZ float64) bool {
	return m.hurtByMob(amount, damageType, source, 0, &[2]float64{fromX, fromZ})
}

// hurtByMob's push, when non-nil, is the point the shove points away from
// instead of source's feet.
func (m *Mob) hurtByMob(amount float64, damageType registry.DamageType, source *Mob, ownerID int32, push *[2]float64) (landed bool) {
	if m.dragon != nil {
		defer func(accepted bool) { landed = landed || accepted }(m.dragonAccepts(amount))
	}
	m.noteWardenHurt(source.ID, true)
	if m.Dead || amount <= 0 || m.Type == TypeBreeze && source.Type == TypeBreeze || m.witherSpares(source.Type) {
		return false
	}
	from := source.HitSource()
	// The source's feet are read when the shove is applied: throughWindow
	// can run a listener that moves it.
	pushFrom := func() (float64, float64) {
		if push != nil {
			return push[0], push[1]
		}
		return source.X, source.Z
	}
	if m.sulfurBlocked(damageType) {
		fromX, fromZ := pushFrom()
		m.hitKnockback(&from, fromX-m.X, fromZ-m.Z, amount)
		return true
	}
	mayLand, soft := m.throughWindow(amount, damageType, false, true, false, hurtContext{causeID: source.ID})
	if mayLand == 0 {
		return false
	}
	if !m.wolfArmorTakes(mayLand, damageType) {
		if !rules.ArmourBypassed(damageType) {
			mayLand = m.armoured(mayLand)
		}
		m.loseHealth(m.absorb(m.afterResistance(mayLand, damageType)))
	}
	m.hurtByPlayer = false
	m.hurtByMelee = false
	// A tame wolf's hurt credits its owner, who may be offline (ID 0); an
	// ownerless one wipes the credit.
	// vanilla: LivingEntity.resolvePlayerResponsibleForDamage
	if source.Type == TypeWolf && source.Tamable.Tame {
		m.playerMemoryTicks, m.playerCreditID = 0, 0
		if source.Tamable.Owner != uuid.Nil {
			m.playerMemoryTicks, m.playerCreditID = playerMemoryTicks, ownerID
		}
	}
	if !damageType.InTag(registry.DamageTypeTagIDNoAnger) {
		m.setRetaliate(source.ID)
		m.noteReinforcement(source.ID)
	}
	m.NoActionTicks = 0
	m.noteLastDamage(damageType)
	m.blowAttacker = source.ID
	m.noteSquidHurt()
	if m.Health == 0 {
		m.dies(damageType, soft)
	}
	if soft {
		return true
	}
	fromX, fromZ := pushFrom()
	m.hitKnockback(&from, fromX-m.X, fromZ-m.Z, amount)
	return true
}

// HurtByArrow is an arrow's blow: window applies, a whole blow's knockback
// follows the arrow's motion rather than its position, and the shooter is
// kept for retaliation. punch's extra shove rides every landed hit, softened
// ones included; the default shove needs a whole hurt. Kill credit goes to
// a player shooter only. landed decides whether the arrow sticks or
// bounces; softened says only the excess went through.
func (m *Mob) HurtByArrow(amount, flightVX, flightVZ, punch float64, shooterID int32, shooterIsPlayer, shooterCreative bool) (landed, softened bool) {
	return m.HurtByArrowFrom(amount, flightVX, flightVZ, punch, shooterID, shooterIsPlayer, shooterCreative, nil)
}

// HurtByArrowFrom is HurtByArrow with the shooter's stance, which a cube
// holding a block bends its shove by.
func (m *Mob) HurtByArrowFrom(amount, flightVX, flightVZ, punch float64, shooterID int32, shooterIsPlayer, shooterCreative bool, from *HitSource) (landed, softened bool) {
	return m.hurtByShot(amount, flightVX, flightVZ, punch, shooterID, shooterIsPlayer, shooterCreative, rules.DamageTypeArrow, from)
}

// HurtByTrident is HurtByArrow under the trident's damage type, which the
// death's loot asks about.
// vanilla: ThrownTrident.onHitEntity, AbstractArrow.doKnockback
func (m *Mob) HurtByTrident(amount, flightVX, flightVZ, punch float64, shooterID int32, shooterIsPlayer, shooterCreative bool) (landed, softened bool) {
	return m.HurtByTridentFrom(amount, flightVX, flightVZ, punch, shooterID, shooterIsPlayer, shooterCreative, nil)
}

// HurtByTridentFrom is HurtByTrident with the thrower's stance.
func (m *Mob) HurtByTridentFrom(amount, flightVX, flightVZ, punch float64, shooterID int32, shooterIsPlayer, shooterCreative bool, from *HitSource) (landed, softened bool) {
	return m.hurtByShot(amount, flightVX, flightVZ, punch, shooterID, shooterIsPlayer, shooterCreative, rules.DamageTypeTrident, from)
}

// hurtByShot is the shared body of HurtByArrow and HurtByTrident.
func (m *Mob) hurtByShot(amount, flightVX, flightVZ, punch float64, shooterID int32, shooterIsPlayer, shooterCreative bool, damageType registry.DamageType, from *HitSource) (landed, softened bool) {
	if m.DragonSitting() {
		return false, false
	}
	if m.dragon != nil {
		before := m.Health
		defer func() { m.dragonSittingDamage(before) }()
		defer dragonHurtResult(m.dragonAccepts(amount), &landed, &softened)
	}

	m.noteWardenHurt(shooterID, false)
	m.enderman.dodged = false
	if m.Dead || amount <= 0 {
		return false, false
	}
	if m.ShulkerClosed() || m.witherDeflects() {
		// vanilla: Shulker.hurtServer, WitherBoss.hurtServer
		return false, false
	}
	if from != nil && m.witherSpares(from.Type) {
		return false, false
	}
	if dodges, teleported := m.dodgesProjectile(damageType, shooterCreative); dodges {
		return teleported, true
	}
	if m.sulfurBlocked(damageType) {
		m.hitKnockback(from, -flightVX, -flightVZ, amount)
		return true, true
	}
	mayLand, soft := m.throughWindow(amount, damageType, shooterCreative, true, shooterIsPlayer, hurtContext{causeID: shooterID})
	if mayLand == 0 {
		return false, false
	}
	if !m.wolfArmorTakes(mayLand, damageType) {
		m.loseHealth(m.absorb(m.afterResistance(m.armoured(mayLand), damageType)))
	}
	m.NoActionTicks = 0
	m.resetAmbientSoundTime()
	if shooterIsPlayer {
		m.hurtByPlayer = true
		m.notePlayerHit()
		m.playerMemoryTicks = playerMemoryTicks
		m.playerCreditID = shooterID
	} else {
		m.hurtByPlayer = false
	}
	m.hurtByMelee = false
	m.setRetaliate(shooterID)
	m.noteReinforcement(shooterID)
	m.noteLastDamage(damageType)
	m.blowAttacker = shooterID
	m.noteSquidHurt()
	if m.Health == 0 {
		m.dies(damageType, soft)
	}
	if soft {
		m.arrowPunch(flightVX, flightVZ, punch)
		return true, true
	}
	m.hitKnockback(from, -flightVX, -flightVZ, amount)
	m.arrowPunch(flightVX, flightVZ, punch)
	return true, false
}

// arrowPunch adds Punch's push along the flight after the hurt's own,
// scaled by knockback resistance.
// vanilla: AbstractArrow.doKnockback, Entity.push
func (m *Mob) arrowPunch(flightVX, flightVZ, punch float64) {
	vx, vy, vz := rules.ArrowPunch(flightVX, flightVZ, punch, math.Max(0, 1-m.KnockbackResistance()))
	if vx == 0 && vy == 0 && vz == 0 {
		return
	}
	m.VX += vx
	m.VY += vy
	m.VZ += vz
	m.Impulse = true
}

// HurtByThrown is a thrown projectile's blow. Even at zero damage (an egg)
// the window opens, the knockback shoves and the voice sounds; inside the
// window only the excess lands. Knockback follows the flight with the old
// velocity halved, or vanilla's jitter roll when the flight has all but
// stopped. The thrower is kept for retaliation regardless. Results are
// landed and softened.
// vanilla: calculateHorizontalHurtKnockbackDirection, hurtServer
func (m *Mob) HurtByThrown(amount, flightVX, flightVZ float64, throwerID int32, throwerIsPlayer, throwerCreative bool, from ...*HitSource) (landed, softened bool) {
	if dodges, teleported := m.dodgesProjectile(rules.DamageTypeThrown, throwerCreative); dodges {
		return teleported, true
	}
	return m.hurtByDamage(amount, rules.DamageTypeThrown, throwerID, throwerIsPlayer, throwerCreative, -flightVX, -flightVZ, from...)
}

// HurtByWindCharge is a wind charge's blow, knocked back against the flight.
// vanilla: AbstractWindCharge.onHitEntity, Projectile.calculateHorizontalHurtKnockbackDirection
func (m *Mob) HurtByWindCharge(amount, flightVX, flightVZ float64, throwerID int32, throwerIsPlayer, throwerCreative bool, from ...*HitSource) (landed, softened bool) {
	if m.DragonSitting() {
		return false, false
	}
	if m.witherDeflects() {
		return false, false
	}
	if dodges, teleported := m.dodgesProjectile(rules.DamageTypeWindCharge, throwerCreative); dodges {
		return teleported, true
	}
	return m.hurtByDamage(amount, rules.DamageTypeWindCharge, throwerID, throwerIsPlayer, throwerCreative, -flightVX, -flightVZ, from...)
}

// HurtByProjectile is a hurting projectile's blow of the type, knocked back
// against the flight.
// vanilla: Projectile.calculateHorizontalHurtKnockbackDirection
func (m *Mob) HurtByProjectile(amount float64, damageType registry.DamageType, flightVX, flightVZ float64, shooterID int32, shooterIsPlayer, shooterCreative bool, from ...*HitSource) (landed, softened bool) {
	if dodges, teleported := m.dodgesProjectile(damageType, shooterCreative); dodges {
		return teleported, true
	}
	return m.hurtByDamage(amount, damageType, shooterID, shooterIsPlayer, shooterCreative, -flightVX, -flightVZ, from...)
}

// lavaContact runs a tick in lava: fire topped up to fifteen seconds and the
// lava's bite through armour, reported even when it kills. Fire-immune
// kinds are spared. A lava cauldron in the box counts though it is not a
// fluid.
// vanilla: LavaFluid.entityInside, LavaCauldronBlock.entityInside
func (m *Mob) lavaContact(w World, scan *contactScan, events []MobEvent) []MobEvent {
	if m.kind().fireImmune {
		return events
	}
	if m.InLava() || m.touches(w, scan, contactLavaCauldron) {
		livingLavaContact(w, m.mobBox(), m.InLava(), func(ticks int, x, y, z int32) {
			m.IgniteForTicksBy(ticks, CombustSource{HasBlock: true, X: x, Y: y, Z: z})
		}, func(amount float64, typ registry.DamageType) { events = m.burnBite(amount, typ, events) })
	}
	return events
}

// powderSnowFlakes kicks up, while the feet are in powder snow and the body
// moves sideways, a snowflake above each powder snow cell of the box on half
// the ticks. A body its rider steers moves by the rider's known movement.
// vanilla: PowderSnowBlock.entityInside, Entity.getKnownMovement
func (m *Mob) powderSnowFlakes(w World, players []Player, events []MobEvent) []MobEvent {
	vx, vz := m.VX, m.VZ
	if p := m.controllingPlayer(players); p != nil {
		vx, vz = p.KnownVX, p.KnownVZ
	}
	if vx == 0 && vz == 0 {
		return events
	}
	feet := m.cell()
	if !rules.IsPowderSnow(w.BlockAt(feet.X, feet.Y, feet.Z)) {
		return events
	}
	m.mobBox().insideCells(func(x, y, z int32) {
		if !rules.IsPowderSnow(w.BlockAt(x, y, z)) || m.Rand.Intn(2) == 0 {
			return
		}
		sx := float64(m.Rand.Float32()*2-1) * 0.083333336
		sz := float64(m.Rand.Float32()*2-1) * 0.083333336
		events = append(events, MobEvent{Mob: m, Kind: MobSparkled, Status: protocol.ParticleSnowflake,
			X: m.X, Y: float64(y + 1), Z: m.Z, VX: sx, VY: 0.05, VZ: sz, Alternative: true})
	})
	return events
}

// controllingPlayer is the player steering the mob, nil for none.
// vanilla: Entity.getControllingPassenger
func (m *Mob) controllingPlayer(players []Player) *Player {
	if m.Horse.RiderID == 0 || !m.Steered() {
		return nil
	}
	for i := range players {
		if players[i].EntityID == m.Horse.RiderID {
			return &players[i]
		}
	}
	return nil
}

// powderSnowContact extinguishes a burning body in powder snow and reports
// each cell for the server to melt. It runs after the fire and lava
// contacts, as EXTINGUISH follows the ignites in a block pass.
// vanilla: PowderSnowBlock.entityInside, InsideBlockEffectType
func (m *Mob) powderSnowContact(w World, scan *contactScan, events []MobEvent) []MobEvent {
	if m.FireTicks <= 0 || !m.touches(w, scan, contactPowderSnow) {
		return events
	}
	var cells [][3]int32
	m.mobBox().insideCells(func(x, y, z int32) {
		if rules.IsPowderSnow(w.BlockAt(x, y, z)) {
			cells = append(cells, [3]int32{x, y, z})
		}
	})
	if len(cells) == 0 {
		return events
	}
	if m.OnFire() {
		events = append(events, MobEvent{Mob: m, Kind: MobExtinguished})
	}
	m.SetMetadataFireTicks(0)
	for _, c := range cells {
		events = append(events, MobEvent{Mob: m, Kind: MobMeltedSnow,
			X: float64(c[0]), Y: float64(c[1]), Z: float64(c[2])})
	}
	return events
}

// cauldronContact extinguishes a burning body in layered cauldrons and
// reports each for the server to drop a level.
// vanilla: LayeredCauldronBlock.entityInside
func (m *Mob) cauldronContact(w World, scan *contactScan, events []MobEvent) []MobEvent {
	if m.FireTicks <= 0 || !m.touches(w, scan, contactLayeredCauldron) {
		return events
	}
	var cells [][3]int32
	m.mobBox().insideCells(func(x, y, z int32) {
		if rules.IsLayeredCauldron(w.BlockAt(x, y, z)) {
			cells = append(cells, [3]int32{x, y, z})
		}
	})
	if len(cells) == 0 {
		return events
	}
	if m.OnFire() {
		events = append(events, MobEvent{Mob: m, Kind: MobExtinguished})
	}
	m.SetMetadataFireTicks(0)
	for _, c := range cells {
		events = append(events, MobEvent{Mob: m, Kind: MobDousedCauldron,
			X: float64(c[0]), Y: float64(c[1]), Z: float64(c[2])})
	}
	return events
}

// witherRoseContact plants wither on a body standing in a wither rose,
// unless the world is peaceful or the body is immune to wither damage.
// vanilla: WitherRoseBlock.entityInside
func (m *Mob) witherRoseContact(w World, scan *contactScan, events []MobEvent) []MobEvent {
	difficulty := w.Difficulty()
	if difficulty == rules.DifficultyPeaceful || !m.touches(w, scan, contactWitherRose) {
		return events
	}
	flowered := false
	m.mobBox().insideCells(func(x, y, z int32) {
		if rules.IsWitherRose(w.BlockAt(x, y, z)) {
			flowered = true
		}
	})
	if !flowered {
		return events
	}
	in, ok := rules.WitherRoseEffect(difficulty, m.InvulnerableTo(rules.DamageTypeWither, false))
	if !ok {
		return events
	}
	if change, added := m.AddEffect(in); added {
		events = append(events, MobEvent{Mob: m, Kind: MobEffectChanged, EffectChange: change})
	}
	return events
}

// cropContact has a ravager destroy each crop it stands in, with drops, when
// mob_griefing is on.
// vanilla: CropBlock.entityInside, PitcherCropBlock.entityInside
func (m *Mob) cropContact(w World, scan *contactScan, events []MobEvent) []MobEvent {
	if m.Type != TypeRavager || !w.MobGriefing() || !m.touches(w, scan, contactCrop) {
		return events
	}
	m.mobBox().insideCells(func(x, y, z int32) {
		if rules.IsTrampledCrop(w.BlockAt(x, y, z)) {
			events = append(events, MobEvent{Mob: m, Kind: MobBrokeBlock, X: float64(x), Y: float64(y), Z: float64(z)})
		}
	})
	return events
}

// eyeblossomContact poisons a bee inside an open eyeblossom.
// vanilla: EyeblossomBlock.entityInside
func (m *Mob) eyeblossomContact(w World, scan *contactScan, events []MobEvent) []MobEvent {
	if m.Type != TypeBee {
		return events
	}
	difficulty := w.Difficulty()
	_, hasPoison := m.effects.Get(mobEffectPoison)
	// Peaceful and an existing poison exclude the effect before any block is read.
	// vanilla: EyeblossomBlock.entityInside
	if hasPoison || difficulty == rules.DifficultyPeaceful || !m.touches(w, scan, contactEyeblossom) {
		return events
	}
	var in effect.Instance
	var ok bool
	m.mobBox().insideCells(func(x, y, z int32) {
		state := w.BlockAt(x, y, z)
		if !ok && registry.BlockOf(state) == registry.BlockOpenEyeblossom {
			in, ok = rules.EyeblossomPoison(difficulty, state, hasPoison)
		}
	})
	if !ok {
		return events
	}
	if change, added := m.AddEffect(in); added {
		events = append(events, MobEvent{Mob: m, Kind: MobEffectChanged, EffectChange: change})
	}
	return events
}

// fireContact runs a tick in fire blocks: the burn is reset to the full
// eight seconds (so walking back in relights fully) and the block bites
// through armour, soul fire double, reported even when it kills. A lit
// campfire bites without lighting. Fire-immune kinds are spared.
// vanilla: BaseFireBlock.fireIgnite — igniteForSeconds(8) tops the counter
// back up to the 160 the fire keeps it at
func (m *Mob) fireContact(w World, scan *contactScan, events []MobEvent) []MobEvent {
	if m.kind().fireImmune || !m.touches(w, scan, contactFire) {
		return events
	}
	fire := m.FireTicks
	livingFireContact(w, m.mobBox(), &fire, func(ticks int, x, y, z int32) {
		m.SetMetadataFireTicks(fire)
		m.IgniteForTicksBy(ticks, CombustSource{HasBlock: true, X: x, Y: y, Z: z})
		fire = m.FireTicks
	}, func(amount float64, typ registry.DamageType) {
		m.SetMetadataFireTicks(fire)
		events = m.burnBite(amount, typ, events)
		fire = m.FireTicks
	})
	m.SetMetadataFireTicks(fire)
	return events
}

// hotFloorContact hurts a body on a magma block unless it wears frost walker
// boots, and not a crouching cat or ocelot.
// vanilla: MagmaBlock.stepOn, Entity.getOnPosLegacy
func (m *Mob) hotFloorContact(w World, events []MobEvent) []MobEvent {
	if !m.OnGround || m.SteppingCarefully() {
		return events
	}
	if !rules.IsHotFloor(w.BlockAt(int32(math.Floor(m.X)), int32(math.Floor(m.Y-0.2)), int32(math.Floor(m.Z)))) ||
		rules.ImmuneToDamage(m.enchPieces(), &rules.EnchContext{DamageType: rules.DamageTypeHotFloor, Direct: true}) {
		return events
	}
	return m.burnBite(1, rules.DamageTypeHotFloor, events)
}

// SteppingCarefully reports whether stepOn of magma, redstone ore, slime and
// turtle eggs is skipped; only cats and ocelots set Crouching.
// vanilla: Cat.isSteppingCarefully, Ocelot.isSteppingCarefully
func (m *Mob) SteppingCarefully() bool { return m.Crouching }

// stepOnBlock reports the block a standing body steps on: a sculk sensor
// (not for a warden) or unlit redstone ore (not for a crouching cat or ocelot).
// vanilla: SculkSensorBlock.stepOn, RedStoneOreBlock.stepOn, Entity.getOnPosLegacy
func (m *Mob) stepOnBlock(w World, events []MobEvent) []MobEvent {
	if !m.OnGround {
		return events
	}
	x, y, z, state := rules.OnPosCell(w, m.X, m.Y, m.Z, m.width()/2)
	kind := MobSteppedOnSculk
	switch b := registry.BlockOf(state); {
	case b == registry.BlockSculkSensor || b == registry.BlockCalibratedSculkSensor:
		if m.Type == TypeWarden {
			return events
		}
	case rules.IsRedstoneOre(state) && !registry.StateLit(state):
		if m.SteppingCarefully() {
			return events
		}
		kind = MobSteppedOnRedstoneOre
	default:
		return events
	}
	return append(events, MobEvent{Mob: m, Kind: kind, X: float64(x), Y: float64(y), Z: float64(z)})
}

// burnBite lands a contact bite through armour and the window, reporting it
// unless the window softened it.
func (m *Mob) burnBite(amount float64, damageType registry.DamageType, events []MobEvent) []MobEvent {
	if landed, soft := m.plainHurt(amount, damageType, false); landed {
		m.noteLastDamage(damageType)
		if !soft {
			events = append(events, MobEvent{Mob: m, Kind: MobBurned, DamageType: damageType})
		}
	}
	return events
}

// cactusContact hurts a body once per cactus cell in its box, through
// armour, reported even when it kills.
// vanilla: CactusBlock.entityInside
func (m *Mob) cactusContact(w World, scan *contactScan, events []MobEvent) []MobEvent {
	if !m.touches(w, scan, contactCactus) {
		return events
	}
	m.mobBox().insideCells(func(x, y, z int32) {
		if !rules.IsCactus(w.BlockAt(x, y, z)) {
			return
		}
		events = m.burnBite(float64(cactusDamage), rules.DamageTypeCactus, events)
	})
	return events
}

// berryBushContact pricks a body for 1 per ripe bush cell in its box after a
// walk of (dx, dz); foxes and bees are immune.
// vanilla: SweetBerryBushBlock.entityInside
func (m *Mob) berryBushContact(w World, scan *contactScan, seated bool, dx, dz float64, events []MobEvent) []MobEvent {
	if seated || m.Type == TypeFox || m.Type == TypeBee || !rules.BerryBushMoving(dx, dz) || !m.touches(w, scan, contactBerryBush) {
		return events
	}
	m.mobBox().insideCells(func(x, y, z int32) {
		if state := w.BlockAt(x, y, z); rules.IsSweetBerryBush(state) && rules.BerryBushBears(state) {
			events = m.burnBite(1, rules.DamageTypeSweetBerryBush, events)
		}
	})
	return events
}

// fireStep runs a tick of standing fire (Entity.baseTick): rain or water
// puts it out (reported as MobExtinguished); otherwise one fire damage
// lands every 20th tick, sourceless, and not while lava touches the box
// (isInLava, not just the feet cell), and the counter drops by one. Every
// kind runs it, whatever lit the fire.
func (m *Mob) fireStep(w World, events []MobEvent) []MobEvent {
	if m.FireTicks == 0 {
		return events
	}
	wet := m.inRain(w) || rules.IsWater(w.BlockAt(m.cell().X, m.cell().Y, m.cell().Z))
	fire := m.FireTicks
	extinguished := livingFireTick(&fire, wet, m.kind().fireImmune, m.InLava(), func(amount float64, typ registry.DamageType) {
		events = m.burnBite(amount, typ, events)
		fire = m.FireTicks
	})
	m.SetMetadataFireTicks(fire)
	if extinguished {
		events = append(events, MobEvent{Mob: m, Kind: MobExtinguished})
	}
	return events
}

// wallStep is LivingEntity.baseTick's isInWall suffocation: in_wall bypasses
// armour and is paced by the hurt cooldown. Mobs are not nudged out of
// walls (vanilla only nudges players and items), so a falling block that
// lands on one kills it.
func (m *Mob) wallStep(w World, events []MobEvent) []MobEvent {
	if m.Type == TypeVex {
		// vanilla: Entity.isInWall, false while Vex.tick holds noPhysics
		return events
	}
	if !rules.EyesInWall(w, m.X, m.Y, m.Z, m.width(), m.eyeHeight()) {
		return events
	}
	if landed, soft := m.plainHurt(1, rules.DamageTypeInWall, false); landed {
		m.noteLastDamage(rules.DamageTypeInWall)
		if !soft {
			events = append(events, MobEvent{Mob: m, Kind: MobBurned, DamageType: rules.DamageTypeInWall})
		}
	}
	return events
}

// voidStep hurts a body below minY - 64 with out_of_world every tick; the
// window paces it to one hit per ten ticks.
// vanilla: Entity.checkBelowWorld, LivingEntity.onBelowWorld
func (m *Mob) voidStep(w World, events []MobEvent) []MobEvent {
	if m.Y >= float64(w.MinY()-64) {
		return events
	}
	return m.burnBite(rules.VoidDamage, rules.DamageTypeOutOfWorld, events)
}

// Tick advances the mob one game tick against w. Its phases: timers count
// down; the goal selector picks this tick's goals and navigation walks the
// path; the body moves; standing conditions settle (fire, daylight burn,
// despawn idleness); and anything to tell comes back as events. A dead mob
// ticks once more to report it. This form pushes against no other mobs;
// the collection's Tick supplies them.
func (m *Mob) Tick(w World, players []Player) []MobEvent {
	return m.tick(w, players, nil)
}

// tick is Tick with the world's other mobs, which goals scan and pushing
// reads when the collection's grid is set.
func (m *Mob) tick(w World, players []Player, others []*Mob) []MobEvent {
	return m.tickInto(nil, new(tickView), w, players, others)
}

// tickInto is tick appending its events to out, so a collection's tick fills
// one slice instead of a fresh one per mob. tick is scratch the caller reuses
// across mobs; tickInto resets it.
func (m *Mob) tickInto(out []MobEvent, tick *tickView, w World, players []Player, others []*Mob) []MobEvent {
	if m.Dead {
		return m.deathStep(out, w, players, others)
	}
	if m.discarded {
		m.gone = true
		return append(out, MobEvent{Mob: m, Kind: MobVanished})
	}
	if m.gone {
		// The despawn already reported it.
		return out
	}
	if m.Health <= 0 && m.Type != TypeEnderDragon {
		return m.deathStep(out, w, players, others)
	}
	m.ticked = true
	if m.RidesVehicle {
		// vanilla: Entity.rideTick
		m.VX, m.VY, m.VZ = 0, 0, 0
	}

	if m.Type == TypeSulfurCube {
		m.sulfurStep()
	}
	// vanilla: ZombieVillager.tick cures independently of NoAI.
	if m.Type == TypeZombieVillager && m.cureStep(w) && m.mayConvert(TypeVillager) {
		return m.converted(out, TypeVillager)
	}

	// vanilla: Skeleton.tick, Hoglin.customServerAiStep convert only a body with its AI.
	if !m.NoAI {
		into := m.freezeConversionStep()
		if into == NoMobType {
			into = m.hoglinConversionStep(w)
		}
		if into != NoMobType && m.mayConvert(into) {
			return m.converted(out, into)
		}
	}

	if m.dragon != nil {
		return m.dragonTick(w, players, others, out)
	}
	m.Age++
	startX, startY, startZ := m.X, m.Y, m.Z
	if m.motion.set {
		m.motion.speed = [3]float64{m.X - m.motion.last[0], m.Y - m.motion.last[1], m.Z - m.motion.last[2]}
	}
	m.motion.last, m.motion.set = [3]float64{m.X, m.Y, m.Z}, true
	out = m.forcedAgeStep(out)
	// Ageing: a baby counts up from -24000 to 0 and grows (the server sends
	// the metadata bit when babyhood flips); a bred adult's positive
	// cooldown counts down. Animals only: a zombie's babyhood is a metadata
	// boolean with no growing.
	// vanilla: AgeableMob.aiStep — a locked baby ages not at all.
	grewUp := false
	if ageableAnimal(m.Type) || m.Type == TypeVillager || m.Type == TypeSulfurCube {
		switch {
		// vanilla: SkeletonHorse.canAgeUp, ZombieHorse.canAgeUp: their foals never grow
		case m.BabyAge < 0 && !m.AgeLocked && m.Type != TypeSkeletonHorse && m.Type != TypeZombieHorse:
			oldWidth, oldHeight := m.width(), m.height()
			m.SetMetadataBabyAge(m.BabyAge + 1)
			if m.BabyAge == 0 {
				m.syncSulfurSize()
				// The box just grew; move to the nearest spot it fits.
				m.refreshDimensions(w, oldWidth, oldHeight)
				// The happy ghast swaps its mind on the crossing.
				m.syncFlyingGoals()
				grewUp = true
			}
		case m.BabyAge > 0:
			m.SetMetadataBabyAge(m.BabyAge - 1)
		}
		// vanilla: Animal.aiStep — only a fertile grown body keeps its love.
		if m.BabyAge != 0 {
			m.InLove = 0
		}
		if m.InLove > 0 {
			m.InLove--
		}
	}
	out = m.ageLockStep(out)
	if m.InvulnTicks > 0 {
		m.InvulnTicks--
	}
	if m.playerMemoryTicks > 0 {
		m.playerMemoryTicks--
	}
	events := out
	var scan contactScan
	if grewUp && m.Type == TypeTurtle {
		events = append(events, MobEvent{Mob: m, Kind: MobGrewUp})
	}
	events, diedInBaseTick := m.baseTick(w, &scan, players, events)
	if into := m.waterConversionStep(); into != NoMobType && m.mayConvert(into) {
		return m.converted(events, into)
	}
	if m.Type == TypeTadpole && m.tadpoleAgeStep() && m.mayConvert(TypeFrog) {
		return append(events, MobEvent{Mob: m, Kind: MobConverted, Into: TypeFrog})
	}
	switch m.Type {
	case TypeWitch:
		events = m.witchStep(w, players, others, events)
	case TypeWanderingTrader:
		events = m.traderUseStep(events)
	}

	// Requests reset each tick; goals restate them.
	m.walk.speed = 0
	m.jump = false
	if m.Type == TypeChicken && !m.IsBaby() && !m.ChickenJockey {
		// Only a grown hen lays, and not a jockey's.
		// vanilla: Chicken.aiStep, Chicken.setChickenJockey
		m.EggTimer--
		if m.EggTimer <= 0 {
			events = append(events, MobEvent{Mob: m, Kind: MobLaidEgg})
			m.EggTimer = eggIntervalMin + m.Rand.Intn(eggIntervalMax-eggIntervalMin+1)
		}
	}
	if m.Type == TypeTurtle && m.LayingEgg && m.layEggCounter >= 1 && m.layEggCounter%5 == 0 {
		// vanilla: Turtle.aiStep
		if at := m.cell(); isSandAt(w, at.X, at.Y-1, at.Z) {
			events = append(events, MobEvent{Mob: m, Kind: MobDugSand, X: float64(at.X), Y: float64(at.Y - 1), Z: float64(at.Z)},
				MobEvent{Mob: m, Kind: MobEntityAction})
		}
	}
	if m.Type == TypeCreeper || m.Type == TypeSulfurCube {
		// The creeper's own tick precedes the goals (Creeper.tick before its
		// aiStep): the fuse winds, and at max swell it explodes.
		var exploded bool
		events, exploded = m.fuseStep(w, events)
		if exploded {
			return events
		}
	}

	if IsEquine(m.Type) {
		m.horseStep(w)
	}
	if m.Type == TypeFox {
		events = m.foxStep(w, events)
	}
	*tick = tickView{w: w, players: players, others: others, prey: m.prey, preyKnown: m.preyKnown, stands: m.stands, events: tick.events[:0]}
	if m.push != nil {
		tick.byID = m.push.byID
	}
	wardenHeld := m.Type == TypeWarden && m.wardenStep(tick)
	if m.isPiglin() && !m.NoAI {
		if into := m.piglinStep(tick); into != NoMobType {
			return m.converted(append(events, tick.events...), into)
		}
	}
	if m.hasAnger() {
		m.angerStep(tick)
	}
	if m.Type == TypeZombifiedPiglin {
		m.piglinAlertStep(tick)
	}
	if m.hoglinKind() {
		m.hoglinStep(tick)
	}
	if m.IsBlaze() {
		m.blazeStep(tick)
	}
	if m.IsWither() {
		m.witherStep(tick)
		tick.events = append(tick.events, MobEvent{Mob: m, Kind: MobWitherBar})
	}
	if m.IsCreaking() {
		m.creakingStep(tick)
	}
	if m.Type == TypePolarBear && m.polar.warn > 0 {
		m.polar.warn--
	}
	if m.OfferFlowerTicks > 0 {
		m.OfferFlowerTicks--
	}
	m.animalAIStep(tick)
	if IsNautilus(m.Type) {
		m.nautilusStep(tick)
	}
	if IsCamel(m.Type) {
		m.camelStep(tick)
	}
	m.zeroSmallVelocity()
	// A seated rider runs its goals and standing conditions, and its travel
	// only sets its velocity; the mount's tick places it. It still shoots or
	// swings from the seat.
	// vanilla: Entity.rideTick, LivingEntity.tick
	seated := m.RidesVehicle
	// A horse its rider steers is moved by the client's reports alone.
	// vanilla: LivingEntity.travel, Mob.isControlledByLocalInstance
	steered := (IsEquine(m.Type) || m.Type == TypeStrider || m.Type == TypeHappyGhast || IsNautilus(m.Type)) && m.Steered()
	if steered {
		// The controlling client owns travel; discard the server's old motion.
		// vanilla: LivingEntity.travelRidden
		m.VX, m.VY, m.VZ = 0, 0, 0
	}
	// The ravager's countdowns, trample and roar run with NoAI too.
	// vanilla: Ravager.aiStep
	if m.Type == TypeRavager {
		m.ravagerStep(tick)
	}
	if m.Type == TypeVillager {
		m.villagerGossipStep(tick)
	}
	if wardenHeld && !m.NoAI && !seated {
		m.stopSeeking()
		events = m.step(w, events)
	}
	if !m.NoAI && !steered && !wardenHeld {
		m.brainStep(tick)
		lookHeld := false
		if !m.goalsStandAside() {
			lookHeld = m.ai.run(m, tick)
		}
		if m.Type == TypeBreeze {
			m.breezeStep(tick)
		}
		m.felineStance()
		if m.isPiglin() || m.hoglinKind() {
			// vanilla: PiglinAi.updateActivity, PiglinBruteAi.updateActivity, HoglinAi.updateActivity, Zoglin.updateActivity
			m.SetMetadataAggressive(m.TargetID != 0)
		}
		if m.Type == TypeEvoker || m.Type == TypeIllusioner {
			m.evokerStep()
		}
		// A rearing or grazing horse takes no walking input.
		// vanilla: AbstractHorse.isImmobile, Llama.isImmobile
		rearing := IsEquine(m.Type) && (m.Horse.Standing > 0 || m.Horse.Eating) || m.ravagerImmobile() || m.camelRefusesToMove() || m.ArmadilloScared() || m.Type == TypePanda && !m.pandaCanAct()
		if !seated {
			if !rearing {
				m.moveControl(w)
				if m.Type == TypeRabbit {
					m.rabbitMoveStep(tick)
				}
			}
			if m.Type == TypeDrowned {
				m.drownedMoveStep(tick)
			}
			if m.isCube() && m.cubeMoveStep() && m.Size > 0 {
				tick.events = append(tick.events, MobEvent{Mob: m, Kind: MobJumped, Pitch: m.cubeSoundPitch(m.Rand)})
			}
		}
		// Navigation and move control may change the pitch and head yaw;
		// the look control must turn from those angles before travel.
		// vanilla: Mob.serverAiStep
		m.lookControlTick(lookHeld)
		if !seated {
			if m.walksByStep() {
				fromX, fromY, fromZ := m.X, m.Y, m.Z
				events = m.step(w, events)
				events = m.stepSoundStep(w, m.X-fromX, m.Y-fromY, m.Z-fromZ, events)
			} else if m.is(TypeSquid) {
				// The squid's own movement: goals, aiStep and travel in one.
				tick.events = append(tick.events, m.squidMoveStep(tick)...)
			} else if m.IsFish() {
				tick.events = append(tick.events, m.fishMoveStep(tick)...)
			} else if m.IsGuardian() {
				events = append(events, m.guardianMoveStep(tick)...)
			} else if m.Type == TypeVex {
				m.vexMoveStep(tick)
			} else if m.Type == TypeParrot {
				events = m.parrotMoveStep(tick, events)
			} else if m.kind().flying {
				// The flyer drives FlyingMoveControl, with no gravity.
				// vanilla: FlyingMoveControl
				m.flyMoveStep(tick)
			} else if m.Type == TypeBat {
				tick.events = append(tick.events, m.batMoveStep(tick)...)
			} else if m.Type == TypePhantom {
				tick.events = append(tick.events, m.phantomMoveStep(tick)...)
			}
			if m.Type == TypeRabbit {
				m.rabbitAfterStep(tick)
			}
			// Player.touch on every cube in reach each tick, only by a body with AI
			// (isEffectiveAi).
			for _, p := range m.cubeTouches(w, players) {
				pitch := (m.Rand.Float32()-m.Rand.Float32())*0.2 + 1
				tick.events = append(tick.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: m.cubeAttackDamage(), Player: p, Touch: true, Pitch: pitch})
			}
		} else {
			m.seatedTravel()
		}
	}
	if m.IsShulker() {
		m.shulkerStep(tick)
		seated = m.RidesVehicle
	}
	tick.events = m.dolphinStep(w, tick.events)
	tick.events = m.flapStep(w, seated, startX, startY, startZ, tick.events)
	if m.Type == TypeSulfurCube && !seated {
		tick.events = m.sulfurTouches(players, tick.events)
	}
	if !seated && m.isCube() {
		// AbstractCubeMob.tick's landing squish.
		if m.OnGround && !m.cube.wasOnGround {
			pitch := ((m.Rand.Float32()-m.Rand.Float32())*0.2 + 1) / 0.8
			tick.events = append(tick.events, MobEvent{Mob: m, Kind: MobSquished, Pitch: pitch})
		}
		m.cube.wasOnGround = m.OnGround
	}
	events = m.berryBushContact(w, &scan, seated, m.X-startX, m.Z-startZ, events)
	// vanilla: LivingEntity.pushEntities
	events = m.pushStep(w, players, others, events)
	m.bodyRotationStep(startX, startZ)
	if !seated {
		if m.is(TypeSpider) {
			// Spider.tick sets climbing after the super tick's move, from this move's
			// horizontal collision.
			m.SetMetadataClimbing(m.horizontalCollision)
		}
	}

	// Standing conditions follow the walk that may have moved the body into
	// shade, water or a crowd. A despawn is final: the mob is gone before any
	// other event is told. Every mob runs the idleness counter
	// (Mob.serverAiStep); animals never despawn by it, but strolls read it.
	m.idleStep(w)
	if m.Type == TypeHappyGhast {
		m.happyGhastStep(w, players)
	}
	if m.Type == TypeStrider {
		m.floatStrider(w)
	}
	if (m.Type == TypeStrider || m.Type == TypePig) && m.Horse.Controlled() {
		m.itemSteeringBoostStep()
	}
	if m.kind().burnsInDaylight {
		tick.events = m.burnStep(w, tick.events)
	}
	tick.events = m.ownGolemStep(w, tick.events)
	tick.events = m.golemCrackStep(tick.events)
	if m.Type == TypeWolf {
		tick.events = m.wolfWetStep(w, tick.events)
		tick.events = m.wolfArmorStep(tick.events)
	}
	tick.events = m.ambientStep(w, players, tick.events)
	if m.despawnStep(w, players) || m.traderDespawnStep() {
		m.gone = true
		return append(append(out, tick.events...), MobEvent{Mob: m, Kind: MobVanished})
	}
	events = append(events, tick.events...)

	if m.Type == TypeVex {
		// vanilla: Vex.tick, after the superclass tick even with NoAI.
		m.SetMetadataNoGravity(true)
	}
	return m.reportDeath(events, w, players, others, !diedInBaseTick)
}

// deathStep distinguishes loaded zero health from a killing blow's effects.
// vanilla: LivingEntity.baseTick
func (m *Mob) deathStep(events []MobEvent, w World, players []Player, others []*Mob) []MobEvent {
	m.TickTimers()
	if !m.Dead {
		m.Dead, m.deathTold = true, true
		return append(events, MobEvent{Mob: m, Kind: MobDying})
	}
	if m.deathTold {
		return events
	}
	// The killing blow still rolls its reinforcement call: hurtServer
	// returns true for it too.
	// vanilla: Zombie.hurtServer
	events = m.queuedStep(m.reinforcementStep(w, events))
	return m.reportDeath(events, w, players, others, false)
}

// reportDeath collects the death once, preserving its position relative to tickDeath.
func (m *Mob) reportDeath(events []MobEvent, w World, players []Player, others []*Mob, afterTick bool) []MobEvent {
	if !m.Dead || m.deathTold {
		return events
	}
	m.deathTold = true
	if m.Type == TypeVillager {
		m.tellWitnesses(w, players, others)
	}
	events = m.deathLeashDrop(append(events, MobEvent{Mob: m, Kind: MobDied, AfterTickDeath: afterTick}))
	// The crack sound follows the death sound: the killing blow's crack level
	// change is never reached by the living tick.
	// vanilla: IronGolem.hurtServer
	return m.golemCrackStep(events)
}

// converted ends the body's tick in its conversion into the given kind.
func (m *Mob) converted(events []MobEvent, into MobType) []MobEvent {
	m.gone = true
	return append(events, MobEvent{Mob: m, Kind: MobConverted, Into: into})
}

// ambientStep rolls the idle sound and appends its MobMoaned. Kinds without
// an ambient sound keep the counter still so their dice stream is untouched;
// a body killed earlier this tick makes no sound.
// vanilla: Mob.baseTick
func (m *Mob) ambientStep(w World, players []Player, events []MobEvent) []MobEvent {
	if !m.Alive() || m.kind().sounds.Ambient == registry.SoundNone || m.PlayingDead() {
		return events
	}
	if m.moanStep() && !m.Silent && (!m.isPiglin() || m.piglinIdle()) {
		moan := MobEvent{Mob: m, Kind: MobMoaned}
		if m.Type == TypeFox {
			m.foxScreech(w, players, &moan)
		}
		events = append(events, moan)
	}
	return events
}

// moanStep runs a tick of the idle sound and reports whether it played. The
// die rolls before the post-increment, matching vanilla's tick.
// vanilla: Mob.baseTick
func (m *Mob) moanStep() bool {
	roll := m.Rand.Intn(ambientDie)
	fired := roll < m.ambientTimer
	m.ambientTimer++
	if !fired {
		return false
	}
	m.ambientTimer = -m.ambientSoundInterval()
	// vanilla: Bat.getAmbientSound
	if m.Type == TypeBat && m.Resting && m.Rand.Intn(4) != 0 {
		return false
	}
	return true
}

// ambientSoundInterval is the kind's interval, else Mob's 80; a ridden
// happy ghast's is six times as long.
// vanilla: HappyGhast.getAmbientSoundInterval
func (m *Mob) ambientSoundInterval() int {
	i := m.kind().ambientInterval
	if i <= 0 {
		i = mobAmbientInterval
	}
	if m.Type == TypeHappyGhast && (m.Horse.Rider != uuid.Nil || len(m.Passengers) > 0 || len(m.Horse.ExtraRiderIDs) > 0) {
		i *= 6
	}
	return i
}

// resetAmbientSoundTime is Mob.playHurtSound's ambientSoundTime =
// -getAmbientSoundInterval(), so a struck body does not mutter at once.
func (m *Mob) resetAmbientSoundTime() { m.ambientTimer = -m.ambientSoundInterval() }

// TradeUpdatedVoice is the sound of a merchant window's result changing, if
// the idle counter has run 20 ticks past its reset.
// vanilla: AbstractVillager.notifyTradeUpdated
func (m *Mob) TradeUpdatedVoice(valid bool) (event registry.Sound, ok bool) {
	if m.ambientTimer <= -m.ambientSoundInterval()+20 {
		return registry.SoundNone, false
	}
	m.resetAmbientSoundTime()
	sounds := merchantVoices[m.Type]
	if valid {
		return sounds.yes, true
	}
	return sounds.no, true
}

var merchantVoices = [registry.EntityTypeCount]struct{ yes, no registry.Sound }{
	TypeVillager:        {registry.SoundEntityVillagerYes, registry.SoundEntityVillagerNo},
	TypeWanderingTrader: {registry.SoundEntityWanderingTraderYes, registry.SoundEntityWanderingTraderNo},
}

// TradeNotified resets the idle counter after a trade.
// vanilla: AbstractVillager.notifyTrade
func (m *Mob) TradeNotified() { m.resetAmbientSoundTime() }

// VoicePitch is LivingEntity.getVoicePitch: 1.0 ± 0.2, plus half for a
// baby; the happy ghast's is a flat 1.0.
// vanilla: LivingEntity.getVoicePitch, HappyGhast.getVoicePitch
func (m *Mob) VoicePitch() float32 {
	if m.is(TypeHappyGhast) {
		return 1.0
	}
	pitch := (m.Rand.Float32()-m.Rand.Float32())*0.2 + 1.0
	if m.IsBaby() {
		pitch += 0.5
		if m.Type == TypeZombieVillager {
			// vanilla: ZombieVillager.getVoicePitch
			pitch += 0.5
		}
	}
	if m.Type == TypeBat {
		// vanilla: Bat.getVoicePitch
		pitch *= 0.95
	}
	return pitch
}

// AmbientVoice is the idle sound's event, volume and rolled pitch.
// vanilla: LivingEntity.getVoicePitch, Mob.getSoundSet
func (m *Mob) AmbientVoice() (event registry.Sound, volume, pitch float32) {
	if m.Type == TypeParrot {
		return m.parrotAmbient(), m.SoundVolume(), m.VoicePitch()
	}
	if m.Type == TypeWolf {
		return m.wolfAmbient(), m.SoundVolume(), m.VoicePitch()
	}
	if m.Type == TypeCat {
		return m.catAmbient(), m.SoundVolume(), m.VoicePitch()
	}
	// vanilla: Villager.getAmbientSound, WanderingTrader.getAmbientSound
	if m.Type == TypeVillager {
		if m.Sleeping {
			return registry.SoundNone, 0, 0
		}
		if m.Trading {
			return registry.SoundEntityVillagerTrade, m.SoundVolume(), m.VoicePitch()
		}
	}
	if m.Type == TypeWanderingTrader && m.Trading {
		return registry.SoundEntityWanderingTraderTrade, m.SoundVolume(), m.VoicePitch()
	}
	if m.Type == TypeAllay && m.AllayHolding() {
		return registry.SoundEntityAllayAmbientWithItem, m.SoundVolume(), m.VoicePitch()
	}
	// vanilla: SkeletonHorse.getAmbientSound
	if m.Type == TypeSkeletonHorse && m.EyeInWater() {
		return registry.SoundEntitySkeletonHorseAmbientWater, m.SoundVolume(), m.VoicePitch()
	}
	return m.Sounds().Ambient, m.SoundVolume(), m.VoicePitch()
}

// Mob's ambient sound numbers: the 1000-sided die rolled against the
// counter each tick, and the 80-tick interval (getAmbientSoundInterval).
const (
	ambientDie         = 1000
	mobAmbientInterval = 80
)

func (m *Mob) animalAIStep(t *tickView) {
	if m.Type == TypePanda {
		m.pandaTick(t) // vanilla: Panda.tick runs even without AI
	}
	if m.Type == TypeCat {
		m.catTick(t)
	}
	if m.NoAI {
		if m.Type == TypePufferfish && m.Alive() {
			t.events = m.pufferStings(t, t.events)
		}
		return
	}
	switch m.Type {
	case TypeBee:
		m.beeStep(t)
	case TypeArmadillo:
		m.armadilloStep(t)
	case TypeSniffer:
		m.snifferStep(t)
	case TypeParrot:
		m.parrotStep(t)
	case TypeAllay:
		m.allayStep(t)
	}
}

// loseHealth takes dmg, already reduced by armour, protection and absorption,
// from the health and marks the hurt for TakeDamagedEvent; no damage marks nothing.
// vanilla: LivingEntity.actuallyHurt
func (m *Mob) loseHealth(dmg float64) {
	if dmg <= 0 {
		return
	}
	m.startHurt(dmg)
	m.SetMetadataHealth(math.Max(0, m.Health-dmg))
	m.damagedEvent = true
}

// TakeDamagedEvent reports whether health was lost since the last call, and clears the mark.
func (m *Mob) TakeDamagedEvent() bool {
	d := m.damagedEvent
	m.damagedEvent = false
	return d
}

// SetMemoryMapOrder selects the immutable native memory-map packing context.
func (m *Mob) SetMemoryMapOrder(order *MemoryMapOrder) {
	m.memoryMapOrder = order
}

// TargetWhy is which goal chose a mob's target, for the goals whose choice a
// plugin tells from the closest-entity search.
type TargetWhy struct {
	ID  int32
	Why TargetReasonKind
}

// TargetReasonKind is the goal behind a target.
type TargetReasonKind uint8

const (
	// TargetByHurt is HurtByTargetGoal: the mob, or one it alerted, was hurt.
	TargetByHurt TargetReasonKind = iota + 1
	// TargetByOwnerHurt is OwnerHurtByTargetGoal: the owner was attacked.
	TargetByOwnerHurt
	// TargetByOwnerAttack is OwnerHurtTargetGoal: the owner attacked.
	TargetByOwnerAttack
	// TargetByDefend is DefendTrustedTargetGoal (fox) or DefendVillageTargetGoal (iron golem).
	TargetByDefend
)

// setTargetBy sets the target and records the goal behind it.
func (m *Mob) setTargetBy(id int32, why TargetReasonKind) {
	m.SetMetadataTargetID(id)
	m.TargetWhy = TargetWhy{ID: id, Why: why}
}
