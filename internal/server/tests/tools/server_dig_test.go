package tools

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// A survival client breaks a block on the START_DESTROY_BLOCK alone when one
// tick of digging finishes it, and the server has to break it there too.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction

var persistentLeaves = map[string]string{"persistent": "true"}

// TestShearsInstaMineLeaves: 15 / 0.2 / 30 = 2.5, so the click breaks the
// leaves, and shears are what the loot table drops the leaves block for.
func TestShearsInstaMineLeaves(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Digger(t, servertest.ItemStack(t, "minecraft:shears", 1), "minecraft:oak_leaves", persistentLeaves, true)

	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.Settle()

	servertest.ExpectAir(t, srv, servertest.DigTarget)
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:oak_leaves")
}

// TestIronHoeInstaMinesLeaves: 6 / 0.2 / 30 is exactly one in float32, which
// the client counts as an insta-mine.
func TestIronHoeInstaMinesLeaves(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Digger(t, servertest.ItemStack(t, "minecraft:iron_hoe", 1), "minecraft:oak_leaves", persistentLeaves, true)

	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.Settle()

	servertest.ExpectAir(t, srv, servertest.DigTarget)
}

// TestHandDoesNotInstaMineStone: 1 / 1.5 / 100 a tick. The start and the
// cancel are each acknowledged, and the stone stands.
func TestHandDoesNotInstaMineStone(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Digger(t, protocol.ItemStack{}, "minecraft:stone", nil, true)
	stone := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])

	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("start acknowledged as sequence %d, want 1", seq)
	}
	c.CancelDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	if seq := c.ExpectBlockAck(); seq != 2 {
		t.Errorf("cancel acknowledged as sequence %d, want 2", seq)
	}
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != stone {
		t.Errorf("block after a started dig = %d, want the stone (%d)", got, stone)
	}
	servertest.ExpectNoDroppedItems(t, srv)
}

// TestEfficiencyDecidesInstaMines: a diamond pickaxe mines at 8, Efficiency
// V adds 5*5+1. That clears netherrack (34 / 0.4 / 30 = 2.8) but not stone
// (34 / 1.5 / 30 = 0.76), and the plain pickaxe does not clear netherrack
// (8 / 0.4 / 30 = 0.67).
func TestEfficiencyDecidesInstaMines(t *testing.T) {
	efficient := func(t *testing.T) protocol.ItemStack {
		return servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:efficiency", 5)
	}
	for _, tc := range []struct {
		name    string
		stack   func(*testing.T) protocol.ItemStack
		block   string
		breaks  bool
		dropped string
	}{
		{"efficiency V on netherrack", efficient, "minecraft:netherrack", true, "minecraft:netherrack"},
		{"efficiency V on stone", efficient, "minecraft:stone", false, ""},
		{"plain pickaxe on netherrack", func(t *testing.T) protocol.ItemStack {
			return servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1)
		}, "minecraft:netherrack", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Digger(t, tc.stack(t), tc.block, nil, true)
			before := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])

			c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
			if seq := c.ExpectBlockAck(); seq != 1 {
				t.Errorf("start acknowledged as sequence %d, want 1", seq)
			}
			c.Settle()

			if tc.breaks {
				servertest.ExpectAir(t, srv, servertest.DigTarget)
				servertest.ExpectOneDroppedItem(t, srv, tc.dropped)
			} else if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != before {
				t.Errorf("block after a started dig = %d, want it standing (%d)", got, before)
			}
		})
	}
}

// TestOffGroundDividesTheDigSpeed: shears on leaves off the ground are
// 2.5 / 5 = 0.5 a tick, no insta-mine.
func TestOffGroundDividesTheDigSpeed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Digger(t, servertest.ItemStack(t, "minecraft:shears", 1), "minecraft:oak_leaves", persistentLeaves, false)
	leaves := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])

	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("start acknowledged as sequence %d, want 1", seq)
	}
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != leaves {
		t.Errorf("block after a started dig off the ground = %d, want the leaves (%d)", got, leaves)
	}
}

// TestTheBlockUpdateReachesTheClientBeforeItsAck pins the order the block
// change of an interaction reaches the client in: the update goes out ahead
// of the drop the break lays, the acknowledgement behind it at the tick end.
// Shears clear leaves in one tick, so the START alone is the whole dig, its
// update and its sequence both.
// vanilla: ServerGamePacketListenerImpl.ackBlockChangesUpTo
func TestTheBlockUpdateReachesTheClientBeforeItsAck(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Digger", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Digger", player.HotbarSlot, servertest.ItemStack(t, "minecraft:shears", 1))
	leaves, ok := registry.FindState("minecraft:oak_leaves", persistentLeaves)
	if !ok {
		t.Fatal("no oak leaves state with persistent")
	}
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], leaves) {
		t.Fatal("could not stand the leaves")
	}
	c.Settle()

	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	var update servertest.BlockChange
	sawUpdate := false
	see := func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			if cells := servertest.BlockChangesIn(t, id, data); len(cells) > 0 && !sawUpdate {
				update = cells[0]
				sawUpdate = true
			}
		case protocol.IDPlayAckBlockChange:
			if !sawUpdate {
				t.Errorf("the ack reached the client before the block update it answers")
			}
		default:
			// The item the break drops and the light it relights, told on
			// the same road as the update.
		}
	}
	// Settle on a ping behind the action so the step cannot outrun the
	// connection goroutine's record of the sequence and flush nothing; the
	// update, flushed ahead of the drop, may already come before its answer.
	c.SettleAndCollect(see)
	clock.Step()
	if !sawUpdate {
		c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
			see(id, data)
			return sawUpdate
		})
	}
	if !sawUpdate {
		t.Fatal("no block update within the wait")
	}
	if update.X != servertest.DigTarget[0] || update.Y != servertest.DigTarget[1] || update.Z != servertest.DigTarget[2] {
		t.Errorf("the update names (%d, %d, %d), want the dug leaves %v",
			update.X, update.Y, update.Z, servertest.DigTarget)
	}
	if update.State != 0 {
		t.Errorf("the update says %d, want air", update.State)
	}

	// The tick end answers the sequence, behind the update.
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the tick end acknowledged sequence %d, want 1", seq)
	}
}

// TestAStoppedDigLeavesItsBlockToTheTicks pins the stop of a survival dig
// that has not yet earned its break: a bare hand takes a hundred and
// fiftieth of the stone a tick - a hundred and fifty to the whole of it -
// so the click that gives up at the start is answered with the bare
// acknowledgement, the stone stands, and the ticks finish what the click
// started, the first of them doubling the click's own share. The stone
// stands through the hundred and forty-eighth tick, whose share still falls
// one hundred and fiftieth short, and is gone at the hundred and
// forty-ninth, whose share brings the whole of it in.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction, STOP_DESTROY_BLOCK
func TestAStoppedDigLeavesItsBlockToTheTicks(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Digger", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c.MoveToGround(0.5, 64, 0.5, true)
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], stone) {
		t.Fatal("could not stand the stone")
	}
	c.Settle()

	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.StopDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.Settle()
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != stone {
		t.Errorf("block after a stopped dig = %d, want the stone (%d)", got, stone)
	}
	servertest.ExpectNoDroppedItems(t, srv)

	// One tick in, the only word on the wire is the stop's own
	// acknowledgement, which the tick end flushes.
	clock.Step()
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the stop acknowledged as sequence %d, want 1", seq)
	}

	// The ticks take the stone: a hundred and fifty shares of a hundred
	// and fiftieth, the first tick's two among them. The bare hand cannot
	// harvest the stone, so the break says nothing about drops.
	servertest.StepTicks(t, srv, clock, 147)
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != stone {
		t.Errorf("block after a hundred and forty-eight ticks = %d, want the stone (%d)", got, stone)
	}
	servertest.StepTicks(t, srv, clock, 2)
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != 0 {
		t.Errorf("block after the ticks finished the dig = %d, want air", got)
	}
	servertest.ExpectNoDroppedItems(t, srv)
}

// TestADelayedDigBreaksAtItsOwnPace: shears off the ground take half a leaf
// a tick, so the click that gives up at half leaves the leaf standing for
// one tick, whose two halves are the whole of it.
// vanilla: ServerPlayerGameMode.tick
func TestADelayedDigBreaksAtItsOwnPace(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Digger", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c.MoveToGround(0.5, 64, 0.5, false)
	srv.GiveSlot("Digger", player.HotbarSlot, servertest.ItemStack(t, "minecraft:shears", 1))
	leaves, ok := registry.FindState("minecraft:oak_leaves", persistentLeaves)
	if !ok {
		t.Fatal("no oak leaves state with persistent")
	}
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], leaves) {
		t.Fatal("could not stand the leaves")
	}
	c.Settle()
	leaf := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])

	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.StopDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.Settle()
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != leaf {
		t.Errorf("block after a stopped dig = %d, want the leaves (%d)", got, leaf)
	}

	// The one tick to come doubles the click's half to the whole of it,
	// and its end acknowledges the stop behind the leaf's going.
	servertest.StepTicks(t, srv, clock, 1)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the stop acknowledged as sequence %d, want 1", seq)
	}
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != 0 {
		t.Errorf("block after the tick finished the dig = %d, want air", got)
	}
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:oak_leaves")
}

// TestAnAbandonedDigBreaksOnTheTicks pins what the client letting go does
// to the dig whose click left the block for the ticks: the dig comes off
// the books and the delayed destroy stays on them, so the stone breaks at
// the pace the click set however long the abort ran ahead of it - vanilla's
// abort clears the digging flag alone. The client re-STARTs the block once
// the ack reverts its ghost and sends the abort when the button comes up,
// which is the START, stop, START, abort the wire carries.
func TestAnAbandonedDigBreaksOnTheTicks(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Digger", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c.MoveToGround(0.5, 64, 0.5, true)
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], stone) {
		t.Fatal("could not stand the stone")
	}
	c.Settle()

	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.StopDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.CancelDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.Settle()
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != stone {
		t.Errorf("block after an abandoned dig = %d, want the stone (%d)", got, stone)
	}

	// The stop and the abort acknowledge together at the first tick end,
	// and the ticks carry the delayed destroy on: the stone stands through
	// the hundred and forty-eighth tick and is gone at the hundred and
	// forty-ninth, the same pace a stop that was never abandoned keeps.
	clock.Step()
	if seq := c.ExpectBlockAck(); seq != 2 {
		t.Errorf("the stop and the abort acknowledged as sequence %d, want 2", seq)
	}
	servertest.StepTicks(t, srv, clock, 147)
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != stone {
		t.Errorf("block after a hundred and forty-eight ticks = %d, want the stone (%d)", got, stone)
	}
	servertest.StepTicks(t, srv, clock, 2)
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != 0 {
		t.Errorf("block after the ticks finished the abandoned dig = %d, want air", got)
	}
	servertest.ExpectNoDroppedItems(t, srv)
}

// TestUseItemAnswersItsSequence pins the Use Item packet's sequence being
// answered like a dig's: the click a client scoops open water with changes
// the world while naming no block in its packet — the water it lifts is
// the server's own finding along the look the packet carries — and the
// sequence still gets the acknowledgement, which is what keeps the
// client's prediction book from filling with unanswered clicks.
// vanilla: ServerGamePacketListenerImpl.handleUseItem
func TestUseItemAnswersItsSequence(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Scooper")

	// Open water where a look straight down finds it: under the feet.
	if !srv.SetBlock(0, 63, 0, registry.MustState("minecraft:water", nil)) {
		t.Fatal("could not pour the water")
	}
	srv.GiveSlot("Scooper", player.HotbarSlot, servertest.ItemStack(t, "minecraft:bucket", 1))
	c.Settle()

	// The scoop's look is the packet's own; a level one misses the water.
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutVarInt(1) // sequence
		b.PutF32(0)    // yaw
		b.PutF32(90)   // pitch: straight down
	})
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the use acknowledged as sequence %d, want 1", seq)
	}
	c.Settle()
	if got := srv.BlockAt(0, 63, 0); got != 0 {
		t.Errorf("water after the scoop = %d, want air", got)
	}
}

// TestTheAcksOfATickMergeOnTheHighestSequence pins the merge the
// acknowledgements of one tick's interactions take: the highest sequence
// recorded is the one the flush answers with, so a lower sequence cannot
// replace a higher one waiting behind it. The flush stands the answer down,
// so a later tick answers the same sequence again — and a negative one
// records nothing, no answer going out for it.
// vanilla: ServerGamePacketListenerImpl.ackBlockChangesUpTo
func TestTheAcksOfATickMergeOnTheHighestSequence(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Digger", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// Two answers recorded in one tick's stop-and-abort, the lower second.
	cancelWith := func(seq int32) {
		c.T.Helper()
		c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
			b.PutVarInt(protocol.PlayerActionCancelledDigging)
			b.PutPosition(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
			b.PutI8(0) // face
			b.PutVarInt(seq)
		})
	}
	cancelWith(7)
	cancelWith(3)
	c.Settle()
	clock.Step()
	if seq := c.ExpectBlockAck(); seq != 7 {
		t.Errorf("the tick acknowledged sequence %d, want 7", seq)
	}

	// The flush stands the answer down, so the same sequence answers
	// again in a later tick.
	cancelWith(7)
	c.Settle()
	clock.Step()
	if seq := c.ExpectBlockAck(); seq != 7 {
		t.Errorf("the repeated sequence acknowledged as %d, want 7", seq)
	}

	// A negative sequence is no answer at all: nothing goes out for it.
	cancelWith(-5)
	c.Settle()
	servertest.StepTicks(t, srv, clock, 1)
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayAckBlockChange)) // the step clock ran the tick
}

// TestACreativeStopWithoutItsStartBreaksNothing pins the finished-digging
// a creative client sends with no dig on the books: creative's break is
// its START's own, and the lone finished click answers with the bare
// acknowledgement, the block it named standing and the wire carrying no
// update for it — the answer vanilla gives a creative stop that no dig is
// behind.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction STOP_DESTROY_BLOCK
func TestACreativeStopWithoutItsStartBreaksNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Breaker")
	c.MoveToGround(0.5, 64, 0.5, true)
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], stone) {
		t.Fatal("could not stand the stone")
	}
	c.Settle()

	// The finished click no START stands behind.
	c.StopDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	acked := false
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		c.T.Helper()
		switch id {
		case protocol.IDPlayBlockUpdate:
			t.Errorf("a lone finished click in creative changed a block")
		case protocol.IDPlayAckBlockChange:
			acked = true
			if seq := protocol.NewReader(data).ReadVarInt(); seq != 1 {
				t.Errorf("the finished click acknowledged sequence %d, want 1", seq)
			}
		default:
			// The wire carries nothing but the acknowledgement here;
			// anything else on it is a standing player's background.
		}
		if acked {
			break
		}
	}
	if !acked {
		t.Fatal("no acknowledgement within the wait")
	}
	c.ExpectNothing(srv)
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != stone {
		t.Errorf("block after a lone finished click = %d, want the stone (%d)", got, stone)
	}
}

// TestAStopOnAnotherBlockEndsNothing pins the finished click on a block
// that is not the dig's own: it is the bare acknowledgement alone — the
// dig it interrupts staying exactly where it was, its block held to the
// click's pace and never left to the ticks, and the block the click named
// standing untouched.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction STOP_DESTROY_BLOCK
func TestAStopOnAnotherBlockEndsNothing(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Digger", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	// Off the ground, shears take half a leaf a tick: a dig that stays
	// short of the seven tenths both on the click and after it.
	c.MoveToGround(0.5, 64, 0.5, false)
	srv.GiveSlot("Digger", player.HotbarSlot, servertest.ItemStack(t, "minecraft:shears", 1))
	leaves, ok := registry.FindState("minecraft:oak_leaves", persistentLeaves)
	if !ok {
		t.Fatal("no oak leaves state with persistent")
	}
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], leaves) {
		t.Fatal("could not stand the leaves")
	}
	grass := registry.MustState("minecraft:short_grass", nil)
	if !srv.SetBlock(4, 64, 0, grass) {
		t.Fatal("could not stand the grass")
	}
	c.Settle()

	// The dig starts on the leaves; the finished click names the grass.
	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.StopDigging(4, 64, 0)
	c.Settle()
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != leaves {
		t.Errorf("block the dig is on after a finished click elsewhere = %d, want the leaves (%d)", got, leaves)
	}
	if got := srv.BlockAt(4, 64, 0); got != grass {
		t.Errorf("block the finished click named = %d, want the grass standing (%d)", got, grass)
	}

	// The click is answered with the ack alone, and the ticks neither
	// break the dig's own block nor lift the one it named.
	clock.Step()
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the stop somewhere else acknowledged as sequence %d, want 1", seq)
	}
	servertest.StepTicks(t, srv, clock, 3)
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != leaves {
		t.Errorf("block the dig is on after its ticks = %d, want the leaves (%d)", got, leaves)
	}
	if got := srv.BlockAt(4, 64, 0); got != grass {
		t.Errorf("block the finished click named after the ticks = %d, want the grass standing (%d)", got, grass)
	}
	servertest.ExpectNoDroppedItems(t, srv)
}

// TestARespawnTakesItsDigOffTheBooks pins the dig the death interrupted:
// the respawn takes it off the books, the delayed destroy with the rest, so
// the block the dig was left to the ticks stands behind the fresh body
// however many run after it. Vanilla's respawn builds the player a new
// game mode; ours resets the old one (see resetDig).
func TestARespawnTakesItsDigOffTheBooks(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c, entityID := servertest.JoinAs(t, servertest.SurvivalConfig(), "Digger", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	}, servertest.OpsListing(t, "Digger"))
	// Off the ground, shears take half a leaf a tick, so the dig's
	// delayed destroy would finish on the first tick behind it.
	c.MoveToGround(0.5, 64, 0.5, false)
	srv.GiveSlot("Digger", player.HotbarSlot, servertest.ItemStack(t, "minecraft:shears", 1))
	leaves, ok := registry.FindState("minecraft:oak_leaves", persistentLeaves)
	if !ok {
		t.Fatal("no oak leaves state with persistent")
	}
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], leaves) {
		t.Fatal("could not stand the leaves")
	}
	c.Settle()

	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.StopDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.Settle()

	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Digger")},
	})
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	servertest.AwaitRespawnSequence(t, c)

	// The death emptied the slots, so the shears go into the respawned
	// player's hand: half a leaf a tick again, the dig - were it still on
	// the books - finished by a single one of the ticks to come.
	srv.GiveSlot("Digger", player.HotbarSlot, servertest.ItemStack(t, "minecraft:shears", 1))
	c.Settle()

	// Ticks that would long since have finished the dig, were it still on
	// the books behind the respawn.
	servertest.StepTicks(t, srv, clock, 4)
	if got := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); got != leaves {
		t.Errorf("block behind a respawned player after its ticks = %d, want the leaves standing (%d)", got, leaves)
	}
}

// attributeDigger joins a survival op on the ground, sets one of its
// attributes' bases with /attribute, hands it the stack and stands dirt at
// the dig target.
func attributeDigger(t *testing.T, attribute string, value float64, stack protocol.ItemStack) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalConfig(), "Digger", servertest.OpsListing(t, "Digger"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.ChatCommand(fmt.Sprintf("attribute @s %s base set %g", attribute, value))
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	if !stack.Empty() {
		srv.GiveSlot("Digger", player.HotbarSlot, stack)
		servertest.RunTicks(t, srv, 1)
	}
	dirt, ok := registry.FindState("minecraft:dirt", nil)
	if !ok {
		t.Fatal("no dirt state")
	}
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], dirt) {
		t.Fatal("could not stand the dirt")
	}
	c.Settle()
	return srv, c
}

// expectDugWithinTicks starts a dig on the target and expects it broken
// within two ticks, as the client's predicted insta-mine leaves it.
func expectDugWithinTicks(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.Settle()
	servertest.UntilTicks(t, srv, 2, "the dirt broken on the first click", func() bool {
		return srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]) == 0
	})
}

// TestBlockBreakSpeedAttributeBreaksDirtOnTheFirstClick: a bare hand on dirt
// at block_break_speed 100 is 1 * 100 / 0.5 / 30 = 6.7 a tick.
// vanilla: Player.getDestroySpeed
func TestBlockBreakSpeedAttributeBreaksDirtOnTheFirstClick(t *testing.T) {
	t.Parallel()
	srv, c := attributeDigger(t, "minecraft:block_break_speed", 100, protocol.ItemStack{})
	expectDugWithinTicks(t, srv, c)
}

// TestMiningEfficiencyAttributeAddsToAToolsSpeed: a wooden shovel on dirt at
// mining_efficiency 100 is (2 + 100) / 0.5 / 30 = 6.8 a tick.
func TestMiningEfficiencyAttributeAddsToAToolsSpeed(t *testing.T) {
	t.Parallel()
	srv, c := attributeDigger(t, "minecraft:mining_efficiency", 100, servertest.ItemStack(t, "minecraft:wooden_shovel", 1))
	expectDugWithinTicks(t, srv, c)
}
