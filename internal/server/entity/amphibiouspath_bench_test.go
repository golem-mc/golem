package entity

import "testing"

// BenchmarkAmphibiousPath plans an axolotl-sized amphibious body across a
// four-deep pool walled in stone.
func BenchmarkAmphibiousPath(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	water := defaultState(b, "minecraft:water")
	w := newTestWorld()
	for y := int32(56); y <= 63; y++ {
		floor(w, -2, -2, 14, 14, y, stone)
	}
	for y := int32(60); y <= 63; y++ {
		floor(w, 0, 0, 12, 12, y, water)
	}
	f := footprintOf(0.75, 0.42)
	f.amphibious = true
	for b.Loop() {
		findPath(w, f, Waypoint{0, 60, 0}, Waypoint{12, 60, 12}, pathMaxRange, 0)
	}
}
