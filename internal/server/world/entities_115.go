package world

import (
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// Data versions of the entity fixes between 1.15 and 1.16.5.
const (
	memoryExpiryVersion     = 2505 // vanilla: DataFixers.addFixers (MemoryExpiryDataFix)
	zombifiedPiglinVersion  = 2509 // vanilla: DataFixers.addFixers (EntityZombifiedPiglinRenameFix)
	projectileOwnerVersion  = 2511 // vanilla: DataFixers.addFixers (EntityProjectileOwnerFix)
	entityUUIDVersion       = 2514 // vanilla: DataFixers.addFixers (EntityUUIDFix, ItemStackUUIDFix)
	gossipUUIDVersion       = 2516 // vanilla: DataFixers.addFixers (GossipUUIDFix)
	legacyAttributesVersion = 2523 // vanilla: DataFixers.addFixers (AttributesRenameLegacy)
	striderGravityVersion   = 2529 // vanilla: DataFixers.addFixers (StriderGravityFix)
	followRangeVersion      = 2533 // vanilla: DataFixers.addFixers (VillagerFollowRangeFix)
	shulkerRotationVersion  = 2535 // vanilla: DataFixers.addFixers (EntityShulkerRotationFix)
)

// uuidKind orders the mob classes EntityUUIDFix handles; each includes the
// fixes of the ones before it.
type uuidKind uint8

const (
	kindLiving uuidKind = iota + 1 // attribute modifiers
	kindMob                        // + leash
	kindAnimal                     // + love cause
	kindOwned                      // + owner
)

// vanilla: EntityUUIDFix (the id sets)
var uuidKinds = func() map[string]uuidKind {
	m := map[string]uuidKind{"minecraft:armor_stand": kindLiving}
	for kind, ids := range map[uuidKind]string{
		kindOwned:  "donkey horse llama mule skeleton_horse trader_llama zombie_horse cat parrot wolf",
		kindAnimal: "bee chicken cow fox mooshroom ocelot panda pig polar_bear rabbit sheep turtle hoglin",
		kindMob: "bat blaze cave_spider cod creeper dolphin drowned elder_guardian ender_dragon enderman endermite evoker " +
			"ghast giant guardian husk illusioner magma_cube pufferfish zombified_piglin salmon shulker silverfish skeleton " +
			"slime snow_golem spider squid stray tropical_fish vex villager iron_golem vindicator pillager wandering_trader " +
			"witch wither wither_skeleton zombie zombie_villager phantom ravager piglin",
	} {
		for _, id := range strings.Fields(ids) {
			m["minecraft:"+id] = kind
		}
	}
	return m
}()

// vanilla: EntityUUIDFix.PROJECTILES
var uuidProjectiles = func() map[string]bool {
	m := map[string]bool{}
	for _, id := range strings.Fields("arrow dragon_fireball firework_rocket fireball llama_spit small_fireball snowball " +
		"spectral_arrow egg ender_pearl experience_bottle potion trident wither_skull") {
		m["minecraft:"+id] = true
	}
	return m
}()

// legacyAttributeNames are the attribute names before 1.16's namespaced ones.
// vanilla: DataFixers.addFixers (AttributesRenameLegacy)
var legacyAttributeNames = map[string]string{
	"generic.maxHealth": "minecraft:generic.max_health", "Max Health": "minecraft:generic.max_health",
	"zombie.spawnReinforcements": "minecraft:zombie.spawn_reinforcements", "Spawn Reinforcements Chance": "minecraft:zombie.spawn_reinforcements",
	"horse.jumpStrength": "minecraft:horse.jump_strength", "Jump Strength": "minecraft:horse.jump_strength",
	"generic.followRange": "minecraft:generic.follow_range", "Follow Range": "minecraft:generic.follow_range",
	"generic.knockbackResistance": "minecraft:generic.knockback_resistance", "Knockback Resistance": "minecraft:generic.knockback_resistance",
	"generic.movementSpeed": "minecraft:generic.movement_speed", "Movement Speed": "minecraft:generic.movement_speed",
	"generic.flyingSpeed": "minecraft:generic.flying_speed", "Flying Speed": "minecraft:generic.flying_speed",
	"generic.attackDamage": "minecraft:generic.attack_damage", "generic.attackKnockback": "minecraft:generic.attack_knockback",
	"generic.attackSpeed": "minecraft:generic.attack_speed", "generic.armorToughness": "minecraft:generic.armor_toughness",
}

// upgradePre1165Entity applies the entity fixes between 1.15 and 1.16.5 that
// are missing at version to one entity (not its passengers), in data version
// order. A player file takes the generic ones too.
func upgradePre1165Entity(e nbt.CompoundTag, version int32) {
	entries := e.Entries()
	id, _ := nbt.Get[nbt.String](entries, "id")
	if id == "minecraft:zombie_pigman" && version < zombifiedPiglinVersion {
		id = "minecraft:zombified_piglin"
		entries["id"] = id
	}
	if id == "minecraft:villager" && version < memoryExpiryVersion {
		if memories, ok := nbt.CompoundView(nbt.Compound(entries)["Brain"]); ok {
			if m, ok := nbt.CompoundView(memories["memories"]); ok {
				for k, v := range m {
					m[k] = nbt.Compound{"value": v}
				}
			}
		}
	}
	if version < projectileOwnerVersion {
		fixProjectileOwner(string(id), entries)
	}
	if version < entityUUIDVersion {
		fixEntityUUIDs(string(id), entries)
	}
	if version < gossipUUIDVersion && (id == "minecraft:villager" || id == "minecraft:zombie_villager") {
		if gossips, ok := entries["Gossips"].(nbt.List); ok {
			for _, g := range gossips.Items {
				if m, ok := nbt.CompoundView(g); ok {
					moveUUIDLongs(m, "Target", "Target")
				}
			}
		}
	}
	if version < legacyAttributesVersion {
		attributes, _ := nbt.Get[nbt.List](entries, "Attributes")
		for _, a := range attributes.Items {
			if m, ok := nbt.CompoundView(a); ok {
				if name, ok := m["Name"].(nbt.String); ok && legacyAttributeNames[string(name)] != "" {
					m["Name"] = nbt.String(legacyAttributeNames[string(name)])
				}
			}
		}
	}
	switch id {
	case "minecraft:strider":
		if version < striderGravityVersion && boolValue(entries["NoGravity"]) {
			entries["NoGravity"] = nbt.Byte(0)
		}
	case "minecraft:villager":
		if version < followRangeVersion {
			attributes, _ := nbt.Get[nbt.List](entries, "Attributes")
			for _, a := range attributes.Items {
				if m, ok := nbt.CompoundView(a); ok {
					if name, _ := m["Name"].(nbt.String); name == "generic.follow_range" && nbt.DoubleFieldOr(m, "Base", 0) == 16 {
						m["Base"] = nbt.Double(48)
					}
				}
			}
		}
	case "minecraft:shulker":
		if rotation, ok := entries["Rotation"].(nbt.List); ok && version < shulkerRotationVersion && len(rotation.Items) > 0 {
			// vanilla writes doubles; the list keeps its element type, which
			// every reader of Rotation converts alike.
			switch v := rotation.Items[0].(type) {
			case nbt.Float:
				rotation.Items[0] = v - 180
			case nbt.Double:
				rotation.Items[0] = v - 180
			}
		}
	}
}

// moveUUIDLongs replaces the long pair oldKey+Most/oldKey+Least with the int
// array newKey, unless either half is zero.
// vanilla: AbstractUUIDFix.replaceUUIDLeastMost
func moveUUIDLongs(m nbt.Compound, oldKey, newKey string) {
	most, _ := nbt.LongValue(m[oldKey+"Most"])
	least, _ := nbt.LongValue(m[oldKey+"Least"])
	if most == 0 || least == 0 {
		return
	}
	delete(m, oldKey+"Most")
	delete(m, oldKey+"Least")
	m[newKey] = uuidInts(uint64(most), uint64(least))
}

// moveUUIDString replaces the string UUID under oldKey with the int array
// under newKey, unless it does not parse.
// vanilla: AbstractUUIDFix.replaceUUIDString
func moveUUIDString(m nbt.Compound, oldKey, newKey string) {
	s, ok := m[oldKey].(nbt.String)
	if !ok {
		return
	}
	if u, err := uuid.Parse(string(s)); err == nil {
		delete(m, oldKey)
		m[newKey] = nbt.UUIDToIntArray(u)
	}
}

// moveUUIDML replaces the compound under oldKey holding long M and L with the
// int array under newKey, unless either is zero.
// vanilla: AbstractUUIDFix.replaceUUIDMLTag
func moveUUIDML(m nbt.Compound, oldKey, newKey string) {
	if u, ok := uuidFromML(m[oldKey]); ok {
		delete(m, oldKey)
		m[newKey] = u
	}
}

// uuidFromML reads a compound of longs M and L as int array UUID; zero halves
// are no UUID.
func uuidFromML(tag nbt.Tag) (nbt.IntArray, bool) {
	ml, ok := nbt.CompoundView(tag)
	if !ok {
		return nil, false
	}
	most, _ := nbt.LongValue(ml["M"])
	least, _ := nbt.LongValue(ml["L"])
	if most == 0 || least == 0 {
		return nil, false
	}
	return uuidInts(uint64(most), uint64(least)), true
}

// fixProjectileOwner moves a projectile's owner to OwnerUUID.
// vanilla: EntityProjectileOwnerFix
func fixProjectileOwner(id string, m nbt.Compound) {
	switch id {
	case "minecraft:egg", "minecraft:ender_pearl", "minecraft:experience_bottle", "minecraft:snowball", "minecraft:potion":
		moveUUIDML(m, "owner", "OwnerUUID")
	case "minecraft:llama_spit":
		if owner, ok := nbt.CompoundView(m["Owner"]); ok {
			moveUUIDLongs(owner, "OwnerUUID", "OwnerUUID")
			if u, ok := owner["OwnerUUID"]; ok {
				m["OwnerUUID"] = u
			}
			delete(m, "Owner")
		}
	case "minecraft:arrow", "minecraft:spectral_arrow", "minecraft:trident":
		moveUUIDLongs(m, "OwnerUUID", "OwnerUUID")
	}
}

// fixEntityUUIDs moves an entity's UUIDs to the int array format.
// vanilla: EntityUUIDFix
func fixEntityUUIDs(id string, m nbt.Compound) {
	moveUUIDLongs(m, "UUID", "UUID")
	kind := uuidKinds[id]
	if kind >= kindOwned {
		moveUUIDString(m, "OwnerUUID", "Owner")
	}
	if kind >= kindAnimal {
		moveUUIDLongs(m, "LoveCause", "LoveCause")
	}
	if kind >= kindMob {
		if leash, ok := nbt.CompoundView(m["Leash"]); ok {
			moveUUIDLongs(leash, "UUID", "UUID")
		}
	}
	if kind >= kindLiving {
		attributes, _ := nbt.Get[nbt.List](m, "Attributes")
		for _, a := range attributes.Items {
			if attr, ok := nbt.CompoundView(a); ok {
				fixModifierUUIDs(attr["Modifiers"])
			}
		}
	}
	if uuidProjectiles[id] {
		if owner, ok := m["OwnerUUID"]; ok {
			delete(m, "OwnerUUID")
			m["Owner"] = owner
		}
	}
	switch id {
	case "minecraft:bee", "minecraft:zombified_piglin":
		moveUUIDString(m, "HurtBy", "HurtBy")
	case "minecraft:fox":
		if trusted, ok := m["TrustedUUIDs"].(nbt.List); ok {
			items := make([]nbt.Tag, len(trusted.Items))
			for i, t := range trusted.Items {
				items[i] = t
				if u, ok := uuidFromML(t); ok {
					items[i] = u
				}
			}
			delete(m, "TrustedUUIDs")
			m["Trusted"] = nbt.List{Elem: nbt.TypeIntArray, Items: items}
		}
	case "minecraft:item":
		moveUUIDML(m, "Owner", "Owner")
		moveUUIDML(m, "Thrower", "Thrower")
	case "minecraft:shulker_bullet":
		moveUUIDML(m, "Owner", "Owner")
		moveUUIDML(m, "Target", "Target")
	case "minecraft:area_effect_cloud", "minecraft:evoker_fangs":
		moveUUIDLongs(m, "OwnerUUID", "Owner")
	case "minecraft:zombie_villager":
		moveUUIDLongs(m, "ConversionPlayer", "ConversionPlayer")
	case "minecraft:piglin":
		if brain, ok := nbt.CompoundView(m["Brain"]); ok {
			if memories, ok := nbt.CompoundView(brain["memories"]); ok {
				if angry, ok := nbt.CompoundView(memories["minecraft:angry_at"]); ok {
					moveUUIDString(angry, "value", "value")
				}
			}
		}
	}
}

// fixModifierUUIDs fixes the UUID of each attribute modifier in a list.
func fixModifierUUIDs(modifiers nbt.Tag) {
	list, _ := modifiers.(nbt.List)
	for _, mod := range list.Items {
		if m, ok := nbt.CompoundView(mod); ok {
			moveUUIDLongs(m, "UUID", "UUID")
		}
	}
}

// fixStackUUIDsAndNames applies ItemStackUUIDFix and the AttributesRenameLegacy
// item step to a pre-1.20.5 stack's tag.
// vanilla: ItemStackUUIDFix, AttributesRenameLegacy.fixItemStackTag
func fixStackUUIDsAndNames(tag nbt.Compound, id string, version int32) {
	if version < entityUUIDVersion {
		fixModifierUUIDs(tag["AttributeModifiers"])
		if skull, ok := nbt.CompoundView(tag["SkullOwner"]); ok && id == "minecraft:player_head" {
			moveUUIDString(skull, "Id", "Id")
		}
	}
	if version < legacyAttributesVersion {
		list, _ := tag["AttributeModifiers"].(nbt.List)
		for _, mod := range list.Items {
			if m, ok := nbt.CompoundView(mod); ok {
				if name, ok := m["AttributeName"].(nbt.String); ok && legacyAttributeNames[string(name)] != "" {
					m["AttributeName"] = nbt.String(legacyAttributeNames[string(name)])
				}
			}
		}
	}
}
