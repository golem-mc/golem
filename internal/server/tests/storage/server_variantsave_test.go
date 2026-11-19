package storage

import (
	"bytes"

	"github.com/golem-mc/golem/internal/protocol"
)

// carries reports whether the stack holds a raw component of the type.
func carries(stack protocol.ItemStack, component int32) bool {
	for _, raw := range stack.Raw {
		if raw.Type == component {
			return true
		}
	}
	return false
}

// sameRaw reports whether two stacks' raw components hold the same bytes per
// type, in any order.
func sameRaw(a, b []protocol.RawComponent) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		found := false
		for _, y := range b {
			if x.Type == y.Type && bytes.Equal(x.Data, y.Data) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
