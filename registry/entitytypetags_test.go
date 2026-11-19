package registry

import "testing"

func TestEntityTypeTagMembership(t *testing.T) {
	if !EntityTypeArrow.InTag(EntityTypeTagIDImpactProjectiles) {
		t.Error("arrow is not in impact_projectiles")
	}
	if EntityTypeCow.InTag(EntityTypeTagIDImpactProjectiles) {
		t.Error("cow is in impact_projectiles")
	}
	if EntityType(-1).InTag(EntityTypeTagIDUndead) || EntityType(EntityTypeCount).InTag(EntityTypeTagIDUndead) {
		t.Error("out-of-range type is in a tag")
	}
	if EntityType(0).InTag(EntityTypeTagOf("minecraft:nope")) {
		t.Error("unknown tag has members")
	}
}

func TestEntityTypeNames(t *testing.T) {
	if EntityTypeCow.Name() != "minecraft:cow" || EntityType(-1).Name() != "" {
		t.Error("Name wrong")
	}
	if id, ok := EntityTypeByName("minecraft:cow"); !ok || id != EntityTypeCow {
		t.Error("EntityTypeByName(cow) wrong")
	}
}
