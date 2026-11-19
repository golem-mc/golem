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
