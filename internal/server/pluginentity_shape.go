package server

import (
	"sync/atomic"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

type handleShape uint32

const (
	shapeUnknown handleShape = iota
	shapeArrow
	shapeProjectile
	shapeCube
	shapeSheep
	shapeCollared
	shapeVariant
	shapeHorse
	shapeCreeper
	shapeTameable
	shapeBreedable
	shapeAgeable
	shapeMob
	shapeLiving
	shapePlain
)

// Each saved key used to classify a handle is written by its entity type
// regardless of the value, so handles across worlds can share the shape.
var handleShapes [registry.EntityTypeCount]atomic.Uint32

func cachedHandleShape(e command.Entity, t registry.EntityType) handleShape {
	if t < 0 || t >= registry.EntityTypeCount {
		return uncachedHandleShape(e, t)
	}
	slot := &handleShapes[t]
	if shape := handleShape(slot.Load()); shape != shapeUnknown {
		return shape
	}
	shape := uncachedHandleShape(e, t)
	slot.Store(uint32(shape))
	return shape
}

func uncachedHandleShape(e command.Entity, t registry.EntityType) handleShape {
	if d, ok := e.(command.DataTarget); ok {
		if tag, ok := d.DataTag(); ok {
			if shape := shapeOf(t, tag.Entries()); shape != shapePlain {
				return shape
			}
		}
	}
	if a, ok := e.(command.AttributeTarget); ok && a.HasAttributes() {
		return shapeLiving
	}
	return shapePlain
}

// vanilla: Mob.addAdditionalSaveData, AgeableMob, Animal, TamableAnimal,
// AbstractHorse, Zombie, Projectile, AbstractArrow
func shapeOf(t registry.EntityType, saved nbt.Compound) handleShape {
	has := func(k string) bool { _, ok := saved[k]; return ok }
	// Mobs retain unmodelled NBT fields, including projectile keys a loader
	// would ignore. Such a key cannot identify a projectile on a mob.
	if !has("PersistenceRequired") {
		if has("HasBeenShot") {
			if has("inGround") {
				return shapeArrow
			}
			return shapeProjectile
		}
		return shapePlain
	}
	if shape := pluginVariantOf(t, saved); shape != shapeUnknown {
		return shape
	}
	switch {
	case has("Sitting") && t != registry.EntityTypeFox, has("Tame"):
		return shapeTameable
	case has("InLove"):
		return shapeBreedable
	case has("Age") || has("IsBaby"):
		return shapeAgeable
	default:
		return shapeMob
	}
}

func handleOf(e pluginEntity, shape handleShape) plugin.Entity {
	living, mob, age, love := livingOps{e}, mobOps{e}, ageOps{e}, loveOps{e}
	switch shape {
	case shapeArrow:
		return pluginArrowEntity{e, projOps{e}, arrowOps{e}}
	case shapeProjectile:
		return pluginProjectileEntity{e, projOps{e}}
	case shapeCube:
		return pluginCubeEntity{e, living, mob, cubeOps{e}}
	case shapeSheep:
		return pluginSheepEntity{e, living, mob, age, love, sheepOps{e}}
	case shapeCollared:
		return pluginCollaredEntity{e, living, mob, age, love, tameOps{e}, variantOps{e}, collarOps{e}}
	case shapeVariant:
		return pluginVariantEntity{e, living, mob, age, love, variantOps{e}}
	case shapeHorse:
		return pluginHorseEntity{e, living, mob, age, love, tameOps{e}, horseOps{e}}
	case shapeCreeper:
		return pluginCreeperEntity{e, living, mob, creeperOps{e}}
	case shapeTameable:
		return pluginTameableEntity{e, living, mob, age, love, tameOps{e}}
	case shapeBreedable:
		return pluginBreedableEntity{e, living, mob, age, love}
	case shapeAgeable:
		return pluginAgeableEntity{e, living, mob, age}
	case shapeMob:
		return pluginMobEntity{e, living, mob}
	case shapeLiving:
		return pluginLivingEntity{e, living}
	default:
		return pluginPlainEntity{e}
	}
}
