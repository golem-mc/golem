package blocks

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// setupTickPair builds a water source and a lit furnace with a smeltable item
// on a stone floor at chunk column cx.
func setupTickPair(srv *server.Server, cx int32) (water, furnace [3]int32) {
	x := cx*16 + 4
	srv.RunLine(fmt.Sprintf("fill %d 63 0 %d 63 12 stone", x-2, x+8))
	srv.RunLine(fmt.Sprintf("fill %d 64 0 %d 66 12 air", x-2, x+8))
	srv.RunLine(fmt.Sprintf("setblock %d 64 4 water", x))
	srv.RunLine(fmt.Sprintf(`setblock %d 64 10 minecraft:furnace[lit=true]{Items:[{Slot:0b,id:"minecraft:raw_iron",count:1},{Slot:1b,id:"minecraft:coal",count:1}],lit_time_remaining:1000s,lit_total_time:1600s,speed_multiplier:1.0f,cooking_total_time:200s}`, x))
	return [3]int32{x, 64, 4}, [3]int32{x, 64, 10}
}

func cookProgress(t *testing.T, srv *server.Server, p [3]int32) int32 {
	t.Helper()
	spent, _, _, _, ok := srv.FurnaceAt(p[0], p[1], p[2])
	if !ok {
		t.Fatalf("no furnace at %v", p)
	}
	return spent
}

// Scheduled fluid ticks and block entities hold still outside the simulation
// distance plus one chunk, and run again once a player comes near.
// vanilla: LevelTicks.tick, Level.tickBlockEntities, ServerLevel.shouldTickBlocksAt
func TestBlocksBeyondTheSimulationDistanceStandStill(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = 10, 4
	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.ViewAsk = 10
	c.Join("Watcher")
	c.Settle()
	servertest.HoldTicks(t, srv)

	farWater, farFurnace := setupTickPair(srv, 7)
	nearWater, nearFurnace := setupTickPair(srv, 2)
	servertest.RunTicks(t, srv, 40)

	if srv.BlockAt(farWater[0]+1, 64, 4) != 0 {
		t.Errorf("far water spread")
	}
	if got := cookProgress(t, srv, farFurnace); got != 0 {
		t.Errorf("far furnace progress = %d, want 0", got)
	}
	if srv.BlockAt(nearWater[0]+1, 64, 4) == 0 {
		t.Errorf("near water did not spread")
	}
	if got := cookProgress(t, srv, nearFurnace); got <= 0 {
		t.Errorf("near furnace progress = %d, want above 0", got)
	}

	c.MoveToGround(float64(farWater[0])+0.5, 64, 0.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 10)
	if srv.BlockAt(farWater[0]+1, 64, 4) == 0 {
		t.Errorf("far water did not spread once a player came near")
	}

	// A forced chunk and its neighbours tick with no player near.
	c.MoveToGround(8.5, 64, 0.5, true)
	c.Settle()
	forcedWater, forcedFurnace := setupTickPair(srv, -9)
	srv.RunLine(fmt.Sprintf("forceload add %d 0", forcedWater[0]))
	servertest.RunTicks(t, srv, 40)
	if srv.BlockAt(forcedWater[0]+1, 64, 4) == 0 {
		t.Errorf("forced water did not spread")
	}
	if got := cookProgress(t, srv, forcedFurnace); got <= 0 {
		t.Errorf("forced furnace progress = %d, want above 0", got)
	}
}
