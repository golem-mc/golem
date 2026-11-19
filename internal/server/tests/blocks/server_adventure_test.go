package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The adventure component tests: the hands adventure mode refuses unless
// the held stack names their target — minecraft:can_place_on for the
// blocks an item builds on, minecraft:can_break for the blocks it digs.
// The components ride the wire as raw bytes (see adventuredStack below);
// the placement and the dig gates read them out of the held stack.

// adventureWorld joins one player in adventure mode, the mode that may
// build and dig only where its items say it may, and stands them on the
// flat world's stone, the ground flag of the step the dig's speed reads.
// The tick keeps running: the answers these tests read — the echoes of a
// placement, the block correction of a refused dig — travel as the tick's
// end flushes them, the acknowledgement behind each.
func adventureWorld(t *testing.T, setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.SurvivalTestConfig()
	cfg.GameMode = "adventure"
	srv, c := servertest.Join(t, cfg, "Actor", setup...)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	return srv, c
}

// adventuredTagStack builds a stack carrying one predicate whose block set is
// a named tag rather than an inline list.
func adventuredTagStack(t *testing.T, item string, component int32, tag string) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, item, 1)
	var value protocol.Buffer
	value.PutVarInt(1) // one BlockPredicate
	value.PutBool(true)
	value.PutVarInt(0) // the named form: a tag follows
	value.PutString(tag)
	value.PutBool(false) // no state property matchers
	value.PutBool(false) // no block-entity NBT
	value.PutVarInt(0)   // no exact component matchers
	value.PutVarInt(0)   // no partial component matchers
	stack.Raw = append(stack.Raw, protocol.RawComponent{Type: component, Data: value.Bytes()})
	return stack
}

// TestAdventureBuildsOnlyOnCanPlaceOnBlocks: an adventure player holding a
// stone whose minecraft:can_place_on names dirt builds on the dirt and is
// refused on the grass block beside it. The refusal answers the vanilla
// way, the echoes of the use telling the client its prediction is undone,
// while the build against dirt lands the block and costs the stack.
// vanilla: ItemStack.useOn, ItemStack.canPlaceOnBlockInAdventureMode
func TestAdventureBuildsOnlyOnCanPlaceOnBlocks(t *testing.T) {
	t.Parallel()
	srv, c := adventureWorld(t)

	stack := servertest.AdventuredStack(t, "minecraft:stone", protocol.DataComponentCanPlaceOn, "minecraft:dirt")
	stack.Count = 3
	srv.GiveSlot("Actor", player.HotbarSlot, stack)
	c.Settle()

	// Clicking the top face of the dirt the predicate names: the stone
	// lands in the cell above it, the placement costing the stack. The ack
	// is read first — it flushes at the tick end behind everything the
	// placement queued, sweeping past the slot the cost is told in while
	// keeping the block updates for the echoes the read after it wants.
	srv.SetBlock(2, 63, 0, registry.BlockStates["minecraft:dirt"])
	c.Settle()
	stone := registry.BlockStates["minecraft:stone"]
	c.PlaceBlock(2, 63, 0, protocol.FaceTop)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the placement acknowledged as sequence %d, want 1", seq)
	}
	echoes := c.ExpectUseEchoes(2, 63, 0, protocol.FaceTop)
	if echoes[1] != stone {
		t.Errorf("the echo above the dirt = %d, want the placed stone (%d)", echoes[1], stone)
	}
	if got := srv.BlockAt(2, 64, 0); got != stone {
		t.Errorf("above the dirt = %d, want the stone (%d)", got, stone)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 2 {
		t.Errorf("the hand after building = a stack of %d, want 2 (the placement cost one)", got.Count)
	}

	// Clicking the grass block beside it: refused, the echoes reverting the
	// prediction, the world keeping its shape.
	grass := registry.BlockStates["minecraft:grass_block"]
	srv.SetBlock(3, 63, 0, grass)
	c.Settle()
	c.PlaceBlock(3, 63, 0, protocol.FaceTop)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the refusal acknowledged as sequence %d, want 1", seq)
	}
	echoes = c.ExpectUseEchoes(3, 63, 0, protocol.FaceTop)
	if echoes[0] != grass {
		t.Errorf("the echo of the clicked grass = %d, want the block standing (%d)", echoes[0], grass)
	}
	if echoes[1] != 0 {
		t.Errorf("the echo above the grass = %d, want air", echoes[1])
	}
	if got := srv.BlockAt(3, 64, 0); got != 0 {
		t.Errorf("above the grass = %d, want air", got)
	}
}

// TestAdventureDigsOnlyCanBreakBlocks: an adventure player holding shears
// whose minecraft:can_break names the oak leaves mines those leaves the
// way survival does — the share a tick of shears clears the whole of the
// block — while the START of a dig on a block the predicate does not name,
// and the START of a bare hand's dig, are answered with the block's own
// state, the correction that stops the client's prediction.
// vanilla: Player.blockActionRestricted, ItemStack.canBreakBlockInAdventureMode,
// ServerPlayerGameMode.handleBlockBreakAction
func TestAdventureDigsOnlyCanBreakBlocks(t *testing.T) {
	t.Parallel()
	srv, c := adventureWorld(t)

	srv.GiveSlot("Actor", player.HotbarSlot,
		servertest.AdventuredStack(t, "minecraft:shears", protocol.DataComponentCanBreak, "minecraft:oak_leaves"))
	c.Settle()
	leaves, ok := registry.FindState("minecraft:oak_leaves", map[string]string{"persistent": "true"})
	if !ok {
		t.Fatal("no oak leaves state with persistent")
	}

	// The stone the predicate does not name: refused, the dig never booked.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	srv.SetBlock(digTarget[0], digTarget[1], digTarget[2], stone)
	c.Settle()
	c.StartDigging(digTarget[0], digTarget[1], digTarget[2])
	if got := c.ExpectBlockUpdate(); got.X != digTarget[0] || got.Y != digTarget[1] ||
		got.Z != digTarget[2] || got.State != stone {
		t.Errorf("the answer to the started dig = %+v, want the stone's own state at %v", got, digTarget)
	}
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the refused start acknowledged as sequence %d, want 1", seq)
	}
	if got := srv.BlockAt(digTarget[0], digTarget[1], digTarget[2]); got != stone {
		t.Errorf("the stone after the refused dig = %d, want it standing (%d)", got, stone)
	}
	servertest.ExpectNoDroppedItems(t, srv)

	// The leaves the predicate names: the dig runs, shears clearing the
	// block with one share, its drops falling.
	srv.SetBlock(digTarget[0], digTarget[1], digTarget[2], leaves)
	c.Settle()
	c.StartDigging(digTarget[0], digTarget[1], digTarget[2])
	c.Settle()
	servertest.ExpectAir(t, srv, digTarget)
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:oak_leaves")

	// A bare hand has no component to dig with: the same correction answers.
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{})
	c.Settle()
	srv.SetBlock(digTarget[0], digTarget[1], digTarget[2], leaves)
	c.Settle()
	c.StartDigging(digTarget[0], digTarget[1], digTarget[2])
	if got := c.ExpectBlockUpdate(); got.State != leaves {
		t.Errorf("the answer to the bare hand's dig = %d, want the leaves standing (%d)", got.State, leaves)
	}
	if got := srv.BlockAt(digTarget[0], digTarget[1], digTarget[2]); got != leaves {
		t.Errorf("the leaves after the bare hand's dig = %d, want them standing (%d)", got, leaves)
	}
}

// TestASpectatorDigsNothing: the restructured dig START that answers
// adventure's can_break keeps spectator exactly where it was — the START
// of a spectator's dig answers nothing but its acknowledgement, books no
// dig and moves no block.
func TestASpectatorDigsNothing(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.GameMode = "spectator"
	srv, c := servertest.Join(t, cfg, "Ghost")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	srv.SetBlock(digTarget[0], digTarget[1], digTarget[2], stone)
	c.Settle()
	c.StartDigging(digTarget[0], digTarget[1], digTarget[2])
	updates := 0
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayBlockUpdate {
			updates++
		}
	})
	if updates != 0 {
		t.Errorf("the spectator's dig heard %d block updates, want none", updates)
	}
	if got := srv.BlockAt(digTarget[0], digTarget[1], digTarget[2]); got != stone {
		t.Errorf("the stone after the spectator's dig = %d, want it standing (%d)", got, stone)
	}
}

// digTarget is the cell the dig tests break, beside the actor on the flat
// world's stone.
var digTarget = [3]int32{2, 64, 0}
