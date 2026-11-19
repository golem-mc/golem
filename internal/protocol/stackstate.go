package protocol

import (
	"maps"
	"slices"
	"unicode/utf16"

	"github.com/golem-mc/golem/nbt"
)

// BlockStatePropertiesTag returns the native block-state property traversal.
func (s ItemStack) BlockStatePropertiesTag() (*nbt.OrderedCompound, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentBlockState {
			raw.decodeBlockStateProperties()
			if raw.blockState != nil {
				return nbt.ShallowCopyCompound(raw.blockState), true
			}
		}
	}
	return nil, false
}

// SetBlockStatePropertiesTag retains CODEC traversal separately from STREAM_CODEC's hash map.
// vanilla: BlockItemStateProperties.CODEC, BlockItemStateProperties.STREAM_CODEC
func (s *ItemStack) SetBlockStatePropertiesTag(tag nbt.CompoundTag) bool {
	var value Buffer
	value.PutVarInt(int32(len(tag.Entries())))
	for _, key := range nbt.HashOrderedKeys(tag) {
		text, ok := tag.Entries()[key].(nbt.String)
		if !ok {
			return false
		}
		value.PutString(key)
		value.PutString(string(text))
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentBlockState, Data: value.Bytes(), blockState: nbt.ShallowCopyCompound(tag)})
	return true
}

func sameBlockStateProperties(a, b RawComponent) bool {
	if a.Type != DataComponentBlockState {
		return false
	}
	a.decodeBlockStateProperties()
	b.decodeBlockStateProperties()
	return a.blockState != nil && b.blockState != nil && nbt.Equal(a.blockState, b.blockState)
}

// vanilla: ByteBufCodecs.map, Object2ObjectOpenHashMap iteration
func (raw *RawComponent) decodeBlockStateProperties() {
	if raw.Type != DataComponentBlockState || raw.blockState != nil {
		return
	}
	r := NewReader(raw.Data)
	count := r.ReadVarInt()
	if count < 0 || int64(count) > int64(r.Remaining()/2) {
		return
	}
	capacity := 2
	for capacity*3/4 < min(int(count), 65536) {
		capacity *= 2
	}
	keys := make([]string, capacity)
	used := make([]bool, capacity)
	values := make(map[string]nbt.Tag)
	insert := func(key string) {
		pos := fastutilStringHash(key) & uint32(len(keys)-1)
		for used[pos] {
			pos = (pos + 1) & uint32(len(keys)-1)
		}
		keys[pos], used[pos] = key, true
	}
	for range count {
		key, value := r.ReadString(32767), r.ReadString(32767)
		if r.Err() != nil {
			return
		}
		if _, exists := values[key]; !exists {
			insert(key)
		}
		values[key] = nbt.String(value)
		if len(values) > len(keys)*3/4 {
			previous, occupied := keys, used
			keys, used = make([]string, len(keys)*2), make([]bool, len(keys)*2)
			for i, key := range slices.Backward(previous) {
				if occupied[i] {
					insert(key)
				}
			}
		}
	}
	if r.Err() != nil || r.Remaining() != 0 {
		return
	}
	tag := nbt.NewOrderedCompound(len(values))
	for i, key := range slices.Backward(keys) {
		if used[i] {
			tag.Set(key, values[key])
		}
	}
	raw.blockState = tag
}

// vanilla: String.hashCode, HashCommon.mix
func fastutilStringHash(key string) uint32 {
	var hash uint32
	for _, unit := range utf16.Encode([]rune(key)) {
		hash = 31*hash + uint32(unit)
	}
	hash *= 0x9e3779b9
	return hash ^ (hash >> 16)
}

// SetDebugStickState replaces the codec's selections, retaining their traversal.
func (s *ItemStack) SetDebugStickState(entries map[string]string, keys []string) {
	s.DebugStick, s.debugStickOrder = maps.Clone(entries), slices.Clone(keys)
	s.NotePatch(DataComponentDebugStickState)
}

// DebugStickTag writes selections in their native traversal.
// vanilla: DebugStickState.CODEC
func (s ItemStack) DebugStickTag() *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(len(s.DebugStick))
	for _, key := range s.debugStickOrder {
		if value, present := s.DebugStick[key]; present {
			tag.Set(key, nbt.String(value))
		}
	}
	if len(tag.Entries()) == len(s.DebugStick) {
		return tag
	}
	for _, key := range slices.Sorted(maps.Keys(s.DebugStick)) {
		if _, present := tag.Entries()[key]; !present {
			tag.Set(key, nbt.String(s.DebugStick[key]))
		}
	}
	return tag
}

// SetDebugStickProperty isolates a selection change from copied stacks.
// vanilla: DebugStickState.withProperty, Util.copyAndPut
func (s *ItemStack) SetDebugStickProperty(block, property string) {
	tag := s.DebugStickTag()
	tag.Set(block, nbt.String(property))
	next := maps.Clone(s.DebugStick)
	if next == nil {
		next = make(map[string]string)
	}
	next[block] = property
	s.DebugStick, s.debugStickOrder = next, nbt.HashOrderedKeys(tag)
	s.NotePatch(DataComponentDebugStickState)
}
