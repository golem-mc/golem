package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestWaterfallDragsAnItemDownOnlyBesideAWall stands two falling water
// columns over the flat world's ground, one in open air and one against a
// stone wall, and lays an item in the top of each: only the walled column's
// current pulls down, so that item falls further.
// vanilla: FlowingFluid.getFlow (the downward share needs a solid face)
func TestWaterfallDragsAnItemDownOnlyBesideAWall(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")
	srv.StopTick() // the columns stay as built: no fluid tick spreads them

	falling, ok := registry.FindState("minecraft:water", map[string]string{"level": "8"})
	if !ok {
		t.Fatal("the block registry has no falling water state")
	}
	stone := servertest.BlockState(t, "minecraft:stone")
	// The ground's top is y=63; the columns stand over it from 66 to 72,
	// clear of the player at x=0.
	const freeX, wallX = 4, 8
	for y := int32(66); y <= 72; y++ {
		for _, set := range []bool{
			srv.SetBlock(freeX, y, 0, falling),
			srv.SetBlock(wallX, y, 0, falling),
			srv.SetBlock(wallX+1, y, 0, stone),
		} {
			if !set {
				t.Fatal("the columns' chunk is not loaded")
			}
		}
	}

	const startY = 71.0
	free := srv.SpawnItemEntity(freeX+0.5, startY+entity.ItemHalfSize, 0.5, "minecraft:stone", 1)
	walled := srv.SpawnItemEntity(wallX+0.5, startY+entity.ItemHalfSize, 0.5, "minecraft:stone", 1)
	srv.TickEntities(10)

	_, freeY, _ := itemFeet(t, srv, free)
	_, walledY, _ := itemFeet(t, srv, walled)
	if walledY >= freeY {
		t.Errorf("after 10 ticks the item beside the wall is at %v and the free one at %v, want the wall's current to have carried it further down", walledY, freeY)
	}
	if freeY < startY-0.1 {
		t.Errorf("the item in the free-standing column fell to %v from %v, want no current dragging it down", freeY, startY)
	}
}
