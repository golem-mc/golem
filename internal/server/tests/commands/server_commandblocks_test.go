package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// commandBlockSelectState resolves the state a named command block takes with
// facing and conditional properties, what a test compares a programmed block
// against when the programming should have changed neither.
func commandBlockSelectState(t *testing.T, name, facing string, conditional bool) uint16 {
	t.Helper()
	props := map[string]string{"facing": facing, "conditional": "false"}
	if conditional {
		props["conditional"] = "true"
	}
	state, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no %s state with %v", name, props)
	}
	return state
}

// TestImpulseCommandBlockRunsOnceOnTheRise programs an impulse block with
// `say hi` and puts a lever on it: one pull says the line once, the lever
// holding does not repeat it, a second pull says it again, and
// command_blocks_work=false stops the run the rise books.
// vanilla: CommandBlock.neighborChanged, BaseCommandBlock.performCommand
func TestImpulseCommandBlockRunsOnceOnTheRise(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=north,conditional=false]")
	c.Settle()
	c.ProgramCommandBlock(2, 64, 0, "say hi", servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say hi")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()

	// The rise runs the command once, and holding the lever runs it no
	// further.
	c.UseBlock(2, 65, 0)
	c.ExpectSayLine(t, "@", "hi")
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))

	// The fall runs nothing; a second rise runs the command again.
	c.UseBlock(2, 65, 0)
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))
	c.UseBlock(2, 65, 0)
	c.ExpectSayLine(t, "@", "hi")
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))

	// With command blocks off, the rise books a run that says nothing;
	// with them back on, the next rise says the line again.
	c.ChatCommand("gamerule command_blocks_work false")
	servertest.AwaitCommandFeedback(t, c, "commands.gamerule.set", "command_blocks_work", "false")
	if srv.GameRule("command_blocks_work") {
		t.Fatal("setup: command_blocks_work is not off")
	}
	c.UseBlock(2, 65, 0)
	c.UseBlock(2, 65, 0)
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))

	c.ChatCommand("gamerule command_blocks_work true")
	servertest.AwaitCommandFeedback(t, c, "commands.gamerule.set", "command_blocks_work", "true")
	c.UseBlock(2, 65, 0)
	c.UseBlock(2, 65, 0)
	c.ExpectSayLine(t, "@", "hi")
}

// TestRepeatingCommandBlockRunsEveryTick programs a repeating block that
// needs redstone and puts a lever on it: a fresh repeating block is not
// always active, so it is silent until the rise starts a loop that says its
// line at the tick rate, and the fall stops the loop within the tick or two
// the fall had already booked. command_blocks_work=false silences the loop
// without stopping it.
// vanilla: CommandBlock.tick (Mode.AUTO), BaseCommandBlock.performCommand
func TestRepeatingCommandBlockRunsEveryTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "repeating_command_block[facing=north,conditional=false]")
	c.Settle()
	c.ProgramCommandBlock(2, 64, 0, "say again", servertest.ProgramModeRepeating, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say again")
	// Only redstone starts a repeating block.
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))

	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))

	// The rise starts the loop, and the lines come while the lever holds.
	c.UseBlock(2, 65, 0)
	c.ExpectSayLine(t, "@", "again")
	c.ExpectSayLine(t, "@", "again")

	// The fall stops the loop: after the tick it had already booked, no
	// further line arrives.
	c.UseBlock(2, 65, 0)
	c.PumpTicks(srv, 10, func(int32, []byte) bool { return false })
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))

	// With command blocks off the loop keeps ticking silently: the rise
	// books a run that says nothing. Turning them back on says the line
	// again on the loop's own next visit, without a new rise, which is
	// what keeps the rule from being only a fuse.
	c.ChatCommand("gamerule command_blocks_work false")
	servertest.AwaitCommandFeedback(t, c, "commands.gamerule.set", "command_blocks_work", "false")
	if srv.GameRule("command_blocks_work") {
		t.Fatal("setup: command_blocks_work is not off")
	}
	c.UseBlock(2, 65, 0)
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))

	c.ChatCommand("gamerule command_blocks_work true")
	servertest.AwaitCommandFeedback(t, c, "commands.gamerule.set", "command_blocks_work", "true")
	c.ExpectSayLine(t, "@", "again")
	c.UseBlock(2, 65, 0)
	c.PumpTicks(srv, 10, func(int32, []byte) bool { return false })
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))
}

// TestChainCommandBlockRunsAfterTheBlockBehindIt chains a conditional block
// off an impulse block: the chain's line follows the origin's when the
// origin succeeded, and a reprogrammed origin that fails says nothing
// itself, leaving the chain nothing to inherit. The always-active chain
// needs no power of its own, exactly the walk's own condition.
// vanilla: CommandBlock.executeChain, CommandBlockEntity.markConditionMet
func TestChainCommandBlockRunsAfterTheBlockBehindIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=east,conditional=false]")
	servertest.Setblock(t, c, 3, 64, 0, "chain_command_block[facing=east,conditional=true]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ProgramCommandBlock(2, 64, 0, "say one", servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say one")
	c.ProgramCommandBlock(3, 64, 0, "say chained", servertest.ProgramModeChain, true, true, true)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say chained")
	c.Settle()

	// The origin's success is the chain's condition: both lines arrive,
	// the origin's first, and the walk stores the condition it met.
	c.UseBlock(2, 65, 0)
	c.ExpectSayLine(t, "@", "one")
	c.ExpectSayLine(t, "@", "chained")
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))
	chainTag, ok := srv.BlockEntityTagAt(3, 64, 0)
	if !ok {
		t.Fatal("the chain block has no entity")
	}
	if met, _ := nbt.Get[nbt.Byte](chainTag, "conditionMet"); met == 0 {
		t.Error("the chain walk did not store the condition it met")
	}

	// An origin reprogrammed with a command that fails says nothing and
	// succeeds at nothing, so the conditional chain stays silent too.
	c.ProgramCommandBlock(2, 64, 0, "notacommand hi", servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "notacommand hi")
	c.Settle()
	c.UseBlock(2, 65, 0)
	c.UseBlock(2, 65, 0)
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))

	// The origin succeeding again unblocks the chain.
	c.ProgramCommandBlock(2, 64, 0, "say one", servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say one")
	c.Settle()
	c.UseBlock(2, 65, 0)
	c.UseBlock(2, 65, 0)
	c.ExpectSayLine(t, "@", "one")
	c.ExpectSayLine(t, "@", "chained")
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))
}

// TestFailedCommandBlockCommandLeavesAChainSilent runs blocks whose command
// fails without changing anything and checks the conditional chain after each
// stays silent: vanilla's throw counts no success, so the chain's condition is
// never met and the next block does not run.
// vanilla: BaseCommandBlock.performCommand, CommandBlock.executeChain
func TestFailedCommandBlockCommandLeavesAChainSilent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=east,conditional=false]")
	servertest.Setblock(t, c, 3, 64, 0, "chain_command_block[facing=east,conditional=true]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ProgramCommandBlock(3, 64, 0, "say chained", servertest.ProgramModeChain, true, true, true)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say chained")
	c.Settle()

	// /clear finding nothing is a failing command a gamemaster-level block
	// may reach; it runs on the rise, the second pull falling back.
	for _, command := range []string{"clear Actor"} {
		c.ProgramCommandBlock(2, 64, 0, command, servertest.ProgramModeImpulse, true, false, false)
		servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", command)
		c.Settle()
		c.UseBlock(2, 65, 0)
		c.UseBlock(2, 65, 0)
		c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))
	}
}

// TestSetblockAutoCommandBlockStartsItself gives a repeating command block its
// auto flag in the same /setblock that places it: the tag load's setAutomatic
// rule books the run, so the block says its line with no redstone at all.
// vanilla: BlockInput.place, CommandBlockEntity.setAutomatic
func TestSetblockAutoCommandBlockStartsItself(t *testing.T) {
	t.Parallel()
	_, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0,
		`repeating_command_block[facing=north,conditional=false]{auto:1b,Command:"say loop"}`)
	c.ExpectSayLine(t, "@", "loop")
}

// TestProgrammingACommandBlockNeedsACreativeOp checks the gate the Program
// Command Block packet answers to, an opped player in creative mode: an
// operator in survival is told they are not allowed and the block keeps
// both its state and its programming; the same operator in creative
// programs it, and the block runs what they left.
// vanilla: handleSetCommandBlock, Player.canUseGameMasterBlocks
func TestProgrammingACommandBlockNeedsACreativeOp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=north,conditional=false]")
	c.Settle()

	// In survival the packet is refused: the operator hears the editor's
	// refuse line and the block keeps its state.
	c.ChatCommand("gamemode survival")
	servertest.AwaitCommandFeedbackText(t, c, "commands.gamemode.success.self",
		protocol.Text{Translate: "gameMode.survival"})
	c.ProgramCommandBlock(2, 64, 0, "say no", servertest.ProgramModeChain, true, true, true)
	servertest.AwaitCommandFeedback(t, c, "advMode.notAllowed")
	if got := srv.BlockAt(2, 64, 0); got != commandBlockSelectState(t, "minecraft:command_block", "north", false) {
		t.Error("a survival operator's programming changed the block")
	}

	// Back in creative, the same operator programs it and the block says
	// what they left on the next rise.
	c.ChatCommand("gamemode creative")
	servertest.AwaitCommandFeedbackText(t, c, "commands.gamemode.success.self",
		protocol.Text{Translate: "gameMode.creative"})
	c.ProgramCommandBlock(2, 64, 0, "say yes", servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say yes")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.UseBlock(2, 65, 0)
	c.ExpectSayLine(t, "@", "yes")
}

// TestCommandBlockAlwaysActiveDefaults checks the always-active flag a fresh
// command block entity starts with: the chain kind is always active, the
// repeating and impulse kinds wait for redstone. A repeating block with the
// wrong default would run unpowered; a chain one with the wrong default would
// not run from the walk.
// vanilla: CommandBlock.newBlockEntity
func TestCommandBlockAlwaysActiveDefaults(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "chain_command_block[facing=north,conditional=false]")
	servertest.Setblock(t, c, 3, 64, 0, "repeating_command_block[facing=north,conditional=false]")
	servertest.Setblock(t, c, 4, 64, 0, "command_block[facing=north,conditional=false]")
	c.Settle()

	for _, tc := range []struct {
		x, y, z   int32
		name      string
		automatic nbt.Byte
	}{
		{2, 64, 0, "chain", 1},
		{3, 64, 0, "repeating", 0},
		{4, 64, 0, "impulse", 0},
	} {
		tag, ok := srv.BlockEntityTagAt(tc.x, tc.y, tc.z)
		if !ok {
			t.Fatalf("the %s block has no entity", tc.name)
		}
		if auto, _ := nbt.Get[nbt.Byte](tag, "auto"); auto != tc.automatic {
			t.Errorf("the %s block's auto = %d, want %d", tc.name, auto, tc.automatic)
		}
	}
}
