package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const sniffer = "minecraft:sniffer"

func snifferType() int32 { id, _ := registry.EntityTypeID(sniffer); return int32(id) }

func TestASnifferSpawnsAsItsOwnType(t *testing.T) {
	t.Parallel()
	c, _, srv := polarWorld(t)
	id := srv.SpawnMob(sniffer, 3.5, 64, 0.5)
	m := c.ExpectMobSpawn(snifferType())
	if m.EntityID != id || m.Health != 14 {
		t.Fatalf("spawn = %d, health %v; want %d, 14", m.EntityID, m.Health, id)
	}
}

func TestTwoSniffersFedTorchflowerSeedsDropAnEggNotABaby(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:torchflower_seeds", 2)
	servertest.HoldTicks(t, srv)
	first := srv.SpawnMob(sniffer, 1.5, 64, 4.5)
	second := srv.SpawnMob(sniffer, 3.5, 64, 4.5)
	c.ExpectMobSpawn(snifferType())
	c.ExpectMobSpawn(snifferType())
	c.UseOn(first)
	c.AwaitEntityEvent(first, loveHearts)
	c.UseOn(second)
	c.AwaitEntityEvent(second, loveHearts)
	servertest.UntilTicks(t, srv, 400, "sniffer egg", func() bool { return len(srv.ItemsInDimension(world.DimensionName)) > 0 })
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Item != "minecraft:sniffer_egg" {
		t.Fatalf("drops = %v, want one sniffer egg", items)
	}
	servertest.RunTicks(t, srv, 20)
	if got := srv.MobCount(); got != 2 {
		t.Fatalf("mobs = %d, want the two parents only", got)
	}
	if got := srv.InventorySlot("Rancher", player.HotbarSlot).Count; got != 0 {
		t.Fatalf("seeds left = %d, want 0", got)
	}
}

func snifferEggHatch(t *testing.T, srv *server.Server) string {
	t.Helper()
	name, props, ok := registry.BlockStateName(srv.BlockAt(2, 64, 2))
	if !ok || name != "minecraft:sniffer_egg" {
		t.Fatalf("the block is %s, not a sniffer egg", name)
	}
	return registry.ParseStateProps(props)["hatch"]
}

func snifferEggWorld(t *testing.T, under string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), 0)
	servertest.SetFarmBlock(t, c, srv, 2, 63, 2, under, nil)
	servertest.SetFarmBlock(t, c, srv, 2, 64, 2, "minecraft:sniffer_egg", nil)
	servertest.HoldTicks(t, srv)
	return srv, c
}

func TestASnifferEggOnMossCracksAndHatchesABaby(t *testing.T) {
	t.Parallel()
	srv, c := snifferEggWorld(t, "minecraft:moss_block")
	// The client reads its packets so the keep-alive exchange is answered.
	c.ReadTicks(srv, 4300, func(int32, []byte) bool { return false })
	if got := snifferEggHatch(t, srv); got != "1" {
		t.Fatalf("hatch at 4300 ticks = %s, want 1", got)
	}
	c.ReadTicks(srv, 12900-4300, func(int32, []byte) bool { return false })
	if registry.BlockOf(srv.BlockAt(2, 64, 2)) != registry.BlockAir {
		t.Fatal("the egg is still standing at 12900 ticks")
	}
	// The hatch tick is jittered, so the baby may have wandered off the block centre.
	for _, m := range srv.Mobs() {
		if m.Kind == sniffer && m.Baby && math.Abs(m.X-2.5) < 4 && math.Abs(m.Z-2.5) < 4 {
			return
		}
	}
	t.Fatalf("no baby sniffer at the egg: %v", srv.Mobs())
}

func TestASnifferEggOnStoneIsSlower(t *testing.T) {
	t.Parallel()
	srv, c := snifferEggWorld(t, "minecraft:dirt")
	c.ReadTicks(srv, 4300, func(int32, []byte) bool { return false })
	if got := snifferEggHatch(t, srv); got != "0" {
		t.Fatalf("hatch at 4300 ticks = %s, want 0", got)
	}
}

func TestADiggingSnifferDropsASeed(t *testing.T) {
	t.Parallel()
	c, _, srv := polarWorld(t)
	for x := int32(2); x <= 14; x++ {
		for z := int32(-3); z <= 3; z++ {
			srv.SetBlock(x, 63, z, registry.BlockGrassBlock.DefaultState())
		}
	}
	id := srv.SpawnMob(sniffer, 6.5, 64, 0.5)
	c.ExpectMobSpawn(snifferType())
	srv.StartSnifferSearch(id, 11, 63, 0)
	servertest.UntilTicks(t, srv, 600, "a seed", func() bool { return len(srv.ItemsInDimension(world.DimensionName)) > 0 })
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item != "minecraft:torchflower_seeds" && it.Item != "minecraft:pitcher_pod" {
			t.Fatalf("drop = %v, want a torchflower seed or pitcher pod", it)
		}
	}
}
