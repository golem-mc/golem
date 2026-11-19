package entity

import "testing"

// BenchmarkBoatGroundedStep measures an empty hull gliding over ice and stone.
func BenchmarkBoatGroundedStep(b *testing.B) {
	w := newTestWorld()
	floor(w, -2, -2, 6, 3, 99, defaultState(b, "minecraft:stone"))
	floor(w, -2, 0, 6, 2, 99, defaultState(b, "minecraft:ice"))
	for _, tc := range []struct {
		name string
		z    float64
	}{{"ice", 1.5}, {"boundary", 0.5}} {
		b.Run(tc.name, func(b *testing.B) {
			boat := NewBoat(1, 1.5, 100, tc.z, 0)
			b.ReportAllocs()
			for b.Loop() {
				boat.X, boat.Y, boat.Z = 1.5, 100, tc.z
				boat.VX, boat.VY, boat.VZ = 0.8, 0, 0
				boat.OnGround = true
				boat.Step(w, false)
			}
		})
	}
}

// BenchmarkBoatLandingSupport measures a hull landing hard enough to bounce:
// on a full block, on a shelf mushroom's short shape with its backing under
// the far part of the hull, and over slime.
func BenchmarkBoatLandingSupport(b *testing.B) {
	w := newTestWorld()
	floor(w, -2, -2, 6, 6, 99, defaultState(b, "minecraft:stone"))
	floor(w, 10, -2, 14, 6, 99, defaultState(b, "minecraft:slime_block"))
	w.set(0, 100, 4, defaultState(b, "minecraft:shelf_mushroom"))
	w.set(0, 100, 5, defaultState(b, "minecraft:stone"))
	for _, tc := range []struct {
		name          string
		x, y, z, rest float64
	}{{"full_block", 3.5, 100.5, 1.5, 100}, {"partial_backed", 0.5, 101.5, 4.5, 101}, {"slime", 12.5, 100.5, 1.5, -1}} {
		b.Run(tc.name, func(b *testing.B) {
			boat := NewBoat(1, tc.x, tc.y, tc.z, 0)
			reset := func() {
				boat.X, boat.Y, boat.Z = tc.x, tc.y, tc.z
				boat.VX, boat.VY, boat.VZ = 0, -1, 0
				boat.OnGround = false
			}
			reset()
			boat.Step(w, false)
			if tc.rest >= 0 && (boat.Y != tc.rest || !boat.OnGround) || tc.rest < 0 && boat.VY <= 0 {
				b.Fatalf("fixture does not land as intended: y=%v vy=%v ground=%v", boat.Y, boat.VY, boat.OnGround)
			}
			b.ReportAllocs()
			for b.Loop() {
				reset()
				boat.Step(w, false)
			}
		})
	}
}
