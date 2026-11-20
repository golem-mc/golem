package registry

// MobCategory is vanilla's MobCategory, the group whose spawn cap an entity
// type counts toward.
type MobCategory uint8

const (
	MobCategoryMonster MobCategory = iota
	MobCategoryCreature
	MobCategoryAmbient
	MobCategoryAxolotls
	MobCategoryUndergroundWaterCreature
	MobCategoryWaterCreature
	MobCategoryWaterAmbient
	MobCategoryMisc

	// MobCategoryCount is the number of categories, for arrays indexed by one.
	MobCategoryCount
)

var mobCategoryNames = [MobCategoryCount]string{
	"monster", "creature", "ambient", "axolotls", "underground_water_creature",
	"water_creature", "water_ambient", "misc",
}

// mobCategoryMax is MobCategory.max, the natural spawn cap per player area
// (-1 for MISC, which never spawns naturally).
var mobCategoryMax = [MobCategoryCount]int{70, 10, 15, 5, 5, 5, 20, -1}

// String returns the category's serialized name (MobCategory.getName).
func (c MobCategory) String() string {
	if c >= MobCategoryCount {
		return ""
	}
	return mobCategoryNames[c]
}

// Max is the vanilla spawn cap of the category (MobCategory.getMaxInstancesPerChunk).
func (c MobCategory) Max() int {
	if c >= MobCategoryCount {
		return -1
	}
	return mobCategoryMax[c]
}

// MobCategoryByName returns the category with a serialized name.
func MobCategoryByName(name string) (MobCategory, bool) {
	for c, n := range mobCategoryNames {
		if n == name {
			return MobCategory(c), true
		}
	}
	return 0, false
}

// Category is the entity type's MobCategory; out of range answers MISC.
func (t EntityType) Category() MobCategory {
	if t < 0 || int(t) >= len(entityTypeCategories) {
		return MobCategoryMisc
	}
	return entityTypeCategories[t]
}
