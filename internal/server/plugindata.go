package server

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
)

// pluginData converts at the boundary; every read and write owns its mutable values.
type pluginData struct {
	read  func(string, func(nbt.Tag))
	keys  func(func(nbt.Tag) bool) []string
	write func(string, nbt.Tag)
}

func (d pluginData) Get(k plugin.Key) (value any, ok bool) {
	d.read(k.String(), func(tag nbt.Tag) { value, ok = pluginDataValue(tag) })
	return value, ok
}
func (d pluginData) Set(k plugin.Key, value any) {
	if parsed, err := plugin.ParseKey(k.String()); err != nil || parsed != k {
		return
	}
	if tag, ok := pluginDataTag(value); ok {
		d.write(k.String(), tag)
	}
}
func (d pluginData) Remove(k plugin.Key) { d.write(k.String(), nil) }
func (d pluginData) Keys() []plugin.Key {
	var keys []plugin.Key
	for _, name := range d.keys(pluginDataSupported) {
		key, err := plugin.ParseKey(name)
		if err == nil {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b plugin.Key) int { return strings.Compare(a.String(), b.String()) })
	return keys
}
func pluginDataTag(value any) (nbt.Tag, bool) {
	switch v := value.(type) {
	case bool:
		if v {
			return nbt.Byte(1), true
		}
		return nbt.Byte(0), true
	case int32:
		return nbt.Int(v), true
	case int64:
		return nbt.Long(v), true
	case float64:
		return nbt.Double(v), true
	case string:
		return nbt.String(v), true
	case []byte:
		return nbt.ByteArray(slices.Clone(v)), true
	case []int32:
		return nbt.IntArray(slices.Clone(v)), true
	case []int64:
		return nbt.LongArray(slices.Clone(v)), true
	case map[string]any:
		c := make(nbt.Compound, len(v))
		for key, child := range v {
			tag, ok := pluginDataTag(child)
			if !ok {
				return nil, false
			}
			c[key] = tag
		}
		return c, true
	default:
		return nil, false
	}
}
func pluginDataValue(tag nbt.Tag) (any, bool) {
	switch v := tag.(type) {
	case nbt.Byte:
		return v != 0, true
	case nbt.Int:
		return int32(v), true
	case nbt.Long:
		return int64(v), true
	case nbt.Double:
		return float64(v), true
	case nbt.String:
		return string(v), true
	case nbt.ByteArray:
		return slices.Clone([]byte(v)), true
	case nbt.IntArray:
		return slices.Clone([]int32(v)), true
	case nbt.LongArray:
		return slices.Clone([]int64(v)), true
	case nbt.CompoundTag:
		out := make(map[string]any, len(v.Entries()))
		for key, child := range v.Entries() {
			value, ok := pluginDataValue(child)
			if !ok {
				return nil, false
			}
			out[key] = value
		}
		return out, true
	default:
		return nil, false
	}
}

// pluginDataSupported validates persisted tags without materializing their values.
func pluginDataSupported(tag nbt.Tag) bool {
	switch v := tag.(type) {
	case nbt.Byte, nbt.Int, nbt.Long, nbt.Double, nbt.String, nbt.ByteArray, nbt.IntArray, nbt.LongArray:
		return true
	case nbt.CompoundTag:
		for _, child := range v.Entries() {
			if !pluginDataSupported(child) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
