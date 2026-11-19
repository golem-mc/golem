package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestSherdsCraftADecoratedPot checks the pot recipe: four bricks or sherds
// on the edge midpoints make a pot whose sides carry them in the order back,
// left, right, front; a corner cell or a fifth item makes none.
func TestSherdsCraftADecoratedPot(t *testing.T) {
	brick, sherd := MustItem("minecraft:brick"), MustItem("minecraft:arms_up_pottery_sherd")
	stack := func(id int32) protocol.ItemStack { return protocol.ItemStack{ItemID: id, Count: 1} }
	grid := func(back, left, right, front int32) []protocol.ItemStack {
		cells := make([]protocol.ItemStack, 9)
		cells[1], cells[3], cells[5], cells[7] = stack(back), stack(left), stack(right), stack(front)
		return cells
	}

	got := CraftResult(grid(brick, sherd, brick, brick), 3)
	if got.ItemID != potItem || got.Count != 1 {
		t.Fatalf("the pot craft = %+v, want one decorated pot", got)
	}
	sides, ok := got.PotDecorationsOf()
	if want := [4]int32{brick, sherd, brick, brick}; !ok || sides[0].ItemID != want[0] || sides[1].ItemID != want[1] || sides[2].ItemID != want[2] || sides[3].ItemID != want[3] {
		t.Errorf("the pot sides = %v (%v), want back brick, left sherd", sides, ok)
	}

	extra := grid(brick, brick, brick, brick)
	extra[0] = stack(brick)
	if r := CraftResult(extra, 3); r.ItemID == potItem {
		t.Errorf("five bricks made %+v, want no pot", r)
	}
	other := grid(brick, brick, brick, brick)
	other[7] = stack(MustItem("minecraft:stick"))
	if r := CraftResult(other, 3); r.ItemID == potItem {
		t.Errorf("a stick side made %+v, want no pot", r)
	}
}
