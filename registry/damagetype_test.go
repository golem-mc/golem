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
