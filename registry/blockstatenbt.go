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
// writes it). A missing name reads as "", and absent property overrides read
// as nil.
// vanilla: BlockState.CODEC
func BlockStateOf(tag nbt.Tag) (string, map[string]string) {
	name, props, _ := blockStateOf(tag, true)
	return name, props
}

// BlockStateOfCodec reads a 26.3 BlockState.CODEC tag: a bare block name
// string, or a compound carrying a string id and an optional properties
// compound. It rejects the legacy Name/Properties form; a malformed
// properties field or non-string property value is ignored so FindState
// leaves those properties at the block's default.
// vanilla: BlockState.CODEC
func BlockStateOfCodec(tag nbt.Tag) (string, map[string]string, bool) {
	return blockStateOf(tag, false)
}

func blockStateOf(tag nbt.Tag, legacy bool) (string, map[string]string, bool) {
	if s, ok := tag.(nbt.String); ok {
		return string(s), nil, true
	}
	block, ok := nbt.CompoundView(tag)
	if !ok {
		return "", nil, false
	}
	name, ok := nbt.Get[nbt.String](block, "id")
	key := "properties"
	if !ok {
		if !legacy {
			return "", nil, false
		}
		name, ok = nbt.Get[nbt.String](block, "Name")
		if !ok {
			return "", nil, false
		}
		key = "Properties"
	}
	var props map[string]string
	if properties, ok := nbt.Get[nbt.Compound](block, key); ok && len(properties) > 0 {
		for k, value := range properties {
			if s, ok := value.(nbt.String); ok {
				if props == nil {
					props = make(map[string]string, len(properties))
				}
				props[k] = string(s)
			}
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
