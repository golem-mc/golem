package world

import (
	"github.com/golem-mc/golem/nbt"
)

// Data versions of the entity fixes of 1.16.5 to 1.20.2, each the version a
// record must be below to need its step.
const (
	arrowPickupVersion    = 2702 // vanilla: DataFixers.addFixers (AbstractArrowPickupFix)
	catVariantVersion     = 3086 // vanilla: DataFixers.addFixers (Change cat variant type)
	frogVariantVersion    = 3087 // vanilla: DataFixers.addFixers (Change frog variant type)
	paintingFieldsVersion = 3090 // vanilla: DataFixers.addFixers (EntityPaintingFieldsRenameFix)
	goatHornsVersion      = 3093 // vanilla: DataFixers.addFixers (EntityGoatMissingStateFix)
	britishShorthairVer   = 3097 // vanilla: DataFixers.addFixers (Rename british shorthair)
	effectDurationVersion = 3322 // vanilla: DataFixers.addFixers (EffectDurationFix)
	projectileItemVersion = 3685 // vanilla: DataFixers.addFixers (FixProjectileStoredItem)
)

var catVariants = [...]string{
	"minecraft:tabby", "minecraft:black", "minecraft:red", "minecraft:siamese", "minecraft:british",
	"minecraft:calico", "minecraft:persian", "minecraft:ragdoll", "minecraft:white", "minecraft:jellie",
	"minecraft:all_black",
}

var frogVariants = [...]string{"minecraft:temperate", "minecraft:warm", "minecraft:cold"}

// upgradeOldEntity applies the entity fixes from 1.16.5 up to the 1.20.2
// shapes to an entity written at version and to its passengers, in data
// version order.
func upgradeOldEntity(e nbt.CompoundTag, version int32) {
	entries := e.Entries()
	id, _ := nbt.Get[nbt.String](entries, "id")
	if version < arrowPickupVersion && (id == "minecraft:arrow" || id == "minecraft:spectral_arrow" || id == "minecraft:trident") {
		if _, ok := entries["pickup"]; !ok {
			pickup := nbt.Byte(1)
			if v, ok := nbt.ByteValue(entries["player"]); ok && v == 0 {
				pickup = 0
			}
			entries["pickup"] = pickup
			delete(entries, "player")
		}
	}
	if id == "minecraft:cat" {
		if version < catVariantVersion {
			variantByIndex(entries, "CatType", "variant", catVariants[:], "minecraft:tabby")
		}
		if version < britishShorthairVer && entries["variant"] == nbt.String("minecraft:british") {
			entries["variant"] = nbt.String("minecraft:british_shorthair")
		}
	}
	if version < frogVariantVersion && id == "minecraft:frog" {
		variantByIndex(entries, "Variant", "variant", frogVariants[:], "")
	}
	if version < paintingFieldsVersion && id == "minecraft:painting" {
		renameField(entries, "Motive", "variant")
		renameField(entries, "Facing", "facing")
	}
	if version < goatHornsVersion && id == "minecraft:goat" {
		entries["HasLeftHorn"] = nbt.Byte(1)
		entries["HasRightHorn"] = nbt.Byte(1)
	}
	if version < effectDurationVersion {
		for _, key := range []string{"Effects", "ActiveEffects", "CustomPotionEffects"} {
			fixEffectTicks(entries[key])
		}
	}
	if version < mobEffectIDVersion {
		upgradeEntityEffects(entries, string(id))
	}
	if version < projectileItemVersion && (id == "minecraft:arrow" || id == "minecraft:spectral_arrow") {
		item := "minecraft:spectral_arrow"
		if id == "minecraft:arrow" {
			item = "minecraft:arrow"
			if potion, _ := entries["Potion"].(nbt.String); potion != "" && potion != "minecraft:empty" {
				item = "minecraft:tipped_arrow"
			}
		}
		entries["item"] = nbt.Compound{"id": nbt.String(item), "Count": nbt.Int(1)}
	}
	if passengers, ok := nbt.Get[nbt.List](entries, "Passengers"); ok {
		for _, p := range passengers.Items {
			if c, ok := p.(nbt.CompoundTag); ok {
				upgradeOldEntity(c, version)
			}
		}
	}
}

// variantByIndex replaces a numeric variant under from with its name under
// to; a value that is not a number is only renamed, and an index with no name
// gets fallback (kept as it is when that is empty).
// vanilla: EntityVariantFix
func variantByIndex(entries nbt.Compound, from, to string, names []string, fallback string) {
	v, ok := entries[from]
	if !ok {
		return
	}
	delete(entries, from)
	n, isNum := nbt.IntValue(v)
	switch {
	case !isNum:
		entries[to] = v
	case n >= 0 && int(n) < len(names):
		entries[to] = nbt.String(names[n])
	case fallback != "":
		entries[to] = nbt.String(fallback)
	default:
		entries[to] = v
	}
}

// fixEffectTicks gives each effect of a list its ticks_active.
// vanilla: EffectDurationFix.fixEffect
func fixEffectTicks(list nbt.Tag) {
	l, ok := list.(nbt.List)
	if !ok {
		return
	}
	for _, item := range l.Items {
		effect, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		data, ok := nbt.CompoundView(effect["FactorCalculationData"])
		if !ok {
			continue
		}
		timestamp, ok := nbt.IntField(data, "effect_changed_timestamp")
		if !ok {
			timestamp = -1
		}
		duration, ok := nbt.IntField(effect, "Duration")
		if !ok {
			duration = -1
		}
		delete(data, "effect_changed_timestamp")
		data["ticks_active"] = nbt.Int(timestamp - duration)
	}
}

// upgradeEntityEffects gives effects their string ids and lower-case fields,
// and a mooshroom its stew_effects.
// vanilla: MobEffectIdFix.entityFixer
func upgradeEntityEffects(entries nbt.Compound, id string) {
	if id == "minecraft:mooshroom" {
		entry := nbt.Compound{}
		fixEffectID(entries, entry, "EffectId", "id")
		if v, ok := entries["EffectDuration"]; ok {
			entry["duration"] = v
		}
		if len(entry) > 0 {
			entries["stew_effects"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{entry}}
		}
		delete(entries, "EffectId")
		delete(entries, "EffectDuration")
	}
	if id == "minecraft:arrow" {
		upgradeEffectList(entries, "CustomPotionEffects", "custom_potion_effects")
	}
	if id == "minecraft:area_effect_cloud" {
		upgradeEffectList(entries, "Effects", "effects")
	}
	upgradeEffectList(entries, "ActiveEffects", "active_effects")
}

// fixEffectID reads a numeric effect id under from in src and sets its name
// under to in dst; an id with no name sets nothing.
func fixEffectID(src, dst nbt.Compound, from, to string) {
	if n, ok := nbt.IntValue(src[from]); ok && n >= 1 && int(n) <= len(oldMobEffectNames) {
		dst[to] = nbt.String(oldMobEffectNames[n-1])
	}
}

func upgradeEffectList(entries nbt.Compound, from, to string) {
	list, ok := entries[from].(nbt.List)
	if !ok {
		return
	}
	delete(entries, from)
	for _, item := range list.Items {
		if effect, ok := nbt.CompoundView(item); ok {
			upgradeEffectInstance(effect)
		}
	}
	entries[to] = list
}

// upgradeEffectInstance renames the fields of one effect and of its hidden
// effect. vanilla: MobEffectIdFix.updateMobEffectInstance
func upgradeEffectInstance(effect nbt.Compound) {
	fixEffectID(effect, effect, "Id", "id")
	delete(effect, "Id")
	for _, r := range [...]struct{ from, to string }{
		{"Ambient", "ambient"}, {"Amplifier", "amplifier"}, {"Duration", "duration"},
		{"ShowParticles", "show_particles"}, {"ShowIcon", "show_icon"}, {"HiddenEffect", "hidden_effect"},
	} {
		renameField(effect, r.from, r.to)
	}
	if hidden, ok := nbt.CompoundView(effect["hidden_effect"]); ok {
		upgradeEffectInstance(hidden)
	}
}

// oldEntityFixesEnd is the version from which upgradeOldEntity has nothing
// left to do: the last of its fixes is 3685.
const oldEntityFixesEnd = projectileItemVersion + 1
