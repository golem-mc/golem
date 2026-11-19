package registry

import (
	"strconv"
	"testing"
)

// wireOverrides is a full redstone wire property map, the largest block's
// states, the way the engine's callers pass one.
var wireOverrides = map[string]string{"power": "7", "north": "side", "south": "none", "east": "up", "west": "side"}

// BenchmarkFindStateLinear is the string-matching FindState the tables
// replace, on redstone wire's 1,296 states.
func BenchmarkFindStateLinear(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		findStateLinear("minecraft:redstone_wire", wireOverrides)
	}
}

// BenchmarkFindState is FindState with the same overrides.
func BenchmarkFindState(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		FindState("minecraft:redstone_wire", wireOverrides)
	}
}

// BenchmarkFindStateOneProp is the common one-property call with a fresh map
// literal.
func BenchmarkFindStateOneProp(b *testing.B) {
	b.ReportAllocs()
	for i := range b.N {
		FindState("minecraft:fire", map[string]string{"age": strconv.Itoa(i & 15)})
	}
}

// BenchmarkFindStateOnePropLinear is BenchmarkFindStateOneProp through the
// string-matching reference.
func BenchmarkFindStateOnePropLinear(b *testing.B) {
	b.ReportAllocs()
	for i := range b.N {
		findStateLinear("minecraft:fire", map[string]string{"age": strconv.Itoa(i & 15)})
	}
}

// BenchmarkWithInt changes a wire's power in place.
func BenchmarkWithInt(b *testing.B) {
	b.ReportAllocs()
	s := BlockRedstoneWire.DefaultState()
	for i := range b.N {
		s, _ = WithInt(s, PropPower, i&15)
	}
}

// BenchmarkWithProp changes a wire's side by value string.
func BenchmarkWithProp(b *testing.B) {
	b.ReportAllocs()
	s := BlockRedstoneWire.DefaultState()
	sides := [3]string{"none", "side", "up"}
	for i := range b.N {
		s, _ = WithProp(s, PropEast, sides[i%3])
	}
}

// BenchmarkWithProps builds a wire state from its default with every
// property given.
func BenchmarkWithProps(b *testing.B) {
	b.ReportAllocs()
	d := BlockRedstoneWire.DefaultState()
	for range b.N {
		WithProps(d, PropValue{PropPower, "7"}, PropValue{PropNorth, "side"}, PropValue{PropSouth, "none"},
			PropValue{PropEast, "up"}, PropValue{PropWest, "side"})
	}
}

// BenchmarkBuildStateTables is the init cost of the tables.
func BenchmarkBuildStateTables(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		buildStateTables()
	}
}
