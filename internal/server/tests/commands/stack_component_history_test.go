package commands

import (
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// Keep the parsed collision order in command input; compact SNBT sorts keys.
func nativeStackInput(tag nbt.Tag) string {
	var b strings.Builder
	switch v := tag.(type) {
	case nbt.CompoundTag:
		b.WriteByte('{')
		for i, key := range nbt.HashOrderedKeys(v) {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Quote(key))
			b.WriteByte(':')
			b.WriteString(nativeStackInput(v.Entries()[key]))
		}
		b.WriteByte('}')
	case nbt.List:
		b.WriteByte('[')
		for i, child := range v.Items {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(nativeStackInput(child))
		}
		b.WriteByte(']')
	default:
		return nbt.Stringify(tag)
	}
	return b.String()
}
