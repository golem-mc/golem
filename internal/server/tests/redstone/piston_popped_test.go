package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestPistonsPopLeavesBudsAndShulkerBoxes extends pistons into blocks whose
// push reaction is POPPED: the cell is air after the push and the filled
// shulker box drops with its contents.
// vanilla: PistonStructureResolver.addBlockLine, PistonBaseBlock.moveBlocks
func TestPistonsPopLeavesBudsAndShulkerBoxes(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)

	targets := []string{"oak_leaves", "small_amethyst_bud[facing=up]", "shulker_box"}
	for i, block := range targets {
		z := int32(i * 4)
		servertest.Setblock(t, actor, 2, 64, z, "piston[facing=east]")
		servertest.Setblock(t, actor, 3, 64, z, block)
	}
	actor.ChatCommand("data merge block 3 64 8 {Items:[{Slot:0b,id:\"minecraft:diamond\",count:1}]}")
	actor.Settle()
	for i := range targets {
		servertest.Setblock(t, actor, 1, 64, int32(i*4), "redstone_block")
	}
	servertest.AwaitTicks(t, srv, 3)
	actor.Settle()

	for i := range targets {
		z := int32(i * 4)
		name, props := servertest.StateProps(srv.BlockAt(2, 64, z))
		if name != "minecraft:piston" || props["extended"] != "true" {
			t.Errorf("piston %d is %s %v, want extended", i, name, props)
		}
		if got := srv.BlockAt(4, 64, z); got != 0 {
			n, _ := servertest.StateProps(got)
			t.Errorf("cell past the head %d holds %s, want air (nothing pushed)", i, n)
		}
		if got := srv.BlockAt(3, 64, z); got == 0 {
			t.Errorf("head cell %d is air", i)
		}
	}

	box := servertest.ItemIDOrDie(t, "minecraft:shulker_box")
	bud := servertest.ItemIDOrDie(t, "minecraft:small_amethyst_bud")
	diamond := servertest.ItemIDOrDie(t, "minecraft:diamond")
	var boxes int
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		switch it.Stack.ItemID {
		case bud:
			t.Errorf("small amethyst bud dropped without silk touch")
		case box:
			boxes++
			if len(it.Stack.Container) != 1 || it.Stack.Container[0].ItemID != diamond {
				t.Errorf("popped shulker box container = %+v, want the diamond", it.Stack.Container)
			}
			if it.Z < 7 || it.Z > 9 {
				t.Errorf("box lies at z %v, want near its cell", it.Z)
			}
		}
	}
	if boxes != 1 {
		t.Errorf("%d shulker boxes dropped, want one", boxes)
	}
}
