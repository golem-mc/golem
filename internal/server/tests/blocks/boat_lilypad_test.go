package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestABoatBreaksTheLilyPadItFloatsInto: a hull overlapping a lily pad's cell
// turns it to air and the pad drops as an item; a pad two cells off the hull
// stays.
// vanilla: LilyPadBlock.entityInside
func TestABoatBreaksTheLilyPadItFloatsInto(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Sailor", servertest.OpsListing(t, "Sailor"))

	for _, x := range []int32{5, 9} {
		servertest.SetFarmBlock(t, c, srv, x, 63, 0, "minecraft:water", nil)
		servertest.SetFarmBlock(t, c, srv, x, 64, 0, "minecraft:lily_pad", nil)
	}
	srv.RunLine("summon minecraft:oak_boat 5.5 64 0.5")

	drops := servertest.AwaitCropBreak(t, c, 5, 64, 0, 0)
	if len(drops) != 1 {
		t.Fatalf("the lily pad laid down %d drops, want 1", len(drops))
	}
	if name, _ := registry.ItemName(drops[0].ItemID); name != "minecraft:lily_pad" || drops[0].Count != 1 {
		t.Errorf("the lily pad dropped %d %s, want 1 minecraft:lily_pad", drops[0].Count, name)
	}
	pad, _ := registry.FindState("minecraft:lily_pad", nil)
	if got := srv.BlockAt(9, 64, 0); got != pad {
		t.Errorf("the pad out of the hull's reach is state %d, want it left at %d", got, pad)
	}
}
