package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestFallingSandLandingUsesEmptyHandCanBeReplaced checks that sand keeps
// snow of two or more layers (and drops as an item) and replaces a sculk
// vein.
// vanilla: FallingBlockEntity.tick, SnowLayerBlock.canBeReplaced
func TestFallingSandLandingUsesEmptyHandCanBeReplaced(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)

	const y = 100
	servertest.Setblock(t, c, 0, y, 0, "stone")
	servertest.Setblock(t, c, 0, y+1, 0, "snow[layers=4]")
	servertest.Setblock(t, c, 0, y+4, 0, "sand")
	if !servertest.ASpawnOfWithinTicks(c, srv, 5, protocol.EntityTypeFallingBlock) {
		t.Fatal("no falling_block spawned")
	}
	if !servertest.ASpawnOfWithinTicks(c, srv, 40, protocol.EntityTypeItem) {
		t.Fatal("sand landing on snow[layers=4] dropped no item")
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || registry.Item(items[0].Stack.ItemID) != registry.ItemSand || items[0].Count != 1 {
		t.Errorf("items after the snow landing = %+v, want exactly one sand", items)
	}
	snow, _ := registry.FindState("minecraft:snow", map[string]string{"layers": "4"})
	if got := srv.BlockAt(0, y+1, 0); got != snow {
		t.Errorf("cell = %d, want snow[layers=4] %d kept", got, snow)
	}

	servertest.Setblock(t, c, 0, y+1, 0, "sculk_vein[down=true]")
	servertest.Setblock(t, c, 0, y+4, 0, "sand")
	sand, _ := registry.FindState("minecraft:sand", nil)
	servertest.UntilTicks(t, srv, 40, "sand replacing the sculk vein", func() bool { return srv.BlockAt(0, y+1, 0) == sand })
	if got := srv.BlockAt(0, y+1, 0); got != sand {
		t.Errorf("cell over stone = %d, want sand %d replacing the sculk vein", got, sand)
	}
}
