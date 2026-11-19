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

// TestASpongeAbsorbRunsItsNestedAbsorbInline covers the placed sponge's
// first removal waking the sponge itself: that nested absorb runs whole
// before the outer search goes on, its own removals waking nothing until it
// ends, so the outer search finds the cells next to the sponge already dry
// and stops short of (5,7).
// vanilla: SpongeBlock.neighborChanged, CollectingNeighborUpdater.addAndRun
func TestASpongeAbsorbRunsItsNestedAbsorbInline(t *testing.T) {
	t.Parallel()

	water := registry.MustState("minecraft:water", map[string]string{"level": "0"})
	spongeItem := servertest.ItemIDOrDie(t, "minecraft:sponge")

	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: spongeItem, Count: 1})
	c.Settle()

	// A 7x6 layer of water; the sponge goes into the gap at (5,64,2).
	const x0, z0, sx, sz = 2, 2, 5, 2
	for x := int32(x0); x < x0+7; x++ {
		for z := int32(z0); z < z0+6; z++ {
			if x != sx || z != sz {
				srv.SetBlock(x, 64, z, water)
			}
		}
	}
	c.PlaceBlock(sx, 63, sz, protocol.FaceTop)
	servertest.WaitForBlock(t, srv, sx, 64, sz, func(s uint16) bool {
		return s == registry.MustState("minecraft:wet_sponge", nil)
	})

	for x := int32(x0); x < x0+7; x++ {
		for z := int32(z0); z < z0+6; z++ {
			if x == sx && z == sz {
				continue
			}
			dist := abs32(x-sx) + abs32(z-sz)
			kept := dist > 6 || (x == 5 && z == 7)
			got := srv.BlockAt(x, 64, z)
			if !kept && got != 0 {
				t.Errorf("water at (%d,%d), %d steps away = %d, want air", x, z, dist, got)
			}
			if kept && got != water {
				t.Errorf("water at (%d,%d), %d steps away = %d, want it left", x, z, dist, got)
			}
		}
	}
}

// TestASpongeInAPoolTakesItsNestedAbsorbToo covers the count a sponge set in
// the middle of a 23x11x23 pool takes, the outer absorb and the nested one
// its first removal runs: 117 cells, measured on the vanilla server.
// vanilla: SpongeBlock.neighborChanged, CollectingNeighborUpdater.addAndRun
func TestASpongeInAPoolTakesItsNestedAbsorbToo(t *testing.T) {
	t.Parallel()

	water := registry.MustState("minecraft:water", map[string]string{"level": "0"})
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	c.Settle()
	// No tick may run: the water flowing back would blur the count.
	servertest.StopTheLoop(t, srv)

	const x0, y0, z0, x1, y1, z1 = -11, 64, -11, 11, 74, 11
	for x := int32(x0); x <= x1; x++ {
		for y := int32(y0); y <= y1; y++ {
			for z := int32(z0); z <= z1; z++ {
				srv.SetBlock(x, y, z, water)
			}
		}
	}
	srv.RunLine("setblock 0 69 0 minecraft:sponge")
	if s := srv.BlockAt(0, 69, 0); s != registry.MustState("minecraft:wet_sponge", nil) {
		t.Fatalf("sponge = %d, want it wet", s)
	}

	removed := 0
	for x := int32(x0); x <= x1; x++ {
		for y := int32(y0); y <= y1; y++ {
			for z := int32(z0); z <= z1; z++ {
				if srv.BlockAt(x, y, z) == 0 {
					removed++
				}
			}
		}
	}
	if removed != 117 {
		t.Errorf("removed %d water cells, want 117", removed)
	}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
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
