package datapack

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/golem-mc/golem/registry"
)

// DecodeSulfurArchetype parses a sulfur_cube_archetype JSON file. Contact
// damage amounts are a number or a uniform provider.
// vanilla: SulfurCubeArchetype.DIRECT_CODEC
func DecodeSulfurArchetype(data []byte) (registry.SulfurArchetype, error) {
	var raw struct {
		Items              json.RawMessage `json:"items"`
		AttributeModifiers *[]struct {
			Attribute string   `json:"attribute"`
			ID        string   `json:"id"`
			Amount    *float64 `json:"amount"`
			Operation string   `json:"operation"`
		} `json:"attribute_modifiers"`
		Buoyant   bool `json:"buoyant"`
		Explosion *struct {
			Power      *int  `json:"power"`
			CausesFire *bool `json:"causes_fire"`
			Fuse       *int  `json:"fuse"`
		} `json:"explosion"`
		ContactDamage *struct {
			DamageType        string          `json:"damage_type"`
			Amount            json.RawMessage `json:"amount"`
			AttributeToSource *bool           `json:"attribute_to_source"`
		} `json:"contact_damage"`
		Knockback *struct {
			Horizontal *float32 `json:"horizontal_power"`
			Vertical   *float32 `json:"vertical_power"`
		} `json:"knockback_modifiers"`
		Sounds *struct {
			Hit       string   `json:"hit_sound"`
			Push      string   `json:"push_sound"`
			Threshold *float32 `json:"push_sound_impulse_threshold"`
			Cooldown  *float32 `json:"push_sound_cooldown"`
		} `json:"sound_settings"`
	}
	var a registry.SulfurArchetype
	if err := json.Unmarshal(data, &raw); err != nil {
		return a, err
	}
	if raw.AttributeModifiers == nil || raw.Knockback == nil || raw.Sounds == nil || len(raw.Items) == 0 {
		return a, fmt.Errorf("missing items, attribute_modifiers, knockback_modifiers or sound_settings")
	}
	var one string
	var list []string
	switch {
	case json.Unmarshal(raw.Items, &one) == nil && strings.HasPrefix(one, "#"):
		a.ItemTag = one[1:]
	case json.Unmarshal(raw.Items, &one) == nil:
		list = []string{one}
	case json.Unmarshal(raw.Items, &list) != nil:
		return a, fmt.Errorf("items is neither a tag, an item nor a list")
	}
	for _, name := range list {
		item, ok := registry.ItemByName(name)
		if !ok {
			return a, fmt.Errorf("unknown item %q", name)
		}
		a.Items = append(a.Items, item)
	}
	for _, m := range *raw.AttributeModifiers {
		attr, ok := registry.AttributeIDByName(m.Attribute)
		op, opOK := registry.AttributeOperationByName(m.Operation)
		if !ok || !opOK || m.Amount == nil || m.ID == "" {
			return a, fmt.Errorf("bad attribute modifier %q", m.ID)
		}
		a.Modifiers = append(a.Modifiers, registry.SulfurModifier{Attribute: attr, ID: m.ID, Amount: *m.Amount, Operation: op})
	}
	a.Buoyant = raw.Buoyant
	if x := raw.Explosion; x != nil {
		if x.Power == nil || x.CausesFire == nil || x.Fuse == nil || *x.Power < 0 || *x.Fuse < 1 {
			return a, fmt.Errorf("bad explosion")
		}
		a.Explosion = &registry.SulfurExplosion{Power: *x.Power, CausesFire: *x.CausesFire, Fuse: *x.Fuse}
	}
	if c := raw.ContactDamage; c != nil {
		damageType, ok := registry.DamageTypeByName(c.DamageType)
		if !ok || c.AttributeToSource == nil {
			return a, fmt.Errorf("bad contact_damage")
		}
		lo, hi, err := amountBounds(c.Amount)
		if err != nil {
			return a, fmt.Errorf("contact_damage amount: %w", err)
		}
		a.Contact = &registry.SulfurContact{DamageType: damageType, Min: lo, Max: hi, AttributeToSource: *c.AttributeToSource}
	}
	if raw.Knockback.Horizontal == nil || raw.Knockback.Vertical == nil {
		return a, fmt.Errorf("bad knockback_modifiers")
	}
	a.KnockbackHorizontal, a.KnockbackVertical = *raw.Knockback.Horizontal, *raw.Knockback.Vertical
	hit, hitOK := registry.SoundEventID(raw.Sounds.Hit)
	push, pushOK := registry.SoundEventID(raw.Sounds.Push)
	if !hitOK || !pushOK || raw.Sounds.Threshold == nil || raw.Sounds.Cooldown == nil {
		return a, fmt.Errorf("bad sound_settings")
	}
	a.HitSound, a.PushSound = registry.Sound(hit+1), registry.Sound(push+1)
	a.PushThreshold, a.PushCooldown = *raw.Sounds.Threshold, *raw.Sounds.Cooldown
	return a, nil
}

// amountBounds reads a constant (a bare number or a constant provider) or a
// uniform one as its bounds, at least zero.
// vanilla: FloatProviders.codec
func amountBounds(data json.RawMessage) (lo, hi float32, err error) {
	var n float32
	if json.Unmarshal(data, &n) == nil {
		lo, hi = n, n
	} else {
		var p struct {
			Type  string   `json:"type"`
			Value *float32 `json:"value"`
			Min   *float32 `json:"min_inclusive"`
			Max   *float32 `json:"max_exclusive"`
		}
		if err := json.Unmarshal(data, &p); err != nil {
			return 0, 0, err
		}
		switch p.Type {
		case "constant", "minecraft:constant":
			if p.Value == nil {
				return 0, 0, fmt.Errorf("missing value")
			}
			lo, hi = *p.Value, *p.Value
		case "uniform", "minecraft:uniform":
			if p.Min == nil || p.Max == nil || *p.Min >= *p.Max {
				return 0, 0, fmt.Errorf("bad uniform bounds")
			}
			lo, hi = *p.Min, *p.Max
		default:
			return 0, 0, fmt.Errorf("unsupported provider %q", p.Type)
		}
	}
	if lo < 0 {
		return 0, 0, fmt.Errorf("below 0")
	}
	return lo, hi, nil
}
