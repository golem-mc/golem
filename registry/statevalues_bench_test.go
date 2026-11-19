package registry

import "testing"

// benchStates is every state ID, walked in a scattered order so the reads
// are not all from one block's few rows.
func benchStates() []uint16 {
	ids := make([]uint16, StateCount)
	for i := range ids {
		ids[i] = uint16(i * 7919 % StateCount)
	}
	return ids
}

// BenchmarkStatePropMap reads three properties through StateProps' map.
func BenchmarkStatePropMap(b *testing.B) {
	ids := benchStates()
	n := 0
	for i := range b.N {
		props := StateProps(ids[i%len(ids)])
		if props["half"] == "top" || props["waterlogged"] == "true" || props["thickness"] == "tip" {
			n++
		}
	}
	_ = n
}

// BenchmarkStatePropTyped reads the same properties through the typed reads.
func BenchmarkStatePropTyped(b *testing.B) {
	ids := benchStates()
	n := 0
	for i := range b.N {
		id := ids[i%len(ids)]
		if StateValue(id, PropHalf) == "top" || StateBool(id, PropWaterlogged) || StateValue(id, PropThickness) == "tip" {
			n++
		}
	}
	_ = n
}
