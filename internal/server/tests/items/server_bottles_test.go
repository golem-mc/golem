package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestBottleFillsFromAWaterSource uses a glass bottle aimed down at a water
// source and checks the held stack becomes a water bottle while the source
// stays: a bottle fill takes no source, unlike a bucket.
// vanilla: BottleItem.use
func TestBottleFillsFromAWaterSource(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Filler")

	bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 1})
	if !srv.SetBlock(4, 63, 4, water) {
		t.Fatal("the world did not take the water")
	}
	c.Settle() // the water's own update crosses the wire before the fill reads it

	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)

	servertest.PollUntil(t, 5*time.Second, func() bool {
		return srv.InventorySlot("Filler", player.HotbarSlot).ItemID == registry.MustItem("minecraft:potion")
	})
	if got, want := srv.InventorySlot("Filler", player.HotbarSlot), servertest.WaterBottle(t); !got.Equal(want) {
		t.Errorf("the hand after the fill = %+v, want a water bottle %+v", got, want)
	}
	if got := srv.BlockAt(4, 63, 4); got != water {
		t.Errorf("the source after the fill = %d, want it still there", got)
	}
}

// TestACreativeBottleFillKeepsTheBottle: a creative hand's fill leaves the
// bottle where it is and adds what the filled result asks for, the hand not
// being one of those. vanilla: ItemUtils.createFilledResult,
// hasInfiniteMaterials
func TestACreativeBottleFillKeepsTheBottle(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dipper")

	bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	srv.GiveSlot("Dipper", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 1})
	if !srv.SetBlock(4, 63, 4, water) {
		t.Fatal("the world did not take the water")
	}
	c.Settle()

	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)

	servertest.PollUntil(t, 5*time.Second, func() bool {
		return srv.InventorySlot("Dipper", player.HotbarSlot+1).ItemID == registry.MustItem("minecraft:potion")
	})
	if got := srv.InventorySlot("Dipper", player.HotbarSlot); got.ItemID != bottle || got.Count != 1 {
		t.Errorf("the hand after the fill = %+v, want the bottle kept", got)
	}
	if got, want := srv.InventorySlot("Dipper", player.HotbarSlot+1), servertest.WaterBottle(t); !got.Equal(want) {
		t.Errorf("the filled result = %+v, want one water bottle %+v", got, want)
	}
}

// TestABottleOnANonWaterTargetDoesNothing aims a bottle down at the stone
// under a player in a world with no water there: the ray stops on the stone
// and the fill asks for water, so the hand is left alone.
// vanilla: BottleItem.use
func TestABottleOnANonWaterTargetDoesNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Filler")

	bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 1})

	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	c.Settle()

	if got := srv.InventorySlot("Filler", player.HotbarSlot); got.ItemID != bottle || got.Count != 1 {
		t.Errorf("the hand after the fill = %+v, want the bottle untouched", got)
	}
	if got := srv.InventorySlot("Filler", player.HotbarSlot+1); !got.Empty() {
		t.Errorf("the filled result = %+v, want nothing", got)
	}
}
