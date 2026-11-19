package fluids

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server"
)

// The observer watching a fluid arrive: vanilla's FlowingFluid.spreadTo and
// FlowingFluid.tick write the moved fluid through Level.setBlock, whose flags
// leave flag 16 out, so the neighbour shape pass runs and an observer watching
// the cell pulses. LiquidBlock.shouldSpreadLiquid writes its cobblestone the
// same way. The lamp on each observer's output proves the two-tick pulse
// carried a signal. vanilla: FlowingFluid.spreadTo, LavaFluid.spreadTo,
// LiquidBlock.shouldSpreadLiquid; Level.setBlock, ObserverBlock.updateShape.

// followFluidObserverPulse ticks physics one game tick at a time, counting how
// many the observer at (ox, oy, oz) stood powered and whether the lamp at
// (lx, ly, lz) behind it was seen lit and later dark.
func followFluidObserverPulse(t *testing.T, srv *server.Server, ox, oy, oz, lx, ly, lz int32, ticks int) (pulseTicks int, lampOn, lampOff bool) {
	t.Helper()
	for range ticks {
		srv.TickPhysics(1)
		if _, powered := servertest.ObserverPowerAt(srv, ox, oy, oz); powered {
			pulseTicks++
		}
		if servertest.LampLitAt(srv, lx, ly, lz) {
			lampOn = true
		} else if lampOn {
			lampOff = true
		}
	}
	return pulseTicks, lampOn, lampOff
}

// TestWaterFlowingIntoTheWatchedCellPulsesTheObserver stands an
// observer[facing=west] at (8,64,3) watching the cell (7,64,3) and puts a
// water source at (6,64,3): the source floods the watched cell on its first
// tick, and the shape pass of the flow write pulses the observer, lighting
// the lamp behind it.
func TestWaterFlowingIntoTheWatchedCellPulsesTheObserver(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	// The lamp sits on the observer's output, east of the face it watches.
	servertest.Setblock(t, c, 9, 64, 3, "redstone_lamp")
	servertest.Setblock(t, c, 8, 64, 3, "observer[facing=west]")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 8, 64, 3); powered {
		t.Fatal("setup: the observer was already pulsing")
	}

	// The source two cells west floods (7,64,3) five ticks after it lands.
	servertest.Setblock(t, c, 6, 64, 3, "water")
	c.Settle()
	pulseTicks, lampOn, lampOff := followFluidObserverPulse(t, srv, 8, 64, 3, 9, 64, 3, 16)

	if name, _ := servertest.StateProps(srv.BlockAt(7, 64, 3)); name != "minecraft:water" {
		t.Fatalf("the watched cell holds %s, want the water that flowed in", name)
	}
	if pulseTicks == 0 {
		t.Error("the water flowed into the watched cell without ever pulsing the observer")
	}
	if !lampOn || !lampOff {
		t.Errorf("the lamp behind the observer lit=%v dark=%v, want it to light and go dark on the pulse", lampOn, lampOff)
	}
}
