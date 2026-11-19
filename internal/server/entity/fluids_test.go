package entity

import "testing"

// TestDeepWaterDriftFollowsVanillasFormulas steps an item and an orb in
// still water deep enough that neither reaches the floor or the surface,
// below y=0, and holds every tick to the formulas written out from the
// vanilla sources with their float constants: ItemEntity.setFluidMovement
// (sideways times 0.99f, up 5.0E-4f while under 0.06f) then the move and the
// 0.98f air drag; ExperienceOrb.setUnderwaterMovement (sideways times 0.99f,
// up 5.0E-4f capped at 0.06f) then the move and 0.98f on every axis.
func TestDeepWaterDriftFollowsVanillasFormulas(t *testing.T) {
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	for x := int32(-8); x <= 8; x++ {
		for z := int32(-8); z <= 8; z++ {
			for y := int32(-64); y <= -48; y++ {
				w.set(x, y, z, water)
			}
		}
	}
	var (
		multiplier = float64(float32(0.99))
		rise       = float64(float32(5.0e-4))
		riseCap    = float64(float32(0.06))
		airDrag    = float64(float32(0.98))
	)

	it := &Item{X: 0.5, Y: -55.5, Z: 0.5, VX: 0.1, VY: -0.05, VZ: -0.04, Health: 5}
	x, y, z, vx, vy, vz := it.X, it.Y, it.Z, it.VX, it.VY, it.VZ
	for tick := 1; tick <= 60; tick++ {
		it.Step(w, nil)
		vx, vz = vx*multiplier, vz*multiplier
		if vy < riseCap {
			vy += rise
		}
		x, y, z = x+vx, y+vy, z+vz
		vx, vy, vz = vx*airDrag, vy*airDrag, vz*airDrag
		if !closeEnough(it.X, x) || !closeEnough(it.Y, y) || !closeEnough(it.Z, z) || !closeEnough(it.VY, vy) {
			t.Fatalf("item tick %d at (%v, %v, %v) vy %v, want (%v, %v, %v) vy %v", tick, it.X, it.Y, it.Z, it.VY, x, y, z, vy)
		}
	}

	o := &Orb{X: 0.5, Y: -55.5, Z: 0.5, VX: 0.1, VY: 0.059, VZ: -0.04, Value: 1}
	x, y, z, vx, vy, vz = o.X, o.Y, o.Z, o.VX, o.VY, o.VZ
	for tick := 1; tick <= 60; tick++ {
		o.Step(w, nil, nil)
		vx, vy, vz = vx*multiplier, min(vy+rise, riseCap), vz*multiplier
		x, y, z = x+vx, y+vy, z+vz
		vx, vy, vz = vx*airDrag, vy*airDrag, vz*airDrag
		if !closeEnough(o.X, x) || !closeEnough(o.Y, y) || !closeEnough(o.Z, z) || !closeEnough(o.VY, vy) {
			t.Fatalf("orb tick %d at (%v, %v, %v) vy %v, want (%v, %v, %v) vy %v", tick, o.X, o.Y, o.Z, o.VY, x, y, z, vy)
		}
	}
}
