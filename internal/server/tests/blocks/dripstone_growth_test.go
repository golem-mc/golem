package blocks

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// A stalactite under a dripstone block and a water source grows a tip down
// or a stalagmite up on the random tick. The dice are seeded; the draw order
// is the transfer's float, the growth float, then the direction.
// vanilla: SpeleothemBlock.randomTick

const (
	growTipY   = 70
	growFloorY = 60
	growChance = float32(0.011377778)
)

// growSeed finds a seed whose draws are the transfer float (ignored), a
// growth float under or over the chance, and the direction bit given.
func growSeed(t *testing.T, grows, down bool) int64 {
	t.Helper()
	for seed := range int64(1_000_000) {
		r := rand.New(rand.NewSource(seed))
		r.Float32()
		hit := r.Float32() < growChance
		if hit != grows {
			continue
		}
		if !grows || (r.Intn(2) == 0) == down {
			return seed
		}
	}
	t.Fatal("no seed found")
	return 0
}

// buildGrowthColumn clears the column, lays a stone floor and puts a
// down-pointing tip at growTipY under dripstone and a water source.
func buildGrowthColumn(t *testing.T, srv *server.Server, above, source uint16) {
	t.Helper()
	for y := int32(growFloorY + 1); y <= growTipY+4; y++ {
		srv.SetBlockInDimension(world.DimensionName, dripX, y, dripZ, 0)
	}
	set := func(y int32, state uint16) {
		srv.SetBlockInDimension(world.DimensionName, dripX, y, dripZ, state)
	}
	set(growFloorY, registry.MustState("minecraft:stone", nil))
	set(growTipY, dripstoneState(t, "down", "tip"))
	set(growTipY+1, above)
	set(growTipY+2, source)
}

func expectDripstone(t *testing.T, srv *server.Server, y int32, direction, thickness string) {
	t.Helper()
	servertest.ExpectBlockNamed(t, srv, dripX, y, dripZ, "minecraft:pointed_dripstone", map[string]string{
		"vertical_direction": direction, "thickness": thickness,
	})
}

func dripstoneBlock() uint16 { return registry.MustState("minecraft:dripstone_block", nil) }

func TestStalactiteGrowsADownwardTip(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	buildGrowthColumn(t, srv, dripstoneBlock(), fluidSource("minecraft:water"))

	srv.RandomTickDripstone(world.DimensionName, growSeed(t, true, true), dripX, growTipY, dripZ)
	expectDripstone(t, srv, growTipY-1, "down", "tip")
	expectDripstone(t, srv, growTipY, "down", "frustum")
}

func TestStalactiteGrowsAStalagmiteOnTheFloor(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	buildGrowthColumn(t, srv, dripstoneBlock(), fluidSource("minecraft:water"))

	srv.RandomTickDripstone(world.DimensionName, growSeed(t, true, false), dripX, growTipY, dripZ)
	expectDripstone(t, srv, growFloorY+1, "up", "tip")
	servertest.ExpectBlockNamed(t, srv, dripX, growTipY-1, dripZ, "minecraft:air", nil)
}

func TestStalagmiteScanLengthensAnExistingTip(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	buildGrowthColumn(t, srv, dripstoneBlock(), fluidSource("minecraft:water"))
	srv.SetBlockInDimension(world.DimensionName, dripX, growFloorY+1, dripZ, dripstoneState(t, "up", "tip"))

	srv.RandomTickDripstone(world.DimensionName, growSeed(t, true, false), dripX, growTipY, dripZ)
	expectDripstone(t, srv, growFloorY+2, "up", "tip")
	expectDripstone(t, srv, growFloorY+1, "up", "frustum")
}

func TestGrowingIntoAFacingTipMergesBoth(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	buildGrowthColumn(t, srv, dripstoneBlock(), fluidSource("minecraft:water"))
	for y := int32(growFloorY + 1); y < growTipY-1; y++ {
		srv.SetBlockInDimension(world.DimensionName, dripX, y, dripZ, dripstoneState(t, "up", "base"))
	}
	srv.SetBlockInDimension(world.DimensionName, dripX, growTipY-1, dripZ, dripstoneState(t, "up", "tip"))

	srv.RandomTickDripstone(world.DimensionName, growSeed(t, true, true), dripX, growTipY, dripZ)
	expectDripstone(t, srv, growTipY, "down", "tip_merge")
	expectDripstone(t, srv, growTipY-1, "up", "tip_merge")
}

func TestWaterUnderTheTipBlocksGrowth(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	buildGrowthColumn(t, srv, dripstoneBlock(), fluidSource("minecraft:water"))
	// canTipGrow wants no fluid where the tip would grow.
	srv.SetBlockInDimension(world.DimensionName, dripX, growTipY-1, dripZ, fluidSource("minecraft:water"))

	srv.RandomTickDripstone(world.DimensionName, growSeed(t, true, true), dripX, growTipY, dripZ)
	servertest.ExpectBlockNamed(t, srv, dripX, growTipY-1, dripZ, "minecraft:water", map[string]string{"level": "0"})
}

func TestStalactiteDoesNotGrowOnAMissedGrowthRoll(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	buildGrowthColumn(t, srv, dripstoneBlock(), fluidSource("minecraft:water"))

	srv.RandomTickDripstone(world.DimensionName, growSeed(t, false, false), dripX, growTipY, dripZ)
	expectDripstone(t, srv, growTipY, "down", "tip")
	servertest.ExpectBlockNamed(t, srv, dripX, growTipY-1, dripZ, "minecraft:air", nil)
}

func TestStalactiteGrowthNeedsDripstoneAndAWaterSource(t *testing.T) {
	t.Parallel()
	flowing := registry.MustState("minecraft:water", map[string]string{"level": "1"})
	for _, tc := range []struct {
		name          string
		above, source uint16
	}{
		{"stone instead of dripstone", registry.MustState("minecraft:stone", nil), fluidSource("minecraft:water")},
		{"flowing water", dripstoneBlock(), flowing},
		{"lava", dripstoneBlock(), fluidSource("minecraft:lava")},
		{"air", dripstoneBlock(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.AttachActor(t)
			srv.StopTick()
			buildGrowthColumn(t, srv, tc.above, tc.source)

			srv.RandomTickDripstone(world.DimensionName, growSeed(t, true, true), dripX, growTipY, dripZ)
			servertest.ExpectBlockNamed(t, srv, dripX, growTipY-1, dripZ, "minecraft:air", nil)
		})
	}
}

func TestOnlyTheStartOfAColumnGrows(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	buildGrowthColumn(t, srv, dripstoneBlock(), fluidSource("minecraft:water"))

	// A tip below the root of a longer column is not the start.
	srv.SetBlockInDimension(world.DimensionName, dripX, growTipY, dripZ, dripstoneState(t, "down", "base"))
	srv.SetBlockInDimension(world.DimensionName, dripX, growTipY-1, dripZ, dripstoneState(t, "down", "tip"))
	srv.RandomTickDripstone(world.DimensionName, growSeed(t, true, true), dripX, growTipY-1, dripZ)
	servertest.ExpectBlockNamed(t, srv, dripX, growTipY-2, dripZ, "minecraft:air", nil)
}

func TestColumnLongerThanTheGrowthWalkDoesNotGrow(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	buildGrowthColumn(t, srv, dripstoneBlock(), fluidSource("minecraft:water"))
	// Seven blocks of column under the start: the tip is eight away.
	srv.SetBlockInDimension(world.DimensionName, dripX, growTipY, dripZ, dripstoneState(t, "down", "base"))
	for y := int32(growTipY - 1); y >= growTipY-6; y-- {
		srv.SetBlockInDimension(world.DimensionName, dripX, y, dripZ, dripstoneState(t, "down", "middle"))
	}
	srv.SetBlockInDimension(world.DimensionName, dripX, growTipY-7, dripZ, dripstoneState(t, "down", "tip"))

	srv.RandomTickDripstone(world.DimensionName, growSeed(t, true, true), dripX, growTipY, dripZ)
	servertest.ExpectBlockNamed(t, srv, dripX, growTipY-8, dripZ, "minecraft:air", nil)
}

func TestMudTurnedToClayLiftsAnItemStandingOnIt(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	buildStalactite(t, srv, world.DimensionName, 68, registry.MustState("minecraft:mud", nil))
	srv.SpawnItemEntity(dripX+0.5, 71+14.0/16.0+entity.ItemHalfSize, dripZ+0.5, "minecraft:stick", 1)

	srv.TransferDripstoneFluid(world.DimensionName, dripX, 69, dripZ, below(dripWaterChance))
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("item entities = %d, want 1", len(items))
	}
	if feet := items[0].Y - entity.ItemHalfSize; math.Abs(feet-72) > 0.001 {
		t.Errorf("item feet y = %v after the mud became clay, want 72", feet)
	}
}
