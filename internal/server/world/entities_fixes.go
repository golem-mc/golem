package world

import (
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// Data versions of the entity fixes of 1.21.2 and later, each the version a
// record must be below to need its step.
const (
	boatSplitVersion          = 4067 // vanilla: DataFixers.addFixers (BoatSplitFix)
	salmonSizeVersion         = 4081 // vanilla: DataFixers.addFixers (EntitySalmonSizeFix)
	tntMinecartFuseVersion    = 4173 // vanilla: DataFixers.addFixers (Rename TNT Minecart fuse)
	cloudDurationScaleVersion = 4296 // vanilla: DataFixers.addFixers (AreaEffectCloudDurationScaleFix)
	fallDistanceVersion       = 4303 // vanilla: DataFixers.addFixers (EntityFallDistanceFloatToDoubleFix)
	thrownPotionSplitVersion  = 4306 // vanilla: DataFixers.addFixers (ThrownPotionSplitFix)
	copperGolemWeatherVersion = 4535 // vanilla: DataFixers.addFixers (CopperGolemWeatherStateFix)
	villagerFinalizedVersion  = 4763 // vanilla: DataFixers.addFixers (VillagerSetVillagerDataFinalized)
	blockStateFieldsVersion   = 5006 // vanilla: DataFixers.addFixers (BlockStateFieldNamesFix)
)

// boatWoods are the boat variants BoatSplitFix gives an entity of their own;
// any other Type is oak. The bamboo boat is a raft.
var boatWoods = map[string]string{
	"spruce": "spruce", "birch": "birch", "jungle": "jungle", "acacia": "acacia",
	"cherry": "cherry", "dark_oak": "dark_oak", "mangrove": "mangrove", "bamboo": "bamboo",
}

// upgradeEntityShapes applies the entity fixes from 1.21.2 on to an entity
// written at version and to its passengers.
func upgradeEntityShapes(e nbt.CompoundTag, version int32) {
	entries := e.Entries()
	id, _ := nbt.Get[nbt.String](entries, "id")
	if version < boatSplitVersion {
		splitBoat(entries, string(id))
	}
	if version < salmonSizeVersion && id == "minecraft:salmon" {
		if typ, _ := entries["type"].(nbt.String); typ != "large" {
			entries["type"] = nbt.String("medium")
		}
	}
	if version < tntMinecartFuseVersion && id == "minecraft:tnt_minecart" {
		renameField(entries, "TNTFuse", "fuse")
	}
	if version < cloudDurationScaleVersion && id == "minecraft:area_effect_cloud" {
		entries["potion_duration_scale"] = nbt.Float(0.25)
	}
	if version < fallDistanceVersion {
		if v, ok := entries["FallDistance"]; ok {
			delete(entries, "FallDistance")
			f, _ := nbt.FloatValue(v)
			entries["fall_distance"] = nbt.Double(f)
		}
	}
	if version < thrownPotionSplitVersion && id == "minecraft:potion" {
		item, _ := nbt.CompoundView(entries["Item"])
		itemID, _ := item["id"].(nbt.String)
		if !strings.Contains(string(itemID), ":") && itemID != "" {
			itemID = "minecraft:" + itemID
		}
		if itemID == "minecraft:lingering_potion" {
			entries["id"] = nbt.String("minecraft:lingering_potion")
		} else {
			entries["id"] = nbt.String("minecraft:splash_potion")
		}
	}
	if version < copperGolemWeatherVersion && id == "minecraft:copper_golem" {
		if v, ok := entries["weather_state"]; ok {
			n, _ := nbt.IntValue(v)
			state := "unaffected"
			if n >= 1 && n <= 3 {
				state = [...]string{"", "exposed", "weathered", "oxidized"}[n]
			}
			entries["weather_state"] = nbt.String(state)
		}
	}
	if version < villagerFinalizedVersion && (id == "minecraft:villager" || id == "minecraft:zombie_villager") {
		entries["VillagerDataFinalized"] = nbt.Byte(1)
	}
	if version < blockStateFieldsVersion {
		renameEntityBlockStates(entries, string(id))
	}
	if passengers, ok := nbt.Get[nbt.List](entries, "Passengers"); ok {
		for _, p := range passengers.Items {
			if c, ok := p.(nbt.CompoundTag); ok {
				upgradeEntityShapes(c, version)
			}
		}
	}
}

// renameEntityBlockStates renames Name and Properties to id and properties in
// the block states an entity saves.
// vanilla: BlockStateFieldNamesFix
func renameEntityBlockStates(entries nbt.Compound, id string) {
	key := ""
	switch id {
	case "minecraft:falling_block":
		key = "BlockState"
	case "minecraft:enderman":
		key = "carriedBlockState"
	case "minecraft:minecart", "minecraft:chest_minecart", "minecraft:furnace_minecart", "minecraft:tnt_minecart",
		"minecraft:hopper_minecart", "minecraft:spawner_minecart", "minecraft:command_block_minecart":
		key = "DisplayState"
	default:
		return
	}
	if state, ok := nbt.CompoundView(entries[key]); ok {
		renameField(state, "Name", "id")
		renameField(state, "Properties", "properties")
	}
}

// splitBoat gives a boat or chest boat the entity id of its Type.
// vanilla: BoatSplitFix
func splitBoat(entries nbt.Compound, id string) {
	if id != "minecraft:boat" && id != "minecraft:chest_boat" {
		return
	}
	typ, _ := entries["Type"].(nbt.String)
	delete(entries, "Type")
	wood := boatWoods[string(typ)]
	switch {
	case wood == "bamboo" && id == "minecraft:boat":
		entries["id"] = nbt.String("minecraft:bamboo_raft")
	case wood == "bamboo":
		entries["id"] = nbt.String("minecraft:bamboo_chest_raft")
	case wood == "":
		wood = "oak"
		fallthrough
	default:
		suffix := "_boat"
		if id == "minecraft:chest_boat" {
			suffix = "_chest_boat"
		}
		entries["id"] = nbt.String("minecraft:" + wood + suffix)
	}
}
