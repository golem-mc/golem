package redstone

// The target block: the graded redstone power a projectile's hit writes on
// it, held for a span the projectile decides and offered to every face as
// a weak signal a comparator reads.
// vanilla: TargetBlock.onProjectileHit, TargetBlock.getRedstoneStrength,
// TargetBlock.updateRedstoneOutput, TargetBlock.tick, TargetBlock.ownSignal

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
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

// awaitTargetPowered drains the wire while polling the target block until
// it carries a power, and returns the property. The wait is counted in the
// server's ticks, the span a hit and its hold run on.
func awaitTargetPowered(t *testing.T, srv *server.Server, c *servertest.Client) string {
	t.Helper()
	var power string
	seen := c.PumpTicks(srv, 40, func(int32, []byte) bool {
		name, props := servertest.StateProps(srv.BlockAt(6, 65, 8))
		if name == "minecraft:target" && props["power"] != "0" {
			power = props["power"]
			return true
		}
		return false
	})
	if !seen {
		t.Fatal("the target block never took a projectile's power")
	}
	return power
}

// awaitTargetQuiet drains the wire while polling the target block until it
// holds no power, the hold's own span the wait rides out in ticks so a
// loaded machine is given the hold rather than a slice of wall time.
func awaitTargetQuiet(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	quiet := c.PumpTicks(srv, 40, func(int32, []byte) bool {
		name, props := servertest.StateProps(srv.BlockAt(6, 65, 8))
		return name == "minecraft:target" && props["power"] == "0"
	})
	if !quiet {
		t.Fatal("the target block's power never ran out")
	}
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

// reachableFrom walks a client to (x, y, z) facing the aim: a shot or a
// throw rides the last move the client reported, and the walk a test must
// make from spawn crosses several blocks in one step, which MoveTo follows
// with the tick ends of a teleported client, so nothing is left to ride.
func reachableFrom(c *servertest.Client, x, y, z float64, yaw, pitch float32) {
	c.T.Helper()
	c.MoveTo(x, y, z, yaw, pitch)
}

// TestArrowsGradeTheTargetByTheLandingPoint checks the mapping vanilla's
// getRedstoneStrength draws: the crossing's position measured off the
// struck face's centre, fifteen wide at the centre, one near the edge and
// eight in between. The shots fly level, so the first tick's line is
// straight and the crossing is where each shot was aimed.
// vanilla: TargetBlock.getRedstoneStrength
func TestArrowsGradeTheTargetByTheLandingPoint(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Idle")

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
		targetRests(t, srv)
		srv.ShootArrow(tc.x, tc.y, tc.z, tc.vx, tc.vy, tc.vz)
		power := awaitTargetPowered(t, srv, c)
		if power != tc.wantPower {
			t.Errorf("a shot at %s gave power %s, want %s", tc.name, power, tc.wantPower)
		}
		servertest.ExpectBlockNamed(t, srv, 6, 65, 8, "minecraft:target",
			map[string]string{"power": tc.wantPower})

		// Wait the hold out before the next shot, so it does not land in
		// a hold the first one still keeps: vanilla lets a later hit
		// change nothing while its own reset is booked.
		awaitTargetQuiet(t, srv, c)
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

// TestASecondTargetHitInsideTheHoldChangesNothing fires a second, off-centre
// arrow while the first hit's hold is still booked: vanilla leaves the power
// and the visit that drops it alone, so the power stays fifteen and falls on
// the first hit's own twentieth tick rather than the second's.
// vanilla: TargetBlock.updateRedstoneOutput
func TestASecondTargetHitInsideTheHoldChangesNothing(t *testing.T) {
	t.Parallel()
	srv, _ := targetPhysicsScene(t)

	srv.ShootArrow(7.5, 65.5, 8.5, -3, 0, 0)
	awaitTargetHit(t, srv)

	// A shot near the face's top edge would grade one on its own; the
	// first hit's fifteen must stand instead.
	srv.ShootArrow(7.5, 65.97, 8.5, -3, 0, 0)
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

// TestASnowballsTargetSignalHoldsForEightTicks pins the shorter hold every
// other projectile's hit is kept — a snowball thrown straight at the face
// centre is gone within a dozen ticks where an arrow's lasts twenty. The
// throw's own landing is close enough to the centre for its power to be a
// sure fifteen.
// vanilla: TargetBlock.updateRedstoneOutput (ACTIVATION_TICKS_OTHER)
func TestASnowballsTargetSignalHoldsForEightTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:snowball"), Count: 1})
	targetRests(t, srv)

	// The thrower half a block east of the face centre, level: the throw
	// hangs a tenth under the eye and sags its first tick's gravity, which
	// between them land the crossing a hundredth off the centre.
	reachableFrom(c, 7.5, 64, 8.5, 90, 0)
	c.Settle()
	// The loop is driven by hand from the throw on: a loaded machine's live
	// loop can run past the hold before a count of its ticks begins.
	srv.StopTick()
	servertest.UseHeld(c, 90, 0)
	c.Settle() // the throw is out; the landing is next tick's flight

	// Three ticks in the power is still full fifteen — the throw landed a
	// hair off the centre — and it is nought by sixteen, a hold half an
	// arrow's.
	servertest.DriveTargetTicks(srv, 3)
	servertest.ExpectBlockNamed(t, srv, 6, 65, 8, "minecraft:target",
		map[string]string{"power": "15"})
	servertest.DriveTargetTicks(srv, 13)
	servertest.ExpectBlockNamed(t, srv, 6, 65, 8, "minecraft:target",
		map[string]string{"power": "0"})
}

// TestAComparatorReadsTheTargetSignal runs the block's power the whole way
// a player's shot takes it: a real bow's arrow hits the face centre, the
// comparator behind the target reads the fifteen, and the wire beyond the
// comparator carries it out — until the hold runs out and the same line
// goes dark.
// vanilla: TargetBlock.ownSignal, ComparatorBlock.getInputSignal
func TestAComparatorReadsTheTargetSignal(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Archer", servertest.OpsListing(t, "Archer"))
	servertest.GiveBowAndArrows(srv, "Archer")

	targetRests(t, srv)
	stone, _ := registry.FindState("minecraft:stone", nil)
	srv.SetBlock(6, 64, 7, stone)
	srv.SetBlock(6, 64, 6, stone)
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	srv.SetBlock(6, 65, 6, wire)
	servertest.Setblock(t, c, 6, 65, 7, "minecraft:comparator[facing=south,mode=compare,powered=false]")
	c.Settle()

	// A full-draw arrow from half a block away, aimed for the centre: the
	// look points a tenth over the crossing, the amount the arrow's spawn
	// hangs below the eye, and the shot's own inaccuracy moves the
	// crossing no further than a fiftieth of a block, far inside the
	// fifteen's margin.
	hang := float64(entity.ArrowSpawnBelow)
	above := 65.62 - (65.5 + hang) // how far the eye sits over the look
	pitch := float32(math.Atan2(above, 0.5) * 180 / math.Pi)
	reachableFrom(c, 7.5, 64, 8.5, 90, pitch)
	servertest.DrawBow(c, 90, pitch)
	servertest.HoldDraw(t, srv, "Archer", 24)
	servertest.ReleaseBow(c)
	c.Settle()

	// The arrow's flight, the hit and the comparator's read of the signal
	// all ride the server's own ticking, so the wire's rise is waited out
	// in ticks from the hit: a wall-clock window can run out before a
	// loaded machine has ticked them through.
	servertest.WaitForTicks(t, srv, 40, func() bool { return servertest.WirePowerAt(srv, 6, 65, 6) == "15" },
		"the wire beyond the comparator never carried the target's power")

	awaitTargetQuiet(t, srv, c)
	servertest.WaitForTicks(t, srv, 40, func() bool { return servertest.WirePowerAt(srv, 6, 65, 6) == "0" },
		"the wire beyond the comparator stayed live past the hold")
}

// TestAFishingBobbersTargetSignalHoldsForEightTicks casts a bobber at the
// target's face: it reads powered within a few ticks and the hold, not an
// arrow's, ends eight ticks after.
// vanilla: Projectile.onHitBlock, TargetBlock.onProjectileHit
func TestAFishingBobbersTargetSignalHoldsForEightTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Angler")
	srv.GiveSlot("Angler", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	targetRests(t, srv)

	reachableFrom(c, 7.5, 64, 8.5, 90, 0)
	c.Settle()
	srv.StopTick()
	servertest.UseHeld(c, 90, 0)
	c.Settle()

	powered := func() bool {
		_, props := servertest.StateProps(srv.BlockAt(6, 65, 8))
		return props["power"] != "0"
	}
	first := -1
	for i := 1; i <= 5; i++ {
		servertest.DriveTargetTicks(srv, 1)
		if powered() {
			first = i
			break
		}
	}
	if first < 0 {
		t.Fatal("the target was not powered within 5 driven ticks of the cast")
	}
	servertest.DriveTargetTicks(srv, 9)
	servertest.ExpectBlockNamed(t, srv, 6, 65, 8, "minecraft:target",
		map[string]string{"power": "0"})
}
