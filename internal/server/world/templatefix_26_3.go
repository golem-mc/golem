package world

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// templateFix26_3 applies the STRUCTURE-reachable fixes after data version 4903.
// Stages are DataFixUtils keys (version*10 + subversion).
func templateFix26_3(value nbt.Tag, kind string, stage int32) nbt.Tag {
	c, _ := value.(nbt.Compound)
	id, _ := nbt.Get[nbt.String](c, "id")
	switch stage {
	case 49960: // vanilla: PotDecorationsComponentUnflatteningFix
		if kind == "data_components" {
			if v, ok := c["minecraft:pot_decorations"]; ok {
				c["minecraft:pot_decorations"] = templateUnpackPot(v)
			}
		}
	case 49961: // vanilla: PotDecorationsBlockEntityUnflatteningFix
		if kind == "block_entity" && id == "minecraft:decorated_pot" {
			v, ok := c["sherds"]
			if !ok {
				v = nbt.List{Elem: nbt.TypeString}
			}
			c["sherds"] = templateUnpackPot(v)
		}
	case 50020: // vanilla: SignsOpFlagFix
		if kind == "block_entity" && (id == "minecraft:sign" || id == "minecraft:hanging_sign") {
			c["allow_op_features"] = nbt.Byte(1)
		}
	case 50060: // vanilla: BlockStateFieldNamesFix
		if kind == "block_state" {
			templateRenameField(c, "Name", "id")
			templateRenameField(c, "Properties", "properties")
		}
	case 50070: // vanilla: SwingAnimationComponentSplitFix
		if kind == "data_components" {
			if animation, ok := c["minecraft:swing_animation"]; ok {
				delete(c, "minecraft:swing_animation")
				c["minecraft:attack_animation"] = animation
				c["minecraft:interact_animation"] = animation
			}
		}
	case 50080: // vanilla: ExplorerMapItemFix, MapColorComponentRemovalFix
		if kind == "data_components" {
			delete(c, "minecraft:map_color")
		}
		if kind == "item_stack" && id == "minecraft:filled_map" {
			templateExplorerMap(c)
		}
	case 50120: // vanilla: ItemRenameFix
		if kind == "item_name" {
			if s, ok := value.(nbt.String); ok {
				if renamed, ok := templateExplorerMapRenames[registry.Qualify(string(s))]; ok {
					return nbt.String(renamed)
				}
			}
		}
	case 50140: // vanilla: RemoveBlockTransformerComponentFix
		if kind == "data_components" {
			delete(c, "minecraft:block_transformer")
		}
	case 50160: // vanilla: UnfilledBuriedTreasureMapFix
		if kind == "item_stack" && id == "minecraft:map" {
			components, _ := nbt.Get[nbt.Compound](c, "components")
			if templatePlainTranslation(components["minecraft:item_name"]) == "filled_map.buried_treasure" {
				name := components["minecraft:item_name"].(nbt.Compound)
				name["translate"] = nbt.String("item.minecraft.buried_treasure_map")
			}
		}
	}
	return value
}

func templateRenameField(c nbt.Compound, from, to string) {
	if v, ok := c[from]; ok {
		delete(c, from)
		c[to] = v
	}
}

// templateUnpackPot aborts on empty IDs; missing or non-string sides become bricks.
// vanilla: PotDecorationsComponentUnflatteningFix.unpackList
func templateUnpackPot(value nbt.Tag) nbt.Tag {
	list, ok := value.(nbt.List)
	if !ok {
		return value
	}
	result := nbt.Compound{}
	for i, side := range [...]string{"back", "left", "right", "front"} {
		id := nbt.String("minecraft:brick")
		if i < len(list.Items) {
			if s, ok := list.Items[i].(nbt.String); ok {
				id = s
			}
		}
		if id == "" {
			return value
		}
		result[side] = nbt.Compound{"id": id}
	}
	return result
}

// templatePlainTranslation is ExtraDataFixUtils.getPlainTranslationKey.
func templatePlainTranslation(value nbt.Tag) string {
	c, _ := nbt.CompoundView(value)
	if len(c) == 1 {
		key, _ := nbt.Get[nbt.String](c, "translate")
		return string(key)
	}
	return ""
}

var templateExplorerMapRenames = map[string]string{
	"minecraft:ocean_explorer_map":    "minecraft:ocean_monument_map",
	"minecraft:swamp_explorer_map":    "minecraft:swamp_hut_map",
	"minecraft:trial_explorer_map":    "minecraft:buried_trial_chambers_map",
	"minecraft:woodland_explorer_map": "minecraft:woodland_mansion_map",
	"minecraft:jungle_explorer_map":   "minecraft:jungle_pyramid_map",
}

// vanilla: ExplorerMapItemFix
func templateExplorerMap(item nbt.CompoundTag) {
	components, _ := nbt.CompoundView(item.Entries()["components"])
	decorations, _ := nbt.CompoundView(components["minecraft:map_decorations"])
	decoration, _ := nbt.CompoundView(decorations["+"])
	typeID, _ := nbt.Get[nbt.String](decoration, "type")
	var name, translation string
	switch registry.Qualify(string(typeID)) {
	case "minecraft:mansion":
		name, translation = "woodland_explorer_map", "mansion"
	case "minecraft:monument":
		name, translation = "ocean_explorer_map", "monument"
	case "minecraft:trial_chambers":
		name, translation = "trial_explorer_map", "trial_chambers"
	case "minecraft:jungle_temple":
		name, translation = "jungle_explorer_map", "explorer_jungle"
	case "minecraft:swamp_hut":
		name, translation = "swamp_explorer_map", "explorer_swamp"
	case "minecraft:red_x":
		name, translation = "buried_treasure_map", "buried_treasure"
	case "minecraft:village_desert", "minecraft:village_plains", "minecraft:village_savanna", "minecraft:village_snowy", "minecraft:village_taiga":
		biome := registry.Qualify(string(typeID))[len("minecraft:village_"):]
		name, translation = biome+"_village_map", "village_"+biome
	default:
		return
	}
	item.Set("id", nbt.String("minecraft:"+name))
	if templatePlainTranslation(components["minecraft:item_name"]) == "filled_map."+translation {
		item.Entries()["components"].(nbt.CompoundTag).Delete("minecraft:item_name")
	}
}
