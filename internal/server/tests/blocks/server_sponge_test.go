package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestASpongePlacedBesideWaterAbsorbsIt covers the sponge's breadth-first
// soak: a source, a flow and a waterlogged slab within six cells go, kelp
// breaks and drops, water past the range stays, and the sponge turns wet.
// vanilla: SpongeBlock.tryAbsorbWater
func TestASpongePlacedBesideWaterAbsorbsIt(t *testing.T) {
	t.Parallel()

	water := registry.MustState("minecraft:water", map[string]string{"level": "0"})
	flow := registry.MustState("minecraft:water", map[string]string{"level": "3"})
	kelp := registry.MustState("minecraft:kelp", map[string]string{"age": "0"})
	slab := registry.MustState("minecraft:oak_slab", map[string]string{"waterlogged": "true"})
	drySlab := registry.MustState("minecraft:oak_slab", map[string]string{"waterlogged": "false"})
	wet := registry.MustState("minecraft:wet_sponge", nil)
	sponge := registry.MustState("minecraft:sponge", nil)
	spongeItem := servertest.ItemIDOrDie(t, "minecraft:sponge")

	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: spongeItem, Count: 1})
	c.Settle()

	// The sponge goes at (3,64,3); the water forms a chain away from it.
	srv.SetBlock(4, 64, 3, water)
	srv.SetBlock(5, 64, 3, flow)
	srv.SetBlock(6, 64, 3, kelp)
	srv.SetBlock(7, 64, 3, slab)
	// The line goes on to depth 6 (x=9), which goes, and depth 7 (x=10),
	// which stays.
	srv.SetBlock(8, 64, 3, water)
	srv.SetBlock(9, 64, 3, water)
	srv.SetBlock(10, 64, 3, water)
	items := srv.ItemCount()

	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	servertest.WaitForBlock(t, srv, 3, 64, 3, func(s uint16) bool { return s == wet })

	for _, pos := range [][3]int32{{4, 64, 3}, {5, 64, 3}, {6, 64, 3}} {
		if s := srv.BlockAt(pos[0], pos[1], pos[2]); s != 0 {
			t.Errorf("block at %v = %d, want air", pos, s)
		}
	}
	if s := srv.BlockAt(7, 64, 3); s != drySlab {
		t.Errorf("waterlogged slab = %d, want the dry slab %d", s, drySlab)
	}
	if s := srv.BlockAt(9, 64, 3); s != 0 {
		t.Errorf("water at depth 6 = %d, want air", s)
	}
	if s := srv.BlockAt(10, 64, 3); s != water {
		t.Errorf("water at depth 7 = %d, want it left at %d", s, water)
	}
	if got := srv.ItemCount(); got <= items {
		t.Errorf("item entities %d -> %d, want the kelp dropped", items, got)
	}

	// A sponge with no water near stays dry.
	c.PlaceBlock(1, 63, 3, protocol.FaceTop)
	c.Settle()
	if s := srv.BlockAt(1, 64, 3); s != sponge {
		t.Errorf("dry placement = %d, want a plain sponge %d", s, sponge)
	}
}

// TestASpongeSoaksWaterThatArrivesBesideIt covers neighborChanged.
// vanilla: SpongeBlock.neighborChanged
func TestASpongeSoaksWaterThatArrivesBesideIt(t *testing.T) {
	t.Parallel()
	wet := registry.MustState("minecraft:wet_sponge", nil)
	spongeItem := servertest.ItemIDOrDie(t, "minecraft:sponge")

	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: spongeItem, Count: 1})
	c.Settle()

	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	c.Settle()
	c.ChatCommand("setblock 4 64 3 minecraft:water")
	servertest.WaitForBlock(t, srv, 3, 64, 3, func(s uint16) bool { return s == wet })
}

// TestAWetSpongePlacedInTheNetherDries covers WetSpongeBlock.onPlace.
// vanilla: WetSpongeBlock.onPlace
func TestAWetSpongePlacedInTheNetherDries(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Builder")
	servertest.CarryIntoNether(t, srv, c, "Builder", 8, 8)

	stone := registry.MustState("minecraft:stone", nil)
	sponge := registry.MustState("minecraft:sponge", nil)
	srv.SetBlockInDimension(world.NetherName, 12, 39, 8, stone)
	srv.SetBlockInDimension(world.NetherName, 12, 40, 8, 0)
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:wet_sponge"), Count: 1})
	c.Settle()

	c.PlaceBlock(12, 39, 8, protocol.FaceTop)
	servertest.UntilTicks(t, srv, 100, "the sponge", func() bool {
		return srv.BlockInDimension(world.NetherName, 12, 40, 8) == sponge
	})
}
