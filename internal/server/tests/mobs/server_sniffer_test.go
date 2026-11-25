package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const sniffer = "minecraft:sniffer"

func snifferType() int32 { id, _ := registry.EntityTypeID(sniffer); return int32(id) }

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
		if m.Kind == sniffer && m.Baby && math.Abs(m.X-2.5) < 12 && math.Abs(m.Z-2.5) < 12 {
			return
		}
	}
	t.Fatalf("no baby sniffer at the egg: %v", srv.Mobs())
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
