package registry

import "testing"

// TestGeneratedDamageTypes checks entries of the generated table against
// vanilla's data/minecraft/damage_type files.
func TestGeneratedDamageTypes(t *testing.T) {
	if got := DamageTypes["minecraft:cactus"]; got.MessageID != "cactus" || got.Exhaustion != 0.1 {
		t.Errorf("cactus = %+v, want message_id cactus, exhaustion 0.1", got)
	}
	if got := DamageTypes["minecraft:fall"]; got.DeathMessage != DeathMessageFallVariants {
		t.Errorf("fall = %+v, want death message type fall_variants", got)
	}
	if got := DamageTypes["minecraft:bad_respawn_point"]; got.DeathMessage != DeathMessageIntentionalGameDesign || got.Scaling != DamageScalesAlways {
		t.Errorf("bad_respawn_point = %+v", got)
	}
}

// TestPackDamageTypeOverridesTheTable checks that ApplyPackDamageTypes layers
// over the table and that nil restores it.
func TestPackDamageTypeOverridesTheTable(t *testing.T) {
	t.Cleanup(func() { ApplyPackDamageTypes(nil) })
	ApplyPackDamageTypes(map[string]DamageTypeInfo{"minecraft:cactus": {MessageID: "spiky"}})
	if got := LookupDamageType("minecraft:cactus").MessageID; got != "spiky" {
		t.Errorf("overridden message_id = %q", got)
	}
	if got := LookupDamageType("minecraft:fall").MessageID; got != "fall" {
		t.Errorf("untouched fall message_id = %q", got)
	}
	ApplyPackDamageTypes(nil)
	if got := LookupDamageType("minecraft:cactus").MessageID; got != "cactus" {
		t.Errorf("restored message_id = %q", got)
	}
}
