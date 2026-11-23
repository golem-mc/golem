package registry

import (
	"github.com/golem-mc/golem/nbt"
	"maps"
	"slices"
)

// BlockStateTag writes a full block state: id,
// plus a properties compound of string values when props (the sorted
// key=value form of BlockStateName) is non-empty.
// vanilla: NbtUtils.writeBlockState
func BlockStateTag(name, props string) nbt.Compound {
	tag := nbt.Compound{"id": nbt.String(name)}
	if props != "" {
		values := nbt.Compound{}
		for key, value := range ParseStateProps(props) {
			values[key] = nbt.String(value)
		}
		tag["properties"] = values
	}
	return tag
}

// BlockStateCodecTag writes default states as names and other states in full.
// An unknown state is written as air.
// vanilla: BlockState.CODEC
func BlockStateCodecTag(state uint16) nbt.Tag {
	block := BlockOf(state)
	if state == block.DefaultState() || int(state) >= StateCount {
		return nbt.String(block.Name())
	}
	name, props, _ := BlockStateName(state)
	tag := BlockStateTag(name, props)
	values, ok := nbt.Get[nbt.Compound](tag, "properties")
	if ok {
		// StateDefinition.appendPropertyCodec nests PairMapCodec, which encodes
		// the second member first, reversing the sorted property declarations.
		ordered := nbt.NewOrderedCompound(len(values))
		keys := slices.Sorted(maps.Keys(values))
		for _, key := range slices.Backward(keys) {
			ordered.Set(key, values[key])
		}
		tag["properties"] = ordered
	}
	return tag
}

// BlockStateOf reads a block state tag's name and its string-valued
// properties, the shape FindState takes. It accepts id/properties, the legacy
// Name/Properties, and a bare string (a default state, as BlockState.CODEC
// writes it). A missing name reads as "", and the map is never nil.
// vanilla: BlockState.CODEC
func BlockStateOf(tag nbt.Tag) (string, map[string]string) {
	props := map[string]string{}
	if s, ok := tag.(nbt.String); ok {
		return string(s), props
	}
	block, _ := nbt.CompoundView(tag)
	name, ok := nbt.Get[nbt.String](block, "id")
	key := "properties"
	if !ok {
		name, _ = nbt.Get[nbt.String](block, "Name")
		key = "Properties"
	}
	if properties, ok := nbt.Get[nbt.Compound](block, key); ok {
		for k, value := range properties {
			if s, ok := value.(nbt.String); ok {
				props[k] = string(s)
			}
		}
	}
	return string(name), props
}

// BlockStateOfCodec reads a 26.3 BlockState.CODEC tag: a bare block name
// string, or a compound carrying a string id and an optional string-valued
// properties compound. It rejects the legacy Name/Properties form and any
// non-string property value.
// vanilla: BlockState.CODEC
func BlockStateOfCodec(tag nbt.Tag) (string, map[string]string, bool) {
	if s, ok := tag.(nbt.String); ok {
		return string(s), map[string]string{}, true
	}
	block, ok := nbt.CompoundView(tag)
	if !ok {
		return "", nil, false
	}
	name, ok := nbt.Get[nbt.String](block, "id")
	if !ok {
		return "", nil, false
	}
	props := map[string]string{}
	if p, has := block["properties"]; has {
		parsed, ok := BlockStatePropertiesTag(p)
		if !ok {
			return "", nil, false
		}
		if parsed != nil {
			props = parsed
		}
	}
	return string(name), props, true
}

// StateOfTag decodes a block state tag (see BlockStateOf) to a state ID;
// false for an unknown block. A property the block lacks, or a value it does
// not take, leaves that property at the block's default.
func StateOfTag(tag nbt.Tag) (uint16, bool) {
	name, props := BlockStateOf(tag)
	return FindState(Qualify(name), props)
}
