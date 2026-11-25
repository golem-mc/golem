package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestTwoOpeningShellsShoveAMobBetweenThemInOrder: the second shell finds the
// mob where the first one left it.
// vanilla: Shulker.onPeekAmountChange
func TestTwoOpeningShellsShoveAMobBetweenThemInOrder(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	// One floor block under the pig only: a floor under a shell would be a
	// reattachment fallback, which NoAI shells lack.
	srv.RunLine(`setblock 1 99 0 stone`)
	srv.RunLine(`setblock -1 100 0 stone`)
	srv.RunLine(`setblock 3 100 0 stone`)
	srv.RunLine(`summon shulker 0.5 100 0.5 {AttachFace:4b,Peek:100b,NoAI:1b}`)
	srv.RunLine(`summon shulker 2.5 100 0.5 {AttachFace:5b,Peek:100b,NoAI:1b}`)
	srv.RunLine(`summon pig 1.46 100 0.5 {NoAI:1b}`)
	pig := srv.MobIDsOfKind("minecraft:pig")[0]
	servertest.RunTicks(t, srv, 3)
	x, _, _, _ := srv.MobPos(pig)
	// The first shell pushes the pig into the second one's slab; refiling the
	// pig at its new box gives 1.5044924, the stale entry 1.5083395.
	if math.Abs(x-1.5044924) > 1e-6 {
		t.Errorf("pig x = %.7f after three ticks, want 1.5044924", x)
	}
}

// TestAMountedShulkerBlocksAStationaryShellsFace: a rider carried across a
// push-grid cell boundary still counts as inside the neighbour's hull.
// vanilla: Shulker.canStayAt, ServerLevel.tickPassenger
func TestAMountedShulkerBlocksAStationaryShellsFace(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`fill -1 99 0 3 99 0 stone`)
	srv.RunLine(`setblock 4 100 0 stone`)
	srv.RunLine(`summon pig -0.3 100 0.5 {Tags:[carrier],Motion:[0.0,0.0,0.0]}`)
	srv.RunLine(`summon shulker -0.3 100 0.5 {Tags:[rider],AttachFace:0b,NoAI:1b}`)
	// The shell is spawned last so it ticks after the pig has carried its rider.
	srv.RunLine(`summon shulker 3.5 100 0.5 {Tags:[still],AttachFace:5b,Peek:100b}`)
	still := srv.MobIDsOfKind("minecraft:shulker")[1]
	srv.RunLine(`ride @e[tag=rider,limit=1] mount @e[tag=carrier,limit=1]`)
	srv.RunLine(`data merge entity @e[tag=carrier,limit=1] {Motion:[2.65d,0.0d,0.0d]}`)
	servertest.RunTicks(t, srv, 1)
	// The rider fills the east face's opening, so the shell takes the floor
	// where it stands rather than teleporting.
	face, _, _ := srv.ShulkerState(still)
	x, y, z, _ := srv.MobPos(still)
	if face != 0 || x != 3.5 || y != 100 || z != 0.5 {
		t.Errorf("stationary shell has face %d at (%v, %v, %v), want face 0 at (3.5, 100, 0.5)", face, x, y, z)
	}
}

// TestAShulkerUnderALilyPadTakesTheStoneBeside: a lily pad's underside is not
// a full face, so a shell with air beneath it attaches to the stone beside it.
// vanilla: Shulker.findAttachableSurface, Block.isFaceFull
func TestAShulkerUnderALilyPadTakesTheStoneBeside(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`setblock 1 100 0 stone`)
	srv.RunLine(`setblock 0 101 0 lily_pad`)
	srv.RunLine(`summon shulker 0.5 100 0.5`)
	shell := srv.MobIDsOfKind("minecraft:shulker")[0]
	servertest.RunTicks(t, srv, 1)
	if face, _, _ := srv.ShulkerState(shell); face != 5 {
		t.Errorf("shell attach face = %d after one tick, want 5 (east, the stone)", face)
	}
}

// TestAnOpeningShellDoesNotShoveItsOwnVehicle: a shell riding a boat, a
// minecart or a mob leaves that carrier in place while the free ravager in its
// swept slab moves. The shell is scaled to 0.75 so the slab lies inside the
// carrier's box, and mounting resets it to face down, so the test turns it
// west and it opens east. Over a cart the ravager stands on the cart's box,
// which keeps ordinary cart collisions out of the measurement.
// vanilla: Shulker.onPeekAmountChange, Entity.isPassengerOfSameVehicle
func TestAnOpeningShellDoesNotShoveItsOwnVehicle(t *testing.T) {
	t.Parallel()
	for _, carrier := range []string{"boat", "minecart", "pig"} {
		t.Run(carrier, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			srv.RunLine(`fill -2 99 -2 4 99 2 stone`)
			var cart int32
			switch carrier {
			case "boat":
				srv.RunLine(`summon oak_boat 0.5 100 0.5 {NoGravity:1b}`)
			case "minecart":
				cart = srv.SpawnMinecartOfKind("minecraft:minecart", 0.5, 100, 0.5)
			default:
				srv.RunLine(`summon pig 0.5 100 0.5 {NoAI:1b}`)
			}
			srv.RunLine(`summon shulker 0.5 100 0.5 {NoAI:1b,Tags:[shell],attributes:[{id:"minecraft:scale",base:0.75d}]}`)
			srv.RunLine(`ride @e[tag=shell,limit=1] mount @e[type=` + carrier + `,limit=1]`)
			servertest.RunTicks(t, srv, 1)
			shell := srv.MobIDsOfKind("minecraft:shulker")[0]
			srv.RunLine(`data merge entity @e[tag=shell,limit=1] {AttachFace:4b}`)
			sx, sy, sz, _ := srv.MobPos(shell)
			carrierX := func() float64 {
				switch carrier {
				case "boat":
					x, _, _, _, _, _ := srv.BoatState(srv.BoatIDs()[0])
					return x
				case "minecart":
					x, _, _, _, _ := srv.MinecartState(cart)
					return x
				}
				x, _, _, _ := srv.MobPos(srv.MobIDsOfKind("minecraft:pig")[0])
				return x
			}
			// A minecart's box is 0.7 tall; the shell's slab is 0.75 tall.
			fy := sy
			if carrier == "minecart" {
				fy = 100.72
			}
			srv.RunLine(fmt.Sprintf(`summon ravager %g %g %g {NoAI:1b,NoGravity:1b}`, sx+1.2, fy, sz))
			free := srv.MobIDsOfKind("minecraft:ravager")[0]
			beforeCarrier := carrierX()
			beforeFree, _, _, _ := srv.MobPos(free)
			srv.RunLine(`data merge entity @e[tag=shell,limit=1] {Peek:100b}`)
			servertest.RunTicks(t, srv, 1)
			if got := carrierX(); got != beforeCarrier {
				t.Errorf("%s x = %v after the shell opened, want %v", carrier, got, beforeCarrier)
			}
			if got, _, _, _ := srv.MobPos(free); got <= beforeFree {
				t.Errorf("free ravager x = %v after the shell opened, want above %v (shell y %v)", got, beforeFree, sy)
			}
		})
	}
}
