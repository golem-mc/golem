package registry

import "testing"

func TestEntityTypeCategory(t *testing.T) {
	for name, want := range map[string]MobCategory{
		"minecraft:zombie":      MobCategoryMonster,
		"minecraft:cow":         MobCategoryCreature,
		"minecraft:bat":         MobCategoryAmbient,
		"minecraft:axolotl":     MobCategoryAxolotls,
		"minecraft:glow_squid":  MobCategoryUndergroundWaterCreature,
		"minecraft:squid":       MobCategoryWaterCreature,
		"minecraft:cod":         MobCategoryWaterAmbient,
		"minecraft:armor_stand": MobCategoryMisc,
		"minecraft:villager":    MobCategoryMisc,
		"minecraft:iron_golem":  MobCategoryMisc,
	} {
		id, ok := EntityTypeByName(name)
		if !ok {
			t.Fatalf("no entity type %s", name)
		}
		if got := id.Category(); got != want {
			t.Errorf("%s: category %v, want %v", name, got, want)
		}
	}
	if len(entityTypeCategories) != len(EntityTypes) {
		t.Errorf("%d categories for %d types", len(entityTypeCategories), len(EntityTypes))
	}
	if MobCategoryCreature.Max() != 10 || MobCategoryMisc.Max() != -1 {
		t.Error("category caps differ from MobCategory.max")
	}
}
