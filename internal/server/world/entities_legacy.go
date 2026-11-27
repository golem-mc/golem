package world

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Data versions of the entity fixes a 1.20.3 or 1.20.4 record still needs.
const (
	blockPosFormatVersion        = 3813 // vanilla: DataFixers.addFixers (BlockPosFormatAndRenamesFix)
	areaEffectCloudPotionVersion = 3818 // vanilla: DataFixers.addFixers (AreaEffectCloudPotionFix)
	legacyAttributeFormatVersion = attributeModifierIdVersion
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
	entries := e.Entries()
	if version < blockPosFormatVersion {
		id, _ := nbt.Get[nbt.String](entries, "id")
		for from, to := range blockPosFields[string(id)] {
			renameBlockPos(entries, from, to)
		}
		renameBlockPos(entries, "Leash", "leash")
	}
	if version < storedItemVersion {
		upgradeLegacyStacks(e, version)
	}
	if version < areaEffectCloudPotionVersion {
		id, _ := nbt.Get[nbt.String](entries, "id")
		if typ, ok := registry.EntityTypeByName(string(id)); ok && typ == registry.EntityTypeAreaEffectCloud {
			upgradeCloudPotion(entries)
		}
	}
	if version < legacyAttributeFormatVersion {
		upgradeEntityAttributes(entries)
	}
	if passengers, ok := nbt.Get[nbt.List](entries, "Passengers"); ok {
		for _, p := range passengers.Items {
			if c, ok := p.(nbt.CompoundTag); ok {
				upgradeLegacyEntity(c, version)
			}
		}
	}
}

// renameBlockPos renames a field and turns an {X,Y,Z} compound in it into an
// int array. vanilla: ExtraDataFixUtils.fixBlockPos
func renameBlockPos(entries nbt.Compound, from, to string) {
	v, ok := entries[from]
	if !ok {
		return
	}
	delete(entries, from)
	if m, ok := nbt.CompoundView(v); ok {
		x, okx := nbt.IntValue(m["X"])
		y, oky := nbt.IntValue(m["Y"])
		z, okz := nbt.IntValue(m["Z"])
		if okx && oky && okz {
			v = nbt.IntArray{x, y, z}
		}
	}
	entries[to] = v
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
