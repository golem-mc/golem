package gamemode

// A click on a gamemaster block with a screen (command blocks, structure
// block, jigsaw, test blocks) is the block's own use for a creative operator:
// the client opens the screen and the server must not also place the held
// block against it. vanilla: CommandBlock.useWithoutItem,
// StructureBlockEntity.usedBy, JigsawBlock.useWithoutItem,
// TestBlock.useWithoutItem, TestInstanceBlock.useWithoutItem

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

var screenBlocks = []string{
	"minecraft:command_block",
	"minecraft:chain_command_block",
	"minecraft:repeating_command_block",
	"minecraft:structure_block",
	"minecraft:jigsaw",
	"minecraft:test_block",
	"minecraft:test_instance_block",
}

// screenClick stands the block at (4, 64, 5), gives the actor stone and
// clicks the block's top face, reporting whether stone appeared above it.
func screenClick(t *testing.T, cfg *config.Config, block string, sneak bool, setup ...func(*server.Server)) (placed bool) {
	t.Helper()
	return screenClickWith(t, cfg, block, sneak, protocol.HandMain, setup...)
}

// screenClickWith is screenClick with the stone in, and the click from, hand.
func screenClickWith(t *testing.T, cfg *config.Config, block string, sneak bool, hand int32, setup ...func(*server.Server)) (placed bool) {
	t.Helper()
	srv, c := servertest.Join(t, cfg, "Actor", setup...)
	state, ok := registry.FindState(block, nil)
	if !ok {
		t.Fatalf("no state for %s", block)
	}
	if !srv.SetBlock(4, 64, 5, state) {
		t.Fatalf("could not stand %s", block)
	}
	stone, _ := registry.ItemID("minecraft:stone")
	slot := player.HotbarSlot
	if hand == protocol.HandOff {
		slot = player.OffHandSlot
	}
	srv.GiveSlot("Actor", slot, protocol.ItemStack{ItemID: stone, Count: 1})
	servertest.PrepareGameMasterPlacement(c)
	if sneak {
		c.SendSneak(true)
		c.Settle()
	}
	c.UseBlockWithHand(hand, 4, 64, 5)
	c.Settle()
	return srv.BlockAt(4, 65, 5) != 0
}

// TestACreativeOperatorOpensAGameMasterScreenInsteadOfPlacing: the held
// block stays in the hand.
func TestACreativeOperatorOpensAGameMasterScreenInsteadOfPlacing(t *testing.T) {
	t.Parallel()
	for _, block := range screenBlocks {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			if screenClick(t, servertest.BlockTestConfig(), block, false, servertest.OpsListing(t, "Actor")) {
				t.Errorf("a creative operator's click on %s placed the held stone against it", block)
			}
		})
	}
}

// TestASneakingOperatorPlacesAgainstAGameMasterBlock: sneaking with an item
// skips the block's use.
func TestASneakingOperatorPlacesAgainstAGameMasterBlock(t *testing.T) {
	t.Parallel()
	for _, block := range screenBlocks {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			if !screenClick(t, servertest.BlockTestConfig(), block, true, servertest.OpsListing(t, "Actor")) {
				t.Errorf("a sneaking operator's click on %s placed nothing", block)
			}
		})
	}
}

// TestAGameMasterScreenNeedsTheGameMasterPermission: a creative player
// without the permission, and an operator in survival, place instead.
func TestAGameMasterScreenNeedsTheGameMasterPermission(t *testing.T) {
	t.Parallel()
	for _, block := range screenBlocks {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			if !screenClick(t, servertest.BlockTestConfig(), block, false) {
				t.Errorf("a creative non-operator's click on %s placed nothing", block)
			}
			if !screenClick(t, servertest.SurvivalTestConfig(), block, false, servertest.OpsListing(t, "Actor")) {
				t.Errorf("a survival operator's click on %s placed nothing", block)
			}
		})
	}
}

// TestAnOffHandClickOnAGameMasterBlockPlaces: only the main hand reaches the
// block's own use, so a stone in the off hand goes on.
func TestAnOffHandClickOnAGameMasterBlockPlaces(t *testing.T) {
	t.Parallel()
	for _, block := range screenBlocks {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			if !screenClickWith(t, servertest.BlockTestConfig(), block, false, protocol.HandOff, servertest.OpsListing(t, "Actor")) {
				t.Errorf("an operator's off-hand click on %s placed nothing", block)
			}
		})
	}
}

// TestOpeningACommandBlockSendsItsData: chunk data leaves a command block's
// tag empty, so an operator's click sends it for the editor; a non-operator's
// click opens nothing and is sent nothing. vanilla: ServerPlayer.openCommandBlock
func TestOpeningACommandBlockSendsItsData(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
	servertest.PrepareGameMasterPlacement(c)
	servertest.Setblock(t, c, 4, 64, 5, "command_block[facing=north]")
	c.ProgramCommandBlock(4, 64, 5, "say hi", servertest.ProgramModeImpulse, true, false, false)
	c.Settle()
	c.UseBlock(4, 64, 5)
	x, y, z, _, data := c.AwaitBlockEntityData()
	if x != 4 || y != 64 || z != 5 {
		t.Fatalf("Block Entity Data at (%d, %d, %d), want (4, 64, 5)", x, y, z)
	}
	if got, _ := data["Command"].(nbt.String); got != "say hi" {
		t.Errorf("Command = %#v, want \"say hi\"", data["Command"])
	}

	srv2, other := servertest.Join(t, servertest.BlockTestConfig(), "Other")
	state, _ := registry.FindState("minecraft:command_block", map[string]string{"facing": "north"})
	if !srv2.SetBlock(4, 64, 5, state) {
		t.Fatal("could not stand the command block")
	}
	servertest.PrepareGameMasterPlacement(other)
	other.UseBlock(4, 64, 5)
	other.ExpectNoneWithinTicks(srv2, 5, servertest.PacketIs(protocol.IDPlayBlockEntityData))
}
