package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server"
)

// The observer a piston moves: a pushed or pulled observer fires one pulse at
// the position it lands on, lighting the dust behind its output for the
// observer's two game ticks. The landing books the pulse itself — the
// Block.updateFromNeighbourShapes the moving piston runs on the block it sets
// down calls the observer's own updateShape — so the face the observer
// watches never has to change.

// followMovedObserverPulse ticks until the moved observer lands at
// (observerX, 64, observerZ) and then follows it, reporting how many game
// ticks its powered state is high and whether the lamp at (lampX, 64, lampZ)
// was seen lit and later dark.
func followMovedObserverPulse(t *testing.T, srv *server.Server, observerX, observerZ, lampX, lampZ int32) (pulseTicks int, sawPulse, lampOn, lampOff bool) {
	t.Helper()
	for tick := 1; tick <= 24; tick++ {
		srv.TickPhysics(1)
		landed, powered := servertest.ObserverPowerAt(srv, observerX, 64, observerZ)
		if !landed {
			continue
		}
		sawPulse = true
		if powered {
			pulseTicks++
		}
		if servertest.LampLitAt(srv, lampX, 64, lampZ) {
			lampOn = true
		} else if lampOn {
			lampOff = true
		}
	}
	return pulseTicks, sawPulse, lampOn, lampOff
}

// TestPushedObserverPulsesAtItsNewPosition pushes an observer one cell east
// with a piston and watches the pulse at the position it lands on: the
// observer runs powered for two game ticks and the dust behind its output
// lights and takes the lamp with it. The observer watches north, so the face
// it sees never changes during the move.
func TestPushedObserverPulsesAtItsNewPosition(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	// The piston pushing east and the observer in front of it; the dust and
	// lamp sit behind the observer's output at the cell the push lands it
	// on: the observer's new cell is 7, its output south one further.
	servertest.Setblock(t, c, 5, 64, 5, "piston[facing=east]")
	servertest.Setblock(t, c, 6, 64, 5, "observer[facing=north]")
	servertest.Setblock(t, c, 7, 64, 6, "redstone_wire[north=side]")
	servertest.Setblock(t, c, 7, 64, 7, "redstone_lamp")
	// The power beside the piston queues the extend for the next tick.
	servertest.Setblock(t, c, 5, 64, 6, "redstone_block")

	pulseTicks, sawPulse, lampOn, lampOff := followMovedObserverPulse(t, srv, 7, 5, 7, 7)
	if !sawPulse {
		t.Fatal("the pushed observer never reached its new position")
	}
	if pulseTicks != 2 {
		t.Errorf("the pushed observer was powered for %d ticks, want the two-tick pulse", pulseTicks)
	}
	if !lampOn || !lampOff {
		t.Errorf("the lamp behind the output lit=%v dark=%v, want it to light and go dark again", lampOn, lampOff)
	}
}

// TestPulledObserverPulsesAtItsNewPosition pulls an observer one cell back
// with a sticky piston and watches the same pulse: an extended sticky piston
// left unpowered retracts, carrying the observer back onto the dust behind
// its output. The observer watches north again, so nothing it watches moves.
func TestPulledObserverPulsesAtItsNewPosition(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	// The sticky piston extended with its head out, the observer on the
	// head's far side, and the dust and lamp behind the observer's output
	// at the cell the pull brings it home to: the observer's new cell is 6,
	// its output south one further. Placing the extended piston unpowered
	// queues its own retraction, which is the pull under test.
	servertest.Setblock(t, c, 5, 64, 5, "sticky_piston[facing=east,extended=true]")
	servertest.Setblock(t, c, 6, 64, 5, "piston_head[facing=east,type=sticky]")
	servertest.Setblock(t, c, 7, 64, 5, "observer[facing=north]")
	servertest.Setblock(t, c, 6, 64, 6, "redstone_wire[north=side]")
	servertest.Setblock(t, c, 6, 64, 7, "redstone_lamp")

	pulseTicks, sawPulse, lampOn, lampOff := followMovedObserverPulse(t, srv, 6, 5, 6, 7)
	if !sawPulse {
		t.Fatal("the pulled observer never reached its new position")
	}
	if pulseTicks != 2 {
		t.Errorf("the pulled observer was powered for %d ticks, want the two-tick pulse", pulseTicks)
	}
	if !lampOn || !lampOff {
		t.Errorf("the lamp behind the output lit=%v dark=%v, want it to light and go dark again", lampOn, lampOff)
	}
}

// TestPushedPoweredObserverEndsUnpowered pushes an observer already part way
// through a pulse: the move captures it powered, and the landing has to end
// the pulse rather than leave the observer lit forever at its new position.
func TestPushedPoweredObserverEndsUnpowered(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 5, 64, 5, "piston[facing=east]")
	servertest.Setblock(t, c, 6, 64, 5, "observer[facing=north]")
	// A block in the face the observer watches books its pulse, so two
	// ticks later the observer is lit and the move catches it that way.
	servertest.Setblock(t, c, 6, 64, 4, "stone")
	srv.TickPhysics(2)
	if _, powered := servertest.ObserverPowerAt(srv, 6, 64, 5); !powered {
		t.Fatal("setup: the observer never lit")
	}
	// The power beside the piston runs the extend with the observer lit.
	servertest.Setblock(t, c, 5, 64, 6, "redstone_block")

	for tick := 1; tick <= 12; tick++ {
		srv.TickPhysics(1)
	}
	landed, powered := servertest.ObserverPowerAt(srv, 7, 64, 5)
	if !landed {
		t.Fatal("the powered observer never reached its new position")
	}
	if powered {
		t.Error("the observer was still powered after the move, want it dark")
	}
}
