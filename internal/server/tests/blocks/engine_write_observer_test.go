package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// watchEngineWrite lays the cells with the physics loop frozen, fails if the
// observer already pulses, runs act, and steps physics until the observer
// pulses.
//
// vanilla: Level.setBlock runs the shape pass for every flag set without 16.
func watchEngineWrite(t *testing.T, lay func(c *servertest.Client), act func(c *servertest.Client), ox, oy, oz int32) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()
	servertest.SeedPhysics(t, srv, cropsSeed)
	lay(c)
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, ox, oy, oz); powered {
		t.Fatal("setup: the observer was already pulsing")
	}
	act(c)
	c.Settle()
	for range 8 {
		srv.TickPhysics(1)
		if _, powered := servertest.ObserverPowerAt(srv, ox, oy, oz); powered {
			return
		}
	}
	t.Error("the engine write never pulsed the observer watching the cell")
}

func TestFallingSandPulsesTheObserverItLeaves(t *testing.T) {
	t.Parallel()
	watchEngineWrite(t, func(c *servertest.Client) {
		servertest.Setblock(t, c, 5, 66, 0, "stone")
		servertest.Setblock(t, c, 5, 67, 0, "sand")
		servertest.Setblock(t, c, 6, 67, 0, "observer[facing=west]")
	}, func(c *servertest.Client) {
		servertest.Setblock(t, c, 5, 66, 0, "air")
	}, 6, 67, 0)
}

// TestABrokenPistonBasePopsItsHeadWithTheBreakEvent checks that the head left
// without its base is destroyed with level event 2001.
//
// vanilla: PistonHeadBlock.updateShape -> Block.updateOrDestroy -> Level.destroyBlock
func TestABrokenPistonBasePopsItsHeadWithTheBreakEvent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	servertest.HoldTicks(t, srv)
	// Keep the extended base powered so setup ticks cannot retract the head.
	servertest.Setblock(t, c, 5, 63, 0, "redstone_block")
	servertest.Setblock(t, c, 5, 64, 0, "piston[extended=true,facing=east]")
	servertest.Setblock(t, c, 6, 64, 0, "piston_head[facing=east,type=normal,short=false]")
	servertest.RunTicks(t, srv, 4)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:piston", map[string]string{"extended": "true", "facing": "east"})
	servertest.ExpectBlockNamed(t, srv, 6, 64, 0, "minecraft:piston_head", map[string]string{"facing": "east", "type": "normal", "short": "false"})
	headState := srv.BlockAt(6, 64, 0)
	c.Settle()
	servertest.Setblock(t, c, 5, 64, 0, "air")
	heard := false
	c.PumpTicks(srv, 4, func(id int32, data []byte) bool {
		if id != protocol.IDPlayWorldEvent {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadI32() != protocol.WorldEventBlockBreak {
			return false
		}
		x, y, z := r.ReadPosition()
		if x == 6 && y == 64 && z == 0 {
			if got := r.ReadI32(); got != int32(headState) {
				t.Errorf("head break event state = %d, want %d", got, headState)
			}
			heard = true
		}
		return heard
	})
	servertest.ExpectAir(t, srv, [3]int32{6, 64, 0})
	if !heard {
		t.Error("the head went without a 2001 event")
	}
}
