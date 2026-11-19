package registry

// The statistic types, by the names the stats file groups its counters under.
const (
	StatMined    = "minecraft:mined"
	StatCrafted  = "minecraft:crafted"
	StatUsed     = "minecraft:used"
	StatBroken   = "minecraft:broken"
	StatPickedUp = "minecraft:picked_up"
	StatDropped  = "minecraft:dropped"
	StatKilled   = "minecraft:killed"
	StatKilledBy = "minecraft:killed_by"
	StatCustom   = "minecraft:custom"
)

var statTypeIDs = indexNames(StatTypes)

var customStatIDs = indexNames(CustomStats)

func indexNames(names []string) map[string]int32 {
	ids := make(map[string]int32, len(names))
	for id, name := range names {
		ids[name] = int32(id)
	}
	return ids
}

// StatID returns the numeric IDs the Award Statistics packet names a
// statistic by: the type's in minecraft:stat_type and the key's in the
// registry that type counts over (blocks for mined, entity types for killed
// and killed_by, minecraft:custom_stat for custom, items for the rest). A
// type or key this version does not have reads as false.
// vanilla: StatType.getRegistry
func StatID(statType, key string) (typeID, keyID int32, ok bool) {
	typeID, ok = statTypeIDs[statType]
	if !ok {
		return 0, 0, false
	}
	switch statType {
	case StatMined:
		keyID, ok = BlockIDs[key]
	case StatKilled, StatKilledBy:
		keyID, ok = EntityTypeID(key)
	case StatCustom:
		keyID, ok = customStatIDs[key]
	default:
		keyID, ok = ItemID(key)
	}
	return typeID, keyID, ok
}

// NoCustomStat is a CustomStat that names no statistic, for a rule that
// counts none.
const NoCustomStat CustomStat = -1

// Name is the statistic type's resource location.
func (t StatType) Name() string { return StatTypes[t] }

// Name is the custom statistic's resource location.
func (k CustomStat) Name() string { return CustomStats[k] }

// StatName is StatID's inverse: the type's and key's resource locations,
// false for an ID this version does not have.
func StatName(typeID, keyID int32) (statType, key string, ok bool) {
	if typeID < 0 || int(typeID) >= len(StatTypes) {
		return "", "", false
	}
	switch StatType(typeID) {
	case StatTypeMined:
		key, ok = blockNamesByID[keyID]
	case StatTypeKilled, StatTypeKilledBy:
		if ok = keyID >= 0 && int(keyID) < len(EntityTypes); ok {
			key = EntityTypes[keyID]
		}
	case StatTypeCustom:
		if ok = keyID >= 0 && int(keyID) < len(CustomStats); ok {
			key = CustomStats[keyID]
		}
	default:
		key, ok = ItemName(keyID)
	}
	return StatTypes[typeID], key, ok
}
