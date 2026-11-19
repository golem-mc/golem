package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// paneState returns the block state ID of minecraft:glass_pane with the given
// property overrides and the block's defaults elsewhere.
func paneState(t *testing.T, overrides map[string]string) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:glass_pane", overrides)
	if !ok {
		t.Fatalf("no glass_pane state for %v", overrides)
	}
	return s
}

// copperBarsState returns the block state ID of minecraft:copper_bars with the
// given property overrides and the block's defaults elsewhere.
func copperBarsState(t *testing.T, overrides map[string]string) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:copper_bars", overrides)
	if !ok {
		t.Fatalf("no copper_bars state for %v", overrides)
	}
	return s
}

// TestSetblockConnectsFences checks that a non-strict /setblock of a fence
// between two fences connects all three: the new fence reads both sides and
// the writer reshapes the two outer fences through the neighbour update.
func TestSetblockConnectsFences(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("setblock 5 64 5 oak_fence")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, fenceState(t, nil))
	c.ChatCommand("setblock 7 64 5 oak_fence")
	servertest.AwaitBlockUpdate(t, c, 7, 64, 5, fenceState(t, nil))

	c.ChatCommand("setblock 6 64 5 oak_fence")
	servertest.WaitFor(t, srv, 60, func() bool {
		return srv.BlockAt(6, 64, 5) == fenceState(t, map[string]string{"east": "true", "west": "true"}) &&
			srv.BlockAt(5, 64, 5) == fenceState(t, map[string]string{"east": "true"}) &&
			srv.BlockAt(7, 64, 5) == fenceState(t, map[string]string{"west": "true"})
	})

	if got := srv.BlockAt(5, 64, 5); got != fenceState(t, map[string]string{"east": "true"}) {
		t.Errorf("west fence = %d, want east=true", got)
	}
	if got := srv.BlockAt(6, 64, 5); got != fenceState(t, map[string]string{"east": "true", "west": "true"}) {
		t.Errorf("middle fence = %d, want east and west", got)
	}
	if got := srv.BlockAt(7, 64, 5); got != fenceState(t, map[string]string{"west": "true"}) {
		t.Errorf("east fence = %d, want west=true", got)
	}
}

// TestSetblockConnectsBarsAndPanes checks that a non-strict /setblock of iron
// bars or a glass pane between two of its own kind connects all three, the
// CrossCollisionBlock case the fence test covers for fences.
func TestSetblockConnectsBarsAndPanes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("setblock 5 64 5 iron_bars")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, barsState(t, nil))
	c.ChatCommand("setblock 7 64 5 iron_bars")
	servertest.AwaitBlockUpdate(t, c, 7, 64, 5, barsState(t, nil))
	c.ChatCommand("setblock 6 64 5 iron_bars")
	servertest.WaitFor(t, srv, 60, func() bool {
		return srv.BlockAt(6, 64, 5) == barsState(t, map[string]string{"east": "true", "west": "true"}) &&
			srv.BlockAt(5, 64, 5) == barsState(t, map[string]string{"east": "true"}) &&
			srv.BlockAt(7, 64, 5) == barsState(t, map[string]string{"west": "true"})
	})
	if got := srv.BlockAt(5, 64, 5); got != barsState(t, map[string]string{"east": "true"}) {
		t.Errorf("west bars = %d, want east=true", got)
	}
	if got := srv.BlockAt(7, 64, 5); got != barsState(t, map[string]string{"west": "true"}) {
		t.Errorf("east bars = %d, want west=true", got)
	}

	c.ChatCommand("setblock 5 64 7 glass_pane")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 7, paneState(t, nil))
	c.ChatCommand("setblock 7 64 7 glass_pane")
	servertest.AwaitBlockUpdate(t, c, 7, 64, 7, paneState(t, nil))
	c.ChatCommand("setblock 6 64 7 glass_pane")
	servertest.WaitFor(t, srv, 60, func() bool {
		return srv.BlockAt(6, 64, 7) == paneState(t, map[string]string{"east": "true", "west": "true"})
	})
	if got := srv.BlockAt(6, 64, 7); got != paneState(t, map[string]string{"east": "true", "west": "true"}) {
		t.Errorf("middle pane = %d, want east and west", got)
	}
}

// TestSetblockRemovingAFenceDisconnectsIt checks that a non-strict /setblock
// of air next to a connected fence disconnects it, the break half of the
// shape update.
func TestSetblockRemovingAFenceDisconnectsIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("setblock 5 64 5 oak_fence")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, fenceState(t, nil))
	c.ChatCommand("setblock 6 64 5 oak_fence")
	servertest.WaitFor(t, srv, 60, func() bool {
		return srv.BlockAt(6, 64, 5) == fenceState(t, map[string]string{"west": "true"}) &&
			srv.BlockAt(5, 64, 5) == fenceState(t, map[string]string{"east": "true"})
	})

	c.ChatCommand("setblock 6 64 5 air")
	servertest.WaitFor(t, srv, 60, func() bool {
		return srv.BlockAt(6, 64, 5) == 0 && srv.BlockAt(5, 64, 5) == fenceState(t, nil)
	})

	if got := srv.BlockAt(5, 64, 5); got != fenceState(t, nil) {
		t.Errorf("fence after the neighbour went = %d, want no connections", got)
	}
}

// TestFallingSandConnectsToWall checks that a falling block's landing next to
// a wall reshapes the wall: the landed sand is a sturdy neighbour, so the
// wall takes a low arm toward it.
func TestFallingSandConnectsToWall(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("setblock 5 64 5 cobblestone_wall")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "none",
	}))
	c.ChatCommand("setblock 6 70 5 sand")

	sand := servertest.BlockState(t, "minecraft:sand")
	servertest.WaitForBlock(t, srv, 6, 64, 5, func(s uint16) bool { return s == sand })
	servertest.WaitFor(t, srv, 60, func() bool {
		_, props := servertest.StateProps(srv.BlockAt(5, 64, 5))
		return props["east"] == "low"
	})

	want := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "low", "west": "none",
	})
	if got := srv.BlockAt(5, 64, 5); got != want {
		t.Errorf("wall beside the landed sand = %d, want east=low", got)
	}
}

// TestPistonPushesAFenceToConnect checks that the physics write behind a
// piston's move reshapes a fence it pushes next to another: the moved fence
// reconnects and the standing one does too.
func TestPistonPushesAFenceToConnect(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	// A piston facing east pushes the fence at (4,64,0) into the gap at
	// (5,64,0), next to the fence at (6,64,0).
	c.ChatCommand("setblock 3 64 0 piston[facing=east]")
	c.ChatCommand("setblock 4 64 0 oak_fence")
	c.ChatCommand("setblock 6 64 0 oak_fence")
	c.Settle()
	c.ChatCommand("setblock 3 64 1 redstone_block")
	servertest.AwaitTicks(t, srv, 8)

	servertest.WaitFor(t, srv, 60, func() bool {
		_, props := servertest.StateProps(srv.BlockAt(5, 64, 0))
		return props["east"] == "true"
	})
	servertest.WaitFor(t, srv, 60, func() bool {
		_, props := servertest.StateProps(srv.BlockAt(6, 64, 0))
		return props["west"] == "true"
	})

	// The piston head behind the fence has a full plate toward it, so the
	// fence also connects west.
	if got := srv.BlockAt(5, 64, 0); got != fenceState(t, map[string]string{"east": "true", "west": "true"}) {
		t.Errorf("pushed fence = %d, want east=true west=true", got)
	}
	if got := srv.BlockAt(6, 64, 0); got != fenceState(t, map[string]string{"west": "true"}) {
		t.Errorf("standing fence = %d, want west=true", got)
	}
}

// TestAReshapedCrossKeepsWaterlogged checks that the neighbour update keeps a
// cross block's other properties: a waterlogged fence, iron bars, glass pane
// or copper bars reshaped by a new neighbour must stay waterlogged.
func TestAReshapedCrossKeepsWaterlogged(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("setblock 5 64 5 oak_fence[waterlogged=true]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, fenceState(t, map[string]string{"waterlogged": "true"}))
	c.ChatCommand("setblock 6 64 5 stone")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, fenceState(t, map[string]string{"east": "true", "waterlogged": "true"}))

	c.ChatCommand("setblock 5 64 7 iron_bars[waterlogged=true]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 7, barsState(t, map[string]string{"waterlogged": "true"}))
	c.ChatCommand("setblock 6 64 7 stone")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 7, barsState(t, map[string]string{"east": "true", "waterlogged": "true"}))

	c.ChatCommand("setblock 5 64 9 glass_pane[waterlogged=true]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 9, paneState(t, map[string]string{"waterlogged": "true"}))
	c.ChatCommand("setblock 6 64 9 stone")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 9, paneState(t, map[string]string{"east": "true", "waterlogged": "true"}))

	c.ChatCommand("setblock 5 64 11 copper_bars[waterlogged=true]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 11, copperBarsState(t, map[string]string{"waterlogged": "true"}))
	c.ChatCommand("setblock 6 64 11 stone")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 11, copperBarsState(t, map[string]string{"east": "true", "waterlogged": "true"}))
}

// fenceState returns the block state ID an oak_fence would be in with the given
// connection overrides, falling back to the default state. It is a test helper
// for the expected state after a placement or a neighbour update.
func fenceState(t *testing.T, overrides map[string]string) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:oak_fence", overrides)
	if !ok {
		t.Fatalf("FindState oak_fence %v: not found", overrides)
	}
	return s
}

// barsState returns the block state ID iron_bars would be in with the given
// connection overrides. It is a test helper for the expected state after a
// placement or a neighbour update.
func barsState(t *testing.T, overrides map[string]string) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:iron_bars", overrides)
	if !ok {
		t.Fatalf("FindState iron_bars %v: not found", overrides)
	}
	return s
}

// wallState returns the block state ID cobblestone_wall would be in with the
// given connection overrides. It is a test helper for the expected state after
// a placement or neighbour update.
func wallState(t *testing.T, overrides map[string]string) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:cobblestone_wall", overrides)
	if !ok {
		t.Fatalf("FindState cobblestone_wall %v: not found", overrides)
	}
	return s
}
