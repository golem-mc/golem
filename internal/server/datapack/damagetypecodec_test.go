package datapack

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestDecodeDamageTypeRequiredFields checks that message_id, scaling and
// exhaustion are required. vanilla: DamageType.DIRECT_CODEC
func TestDecodeDamageTypeRequiredFields(t *testing.T) {
	for _, tc := range []struct{ json, missing string }{
		{`{"scaling":"never","exhaustion":0.1}`, "message_id"},
		{`{"message_id":"zap","exhaustion":0.1}`, "scaling"},
		{`{"message_id":"zap","scaling":"never"}`, "exhaustion"},
	} {
		_, err := DecodeDamageType([]byte(tc.json))
		if err == nil || !strings.Contains(err.Error(), tc.missing) {
			t.Errorf("DecodeDamageType(%s) error = %v, want missing %s", tc.json, err, tc.missing)
		}
	}
	got, err := DecodeDamageType([]byte(`{"message_id":"zap","scaling":"always","exhaustion":0.5}`))
	if err != nil || got.MessageID != "zap" || got.Exhaustion != 0.5 {
		t.Errorf("DecodeDamageType(complete) = %+v, %v", got, err)
	}
}

// TestDecodeDamageTypeEffects checks that effects defaults to hurt, is read
// when given and refuses an unknown value. vanilla: DamageEffects.CODEC
func TestDecodeDamageTypeEffects(t *testing.T) {
	const head = `{"message_id":"zap","scaling":"never","exhaustion":0.1`
	for _, tc := range []struct {
		json string
		want registry.DamageEffects
	}{
		{head + `}`, registry.DamageEffectsHurt},
		{head + `,"effects":"burning"}`, registry.DamageEffectsBurning},
		{head + `,"effects":"freezing"}`, registry.DamageEffectsFreezing},
	} {
		got, err := DecodeDamageType([]byte(tc.json))
		if err != nil || got.Effects != tc.want {
			t.Errorf("DecodeDamageType(%s) effects = %v, %v, want %v", tc.json, got.Effects, err, tc.want)
		}
	}
	if _, err := DecodeDamageType([]byte(head + `,"effects":"zapping"}`)); err == nil || !strings.Contains(err.Error(), "effects") {
		t.Errorf("DecodeDamageType(unknown effects) error = %v, want unknown effects", err)
	}
}
