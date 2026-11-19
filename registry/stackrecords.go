package registry

import (
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// recordFieldsOf returns the fields of a record component by its qualified
// name, nil for any other component.
func recordFieldsOf(name string) (int32, []protocol.RecordField) {
	id, ok := ComponentID(name)
	if !ok {
		return 0, nil
	}
	return id, protocol.RecordComponents[id]
}

// readRecordComponent lays a record component on the stack; modeled false
// for any other name.
// vanilla: UseEffects.CODEC, SwingAnimation.CODEC, KineticWeapon.CODEC,
// PiercingWeapon.CODEC, CookingFuel.CODEC, BrewingFuel.CODEC,
// VillagerFood.CODEC, MobVisibility.CODEC
func readRecordComponent(stack *protocol.ItemStack, name string, value nbt.Tag) (ok, modeled bool) {
	id, fields := recordFieldsOf(name)
	if fields == nil {
		return false, false
	}
	values, ok := recordValuesOf(fields, value)
	if ok {
		stack.SetRecord(id, values)
	}
	return ok, true
}

func recordValuesOf(fields []protocol.RecordField, value nbt.Tag) ([]any, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return nil, false
	}
	values := make([]any, len(fields))
	for i, f := range fields {
		tag, has := entry[f.Key]
		if !has {
			if f.Mode == protocol.FieldRequired {
				return nil, false
			}
			values[i] = f.Def
			continue
		}
		if values[i], ok = recordFieldOf(f, tag); !ok {
			return nil, false
		}
	}
	return values, true
}

func recordFieldOf(f protocol.RecordField, tag nbt.Tag) (any, bool) {
	switch f.Kind {
	case protocol.RecordBool:
		_, ok := nbt.ByteValue(tag)
		return protocol.FlagOn(tag), ok
	case protocol.RecordVarInt:
		n, ok := nbt.IntValue(tag)
		return n, ok && (!f.Ranged || float64(n) >= f.Min && float64(n) <= f.Max)
	case protocol.RecordFloat:
		v, ok := floatOf(tag)
		return v, ok && (!f.Ranged || float64(v) >= f.Min && float64(v) <= f.Max)
	case protocol.RecordEnum:
		name, ok := tag.(nbt.String)
		for i, n := range f.Names {
			if ok && n == string(name) {
				return int32(i), true
			}
		}
		return nil, false
	case protocol.RecordResInt:
		if n, ok := nbt.IntValue(tag); ok {
			return protocol.Resolvable{Int: n}, true
		}
		return resolvableProviderOf(tag)
	case protocol.RecordResFloat:
		if v, ok := floatOf(tag); ok {
			return protocol.Resolvable{Float: v}, true
		}
		return resolvableProviderOf(tag)
	case protocol.RecordSound:
		return soundHolderOf(tag)
	case protocol.RecordEntitySet:
		return entitySetOf(tag)
	default:
		return recordValuesOf(f.Fields, tag)
	}
}

func resolvableProviderOf(tag nbt.Tag) (any, bool) {
	name, ok := tag.(nbt.String)
	if !ok || !protocol.ValidIdentifier(string(name)) {
		return nil, false
	}
	return protocol.Resolvable{Provider: Qualify(string(name))}, true
}

// recordTag writes a record in its CODEC's stored shape, leaving out the
// fields that hold their defaults and the absent optionals.
func recordTag(fields []protocol.RecordField, values []any) (*nbt.OrderedCompound, bool) {
	out := nbt.NewOrderedCompound(len(fields))
	for i, f := range fields {
		v := values[i]
		if v == nil || f.Mode == protocol.FieldDefault && v == f.Def {
			continue
		}
		var tag nbt.Tag
		ok := true
		switch v := v.(type) {
		case bool:
			tag = nbt.Byte(boolByte(v))
		case int32:
			tag = nbt.Int(v)
			if f.Kind == protocol.RecordEnum {
				tag = nbt.String(f.Names[v])
			}
		case float32:
			tag = nbt.Float(v)
		case protocol.Resolvable:
			switch {
			case v.Provider != "":
				tag = nbt.String(v.Provider)
			case f.Kind == protocol.RecordResInt:
				tag = nbt.Int(v.Int)
			default:
				tag = nbt.Float(v.Float)
			}
		case protocol.SoundHolder:
			tag, ok = soundHolderTag(v)
		case protocol.EntityTypeSet:
			tag = entitySetTag(v)
		case []any:
			tag, ok = recordTag(f.Fields, v)
		}
		if !ok {
			return nil, false
		}
		out.Set(f.Key, tag)
	}
	return out, true
}

// withRecordTags adds the stack's own record components to components.
func withRecordTags(components nbt.Compound, stack protocol.ItemStack) nbt.Compound {
	for _, raw := range stack.Raw {
		fields := protocol.RecordComponents[raw.Type]
		if fields == nil {
			continue
		}
		values, ok := stack.RecordOf(raw.Type)
		if !ok {
			continue
		}
		if tag, ok := recordTag(fields, values); ok {
			if components == nil {
				components = nbt.Compound{}
			}
			components[componentNames[raw.Type]] = tag
		}
	}
	return components
}

// recordMalformed words why a record's stored shape does not parse, field
// errors last field first, or "" when it parses or the failure is a sound
// or entity set the codec names with a registry's own text.
// vanilla: RecordCodecBuilder, MapCodec.decode
func recordMalformed(fields []protocol.RecordField, tag nbt.Tag) string {
	entry, ok := nbt.CompoundView(tag)
	if !ok {
		return "Not a map: " + nbt.Stringify(tag)
	}
	msg := ""
	for _, f := range fields {
		failure := ""
		if value, has := entry[f.Key]; has {
			failure = recordFieldMalformed(f, value)
		} else if f.Mode == protocol.FieldRequired {
			failure = "No key " + f.Key + " in MapLike[" + nbt.Stringify(tag) + "]"
		}
		msg = joinFieldErrors(failure, msg)
	}
	return msg
}

func recordFieldMalformed(f protocol.RecordField, tag nbt.Tag) string {
	switch f.Kind {
	case protocol.RecordBool:
		return textNumberMalformed(tag)
	case protocol.RecordVarInt:
		n, ok := nbt.IntValue(tag)
		if !ok {
			return "Not a number"
		}
		if f.Ranged && (float64(n) < f.Min || float64(n) > f.Max) {
			return rangeMessage(f, strconv.FormatInt(int64(n), 10))
		}
	case protocol.RecordFloat:
		v, ok := floatOf(tag)
		if !ok {
			return "Not a number"
		}
		if f.Ranged && (float64(v) < f.Min || float64(v) > f.Max) {
			return rangeMessage(f, javaFloat(v))
		}
	case protocol.RecordEnum:
		name, ok := tag.(nbt.String)
		if !ok {
			return "Not a string"
		}
		if _, ok := recordFieldOf(f, tag); !ok {
			return "Unknown element name:" + string(name)
		}
	case protocol.RecordResInt, protocol.RecordResFloat:
		if _, ok := recordFieldOf(f, tag); !ok {
			return "Failed to parse either. First: Not a number; Second: " + resourceLocationMalformed(tag)
		}
	case protocol.RecordNested:
		return recordMalformed(f.Fields, tag)
	}
	return ""
}

// rangeMessage is the codec's refusal of a bounded number: its own word
// for the two common bounds, else the range.
func rangeMessage(f protocol.RecordField, shown string) string {
	switch {
	case f.Kind == protocol.RecordVarInt && f.Min == 0:
		return "Value must be non-negative: " + shown
	case f.Kind == protocol.RecordVarInt && f.Min == 1:
		return "Value must be positive: " + shown
	}
	return "Value must be within range [" + javaFloat(float32(f.Min)) + ";" + javaFloat(float32(f.Max)) + "]: " + shown
}

// javaFloat prints a float the way Float.toString does for ordinary values.
func javaFloat(v float32) string {
	s := strconv.FormatFloat(float64(v), 'f', -1, 32)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}
