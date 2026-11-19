package blocks

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// A stalactite drips the fluid above its root into a cauldron below its tip,
// or turns mud there into clay. The random tick's die is pinned through
// TransferDripstoneFluid so the chances are tested at their edges.
// vanilla: PointedDripstoneBlock.maybeTransferFluid,
// AbstractCauldronBlock.tick

const (
	dripX, dripZ = 5, 0
	cauldronY    = 64

	worldEventDripstoneDrip = 1504
	worldEventCauldronWater = 1047
	worldEventCauldronLava  = 1046

	// Vanilla's transfer chances, the thresholds the edges are pinned around.
	dripWaterChance = float32(0.17578125)
	dripLavaChance  = float32(0.05859375)
)

func fluidSource(name string) uint16 {
	return registry.MustState(name, map[string]string{"level": "0"})
}

func dripCauldron(name string, level string) uint16 {
	if level == "" {
		return registry.MustState(name, nil)
	}
	return registry.MustState(name, map[string]string{"level": level})
}

// buildStalactite clears the column above the cauldron cell and builds a
// two-block stalactite whose tip is at tipY, a stone root above it and the
// block given on top of the root. The cauldron cell is left for the caller.
func buildStalactite(t *testing.T, srv *server.Server, dimension string, tipY int32, above uint16) {
	t.Helper()
	set := func(y int32, state uint16) {
		// A write that changes nothing reports false.
		srv.SetBlockInDimension(dimension, dripX, y, dripZ, state)
		if got := srv.BlockInDimension(dimension, dripX, y, dripZ); got != state {
			t.Fatalf("block at y=%d is %d, want %d", y, got, state)
		}
	}
	for y := int32(cauldronY + 1); y <= tipY+4; y++ {
		set(y, 0)
	}
	set(tipY, dripstoneState(t, "down", "tip"))
	set(tipY+1, dripstoneState(t, "down", "base"))
	set(tipY+2, registry.MustState("minecraft:stone", nil))
	set(tipY+3, above)
}

// below is the roll just under a chance.
func below(chance float32) float32 { return math.Nextafter32(chance, 0) }

func TestStalactiteDripsWaterIntoACauldronAfterTheFallDelay(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		tipY  int32
		delay int
	}{
		{"four blocks of fall", 68, 54},
		{"six blocks of fall", 70, 56},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			srv.StopTick()
			buildStalactite(t, srv, world.DimensionName, tc.tipY, fluidSource("minecraft:water"))
			srv.SetBlock(dripX, cauldronY, dripZ, dripCauldron("minecraft:cauldron", ""))

			srv.TransferDripstoneFluid(world.DimensionName, dripX, tc.tipY+1, dripZ, below(dripWaterChance))
			if x, y, z, _ := c.AwaitWorldEvent(worldEventDripstoneDrip); x != dripX || y != tc.tipY || z != dripZ {
				t.Errorf("drip event at (%d,%d,%d), want the tip (%d,%d,%d)", x, y, z, dripX, tc.tipY, dripZ)
			}

			srv.TickPhysics(tc.delay - 1)
			servertest.ExpectBlockNamed(t, srv, dripX, cauldronY, dripZ, "minecraft:cauldron", nil)
			srv.TickPhysics(1)
			servertest.ExpectBlockNamed(t, srv, dripX, cauldronY, dripZ, "minecraft:water_cauldron", map[string]string{"level": "1"})
			if x, y, z, _ := c.AwaitWorldEvent(worldEventCauldronWater); x != dripX || y != cauldronY || z != dripZ {
				t.Errorf("fill event at (%d,%d,%d), want the cauldron", x, y, z)
			}
		})
	}
}

func TestStalactiteDripsLavaIntoAnEmptyCauldron(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()
	buildStalactite(t, srv, world.DimensionName, 68, fluidSource("minecraft:lava"))
	srv.SetBlock(dripX, cauldronY, dripZ, dripCauldron("minecraft:cauldron", ""))

	srv.TransferDripstoneFluid(world.DimensionName, dripX, 69, dripZ, below(dripLavaChance))
	srv.TickPhysics(54)
	servertest.ExpectBlockNamed(t, srv, dripX, cauldronY, dripZ, "minecraft:lava_cauldron", nil)
	c.AwaitWorldEvent(worldEventDripstoneDrip)
	c.AwaitWorldEvent(worldEventCauldronLava)
}

// TestStalactiteTransferChances pins the die at both sides of each chance.
func TestStalactiteTransferChances(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		source string
		roll   float32
		want   string
	}{
		{"water just under", "minecraft:water", below(dripWaterChance), "minecraft:water_cauldron"},
		{"water at the chance", "minecraft:water", dripWaterChance, "minecraft:cauldron"},
		{"lava just under", "minecraft:lava", below(dripLavaChance), "minecraft:lava_cauldron"},
		{"lava at the chance", "minecraft:lava", dripLavaChance, "minecraft:cauldron"},
		{"lava between the chances", "minecraft:lava", 0.1, "minecraft:cauldron"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.AttachActor(t)
			srv.StopTick()
			buildStalactite(t, srv, world.DimensionName, 68, fluidSource(tc.source))
			srv.SetBlock(dripX, cauldronY, dripZ, dripCauldron("minecraft:cauldron", ""))

			srv.TransferDripstoneFluid(world.DimensionName, dripX, 69, dripZ, tc.roll)
			srv.TickPhysics(60)
			servertest.ExpectBlockNamed(t, srv, dripX, cauldronY, dripZ, tc.want, nil)
		})
	}
}

func TestStalactiteWithoutAFluidSourceLeavesTheCauldronEmpty(t *testing.T) {
	t.Parallel()
	flowing := registry.MustState("minecraft:water", map[string]string{"level": "1"})
	for _, tc := range []struct {
		name  string
		above uint16
	}{
		{"air above the root", 0},
		{"flowing water above the root", flowing},
		{"stone above the root", registry.MustState("minecraft:stone", nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.AttachActor(t)
			srv.StopTick()
			buildStalactite(t, srv, world.DimensionName, 68, tc.above)
			srv.SetBlock(dripX, cauldronY, dripZ, dripCauldron("minecraft:cauldron", ""))

			srv.TransferDripstoneFluid(world.DimensionName, dripX, 69, dripZ, 0)
			srv.TickPhysics(60)
			servertest.ExpectBlockNamed(t, srv, dripX, cauldronY, dripZ, "minecraft:cauldron", nil)
		})
	}
}

func TestStalactiteTurnsMudIntoClay(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()
	buildStalactite(t, srv, world.DimensionName, 68, registry.MustState("minecraft:mud", nil))

	srv.TransferDripstoneFluid(world.DimensionName, dripX, 69, dripZ, below(dripWaterChance))
	servertest.ExpectBlockNamed(t, srv, dripX, 71, dripZ, "minecraft:clay", nil)
	if x, y, z, _ := c.AwaitWorldEvent(worldEventDripstoneDrip); x != dripX || y != 68 || z != dripZ {
		t.Errorf("drip event at (%d,%d,%d), want the tip", x, y, z)
	}
}
