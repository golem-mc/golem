package world

import "testing"

// TestWireReshapeWakesItsSlopePartner covers a wire whose shape changes
// through a neighbour's shape update carrying the change around a step: the
// crest on the step must take the east and west line the reshaped foot's new
// climb gives it.
func TestWireReshapeWakesItsSlopePartner(t *testing.T) {
	w, p := newPhysicsWorld(t)
	stone := rsState(t, "minecraft:stone", nil)
	wire := rsState(t, "minecraft:redstone_wire", nil)

	// A step with its crest wire, the foot beside it, and a wire south of
	// the foot.
	w.SetBlock(12, 63, 10, stone)
	w.SetBlock(12, 64, 10, wire)
	w.SetBlock(13, 63, 10, wire)
	w.SetBlock(13, 63, 11, wire)

	// The wire placed south of the foot reshapes the foot, whose new west
	// climb must reach the crest.
	setBlock(t, w, p, 13, 63, 12, wire)

	if _, props := rsNameAt(t, w, 12, 64, 10); props["east"] != "side" || props["west"] != "side" {
		t.Fatalf("crest = %v, want east=side, west=side", props)
	}
}

// TestWireReshapeWakesTheOldStatesSlopePartner pins the old shape's slope
// wake in wireReshaped: a wire whose own write leaves its connections
// unchanged still tells the crest across its climb to re-derive, the wake
// Level.setBlock runs on the replaced state before the shape pass. The crest
// starts stale so only that wake can fix it.
func TestWireReshapeWakesTheOldStatesSlopePartner(t *testing.T) {
	w, p := newPhysicsWorld(t)
	stone := rsState(t, "minecraft:stone", nil)
	foot := rsState(t, "minecraft:redstone_wire", map[string]string{"west": "up"})
	powered := rsState(t, "minecraft:redstone_wire", map[string]string{"power": "1", "west": "up"})

	w.SetBlock(12, 63, 10, stone)
	w.SetBlock(12, 64, 10, rsState(t, "minecraft:redstone_wire", nil))
	w.SetBlock(13, 63, 10, powered)

	var changes []BlockChange
	p.wireReshaped(13, 63, 10, foot, powered, &changes)

	if _, props := rsNameAt(t, w, 12, 64, 10); props["east"] != "side" {
		t.Fatalf("crest = %v, want east=side", props)
	}
}

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

// TestSlopeChangesWalkTheVanillaOrder pins
// RedStoneWireBlock.updateIndirectNeighbourShapes' Direction.Plane.HORIZONTAL
// walk: the east slope is asked before the south one.
func TestSlopeChangesWalkTheVanillaOrder(t *testing.T) {
	w, p := newPhysicsWorld(t)
	stone := rsState(t, "minecraft:stone", nil)
	wire := rsState(t, "minecraft:redstone_wire", nil)
	center := rsState(t, "minecraft:redstone_wire", map[string]string{"east": "up", "south": "up"})

	// A step and crest to the center's east and south.
	w.SetBlock(13, 63, 9, stone)
	w.SetBlock(13, 64, 9, wire)
	w.SetBlock(12, 63, 10, stone)
	w.SetBlock(12, 64, 10, wire)
	w.SetBlock(12, 63, 9, center)

	var changes []BlockChange
	p.wireSlopeChanges(12, 63, 9, center, &changes)

	first := func(x, z int32) int {
		for i, ch := range changes {
			if ch.X == x && ch.Z == z {
				return i
			}
		}
		return -1
	}
	east, south := first(13, 9), first(12, 10)
	if east < 0 || south < 0 {
		t.Fatalf("slope updates missing: east=%d south=%d in %v", east, south, changes)
	}
	if east > south {
		t.Fatalf("slope order east=%d south=%d, want east before south", east, south)
	}
}
