package world

import (
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Data versions of the entity fixes a 1.20.3 or 1.20.4 record still needs.
const (
	horseBodyArmorVersion        = 3808 // vanilla: DataFixers.addFixers (HorseBodyArmorItemFix)
	chestedHorsesSlotVersion     = 3809 // ChestedHorsesInventoryZeroIndexingFix
	wolfHealthVersion            = 3812 // FixWolfHealth
	areaEffectCloudPotionVersion = 3818 // vanilla: DataFixers.addFixers (AreaEffectCloudPotionFix)
)

// blockPosFields are the entity fields vanilla's BlockPosFormatAndRenamesFix
// turns from an {X,Y,Z} compound into an int array, by entity id, with their
// new names.
var blockPosFields = map[string]map[string]string{
	"minecraft:bee":              {"HivePos": "hive_pos", "FlowerPos": "flower_pos"},
	"minecraft:end_crystal":      {"BeamTarget": "beam_target"},
	"minecraft:wandering_trader": {"WanderTarget": "wander_target"},
	"minecraft:witch":            {"PatrolTarget": "patrol_target"},
	"minecraft:ravager":          {"PatrolTarget": "patrol_target"},
	"minecraft:pillager":         {"PatrolTarget": "patrol_target"},
	"minecraft:illusioner":       {"PatrolTarget": "patrol_target"},
	"minecraft:evoker":           {"PatrolTarget": "patrol_target"},
	"minecraft:vindicator":       {"PatrolTarget": "patrol_target"},
}

// upgradeLegacyEntity applies the fixes between version (below 4055 and
// 3945's shape) and the 1.20.5 shapes to an entity and its passengers, in
// data version order.
func upgradeLegacyEntity(e nbt.CompoundTag, version int32) {
	upgradeOldEntity(e, version) // recurses into passengers itself
	if version < storedItemVersion {
		upgradeLegacyStacks(e, version) // likewise
	}
	upgradeLegacyEntityFields(e, version)
}

func upgradeLegacyEntityFields(e nbt.CompoundTag, version int32) {
	entries := e.Entries()
	name := entityName(entries)
	if version < horseBodyArmorVersion {
		upgradeHorseBodyArmor(entries, name)
	}
	if version < chestedHorsesSlotVersion {
		upgradeChestedSlots(entries, name)
	}
	if version < wolfHealthVersion && name == "minecraft:wolf" {
		upgradeWolfHealth(entries)
	}
	if version < blockPosFormatVersion {
		id, _ := nbt.Get[nbt.String](entries, "id")
		for from, to := range blockPosFields[string(id)] {
			fixBlockPosField(entries, from, to)
		}
		fixBlockPosField(entries, "Leash", "leash")
	}
	if version < areaEffectCloudPotionVersion {
		id, _ := nbt.Get[nbt.String](entries, "id")
		if typ, ok := registry.EntityTypeByName(string(id)); ok && typ == registry.EntityTypeAreaEffectCloud {
			upgradeCloudPotion(entries)
		}
	}
	if version < attributeModifierIdVersion {
		upgradeEntityAttributes(entries)
	}
	if passengers, ok := nbt.Get[nbt.List](entries, "Passengers"); ok {
		for _, p := range passengers.Items {
			if c, ok := p.(nbt.CompoundTag); ok {
				upgradeLegacyEntityFields(c, version)
			}
		}
	}
}

// entityName is the entity's namespaced id.
// vanilla: NamespacedSchema.ensureNamespaced
func entityName(entries nbt.Compound) string {
	id, _ := nbt.Get[nbt.String](entries, "id")
	if !strings.Contains(string(id), ":") {
		return "minecraft:" + string(id)
	}
	return string(id)
}

// upgradeHorseBodyArmor moves a horse's ArmorItem, or a llama's DecorItem, to
// body_armor_item, which always drops, and clears the horse's copy in the
// chest slot of ArmorItems. vanilla: HorseBodyArmorItemFix
func upgradeHorseBodyArmor(entries nbt.Compound, name string) {
	var from string
	switch name {
	case "minecraft:horse":
		from = "ArmorItem"
	case "minecraft:llama", "minecraft:trader_llama":
		from = "DecorItem"
	default:
		return
	}
	item, ok := entries[from]
	if !ok {
		return
	}
	delete(entries, from)
	entries["body_armor_item"] = item
	entries["body_armor_drop_chance"] = nbt.Float(2)
	if name != "minecraft:horse" {
		return
	}
	if armor, ok := entries["ArmorItems"].(nbt.List); ok && len(armor.Items) > 2 {
		armor.Items[2] = nbt.Compound{}
	}
	if chances, ok := entries["ArmorDropChances"].(nbt.List); ok && len(chances.Items) > 2 {
		chances.Items[2] = nbt.Float(defaultEquipmentDropOdds)
	}
}

// upgradeChestedSlots shifts the Slot of a pack animal's chest down by two:
// slots 0 and 1 used to be the saddle and armor.
// vanilla: ChestedHorsesInventoryZeroIndexingFix
func upgradeChestedSlots(entries nbt.Compound, name string) {
	switch name {
	case "minecraft:llama", "minecraft:trader_llama", "minecraft:mule", "minecraft:donkey":
	default:
		return
	}
	items, _ := entries["Items"].(nbt.List)
	for _, item := range items.Items {
		if c, ok := nbt.CompoundView(item); ok {
			slot := int32(2)
			if v, ok := nbt.IntValue(c["Slot"]); ok {
				slot = v
			}
			c["Slot"] = nbt.Byte(slot - 2)
		}
	}
}

// upgradeWolfHealth doubles the health of a wolf whose max health is still the
// old 20. vanilla: FixWolfHealth
func upgradeWolfHealth(entries nbt.Compound) {
	attributes, _ := entries["Attributes"].(nbt.List)
	for _, item := range attributes.Items {
		a, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		name, _ := a["Name"].(nbt.String)
		base, isDouble := a["Base"].(nbt.Double)
		if (name != "minecraft:generic.max_health" && name != "generic.max_health") || !isDouble || base != 20 {
			continue
		}
		a["Base"] = nbt.Double(40)
		if health, ok := nbt.FloatField(entries, "Health"); ok {
			entries["Health"] = nbt.Float(health * 2)
		}
	}
}

// upgradeCloudPotion folds Color, effects and Potion into potion_contents.
// vanilla: AreaEffectCloudPotionFix
func upgradeCloudPotion(entries nbt.Compound) {
	contents := nbt.Compound{}
	for _, f := range [...]struct{ from, to string }{
		{"Color", "custom_color"}, {"effects", "custom_effects"}, {"Potion", "potion"},
	} {
		if v, ok := entries[f.from]; ok {
			contents[f.to] = v
			delete(entries, f.from)
		}
	}
	if len(contents) > 0 {
		entries["potion_contents"] = contents
	}
}

// upgradeEntityAttributes renames Attributes and the fields of each attribute
// and gives its modifiers ids. vanilla: AttributeModifierIdFix.fixEntity
func upgradeEntityAttributes(entries nbt.Compound) {
	list, ok := entries["Attributes"].(nbt.List)
	if !ok {
		return
	}
	delete(entries, "Attributes")
	entries["attributes"] = list
	for _, item := range list.Items {
		a, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		renameField(a, "Name", "id")
		renameField(a, "Base", "base")
		renameField(a, "Modifiers", "modifiers")
		if modifiers, ok := a["modifiers"].(nbt.List); ok {
			for _, mi := range modifiers.Items {
				if m, ok := nbt.CompoundView(mi); ok {
					renameField(m, "UUID", "uuid")
					renameField(m, "Name", "name")
					renameField(m, "Amount", "amount")
					if op, ok := m["Operation"]; ok {
						delete(m, "Operation")
						n, _ := nbt.IntValue(op)
						m["operation"] = nbt.String(modifierOperation(n))
					}
				}
			}
			a["modifiers"] = fixModifierIDs(modifiers)
		}
	}
}

func modifierOperation(n int32) string {
	switch n {
	case 0:
		return "add_value"
	case 1:
		return "add_multiplied_base"
	case 2:
		return "add_multiplied_total"
	}
	return "invalid"
}
