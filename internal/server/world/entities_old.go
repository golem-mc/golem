package world

import (
	"maps"

	"github.com/golem-mc/golem/nbt"
)

// Data versions of the entity and spawner fixes between 1.16.5 and 1.20.3.
const (
	arrowPickupVersion       = 2702 // vanilla: DataFixers.addFixers (AbstractArrowPickupFix)
	spawnerDataVersion       = 2831 // vanilla: DataFixers.addFixers (SpawnerDataFix)
	catVariantVersion        = 3086 // vanilla: DataFixers.addFixers (EntityVariantFix for cats)
	frogVariantVersion       = 3087 // vanilla: DataFixers.addFixers (EntityVariantFix for frogs)
	paintingFieldsVersion    = 3090 // vanilla: DataFixers.addFixers (EntityPaintingFieldsRenameFix)
	goatStateVersion         = 3093 // vanilla: DataFixers.addFixers (EntityGoatMissingStateFix)
	britishShorthairVersion  = 3097 // vanilla: DataFixers.addFixers (VariantRenameFix)
	spawnEggVersion          = 3209 // vanilla: DataFixers.addFixers (ItemStackSpawnEggFix)
	effectDurationVersion    = 3322 // vanilla: DataFixers.addFixers (EffectDurationFix)
	primedTntVersion         = 3683 // vanilla: DataFixers.addFixers (PrimedTntBlockStateFixer)
	projectileItemVersion    = 3685 // vanilla: DataFixers.addFixers (FixProjectileStoredItem)
	tippedArrowPotionVersion = 3818 // vanilla: DataFixers.addFixers (TippedArrowPotionToItemFix, v3818_2)
)

var catVariants = [...]string{
	"minecraft:tabby", "minecraft:black", "minecraft:red", "minecraft:siamese", "minecraft:british",
	"minecraft:calico", "minecraft:persian", "minecraft:ragdoll", "minecraft:white", "minecraft:jellie",
	"minecraft:all_black",
}

var frogVariants = [...]string{"minecraft:temperate", "minecraft:warm", "minecraft:cold"}

// upgradeOldEntity applies the entity fixes of the versions between 1.16.5 and
// 1.20.3 that are still missing at version to an entity and its passengers,
// in data version order. It runs before the item stacks are componentized, as
// the fixes after it that make or edit a stack work on the pre-1.20.5 shape.
func upgradeOldEntity(e nbt.CompoundTag, version int32) {
	entries := e.Entries()
	if version < entityFixesBelow {
		upgradePre1165Entity(e, version)
	}
	id, _ := nbt.Get[nbt.String](entries, "id")
	switch string(id) {
	case "minecraft:arrow", "minecraft:spectral_arrow", "minecraft:trident":
		if version < arrowPickupVersion {
			if _, has := entries["pickup"]; !has {
				player := true
				if v, ok := entries["player"]; ok {
					player = boolValue(v)
				}
				pickup := nbt.Byte(0)
				if player {
					pickup = 1
				}
				delete(entries, "player")
				entries["pickup"] = pickup
			}
		}
	case "minecraft:spawner_minecart":
		if version < spawnerDataVersion {
			fixSpawnerData(entries, version)
		}
	case "minecraft:cat":
		if version < catVariantVersion {
			variantFromInt(entries, "CatType", catVariants[:], "minecraft:tabby")
		}
		if version < britishShorthairVersion {
			if v, ok := entries["variant"].(nbt.String); ok && v == "minecraft:british" {
				entries["variant"] = nbt.String("minecraft:british_shorthair")
			}
		}
	case "minecraft:frog":
		if version < frogVariantVersion {
			variantFromInt(entries, "Variant", frogVariants[:], "")
		}
	case "minecraft:painting":
		if version < paintingFieldsVersion {
			renameField(entries, "Motive", "variant")
			renameField(entries, "Facing", "facing")
		}
	case "minecraft:goat":
		if version < goatStateVersion {
			entries["HasLeftHorn"] = nbt.Byte(1)
			entries["HasRightHorn"] = nbt.Byte(1)
		}
	case "minecraft:tnt":
		if version < primedTntVersion {
			entries["block_state"] = nbt.Compound{"Name": nbt.String("minecraft:tnt")}
			if fuse, ok := entries["Fuse"]; ok {
				entries["fuse"] = fuse
			}
		}
	}
	if version < effectDurationVersion {
		for _, field := range [...]string{"Effects", "ActiveEffects", "CustomPotionEffects"} {
			fixEffectDurations(entries[field])
		}
	}
	if version < mobEffectIDVersion {
		fixEntityEffectIDs(string(id), entries)
	}
	if string(id) == "minecraft:arrow" || string(id) == "minecraft:spectral_arrow" {
		if version < projectileItemVersion {
			item := "minecraft:spectral_arrow"
			if string(id) == "minecraft:arrow" {
				item = "minecraft:arrow"
				if potion, _ := nbt.Get[nbt.String](entries, "Potion"); potion != "" && potion != "minecraft:empty" {
					item = "minecraft:tipped_arrow"
				}
			}
			entries["item"] = nbt.Compound{"id": nbt.String(item), "Count": nbt.Int(1)}
		}
		if string(id) == "minecraft:arrow" && version < tippedArrowPotionVersion {
			moveArrowPotion(entries)
		}
	}
	if passengers, ok := nbt.Get[nbt.List](entries, "Passengers"); ok {
		for _, p := range passengers.Items {
			if c, ok := p.(nbt.CompoundTag); ok {
				upgradeOldEntity(c, version)
			}
		}
	}
}

// variantFromInt sets variant to the name of the number under field, keeping
// field; a number with no name gets fallback, and with none the entity is left.
// vanilla: EntityVariantFix
func variantFromInt(entries nbt.Compound, field string, names []string, fallback string) {
	n, ok := nbt.IntValue(entries[field])
	if !ok {
		return
	}
	switch {
	case n >= 0 && int(n) < len(names):
		entries["variant"] = nbt.String(names[n])
	case fallback != "":
		entries["variant"] = nbt.String(fallback)
	}
}

// moveArrowPotion moves an arrow's potion, effects and colour into its item's
// tag, where the componentization turns them into potion_contents.
// vanilla: TippedArrowPotionToItemFix
func moveArrowPotion(entries nbt.Compound) {
	potion, hasPotion := takeKey(entries, "Potion")
	effects, hasEffects := takeKey(entries, "custom_potion_effects")
	color, hasColor := takeKey(entries, "Color")
	if !hasPotion && !hasEffects && !hasColor {
		return
	}
	item, _ := nbt.CompoundView(entries["item"])
	if item == nil {
		item = nbt.Compound{}
	}
	tag, _ := nbt.CompoundView(item["tag"])
	if tag == nil {
		tag = nbt.Compound{}
	}
	if hasPotion {
		tag["Potion"] = potion
	}
	if hasEffects {
		tag["custom_potion_effects"] = effects
	}
	if hasColor {
		tag["CustomPotionColor"] = color
	}
	item["tag"] = tag
	entries["item"] = item
}

// fixEffectDurations rewrites each effect's FactorCalculationData timestamp
// into the ticks it has been active.
// vanilla: EffectDurationFix.fixEffect
func fixEffectDurations(list nbt.Tag) {
	effects, ok := list.(nbt.List)
	if !ok {
		return
	}
	for _, item := range effects.Items {
		effect, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		factor, ok := nbt.CompoundView(effect["FactorCalculationData"])
		if !ok {
			continue
		}
		timestamp := int32(-1)
		if v, ok := nbt.IntValue(factor["effect_changed_timestamp"]); ok {
			timestamp = v
		}
		duration := int32(-1)
		if v, ok := nbt.IntValue(effect["Duration"]); ok {
			duration = v
		}
		delete(factor, "effect_changed_timestamp")
		factor["ticks_active"] = nbt.Int(timestamp - duration)
	}
}

// fixEntityEffectIDs gives an entity's effects their names, and its fields
// their new names.
// vanilla: MobEffectIdFix.entityFixer
func fixEntityEffectIDs(id string, entries nbt.Compound) {
	switch id {
	case "minecraft:mooshroom":
		entry := nbt.Compound{}
		if v, has := entries["EffectId"]; has {
			entry["id"] = v
			fixBeaconEffect(entry, "id", "id")
		}
		if d, ok := entries["EffectDuration"]; ok {
			entry["duration"] = d
		}
		if len(entry) > 0 {
			entries["stew_effects"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{entry}}
		}
		delete(entries, "EffectId")
		delete(entries, "EffectDuration")
	case "minecraft:arrow":
		fixEffectInstances(entries, "CustomPotionEffects", "custom_potion_effects")
	case "minecraft:area_effect_cloud":
		fixEffectInstances(entries, "Effects", "effects")
	}
	fixEffectInstances(entries, "ActiveEffects", "active_effects")
}

// fixEffectInstances renames the list under from to to, with each effect's
// numeric Id replaced by its name and its fields renamed.
// vanilla: MobEffectIdFix.updateMobEffectInstanceList
func fixEffectInstances(entries nbt.Compound, from, to string) {
	list, ok := entries[from].(nbt.List)
	if !ok {
		return
	}
	delete(entries, from)
	for _, item := range list.Items {
		if effect, ok := nbt.CompoundView(item); ok {
			fixEffectInstance(effect)
		}
	}
	entries[to] = list
}

func fixEffectInstance(effect nbt.Compound) {
	fixBeaconEffect(effect, "Id", "id")
	for _, r := range [...][2]string{
		{"Ambient", "ambient"}, {"Amplifier", "amplifier"}, {"Duration", "duration"},
		{"ShowParticles", "show_particles"}, {"ShowIcon", "show_icon"},
	} {
		renameField(effect, r[0], r[1])
	}
	if hidden, ok := nbt.CompoundView(effect["HiddenEffect"]); ok {
		fixEffectInstance(hidden)
		renameField(effect, "HiddenEffect", "hidden_effect")
	}
}

// fixStackEffects applies the effect fixes to the tag of a pre-1.20.5 stack.
// vanilla: MobEffectIdFix.itemStackFixer, EffectDurationFix
func fixStackEffects(tag nbt.Compound, id string, version int32) {
	potion := id == "minecraft:potion" || id == "minecraft:splash_potion" ||
		id == "minecraft:lingering_potion" || id == "minecraft:tipped_arrow"
	if potion && version < effectDurationVersion {
		fixEffectDurations(tag["CustomPotionEffects"])
	}
	if version >= mobEffectIDVersion {
		return
	}
	switch {
	case id == "minecraft:suspicious_stew":
		list, ok := tag["Effects"].(nbt.List)
		if !ok {
			return
		}
		delete(tag, "Effects")
		for _, item := range list.Items {
			if entry, ok := nbt.CompoundView(item); ok {
				fixBeaconEffect(entry, "EffectId", "id")
				renameField(entry, "EffectDuration", "duration")
			}
		}
		tag["effects"] = list
	case potion:
		fixEffectInstances(tag, "CustomPotionEffects", "custom_potion_effects")
	}
}

// spawnEggs are the entities ItemStackSpawnEggFix knows an egg for.
var spawnEggs = func() map[string]bool {
	m := map[string]bool{}
	for _, name := range [...]string{
		"bat", "blaze", "cave_spider", "chicken", "cow", "creeper", "donkey", "elder_guardian",
		"ender_dragon", "enderman", "endermite", "ghast", "guardian", "horse", "husk", "iron_golem",
		"llama", "magma_cube", "mooshroom", "mule", "ocelot", "pufferfish", "parrot", "pig",
		"polar_bear", "rabbit", "sheep", "shulker", "silverfish", "skeleton", "skeleton_horse",
		"slime", "snow_golem", "spider", "squid", "stray", "turtle", "vex", "villager", "witch",
		"wither", "wither_skeleton", "wolf", "zombie", "zombie_horse", "zombie_villager",
	} {
		m["minecraft:"+name] = true
	}
	return m
}()

// fixSpawnEgg turns a pig spawn egg with an EntityTag into the egg of that
// entity, or leaves it a pig egg when the entity has none.
// vanilla: ItemStackSpawnEggFix
func fixSpawnEgg(entries nbt.Compound) {
	tag, _ := nbt.CompoundView(entries["tag"])
	entity, _ := nbt.CompoundView(tag["EntityTag"])
	id, ok := nbt.Get[nbt.String](entity, "id")
	if !ok {
		return
	}
	if spawnEggs[string(id)] {
		entries["id"] = nbt.String(id + "_spawn_egg")
	} else {
		entries["id"] = nbt.String("minecraft:pig_spawn_egg")
	}
}

// fixSpawnerData wraps a spawner's SpawnData and SpawnPotentials in the
// weighted entry shape, then brings the entities inside them up to date.
// vanilla: SpawnerDataFix
func fixSpawnerData(entries nbt.Compound, version int32) {
	if data, ok := nbt.CompoundView(entries["SpawnData"]); ok {
		entries["SpawnData"] = nbt.Compound{"entity": data}
	}
	if list, ok := entries["SpawnPotentials"].(nbt.List); ok {
		for i, item := range list.Items {
			old, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			weight := nbt.Int(1)
			if w, ok := nbt.IntValue(old["Weight"]); ok {
				weight = nbt.Int(w)
			}
			data := maps.Clone(old)
			delete(data, "Weight")
			delete(data, "Entity")
			if entity, ok := old["Entity"]; ok {
				data["entity"] = entity
			}
			list.Items[i] = nbt.Compound{"weight": weight, "data": data}
		}
	}
	upgradeSpawnerEntities(entries, version)
}

// upgradeSpawnerEntities upgrades the entities a spawner in the weighted entry
// shape holds.
func upgradeSpawnerEntities(entries nbt.Compound, version int32) {
	upgradeOne := func(data nbt.Tag) {
		if m, ok := nbt.CompoundView(data); ok {
			if entity, ok := nbt.CompoundView(m["entity"]); ok {
				upgradeEntities(nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{entity}}, version)
			}
		}
	}
	upgradeOne(entries["SpawnData"])
	if list, ok := entries["SpawnPotentials"].(nbt.List); ok {
		for _, item := range list.Items {
			if m, ok := nbt.CompoundView(item); ok {
				upgradeOne(m["data"])
			}
		}
	}
}
