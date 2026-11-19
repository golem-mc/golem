package protocol

import "math"

// RecordKind is the wire type of one record component field.
type RecordKind uint8

const (
	RecordBool RecordKind = iota
	RecordVarInt
	RecordFloat
	RecordEnum
	RecordResInt
	RecordResFloat
	RecordSound
	RecordEntitySet
	RecordNested
)

// FieldMode says how a field is absent in the stored shape.
type FieldMode uint8

const (
	// FieldRequired has no default: the codec refuses a missing key.
	FieldRequired FieldMode = iota
	// FieldDefault falls back to Def, which the stored shape leaves out.
	FieldDefault
	// FieldOptional is an Optional<T>: a presence flag on the wire.
	FieldOptional
)

// RecordField is one field of a record component's CODEC and STREAM_CODEC,
// which share the field order.
type RecordField struct {
	Key  string
	Kind RecordKind
	Mode FieldMode
	// Def is the FieldDefault value: bool, int32 (an enum's index
	// included) or float32.
	Def any
	// Ranged bounds a VarInt or Float to [Min, Max].
	Ranged   bool
	Min, Max float64
	// Names is an enum's constants by wire ID.
	Names []string
	// Fields is a RecordNested field's own record.
	Fields []RecordField
}

// Resolvable is a ResolvableInt or ResolvableFloat: a constant, or the key
// of a context provider when Provider is set.
// vanilla: ResolvableInt.STREAM_CODEC, ResolvableFloat.STREAM_CODEC
type Resolvable struct {
	Provider string
	Int      int32
	Float    float32
}

func intField(key string, mode FieldMode, def any, min float64) RecordField {
	return RecordField{Key: key, Kind: RecordVarInt, Mode: mode, Def: def, Ranged: true, Min: min, Max: math.MaxInt32}
}

func floatField(key string, def float32) RecordField {
	return RecordField{Key: key, Kind: RecordFloat, Mode: FieldDefault, Def: def}
}

func swingAnimation() []RecordField {
	return []RecordField{
		{Key: "type", Kind: RecordEnum, Mode: FieldDefault, Def: int32(1), Names: []string{"none", "whack", "stab"}},
		intField("duration", FieldDefault, int32(6), 0),
	}
}

func kineticCondition(key string) RecordField {
	return RecordField{Key: key, Kind: RecordNested, Mode: FieldOptional, Fields: []RecordField{
		intField("max_duration_ticks", FieldRequired, nil, 0),
		floatField("min_speed", 0),
		floatField("min_relative_speed", 0),
	}}
}

// RecordComponents is the record components held as raw bytes whose only
// reading is the codec round trip.
// vanilla: UseEffects, SwingAnimation, KineticWeapon, PiercingWeapon,
// CookingFuel, BrewingFuel, VillagerFood, MobVisibility
var RecordComponents = map[int32][]RecordField{
	DataComponentUseEffects: {
		{Key: "can_sprint", Kind: RecordBool, Mode: FieldDefault, Def: false},
		{Key: "interact_vibrations", Kind: RecordBool, Mode: FieldDefault, Def: true},
		{Key: "speed_multiplier", Kind: RecordFloat, Mode: FieldDefault, Def: float32(0.2), Ranged: true, Min: 0, Max: 1},
	},
	DataComponentAttackAnimation:   swingAnimation(),
	DataComponentInteractAnimation: swingAnimation(),
	DataComponentKineticWeapon: {
		intField("contact_cooldown_ticks", FieldDefault, int32(10), 0),
		intField("delay_ticks", FieldDefault, int32(0), 0),
		kineticCondition("dismount_conditions"),
		kineticCondition("knockback_conditions"),
		kineticCondition("damage_conditions"),
		floatField("forward_movement", 0),
		floatField("damage_multiplier", 1),
		{Key: "sound", Kind: RecordSound, Mode: FieldOptional},
		{Key: "hit_sound", Kind: RecordSound, Mode: FieldOptional},
	},
	DataComponentPiercingWeapon: {
		{Key: "deals_knockback", Kind: RecordBool, Mode: FieldDefault, Def: true},
		{Key: "dismounts", Kind: RecordBool, Mode: FieldDefault, Def: false},
		{Key: "sound", Kind: RecordSound, Mode: FieldOptional},
		{Key: "hit_sound", Kind: RecordSound, Mode: FieldOptional},
	},
	DataComponentCookingFuel: {
		{Key: "burn_time", Kind: RecordResInt},
		{Key: "speed_multiplier", Kind: RecordResFloat},
	},
	DataComponentBrewingFuel: {
		{Key: "uses", Kind: RecordResInt},
		{Key: "speed_multiplier", Kind: RecordResFloat},
	},
	DataComponentVillagerFood: {
		intField("nutrition", FieldRequired, nil, 1),
	},
	DataComponentMobVisibility: {
		{Key: "targeting_entity_types", Kind: RecordEntitySet},
		{Key: "visibility", Kind: RecordFloat, Ranged: true, Min: 0, Max: 10},
	},
}

// ReadRecord reads one record's STREAM_CODEC as a value per field: bool,
// int32, float32, Resolvable, SoundHolder, EntityTypeSet or []any for a
// nested record, and nil for an absent optional.
func ReadRecord(r *Reader, fields []RecordField) ([]any, bool) {
	values := make([]any, len(fields))
	for i, f := range fields {
		if f.Mode == FieldOptional && !r.ReadBool() {
			continue
		}
		var ok bool
		if values[i], ok = readRecordValue(r, f); !ok || r.Err() != nil {
			return nil, false
		}
	}
	return values, true
}

func readRecordValue(r *Reader, f RecordField) (any, bool) {
	switch f.Kind {
	case RecordBool:
		return r.ReadBool(), true
	case RecordVarInt:
		n := r.ReadVarInt()
		return n, !f.Ranged || float64(n) >= f.Min && float64(n) <= f.Max
	case RecordEnum:
		n := r.ReadVarInt()
		return n, n >= 0 && int(n) < len(f.Names)
	case RecordFloat:
		v := r.ReadF32()
		return v, finite(v) && (!f.Ranged || float64(v) >= f.Min && float64(v) <= f.Max)
	case RecordResInt, RecordResFloat:
		var v Resolvable
		if r.ReadBool() {
			if f.Kind == RecordResInt {
				v.Int = r.ReadI32()
			} else {
				v.Float = r.ReadF32()
			}
			return v, finite(v.Float)
		}
		v.Provider = r.ReadString(32767)
		return v, ValidIdentifier(v.Provider)
	case RecordSound:
		return readSoundHolder(r)
	case RecordEntitySet:
		return readEntitySet(r)
	default:
		return ReadRecord(r, f.Fields)
	}
}

// PutRecord writes values as ReadRecord reads them.
func PutRecord(b *Buffer, fields []RecordField, values []any) {
	for i, f := range fields {
		if f.Mode == FieldOptional {
			b.PutBool(values[i] != nil)
			if values[i] == nil {
				continue
			}
		}
		switch v := values[i].(type) {
		case bool:
			b.PutBool(v)
		case int32:
			b.PutVarInt(v)
		case float32:
			b.PutF32(v)
		case Resolvable:
			b.PutBool(v.Provider == "")
			switch {
			case v.Provider != "":
				b.PutString(v.Provider)
			case f.Kind == RecordResInt:
				b.PutI32(v.Int)
			default:
				b.PutF32(v.Float)
			}
		case SoundHolder:
			PutSoundHolder(b, v)
		case EntityTypeSet:
			v.Put(b)
		case []any:
			PutRecord(b, f.Fields, v)
		}
	}
}

// Put writes the set as readEntitySet reads it.
func (set EntityTypeSet) Put(b *Buffer) {
	if set.Tag != "" {
		b.PutVarInt(0)
		b.PutString(set.Tag)
		return
	}
	b.PutVarInt(int32(len(set.IDs)) + 1)
	for _, id := range set.IDs {
		b.PutVarInt(id)
	}
}

// RecordOf returns the stack's own record component, false when it has
// none or holds bytes that do not read.
func (s ItemStack) RecordOf(component int32) ([]any, bool) {
	for _, raw := range s.Raw {
		if raw.Type == component {
			r := NewReader(raw.Data)
			values, ok := ReadRecord(r, RecordComponents[component])
			return values, ok && r.Remaining() == 0
		}
	}
	return nil, false
}

// SetRecord lays a record component on the stack in its wire shape.
func (s *ItemStack) SetRecord(component int32, values []any) {
	var b Buffer
	PutRecord(&b, RecordComponents[component], values)
	s.Raw = append([]RawComponent(nil), s.Raw...)
	s.putRaw(RawComponent{Type: component, Data: b.Bytes()})
}
