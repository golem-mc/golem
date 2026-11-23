package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: AttackRange.CODEC
var attackRangeDefaults = protocol.AttackRangeComponent{MaxReach: 3, MaxCreativeReach: 5, HitboxMargin: 0.3, MobFactor: 1}

// attackRangeOf reads the optional fields and bounds of AttackRange.CODEC.
func attackRangeOf(value nbt.Tag) (protocol.AttackRangeComponent, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.AttackRangeComponent{}, false
	}
	a := attackRangeDefaults
	for _, f := range []struct {
		key string
		dst *float32
		max float32
	}{
		{"min_reach", &a.MinReach, 64},
		{"max_reach", &a.MaxReach, 64},
		{"min_creative_reach", &a.MinCreativeReach, 64},
		{"max_creative_reach", &a.MaxCreativeReach, 64},
		{"hitbox_margin", &a.HitboxMargin, 1},
		{"mob_factor", &a.MobFactor, 2},
	} {
		if field, has := entry[f.key]; has {
			v, ok := floatOf(field)
			if !ok || v < 0 || v > f.max {
				return protocol.AttackRangeComponent{}, false
			}
			*f.dst = v
		}
	}
	return a, true
}

// attackRangeTag writes the fields that differ from the codec defaults.
func attackRangeTag(a protocol.AttackRangeComponent) *nbt.OrderedCompound {
	d := attackRangeDefaults
	out := nbt.NewOrderedCompound(6)
	for _, f := range []struct {
		key                 string
		value, defaultValue float32
	}{
		{"min_reach", a.MinReach, d.MinReach},
		{"max_reach", a.MaxReach, d.MaxReach},
		{"min_creative_reach", a.MinCreativeReach, d.MinCreativeReach},
		{"max_creative_reach", a.MaxCreativeReach, d.MaxCreativeReach},
		{"hitbox_margin", a.HitboxMargin, d.HitboxMargin},
		{"mob_factor", a.MobFactor, d.MobFactor},
	} {
		if f.value != f.defaultValue {
			out.Set(f.key, nbt.Float(f.value))
		}
	}
	return out
}

// withAttackRangeTag adds the stack's own stored attack_range to components.
func withAttackRangeTag(components nbt.Compound, stack protocol.ItemStack) nbt.Compound {
	if a, ok := stack.AttackRangeOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:attack_range"] = attackRangeTag(a)
	}
	return components
}
