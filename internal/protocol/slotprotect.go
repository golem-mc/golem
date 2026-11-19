package protocol

// readDeathProtection reads a minecraft:death_protection value: a list of
// consume effects. vanilla: DeathProtection.STREAM_CODEC
func readDeathProtection(r *Reader) ([]ConsumeEffectComponent, bool) {
	count := r.ReadVarInt()
	// An effect is at least a type ID and nothing more.
	if r.Err() != nil || count < 0 || int(count) > r.Remaining() {
		return nil, false
	}
	effects := make([]ConsumeEffectComponent, 0, count)
	for range count {
		e, ok := readConsumeEffect(r)
		if !ok {
			return nil, false
		}
		effects = append(effects, e)
	}
	return effects, r.Err() == nil
}

// rawOf returns the stack's raw value of a component, false when it has none.
func (s ItemStack) rawOf(component int32) ([]byte, bool) {
	for _, raw := range s.Raw {
		if raw.Type == component {
			return raw.Data, true
		}
	}
	return nil, false
}

// HasGlider reports whether the stack carries its own minecraft:glider.
func (s ItemStack) HasGlider() bool {
	_, ok := s.rawOf(DataComponentGlider)
	return ok
}

// SetGlider lays a minecraft:glider on the stack.
func (s *ItemStack) SetGlider() {
	s.Raw = append([]RawComponent(nil), s.Raw...)
	s.putRaw(RawComponent{Type: DataComponentGlider})
}

// DeathProtectionOf returns the death effects of the stack's own
// minecraft:death_protection, false for a stack without one.
func (s ItemStack) DeathProtectionOf() ([]ConsumeEffectComponent, bool) {
	data, ok := s.rawOf(DataComponentDeathProtection)
	if !ok {
		return nil, false
	}
	r := NewReader(data)
	effects, ok := readDeathProtection(r)
	return effects, ok && r.Remaining() == 0
}

// SetDeathProtection lays a minecraft:death_protection on the stack in its
// wire shape, replacing any the stack had.
func (s *ItemStack) SetDeathProtection(effects []ConsumeEffectComponent) {
	var value Buffer
	value.PutVarInt(int32(len(effects)))
	for _, e := range effects {
		putConsumeEffect(&value, e)
	}
	s.Raw = append([]RawComponent(nil), s.Raw...)
	s.putRaw(RawComponent{Type: DataComponentDeathProtection, Data: value.Bytes()})
}

// DamageTypeTagIndex is installed by registry to bind tag names while decoding.
// The IDs remain valid across reloads and never appear on the wire.
var DamageTypeTagIndex func(string) uint16

type damageComponents struct {
	blocks    BlocksAttacksComponent
	resistant DamageSetComponent
	valid     bool
}

func (raw *RawComponent) decodeDamageComponents() {
	if raw.Type != DataComponentDamageResistant && raw.Type != DataComponentBlocksAttacks {
		return
	}
	r := NewReader(raw.Data)
	d := new(damageComponents)
	if raw.Type == DataComponentDamageResistant {
		d.resistant, d.valid = readDamageTypes(r)
	} else {
		d.blocks, d.valid = readBlocksAttacks(r)
	}
	d.valid = d.valid && r.Remaining() == 0
	raw.damage = d
}

// DamageResistantOf returns an editable copy of the damage types of the stack's own
// minecraft:damage_resistant (always Present), false for a stack without one.
func (s ItemStack) DamageResistantOf() (DamageSetComponent, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentDamageResistant && raw.damage != nil {
			return raw.damage.resistant.clone(), raw.damage.valid
		}
	}
	data, ok := s.rawOf(DataComponentDamageResistant)
	if !ok {
		return DamageSetComponent{}, false
	}
	r := NewReader(data)
	set, ok := readDamageTypes(r)
	return set, ok && r.Remaining() == 0
}

// SetDamageResistant lays a minecraft:damage_resistant on the stack in its
// wire shape, replacing any the stack had.
func (s *ItemStack) SetDamageResistant(set DamageSetComponent) {
	var value Buffer
	putDamageTypes(&value, set)
	s.Raw = append([]RawComponent(nil), s.Raw...)
	s.putRaw(RawComponent{Type: DataComponentDamageResistant, Data: value.Bytes()})
}
