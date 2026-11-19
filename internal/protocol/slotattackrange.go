package protocol

import "slices"

// AttackRangeComponent is a stack's own minecraft:attack_range, in blocks.
// vanilla: AttackRange.STREAM_CODEC
type AttackRangeComponent struct {
	MinReach, MaxReach                 float32
	MinCreativeReach, MaxCreativeReach float32
	HitboxMargin                       float32
	MobFactor                          float32
}

// readAttackRange reads the six floats of AttackRange.STREAM_CODEC.
func readAttackRange(r *Reader) (AttackRangeComponent, bool) {
	a := AttackRangeComponent{
		MinReach: r.ReadF32(), MaxReach: r.ReadF32(),
		MinCreativeReach: r.ReadF32(), MaxCreativeReach: r.ReadF32(),
		HitboxMargin: r.ReadF32(), MobFactor: r.ReadF32(),
	}
	return a, r.Err() == nil && finite(a.MinReach, a.MaxReach, a.MinCreativeReach, a.MaxCreativeReach, a.HitboxMargin, a.MobFactor)
}

// AttackRangeOf returns the stack's own minecraft:attack_range, if present.
func (s ItemStack) AttackRangeOf() (AttackRangeComponent, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentAttackRange {
			r := NewReader(raw.Data)
			a, ok := readAttackRange(r)
			return a, ok && r.Remaining() == 0
		}
	}
	return AttackRangeComponent{}, false
}

// SetAttackRange replaces the stack's own minecraft:attack_range.
func (s *ItemStack) SetAttackRange(a AttackRangeComponent) {
	var value Buffer
	value.PutF32(a.MinReach)
	value.PutF32(a.MaxReach)
	value.PutF32(a.MinCreativeReach)
	value.PutF32(a.MaxCreativeReach)
	value.PutF32(a.HitboxMargin)
	value.PutF32(a.MobFactor)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentAttackRange, Data: value.Bytes()})
}
