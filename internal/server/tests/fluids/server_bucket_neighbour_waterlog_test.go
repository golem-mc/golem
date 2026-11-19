package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// pourBesideTheFace pours a water bucket against the west face of a stone
// at (10, 64, 0), the ray passing over the block at (9, 64, 0), and returns
// that cell's block and properties and the item left in hand.
func pourBesideTheFace(t *testing.T, name string, props map[string]string, check ...func(*servertest.Client)) (string, map[string]string, int32) {
	t.Helper()
	// Survival, so the pour empties the hand: a creative bucket stays full.
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.Join(t, cfg, "Pourer", servertest.OpsListing(t, "Pourer"))

	srv.StopTick()
	servertest.SetFarmBlock(t, c, srv, 10, 64, 0, "minecraft:stone", nil)
	servertest.SetFarmBlock(t, c, srv, 9, 64, 0, name, props)
	srv.GiveSlot("Pourer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:water_bucket"), Count: 1})

	c.FacePlayer(8.5, 64, 0.5, -90, 29)
	servertest.UseHeld(c, -90, 29)
	for _, f := range check {
		f(c)
	}
	c.Settle()

	got, gotProps, ok := registry.BlockStateName(srv.BlockAt(9, 64, 0))
	if !ok {
		t.Fatal("the block beside the face names no block")
	}
	held := srv.InventorySlot("Pourer", player.HotbarSlot)
	return got, registry.ParseStateProps(gotProps), held.ItemID
}

// TestAWaterBucketWaterlogsTheSlabBesideTheClickedFace: a bottom slab in the
// cell beyond the clicked face takes the water rather than refusing it.
// vanilla: BucketItem.emptyContents, SlabBlock.canPlaceLiquid
func TestAWaterBucketWaterlogsTheSlabBesideTheClickedFace(t *testing.T) {
	t.Parallel()
	name, props, held := pourBesideTheFace(t, "minecraft:stone_slab",
		map[string]string{"type": "bottom", "waterlogged": "false"})
	if name != "minecraft:stone_slab" || props["type"] != "bottom" || props["waterlogged"] != "true" {
		t.Errorf("the block beside the face = %s %v, want a waterlogged bottom stone slab", name, props)
	}
	if held != servertest.ItemIDOrDie(t, "minecraft:bucket") {
		t.Errorf("the hand after the pour = item %d, want minecraft:bucket", held)
	}
}
