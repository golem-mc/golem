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
