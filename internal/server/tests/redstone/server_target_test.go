package redstone

// The target block: the graded redstone power a projectile's hit writes on
// it, held for a span the projectile decides and offered to every face as
// a weak signal a comparator reads.
// vanilla: TargetBlock.onProjectileHit, TargetBlock.getRedstoneStrength,
// TargetBlock.updateRedstoneOutput, TargetBlock.tick, TargetBlock.ownSignal

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server"
)

// targetRests is the target block's quiet state, the one both a hit's hold
// runs out into and vanilla's own placement leaves it in.
//
// The target sits at (6, 65, 8), floating a block over the ground: nothing
// vanilla makes it need a support beneath it, so the shot's flight and the
// faces around it stay unobstructed.
func targetRests(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.SetBlock(6, 65, 8, servertest.MustState(t, "minecraft:target", nil))
	servertest.ExpectBlockNamed(t, srv, 6, 65, 8, "minecraft:target", map[string]string{"power": "0"})
}

// targetPhysicsScene lays an empty target at (6, 65, 8) on a stopped loop, so
// a hit's hold can be counted tick by tick.
func targetPhysicsScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()
	targetRests(t, srv)
	return srv, c
}

// awaitTargetHit drives the stopped loop until the target at (6, 65, 8)
// carries a projectile's power.
func awaitTargetHit(t *testing.T, srv *server.Server) {
	t.Helper()
	for range 40 {
		servertest.DriveTargetTicks(srv, 1)
		if _, props := servertest.StateProps(srv.BlockAt(6, 65, 8)); props["power"] != "0" {
			return
		}
	}
	t.Fatal("the target block never took a projectile's power")
}

// TestArrowsGradeTheTargetByTheLandingPoint checks the mapping vanilla's
// getRedstoneStrength draws: the crossing's position measured off the
// struck face's centre, fifteen wide at the centre, one near the edge and
// eight in between. The shots fly level, so the first tick's line is
// straight and the crossing is where each shot was aimed.
// vanilla: TargetBlock.getRedstoneStrength
func TestArrowsGradeTheTargetByTheLandingPoint(t *testing.T) {
	t.Parallel()
	srv, _ := targetPhysicsScene(t)

	for _, tc := range []struct {
		name       string
		x, y, z    float64
		vx, vy, vz float64
		wantPower  string
	}{
		{
			name:      "the exact centre of the east face",
			x:         7.5,
			y:         65.5,
			z:         8.5,
			vx:        -3,
			wantPower: "15",
		},
		{
			name:      "the exact centre of the top face",
			x:         6.5,
			y:         67.5,
			z:         8.5,
			vy:        -3,
			wantPower: "15",
		},
		{
			name:      "a quarter off the centre of both axes",
			x:         7.5,
			y:         65.75,
			z:         8.75,
			vx:        -3,
			wantPower: "8",
		},
		{
			name:      "near the top edge of the east face",
			x:         7.5,
			y:         65.97,
			z:         8.5,
			vx:        -3,
			wantPower: "1",
		},
	} {
		srv.ShootArrow(tc.x, tc.y, tc.z, tc.vx, tc.vy, tc.vz)
		awaitTargetHit(t, srv)
		servertest.ExpectBlockNamed(t, srv, 6, 65, 8, "minecraft:target",
			map[string]string{"power": tc.wantPower})

		// Wait the hold out before the next shot: vanilla lets a later hit
		// change nothing while its own reset is booked.
		servertest.DriveTargetTicks(srv, 20)
		servertest.ExpectBlockNamed(t, srv, 6, 65, 8, "minecraft:target",
			map[string]string{"power": "0"})
	}
}

// TestAnArrowsTargetSignalHoldsForTwentyTicks pins the hold an arrow's hit
// is kept: still at fifteen nineteen ticks after the landing, nought on the
// twentieth, the exact span ACTIVATION_TICKS_ARROWS books.
// vanilla: TargetBlock.updateRedstoneOutput (ACTIVATION_TICKS_ARROWS)
func TestAnArrowsTargetSignalHoldsForTwentyTicks(t *testing.T) {
	t.Parallel()
	srv, _ := targetPhysicsScene(t)

	srv.ShootArrow(7.5, 65.5, 8.5, -3, 0, 0)
	awaitTargetHit(t, srv)

	servertest.DriveTargetTicks(srv, 19)
	servertest.ExpectBlockNamed(t, srv, 6, 65, 8, "minecraft:target",
		map[string]string{"power": "15"})
	servertest.DriveTargetTicks(srv, 1)
	servertest.ExpectBlockNamed(t, srv, 6, 65, 8, "minecraft:target",
		map[string]string{"power": "0"})
}

// TestAnObserverWatchingATargetPulsesOnTheHitAndTheReset puts an observer
// behind the target's hit face: the hit's flag-3 write and the tick that
// ends the hold both run the shape pass the observer reacts to, so it pulses
// once as the power rises and again as it falls.
// vanilla: TargetBlock.updateRedstoneOutput, TargetBlock.tick, Level.setBlock
func TestAnObserverWatchingATargetPulsesOnTheHitAndTheReset(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 6, 65, 8, "target")
	servertest.Setblock(t, c, 6, 65, 10, "redstone_lamp")
	// The observer last: placing the target first keeps its shape update
	// from booking a pulse on the observer before the scene is ready.
	servertest.Setblock(t, c, 6, 65, 9, "observer[facing=north]")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 6, 65, 9); powered {
		t.Fatal("setup: the observer was already pulsing")
	}

	srv.ShootArrow(7.5, 65.5, 8.5, -3, 0, 0)

	hitPulse, resetPulse, lampOn := false, false, false
	was := false
	for range 60 {
		servertest.DriveTargetTicks(srv, 1)
		_, powered := servertest.ObserverPowerAt(srv, 6, 65, 9)
		if powered && !was {
			if _, props := servertest.StateProps(srv.BlockAt(6, 65, 8)); props["power"] != "0" {
				hitPulse = true
			} else {
				resetPulse = true
			}
		}
		was = powered
		if servertest.LampLitAt(srv, 6, 65, 10) {
			lampOn = true
		}
	}
	if !hitPulse {
		t.Error("the observer never pulsed when the target took the hit's power")
	}
	if !resetPulse {
		t.Error("the observer never pulsed when the hold dropped the power")
	}
	if !lampOn {
		t.Error("the lamp behind the observer never lit")
	}
}
