package datapack

import (
	"encoding/json"
	"fmt"

	"github.com/golem-mc/golem/registry"
)

// DecodeDamageType parses a damage_type JSON file. The optional
// death_message_type defaults to "default" and effects to "hurt".
// vanilla: DamageType.CODEC
func DecodeDamageType(data []byte) (registry.DamageTypeInfo, error) {
	var raw struct {
		MessageID        *string  `json:"message_id"`
		Scaling          *string  `json:"scaling"`
		Exhaustion       *float64 `json:"exhaustion"`
		DeathMessageType string   `json:"death_message_type"`
		Effects          string   `json:"effects"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return registry.DamageTypeInfo{}, err
	}
	if raw.MessageID == nil {
		return registry.DamageTypeInfo{}, fmt.Errorf("missing message_id")
	}
	if raw.Scaling == nil {
		return registry.DamageTypeInfo{}, fmt.Errorf("missing scaling")
	}
	if raw.Exhaustion == nil {
		return registry.DamageTypeInfo{}, fmt.Errorf("missing exhaustion")
	}
	t := registry.DamageTypeInfo{MessageID: *raw.MessageID, Exhaustion: *raw.Exhaustion}
	switch *raw.Scaling {
	case "never":
		t.Scaling = registry.DamageScalesNever
	case "when_caused_by_living_non_player":
		t.Scaling = registry.DamageScalesWhenCausedByLivingNonPlayer
	case "always":
		t.Scaling = registry.DamageScalesAlways
	default:
		return t, fmt.Errorf("unknown scaling %q", *raw.Scaling)
	}
	switch raw.DeathMessageType {
	case "", "default":
	case "fall_variants":
		t.DeathMessage = registry.DeathMessageFallVariants
	case "intentional_game_design":
		t.DeathMessage = registry.DeathMessageIntentionalGameDesign
	default:
		return t, fmt.Errorf("unknown death_message_type %q", raw.DeathMessageType)
	}
	switch raw.Effects {
	case "", "hurt":
	case "thorns":
		t.Effects = registry.DamageEffectsThorns
	case "drowning":
		t.Effects = registry.DamageEffectsDrowning
	case "burning":
		t.Effects = registry.DamageEffectsBurning
	case "poking":
		t.Effects = registry.DamageEffectsPoking
	case "freezing":
		t.Effects = registry.DamageEffectsFreezing
	default:
		return t, fmt.Errorf("unknown effects %q", raw.Effects)
	}
	return t, nil
}
