package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// A powered floor lever on a hopper locks it; the hopper's enabled write then
// shape-updates the lever, whose support (the hollow hopper top) is gone.
// vanilla: HopperBlock.checkPoweredState, FaceAttachedHorizontalDirectionalBlock.updateShape
func TestHopperEnabledChangeDropsFloorLever(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 5, 64, 0, "hopper[facing=down,enabled=true]")
	servertest.Setblock(t, c, 5, 65, 0, "lever[face=floor,facing=north,powered=true]")
	servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:hopper", map[string]string{"enabled": "true"})
	if n := countDropped(srv.ItemsInDimension(world.DimensionName), "minecraft:lever"); n != 1 {
		t.Fatalf("dropped levers = %d, want 1", n)
	}
}

// A comparator beside a hopper keeps its place when the hopper's enabled write
// shape-updates it from the side; only a change below drops it.
// vanilla: ComparatorBlock.updateShape
func TestHopperEnabledChangeKeepsSideComparator(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 5, 70, 0, "hopper[facing=down,enabled=true]")
	servertest.Setblock(t, c, 6, 70, 0, "comparator[facing=north] strict")
	servertest.Setblock(t, c, 4, 70, 0, "redstone_block")
	servertest.ExpectBlockNamed(t, srv, 5, 70, 0, "minecraft:hopper", map[string]string{"enabled": "false"})
	servertest.ExpectBlockNamed(t, srv, 6, 70, 0, "minecraft:comparator", nil)
	if n := countDropped(srv.ItemsInDimension(world.DimensionName), "minecraft:comparator"); n != 0 {
		t.Fatalf("dropped comparators = %d, want 0", n)
	}
}

// A comparator whose lower neighbour's mode toggle shape-updates the comparator
// above it loses its support: it drops with the break event.
// vanilla: ComparatorBlock.updateShape, Block.destroyBlock
func TestStackedComparatorBreakShowsBreakEvent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 5, 64, 0, "comparator[facing=north]")
	servertest.Setblock(t, c, 5, 65, 0, "comparator[facing=north] strict")
	c.UseBlock(5, 64, 0)
	got := c.ReadBreak(5, 65, 0, func(b servertest.BreakWire) bool {
		return b.Air && len(b.Drops) == 1
	})
	if !got.Air || !got.BlockEventSeen {
		t.Fatalf("upper comparator air=%v breakEvent=%v, want both", got.Air, got.BlockEventSeen)
	}
	if registry.BlockOf(got.BlockEventData) != registry.BlockComparator {
		t.Fatalf("break event data is block %d, want comparator", registry.BlockOf(got.BlockEventData))
	}
}

// An unsupported attached stem facing a comparator whose mode toggles turns
// back into a plain age-7 stem rather than breaking: the fruit-facing
// conversion answers before the support check.
// vanilla: AttachedStemBlock.updateShape
func TestComparatorToggleTurnsFacingAttachedStemBack(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 5, 64, 0, "comparator[facing=north]")
	servertest.Setblock(t, c, 5, 64, 1, "attached_pumpkin_stem[facing=north] strict")
	c.UseBlock(5, 64, 0)
	c.AwaitBlockState(5, 64, 0, servertest.MustState(t, "minecraft:comparator", map[string]string{"facing": "north", "mode": "subtract"}))
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:comparator", map[string]string{"mode": "subtract"})
	servertest.ExpectBlockNamed(t, srv, 5, 64, 1, "minecraft:pumpkin_stem", map[string]string{"age": "7"})
	if n := countDropped(srv.ItemsInDimension(world.DimensionName), "minecraft:pumpkin_seeds"); n != 0 {
		t.Fatalf("dropped pumpkin seeds = %d, want 0", n)
	}
}

// An attached stem facing a hopper whose enabled write shape-updates it turns
// back into a plain stem with the shape flags, so the unsupported rail beside
// it only gets a shape update and stays.
// vanilla: AttachedStemBlock.updateShape, Block.updateOrDestroy, BaseRailBlock.neighborChanged
func TestHopperEnabledChangeStemConversionKeepsRail(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 5, 70, 0, "hopper[facing=down,enabled=true]")
	servertest.Setblock(t, c, 5, 69, 1, "farmland")
	servertest.Setblock(t, c, 5, 70, 1, "attached_pumpkin_stem[facing=north] strict")
	servertest.Setblock(t, c, 6, 70, 1, "rail strict")
	servertest.Setblock(t, c, 4, 70, 0, "redstone_block")
	servertest.ExpectBlockNamed(t, srv, 5, 70, 0, "minecraft:hopper", map[string]string{"enabled": "false"})
	servertest.ExpectBlockNamed(t, srv, 5, 70, 1, "minecraft:pumpkin_stem", map[string]string{"age": "7"})
	servertest.ExpectBlockNamed(t, srv, 6, 70, 1, "minecraft:rail", nil)
	if n := countDropped(srv.ItemsInDimension(world.DimensionName), "minecraft:rail"); n != 0 {
		t.Fatalf("dropped rails = %d, want 0", n)
	}
}

// A lichen above a hopper loses its down face when the hopper's enabled write
// shape-updates it; the trimmed replacement keeps the shape flags, so the
// unsupported rail beside the lichen stays.
// vanilla: MultifaceBlock.updateShape, Block.updateOrDestroy, BaseRailBlock.neighborChanged
func TestHopperEnabledChangeLichenTrimKeepsRail(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 5, 70, 0, "hopper[facing=down,enabled=true]")
	servertest.Setblock(t, c, 5, 71, 1, "stone")
	servertest.Setblock(t, c, 5, 71, 0, "glow_lichen[down=true,south=true] strict")
	servertest.Setblock(t, c, 6, 71, 0, "rail strict")
	servertest.Setblock(t, c, 4, 70, 0, "redstone_block")
	servertest.ExpectBlockNamed(t, srv, 5, 70, 0, "minecraft:hopper", map[string]string{"enabled": "false"})
	servertest.ExpectBlockNamed(t, srv, 5, 71, 0, "minecraft:glow_lichen", map[string]string{"down": "false", "south": "true"})
	servertest.ExpectBlockNamed(t, srv, 6, 71, 0, "minecraft:rail", nil)
	if n := countDropped(srv.ItemsInDimension(world.DimensionName), "minecraft:rail"); n != 0 {
		t.Fatalf("dropped rails = %d, want 0", n)
	}
}
