package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// countOf sums the item entities of the overworld holding the item.
func countOf(items []server.TestItem, id int32) int32 {
	var n int32
	for _, it := range items {
		if it.Stack.ItemID == id {
			n += it.Count
		}
	}
	return n
}

// spillConfig is the block test world with only the spawn chunk in view.
func spillConfig() *config.Config {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	return cfg
}

// TestAShulkerBoxBurnedInLavaSpillsItsContents: a destroyed container item
// lays its stacks down where it burned. vanilla: BlockItem.onDestroyed
func TestAShulkerBoxBurnedInLavaSpillsItsContents(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, spillConfig(), "Burner")
	servertest.HoldTicks(t, srv)

	ingot := servertest.MustItemID(t, "minecraft:netherite_ingot")
	box := servertest.MustItemID(t, "minecraft:shulker_box")
	if !srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil)) {
		t.Fatal("the world did not take the lava")
	}
	srv.SpawnItemStack(4.5, 64.5, 4.5, protocol.ItemStack{ItemID: box, Count: 1,
		Container: []protocol.ItemStack{{ItemID: ingot, Count: 5}}})

	servertest.UntilTicks(t, srv, 40, "the ingots spilling", func() bool {
		return countOf(srv.ItemsInDimension("minecraft:overworld"), ingot) == 5
	})
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Stack.ItemID == box {
			t.Errorf("the shulker box survived the lava: %+v", it)
		}
		if it.Stack.ItemID == ingot && it.PickupDelay != 0 {
			t.Errorf("a spilled ingot has pickup delay %d, want 0", it.PickupDelay)
		}
		if it.Stack.ItemID == ingot && (it.X < 3 || it.X > 6 || it.Z < 3 || it.Z > 6) {
			t.Errorf("the ingots spilled at (%v %v %v), want near the lava", it.X, it.Y, it.Z)
		}
	}
}

// TestABundleBlownUpSpillsItsContents: a bundle a blast kills lays down what
// it held. vanilla: BundleItem.onDestroyed
func TestABundleBlownUpSpillsItsContents(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, spillConfig(), "Bomber")
	servertest.HoldTicks(t, srv)

	diamond := servertest.MustItemID(t, "minecraft:diamond")
	bundle := servertest.MustItemID(t, "minecraft:bundle")
	var stack protocol.ItemStack
	stack.ItemID, stack.Count = bundle, 1
	stack.SetBundleContents([]protocol.ItemStack{{ItemID: diamond, Count: 1}})
	srv.SpawnItemStack(4.5, 64, 0.5, stack)
	srv.SpawnTNT(4.5, 64, 0.5, 1)

	servertest.UntilTicks(t, srv, 20, "the diamond spilling", func() bool {
		return countOf(srv.ItemsInDimension("minecraft:overworld"), diamond) == 1
	})
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Stack.ItemID == bundle {
			t.Errorf("the bundle survived the blast: %+v", it)
		}
	}
}

// TestComponentsOnTheWrongItemKindSpillNothing: only a block item spills its
// container and only a bundle its bundle_contents. vanilla: Item.onDestroyed
func TestComponentsOnTheWrongItemKindSpillNothing(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, spillConfig(), "Burner")
	servertest.HoldTicks(t, srv)

	ingot := servertest.MustItemID(t, "minecraft:netherite_ingot")
	emerald := servertest.MustItemID(t, "minecraft:emerald")
	diamond := servertest.MustItemID(t, "minecraft:diamond")
	bundle := servertest.MustItemID(t, "minecraft:bundle")
	box := servertest.MustItemID(t, "minecraft:shulker_box")
	if !srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil)) {
		t.Fatal("the world did not take the lava")
	}
	inner := []protocol.ItemStack{{ItemID: ingot, Count: 1}}
	bare := protocol.ItemStack{ItemID: diamond, Count: 1, Container: inner}
	bare.SetBundleContents(inner)
	srv.SpawnItemStack(4.5, 64.5, 4.5, bare)
	// A bundle with both components spills the bundle's alone, a box with
	// both the container's alone.
	both := protocol.ItemStack{ItemID: bundle, Count: 1, Container: []protocol.ItemStack{{ItemID: emerald, Count: 1}}}
	both.SetBundleContents(inner)
	srv.SpawnItemStack(4.5, 64.5, 4.5, both)
	boxBoth := protocol.ItemStack{ItemID: box, Count: 1, Container: []protocol.ItemStack{{ItemID: emerald, Count: 2}}}
	boxBoth.SetBundleContents(inner)
	srv.SpawnItemStack(4.5, 64.5, 4.5, boxBoth)

	servertest.UntilTicks(t, srv, 40, "the lava taking all three", func() bool {
		return countOf(srv.ItemsInDimension("minecraft:overworld"), emerald) == 2
	})
	items := srv.ItemsInDimension("minecraft:overworld")
	if got := countOf(items, ingot); got != 1 {
		t.Errorf("%d ingots spilled, want 1 (the bundle's)", got)
	}
}

// TestAContainerOverSeveralLavaCellsSpillsOnce: an item that overlaps more
// than one damaging cell is destroyed, and spills, once.
func TestAContainerOverSeveralLavaCellsSpillsOnce(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, spillConfig(), "Burner")
	servertest.HoldTicks(t, srv)

	ingot := servertest.MustItemID(t, "minecraft:netherite_ingot")
	box := servertest.MustItemID(t, "minecraft:shulker_box")
	for _, x := range []int32{4, 5} {
		for _, z := range []int32{4, 5} {
			if !srv.SetBlock(x, 64, z, registry.MustState("minecraft:lava", nil)) {
				t.Fatal("the world did not take the lava")
			}
		}
	}
	srv.SpawnItemStack(5, 64.5, 5, protocol.ItemStack{ItemID: box, Count: 1,
		Container: []protocol.ItemStack{{ItemID: ingot, Count: 5}}})

	servertest.UntilTicks(t, srv, 40, "the ingots spilling", func() bool {
		return countOf(srv.ItemsInDimension("minecraft:overworld"), ingot) > 0
	})
	servertest.RunTicks(t, srv, 2)
	if n := countOf(srv.ItemsInDimension("minecraft:overworld"), ingot); n != 5 {
		t.Errorf("%d ingots after the burn, want 5", n)
	}
}
