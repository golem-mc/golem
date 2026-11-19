package commands

import (
	"encoding/binary"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestRefusedCommandBlockCommandLeavesAChainSilent runs commands whose
// failure path sends the runner a bare red refusal and answers nil — a
// summon of a type this server does not simulate, an effect clear with
// nothing to clear, a stopwatch that is not there. Each refusal is the
// command's own failure, so the block counts no success and the conditional
// chain after it stays silent.
// vanilla: BaseCommandBlock.performCommand
func TestRefusedCommandBlockCommandLeavesAChainSilent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=east,conditional=false]")
	servertest.Setblock(t, c, 3, 64, 0, "chain_command_block[facing=east,conditional=true]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ProgramCommandBlock(3, 64, 0, "say chained", servertest.ProgramModeChain, true, true, true)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say chained")
	c.Settle()

	for _, command := range []string{
		"summon minecraft:notamob",
		"effect clear Actor",
		"stopwatch remove minecraft:nope",
	} {
		c.ProgramCommandBlock(2, 64, 0, command, servertest.ProgramModeImpulse, true, false, false)
		servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", command)
		c.Settle()
		c.UseBlock(2, 65, 0)
		c.UseBlock(2, 65, 0)
		c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))
	}
}

// TestTrailingCommandBlockLinesRefuseInsteadOfStoppingTheTick runs the
// lines that end in input their read left behind — a trailing space, past
// plain and redirecting words — which answer as the dispatcher's refusals
// from the block's tick, counting no success and leaving the conditional
// chain after them silent.
// vanilla: BaseCommandBlock.performCommand, Commands.finishParsing
func TestTrailingCommandBlockLinesRefuseInsteadOfStoppingTheTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=east,conditional=false]")
	servertest.Setblock(t, c, 3, 64, 0, "chain_command_block[facing=east,conditional=true]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ProgramCommandBlock(3, 64, 0, "say chained", servertest.ProgramModeChain, true, true, true)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say chained")
	c.Settle()

	for _, command := range []string{"time set daytime ", "execute as @a ", "execute as @a run "} {
		c.ProgramCommandBlock(2, 64, 0, command, servertest.ProgramModeImpulse, true, false, false)
		servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", command)
		c.Settle()
		c.UseBlock(2, 65, 0)
		c.UseBlock(2, 65, 0)
		c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))
	}
}

// TestForkedFailureCommandBlockLeavesAChainSilent runs an effect clear
// nothing can satisfy inside an execute fork, which fails in its context and
// so counts no success for the block, while a fork whose run succeeds still
// lets the conditional chain speak.
// vanilla: BaseCommandBlock.performCommand, ExecuteCommand
func TestForkedFailureCommandBlockLeavesAChainSilent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=east,conditional=false]")
	servertest.Setblock(t, c, 3, 64, 0, "chain_command_block[facing=east,conditional=true]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ProgramCommandBlock(3, 64, 0, "say chained-fork", servertest.ProgramModeChain, true, true, true)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say chained-fork")
	c.Settle()

	failing := "execute as @p run effect clear @p"
	c.ProgramCommandBlock(2, 64, 0, failing, servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", failing)
	c.Settle()
	c.UseBlock(2, 65, 0)
	c.UseBlock(2, 65, 0)
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))

	passing := "execute as @p run say ran"
	c.ProgramCommandBlock(2, 64, 0, passing, servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", passing)
	c.Settle()
	c.UseBlock(2, 65, 0)
	c.UseBlock(2, 65, 0)
	c.ExpectSayLineAs(t, servertest.PlayerDisplayComponent("Actor"), "ran")
	c.ExpectSayLine(t, "@", "chained-fork")
}

// TestOverflowingGiveCommandBlockStopsItsChain gives the origin a /give of
// more than a hundred maximum stacks: the refusal is a throw, so the block
// counts no success and the conditional chain after it stays quiet.
// vanilla: GiveCommand.giveItem, BaseCommandBlock.performCommand
func TestOverflowingGiveCommandBlockStopsItsChain(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=east,conditional=false]")
	servertest.Setblock(t, c, 3, 64, 0, "chain_command_block[facing=east,conditional=true]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ProgramCommandBlock(3, 64, 0, "say chained-give", servertest.ProgramModeChain, true, true, true)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say chained-give")
	c.ProgramCommandBlock(2, 64, 0, "give @p minecraft:stone 6401", servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "give @p minecraft:stone 6401")
	c.Settle()

	c.UseBlock(2, 65, 0)
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))
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

// TestSetblockAutoRetaggedCommandBlockStartsItself lays a repeating command
// block first and then gives it the auto flag in a second /setblock: the
// block already stands, so the tag lands through the retag path a /data
// merge's load shares, and its own setAutomatic rule books the run.
// vanilla: CommandBlockEntity.setAutomatic
func TestSetblockAutoRetaggedCommandBlockStartsItself(t *testing.T) {
	t.Parallel()
	_, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "repeating_command_block[facing=north,conditional=false]")
	c.Settle()
	// The same block again, now carrying a command and the auto flag: the
	// tag load is the only thing that can start it, there being no lever.
	servertest.Setblock(t, c, 2, 64, 0,
		`repeating_command_block[facing=north,conditional=false]{Command:"say loop",auto:1b}`)
	c.ExpectSayLine(t, "@", "loop")
}

// TestSetblockRetaggedAutoCommandBlockStaysSilent re-tags a command block that
// is already always active with a new command: the same state keeps the block
// entity, so the auto flag does not change and the tag load's setAutomatic
// rule books no run.
// vanilla: CommandBlockEntity.setAutomatic
func TestSetblockRetaggedAutoCommandBlockStaysSilent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0,
		`command_block[facing=north,conditional=false]{auto:1b,Command:"say a"}`)
	c.ExpectSayLine(t, "@", "a")
	c.Settle()

	// The same state again, now Command:"say b": the block entity is kept
	// with its auto flag already on, so the load owes no run.
	servertest.Setblock(t, c, 2, 64, 0,
		`command_block[facing=north,conditional=false]{auto:1b,Command:"say b"}`)
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))
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

// TestCommandBlocksRunInTheNetherToo carries the actor into the nether -
// their own dimension is where the programming packet and the held chunks
// are, and a block nobody holds a chunk for is swept before its run - and
// programs a block there: the redstone block placed beside it gives the
// rise the nether's own engine books, and the run's say reaches the whole
// server, the broadcast the overworld hears. The nether and the end share
// the one activation path; this covers its dimension other than the
// overworld.
// vanilla: CommandBlock.neighborChanged, handleSetCommandBlock
func TestCommandBlocksRunInTheNetherToo(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.NetherTestConfig(t, servertest.BlockTestConfig()), "Actor", servertest.OpsListing(t, "Actor"))

	// A block to stand on, so the carried actor lands somewhere the lava
	// did not choose.
	c.ChatCommand("execute in minecraft:the_nether run setblock 8 40 8 minecraft:stone")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "8", "40", "8")
	srv.MovePlayerToDimension("Actor", world.NetherName, 8.5, 41, 8.5)
	c.ExpectViewWithinTicks(srv, 200)
	c.Settle()

	// In the nether now: the actor's own /setblock and programming packet
	// land there. The redstone block beside a programmed block gives the
	// rise its own placement books.
	servertest.Setblock(t, c, 10, 40, 8, "command_block[facing=north,conditional=false]")
	c.ProgramCommandBlock(10, 40, 8, "say hi there", servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say hi there")
	servertest.Setblock(t, c, 9, 40, 8, "redstone_block")

	c.ExpectSayLine(t, "@", "hi there")
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

// TestCommandBlockTrackingOffHidesTheAdminEcho runs a block whose command
// reaches the operators with output tracking off: the block runs with
// vanilla's null source, so no echo arrives; tracked, the same run is echoed.
// vanilla: BaseCommandBlock.createSource, CloseableCommandBlockSource.shouldInformAdmins
func TestCommandBlockTrackingOffHidesTheAdminEcho(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=north,conditional=false]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()

	// Tracking off: the run reaches the operators with no echo. The run's
	// own /setblock is the sign it happened, so the wait ends when the
	// cell changed rather than on a fixed window.
	c.ProgramCommandBlock(2, 64, 0, "setblock 5 64 0 stone", servertest.ProgramModeImpulse, false, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "setblock 5 64 0 stone")
	c.UseBlock(2, 65, 0)
	stone := servertest.SupportState(t, "minecraft:stone", nil)
	echoed := false
	collect := func(id int32, _ []byte) bool {
		if id == protocol.IDPlaySystemChat {
			echoed = true
		}
		return false
	}
	for i := 0; i < 40 && srv.BlockAt(5, 64, 0) != stone; i++ {
		c.PumpTicks(srv, 1, collect)
	}
	if srv.BlockAt(5, 64, 0) != stone {
		t.Fatal("the untracked run never changed the block")
	}
	c.PumpTicks(srv, 2, collect)
	if echoed {
		t.Error("an untracked command block echoed to the operator")
	}

	// Tracking on: the same run is echoed to the operator.
	c.ProgramCommandBlock(2, 64, 0, "setblock 6 64 0 stone", servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "setblock 6 64 0 stone")
	c.UseBlock(2, 65, 0)
	c.Settle()
	c.UseBlock(2, 65, 0)
	servertest.AwaitAdminBroadcast(t, c, protocol.Text{Text: "@"}, protocol.Text{
		Translate: "commands.setblock.success",
		With:      []protocol.Text{servertest.NumberText(6), servertest.NumberText(64), servertest.NumberText(0)},
	})
}

// TestDisabledCommandBlockKeepsItsLastOutput runs a block once and then turns
// command_blocks_work off: vanilla clears the last output only while a command
// actually runs, so the disabled run leaves the line the first run saved.
// vanilla: BaseCommandBlock.performCommand
func TestDisabledCommandBlockKeepsItsLastOutput(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=north,conditional=false]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ProgramCommandBlock(2, 64, 0, "setblock 5 64 0 stone", servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "setblock 5 64 0 stone")

	c.UseBlock(2, 65, 0)
	// The run's own echo is the sign it happened and left the output.
	servertest.AwaitAdminBroadcast(t, c, protocol.Text{Text: "@"}, protocol.Text{
		Translate: "commands.setblock.success",
		With:      []protocol.Text{servertest.NumberText(5), servertest.NumberText(64), servertest.NumberText(0)},
	})
	tag, ok := srv.BlockEntityTagAt(2, 64, 0)
	if !ok || tag["LastOutput"] == nil {
		t.Fatal("setup: the run left no last output")
	}

	c.ChatCommand("gamerule command_blocks_work false")
	servertest.AwaitCommandFeedback(t, c, "commands.gamerule.set", "command_blocks_work", "false")
	c.UseBlock(2, 65, 0)
	c.Settle()
	c.UseBlock(2, 65, 0)
	c.Settle()
	c.PumpTicks(srv, 10, func(int32, []byte) bool { return false })

	tag, ok = srv.BlockEntityTagAt(2, 64, 0)
	if !ok || tag["LastOutput"] == nil {
		t.Error("a disabled run cleared the last output")
	}
}

// TestRepeatingCommandBlockStopsWhenSwitchedToChain runs a repeating block on
// a lever, stops the loop, reprograms the block to chain and starts the loop
// again: the visit the running loop had booked must do nothing in chain mode,
// so the block's last execution stands. The loop is stopped across the switch
// so the switch and the booked visit cannot race.
// vanilla: CommandBlock.tick (SEQUENCE)
func TestRepeatingCommandBlockStopsWhenSwitchedToChain(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "repeating_command_block[facing=north,conditional=false]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ProgramCommandBlock(2, 64, 0, "say again", servertest.ProgramModeRepeating, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say again")

	c.UseBlock(2, 65, 0)
	c.ExpectSayLine(t, "@", "again")

	srv.StopTick()
	c.ProgramCommandBlock(2, 64, 0, "say again", servertest.ProgramModeChain, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say again")
	tag, ok := srv.BlockEntityTagAt(2, 64, 0)
	if !ok {
		t.Fatal("the command block has no entity")
	}
	before, _ := nbt.Get[nbt.Long](tag, "LastExecution")

	srv.StartTick()
	c.PumpTicks(srv, 20, func(int32, []byte) bool { return false })
	tag, ok = srv.BlockEntityTagAt(2, 64, 0)
	if !ok {
		t.Fatal("the command block has no entity")
	}
	if after, _ := nbt.Get[nbt.Long](tag, "LastExecution"); after != before {
		t.Errorf("a chain-mode visit ran the block: last execution %d -> %d", before, after)
	}
}

// TestCommandBlockRunTimeRefusalWaitsForItsContext runs refusals vanilla
// raises only once the command runs — an unknown dimension, an unsummonable
// type — behind a conditional that leaves the chain no context. The command
// parses, nothing is left to refuse, and the run clears the block's last
// output and leaves none; the same refusal with a context left records its
// bare message.
// vanilla: BuildContexts.execute, DimensionArgument.getDimension,
// ResourceArgument.getSummonableEntityType
func TestCommandBlockRunTimeRefusalWaitsForItsContext(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=north,conditional=false]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()

	run := func(command string) any {
		t.Helper()
		c.ProgramCommandBlock(2, 64, 0, command, servertest.ProgramModeImpulse, true, false, false)
		servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", command)
		c.Settle()
		c.UseBlock(2, 65, 0)
		c.UseBlock(2, 65, 0)
		c.PumpTicks(srv, 10, func(int32, []byte) bool { return false })
		tag, ok := srv.BlockEntityTagAt(2, 64, 0)
		if !ok {
			t.Fatal("the command block is gone")
		}
		return tag["LastOutput"]
	}

	for _, command := range []string{
		"execute if entity @e[tag=nobody] in minecraft:foo run say hi",
		"execute if entity @e[tag=nobody] run summon minecraft:player",
	} {
		// The refusal with a context to throw in is the last output each
		// case must clear.
		out := run("execute in minecraft:foo run say hi")
		if !strings.Contains(fmt.Sprint(out), "argument.dimension.invalid") {
			t.Fatalf("setup: the refused run left %v, want the dimension refusal", out)
		}
		if out := run(command); out != nil {
			t.Errorf("%q left %v, want no output", command, out)
		}
	}
}

// TestForkedConditionalFailureCommandBlockLeavesAChainSilent runs an execute
// whose trailing if or unless fails, forked by an as and not, so the block
// counts no success and the conditional chain after it stays silent; the
// passing twin of each lets the chain speak.
// vanilla: ContextChain.runExecutable, ExecuteCommand.addConditional
func TestForkedConditionalFailureCommandBlockLeavesAChainSilent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=east,conditional=false]")
	servertest.Setblock(t, c, 3, 64, 0, "chain_command_block[facing=east,conditional=true]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ProgramCommandBlock(3, 64, 0, "say chained-cond", servertest.ProgramModeChain, true, true, true)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "say chained-cond")
	c.Settle()

	run := func(command string, speaks bool) {
		t.Helper()
		c.ProgramCommandBlock(2, 64, 0, command, servertest.ProgramModeImpulse, true, false, false)
		servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", command)
		c.Settle()
		c.UseBlock(2, 65, 0)
		c.UseBlock(2, 65, 0)
		if speaks {
			c.ExpectSayLine(t, "@", "chained-cond")
		}
		c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisguisedChat))
	}
	for _, command := range []string{
		"execute as @p if entity @e[type=minecraft:pig]",
		"execute unless entity @p",
		"execute as @p unless entity @p",
		"execute if entity @e[type=minecraft:pig]",
		"execute as @p if score @s nothing matches 1",
		"execute as @p if entity @e[type=minecraft:pig] run say never",
	} {
		run(command, false)
	}
	run("execute as @p if entity @p", true)
	run("execute as @p unless entity @e[type=minecraft:pig]", true)
}

// TestForkedCommandBlockCountsEverySuccess forks a run over two armor stands
// and reads the block's success count back.
// vanilla: CommandSourceStack.withCallback, BaseCommandBlock.performCommand
func TestForkedCommandBlockCountsEverySuccess(t *testing.T) {
	t.Parallel()
	_, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=east,conditional=false]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ChatCommand("summon minecraft:armor_stand 6 64 6 {UUID:[I;1,2,3,4]}")
	c.ChatCommand("summon minecraft:armor_stand 7 64 6 {UUID:[I;5,6,7,8]}")
	c.Settle()
	command := "execute as @e[type=minecraft:armor_stand] run say counted"
	c.ProgramCommandBlock(2, 64, 0, command, servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", command)
	c.Settle()
	c.UseBlock(2, 65, 0)
	// @e sorts by distance from the block, so the nearer stand speaks first.
	for _, words := range [][4]uint32{{1, 2, 3, 4}, {5, 6, 7, 8}} {
		var id uuid.UUID
		for i, w := range words {
			binary.BigEndian.PutUint32(id[i*4:], w)
		}
		c.ExpectSayLineAs(t, servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", id), "counted")
	}
	c.Settle()

	c.ChatCommand("data get block 2 64 0 SuccessCount")
	c.ExpectCommandFeedback("commands.data.block.query", "2", "64", "0", "2")
}

// TestCommandBlockExecuteTeleportKeepsItsFeedback runs execute-as teleports
// from a command block, back to back with different tails, and checks each
// run leaves the success line its own tail's form names: a position, then a
// destination entity. Switching send_command_feedback off leaves no line.
// vanilla: TeleportCommand.performTeleport
func TestCommandBlockExecuteTeleportKeepsItsFeedback(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=north,conditional=false]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	for range 3 {
		c.ChatCommand(`summon armor_stand 8 64 8 {Tags:["tpfb"]}`)
		c.Settle()
	}

	run := func(command string) string {
		t.Helper()
		c.ProgramCommandBlock(2, 64, 0, command, servertest.ProgramModeImpulse, true, false, false)
		servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", command)
		c.Settle()
		c.UseBlock(2, 65, 0)
		c.UseBlock(2, 65, 0)
		c.PumpTicks(srv, 10, func(int32, []byte) bool { return false })
		tag, ok := srv.BlockEntityTagAt(2, 64, 0)
		if !ok {
			t.Fatal("the command block is gone")
		}
		return fmt.Sprint(tag["LastOutput"])
	}

	if out := run("execute as @e[tag=tpfb] run tp @s 9 64 9"); !strings.Contains(out, "commands.teleport.success.location.single") {
		t.Errorf("a position tail left %q, want the location line", out)
	}
	if out := run("execute as @e[tag=tpfb] run tp @s @e[tag=tpfb,limit=1]"); !strings.Contains(out, "commands.teleport.success.entity.single") {
		t.Errorf("a destination tail left %q, want the entity line", out)
	}

	c.ChatCommand("gamerule send_command_feedback false")
	c.Settle()
	if out := run("execute as @e[tag=tpfb] run tp @s 9 64 9"); strings.Contains(out, "teleport") {
		t.Errorf("a run with feedback off left %q", out)
	}
}

// TestACommandBlockPlacedWithDataKeepsItsFlags places a repeating command
// block from a stack whose block_entity_data makes it always active with
// output tracking off: the data's flags survive the placement and the block
// starts itself with no redstone. A plain stack placed the same way takes the
// kind's defaults and stays silent, and so does data that is always active
// but already powered: the load books nothing and losing the power runs
// nothing.
// vanilla: CommandBlock.setPlacedBy, CommandBlockEntity.setAutomatic
func TestACommandBlockPlacedWithDataKeepsItsFlags(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	c.ChatCommand(`give @s repeating_command_block[block_entity_data={id:"minecraft:command_block",Command:"say looping",auto:1b,TrackOutput:0b}]`)
	c.Settle()
	c.PlaceBlock(2, 63, 0, protocol.FaceTop)
	c.ExpectSayLine(t, "@", "looping")

	tag, ok := srv.BlockEntityTagAt(2, 64, 0)
	if !ok {
		t.Fatal("the placed block has no entity")
	}
	if auto, _ := nbt.Get[nbt.Byte](tag, "auto"); auto != 1 {
		t.Errorf("the placed block's auto = %d, want 1", auto)
	}
	if track, _ := nbt.Get[nbt.Byte](tag, "TrackOutput"); track != 0 {
		t.Errorf("the placed block's TrackOutput = %d, want 0", track)
	}

	// Breaking the looping block stops its lines before the plain placement.
	servertest.Setblock(t, c, 2, 64, 0, "air")
	c.Settle()
	c.ChatCommand("clear @s")
	c.ChatCommand("give @s repeating_command_block")
	c.Settle()
	c.PlaceBlock(2, 63, 2, protocol.FaceTop)
	c.Settle()
	plain, ok := srv.BlockEntityTagAt(2, 64, 2)
	if !ok {
		t.Fatal("the plain block has no entity")
	}
	if auto, _ := nbt.Get[nbt.Byte](plain, "auto"); auto != 0 {
		t.Errorf("the plain block's auto = %d, want 0", auto)
	}

	c.ChatCommand("clear @s")
	c.ChatCommand(`give @s repeating_command_block[block_entity_data={id:"minecraft:command_block",Command:"say looping",auto:1b,powered:1b,TrackOutput:0b}]`)
	c.Settle()
	c.PlaceBlock(2, 63, -2, protocol.FaceTop)
	c.Settle()
	if _, ok := srv.BlockEntityTagAt(2, 64, -2); !ok {
		t.Fatal("the powered-data block has no entity")
	}
	c.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlayDisguisedChat))
}

// TestCommandBlockLastOutputCarriesTheClockPrefix checks the saved last output
// is "[HH:mm:ss] " with the message as its one sibling.
// vanilla: CloseableCommandBlockSource.sendSystemMessage
func TestCommandBlockLastOutputCarriesTheClockPrefix(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=north,conditional=false]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()
	c.ProgramCommandBlock(2, 64, 0, "setblock ~ ~2 ~ stone", servertest.ProgramModeImpulse, true, false, false)
	servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", "setblock ~ ~2 ~ stone")
	c.UseBlock(2, 65, 0)
	c.PumpTicks(srv, 10, func(int32, []byte) bool { return false })

	tag, ok := srv.BlockEntityTagAt(2, 64, 0)
	if !ok {
		t.Fatal("the command block is gone")
	}
	out, ok := tag["LastOutput"].(nbt.Compound)
	if !ok {
		t.Fatalf("LastOutput = %#v, want a component", tag["LastOutput"])
	}
	if text, _ := out["text"].(nbt.String); !regexp.MustCompile(`^\[\d\d:\d\d:\d\d\] $`).MatchString(string(text)) {
		t.Errorf("text = %q, want the [HH:mm:ss] prefix", text)
	}
	extra, _ := out["extra"].(nbt.List)
	if len(extra.Items) != 1 {
		t.Fatalf("extra = %#v, want the one message", out["extra"])
	}
	if msg, _ := extra.Items[0].(nbt.Compound); msg["translate"] != nbt.String("commands.setblock.success") {
		t.Errorf("extra[0] = %#v, want commands.setblock.success", extra.Items[0])
	}
}
