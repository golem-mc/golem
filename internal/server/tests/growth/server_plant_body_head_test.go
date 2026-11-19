package growth

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAGrowingPlantBodyBecomesAHeadWhenItsTipGoes checks that cutting a
// growing plant's tip turns the body left at the cut into a head with a random
// age, leaving the body behind it alone, and that a cave vine keeps its
// berries across the conversion.
// vanilla: GrowingPlantBodyBlock.updateShape
func TestAGrowingPlantBodyBecomesAHeadWhenItsTipGoes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	type column struct {
		x      int32
		blocks []string // from the support along the growth
		dy     int32
		cut    string // what replaces the cell past the last body
		head   string
		body   string
	}
	columns := []column{
		{x: 2, dy: 1, cut: "air", head: "minecraft:twisting_vines", body: "minecraft:twisting_vines_plant",
			blocks: []string{"stone", "twisting_vines_plant", "twisting_vines_plant", "twisting_vines"}},
		{x: 4, dy: -1, cut: "air", head: "minecraft:weeping_vines", body: "minecraft:weeping_vines_plant",
			blocks: []string{"stone", "weeping_vines_plant", "weeping_vines_plant", "weeping_vines"}},
		{x: 6, dy: -1, cut: "air", head: "minecraft:cave_vines", body: "minecraft:cave_vines_plant",
			blocks: []string{"stone", "cave_vines_plant[berries=true]", "cave_vines_plant[berries=true]", "cave_vines"}},
		{x: 8, dy: 1, cut: "water", head: "minecraft:kelp", body: "minecraft:kelp_plant",
			blocks: []string{"stone", "kelp_plant", "kelp_plant", "kelp"}},
	}
	const baseY, z = int32(70), int32(0)
	for _, col := range columns {
		if col.cut == "water" {
			for i := 1; i < len(col.blocks); i++ {
				servertest.Setblock(t, c, col.x, baseY+int32(i)*col.dy, z, "water")
			}
		}
		for i, b := range col.blocks {
			servertest.Setblock(t, c, col.x, baseY+int32(i)*col.dy, z, b)
		}
	}
	for _, col := range columns {
		tipY := baseY + int32(len(col.blocks)-1)*col.dy
		lastY, behindY := tipY-col.dy, tipY-2*col.dy
		servertest.Setblock(t, c, col.x, tipY, z, col.cut)

		name, props := servertest.StateProps(srv.BlockAt(col.x, lastY, z))
		if name != col.head {
			t.Fatalf("%s: the last body is %s %v after its tip went, want %s", col.body, name, props, col.head)
		}
		if age, err := strconv.Atoi(props["age"]); err != nil || age < 0 || age > 24 {
			t.Errorf("%s: the new head's age is %q, want 0..24", col.head, props["age"])
		}
		if col.head == "minecraft:cave_vines" && props["berries"] != "true" {
			t.Errorf("the new cave vines head has berries=%q, want true", props["berries"])
		}
		behind, behindProps := servertest.StateProps(srv.BlockAt(col.x, behindY, z))
		if behind != col.body {
			t.Errorf("%s: the body behind the new head is %s %v", col.body, behind, behindProps)
		}
		if col.head == "minecraft:cave_vines" && behindProps["berries"] != "true" {
			t.Errorf("the cave vines body behind the new head lost its berries: %v", behindProps)
		}
	}
}
