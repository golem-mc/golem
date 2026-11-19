package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAFastItemTakesTheEffectOfACellItCrossedInOneTick: an item flying three
// blocks in one tick over a single fluid cell, from air to air, meets that
// cell's fluid on the way: water puts it out, lava lights it.
// vanilla: Entity.checkInsideBlocks
func TestAFastItemTakesTheEffectOfACellItCrossedInOneTick(t *testing.T) {
	t.Parallel()
	cases := []struct {
		fluid       string
		fire        int
		wantBurning bool
	}{
		{"minecraft:water", 100, false},
		{"minecraft:lava", 0, true},
	}
	for _, c := range cases {
		t.Run(c.fluid, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.AttachActor(t)
			srv.StopTick()
			// The fluids do not flow while the tick is stopped.
			if !srv.SetBlock(4, 65, 0, servertest.BlockStateWith(t, c.fluid, "level=0")) {
				t.Fatal("could not place the fluid")
			}
			// Box x from 2.375 to 2.625 at the start, 5.375 to 5.625 at the
			// end: neither box reaches the cell at x=4.
			id := srv.SpawnItemEntityMotion(2.5, 65.5, 0.5, "minecraft:stick", 1, 3, 0, 0)
			srv.SetItemFire(id, c.fire)
			srv.TickEntities(1)

			x := 0.0
			for _, it := range srv.ItemsInDimension("minecraft:overworld") {
				if it.ID == id {
					x = it.X
				}
			}
			if x < 5.375 {
				t.Fatalf("the item is at x=%v, want it past the fluid cell", x)
			}
			if burning := srv.ItemFire(id) > 0; burning != c.wantBurning {
				t.Errorf("crossing %s: fire ticks %d, want burning %v", c.fluid, srv.ItemFire(id), c.wantBurning)
			}
		})
	}
}
