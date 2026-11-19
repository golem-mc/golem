package world

import "testing"

// TestShapeUpdateWalksTheVanillaOrder pins BlockBehaviour.UPDATE_SHAPE_ORDER
// through the writes a removal cascades: the west and east wires beside the
// removed block are asked before the north one.
func TestShapeUpdateWalksTheVanillaOrder(t *testing.T) {
	w, p := newPhysicsWorld(t)
	block := rsState(t, "minecraft:redstone_block", nil)

	w.SetBlock(12, 63, 9, block)
	w.SetBlock(11, 63, 9, rsState(t, "minecraft:redstone_wire", map[string]string{"east": "side"}))
	w.SetBlock(13, 63, 9, rsState(t, "minecraft:redstone_wire", map[string]string{"west": "side"}))
	w.SetBlock(12, 63, 8, rsState(t, "minecraft:redstone_wire", map[string]string{"south": "side"}))
	w.SetBlock(12, 63, 9, airState)

	var changes []BlockChange
	p.shapeUpdateNeighbors(12, 63, 9, &changes)

	first := func(x, z int32) int {
		for i, ch := range changes {
			if ch.X == x && ch.Z == z {
				return i
			}
		}
		return -1
	}
	west, east, north := first(11, 9), first(13, 9), first(12, 8)
	if west < 0 || east < 0 || north < 0 {
		t.Fatalf("shape updates missing: west=%d east=%d north=%d in %v", west, east, north, changes)
	}
	if west > north || east > north {
		t.Fatalf("shape order west=%d east=%d north=%d, want west and east before north", west, east, north)
	}
}
