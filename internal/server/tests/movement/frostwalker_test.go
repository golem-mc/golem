package movement

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Frost Walker on the wire: the boots' location_changed effect freezes the
// source water under a body that crosses a cell, lands a fall, or has just
// put the boots on, into frosted ice of age zero — the vanilla disk one
// block below the feet, radius three at the first level and four at the
// second, taking only the source water with air above it and no cell a
// body's box reaches — while an airborne step, a spectator and bare feet
// run nothing. On the tick engine's own schedule the ice ages and melts
// back to water, and the boots hold the sear of the magma block underfoot
// off their wearer besides.

// frostWalkerPondServer starts a server holding a pond of still water set
// into the flat world's surface at y=63, the cells x=0..12, z=76..88, with
// glass floated on one cell to hold the water beneath it back, and joins
// name to it with boots in the feet slot. The two chunks of the pond are
// held so the water lands in loaded cells, the loop is stopped so the water
// sits still while the test walks, and the connect closure comes back so a
// test can add a second body. Any setup the caller passes runs at server
// start, for the tests that need an operator.
func frostWalkerPondServer(t *testing.T, name string, boots protocol.ItemStack, setup ...func(*server.Server)) (*server.Server, func() *servertest.Client, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.EnvironmentConfig(), setup...)
	servertest.StopTicking(t, srv)
	t.Cleanup(srv.HoldChunk(0, 76))
	t.Cleanup(srv.HoldChunk(0, 80))
	c := connect()
	// The pond's far chunk lies five chunks out: asked for a view of six, the
	// join sends it, so a freeze on the pond reaches the client as a Block
	// Update rather than in the chunk itself.
	c.ViewAsk = 6
	c.JoinAs(name)
	srv.GiveSlot(name, player.ArmorSlot+3, boots)
	water := servertest.BlockState(t, "minecraft:water")
	for x := int32(0); x <= 12; x++ {
		for z := int32(76); z <= 88; z++ {
			if !srv.SetBlock(x, 63, z, water) {
				t.Fatal("the pond's chunk was not loaded")
			}
		}
	}
	if !srv.SetBlock(2, 64, 79, servertest.BlockState(t, "minecraft:glass")) {
		t.Fatal("the pond's chunk was not loaded")
	}
	return srv, connect, c
}

// frostWalkerPond is frostWalkerPondServer for the single-body tests.
func frostWalkerPond(t *testing.T, name string, boots protocol.ItemStack, setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, _, c := frostWalkerPondServer(t, name, boots, setup...)
	return srv, c
}

// frostWalkerDisk names the cells the vanilla replace_disk takes around a
// step onto (x, z) with the feet at feetY: every cell of the radius's
// square whose centre lies strictly within the radius of the walker's own
// position, on the level a block below the feet — the formula of the data
// pack's frost_walker.json, not the server's.
func frostWalkerDisk(x, feetY, z float64, radius int) map[[3]int32]bool {
	cells := map[[3]int32]bool{}
	y := int32(math.Floor(feetY)) - 1
	for cx := int32(math.Floor(x)) - int32(radius); cx <= int32(math.Floor(x))+int32(radius); cx++ {
		for cz := int32(math.Floor(z)) - int32(radius); cz <= int32(math.Floor(z))+int32(radius); cz++ {
			dx, dz := x-float64(cx)-0.5, z-float64(cz)-0.5
			if dx*dx+dz*dz < float64(radius*radius) {
				cells[[3]int32{cx, y, cz}] = true
			}
		}
	}
	return cells
}

// blockUpdatesAfter returns the block updates that reached the client in
// the settle's window, as each cell's list of the states its updates
// carried.
func blockUpdatesAfter(t *testing.T, c *servertest.Client) map[[3]int32][]uint16 {
	t.Helper()
	updates := map[[3]int32][]uint16{}
	c.SettleAndCollect(func(id int32, data []byte) {
		for _, ch := range servertest.BlockChangesIn(t, id, data) {
			cell := [3]int32{ch.X, ch.Y, ch.Z}
			updates[cell] = append(updates[cell], ch.State)
		}
	})
	return updates
}

// checkTheFrozenSetsAreEqual holds one disk's worth of freeze against the
// vanilla formula: every cell the formula names is there, is the frosted
// ice of age zero on the wire and in the world, and nothing else changed.
func checkTheFrozenSetsAreEqual(t *testing.T, srv *server.Server, updates map[[3]int32][]uint16, want map[[3]int32]bool) {
	t.Helper()
	frozen := servertest.BlockStateWith(t, "minecraft:frosted_ice", "age=0")
	seen := map[[3]int32]bool{}
	for cell, states := range updates {
		if len(states) != 1 || states[0] != frozen {
			t.Errorf("cell %v was updated to %v, want one update to the frosted ice of age zero", cell, states)
		}
		seen[cell] = true
		if !want[cell] {
			t.Errorf("cell %v changed, want it left alone", cell)
		}
	}
	for cell := range want {
		if seen[cell] {
			continue
		}
		t.Errorf("cell %v saw no block update, want it frozen", cell)
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != frozen {
			t.Errorf("cell %v holds state %d in the world, want the frosted ice %d", cell, got, frozen)
		}
	}
}

// TestFrostWalkerFreezesTheWaterAroundAStep checks the disk of a Frost
// Walker II walk: the step out onto the pond freezes every cell the
// vanilla formula names — four blocks around the walker's own position,
// strictly, the one under the feet among them, but not the water a block
// short of the radius nor the one the glass roofs — and the step onto new
// ice ahead grows the bridge the same way.
func TestFrostWalkerFreezesTheWaterAroundAStep(t *testing.T) {
	t.Parallel()
	srv, c := frostWalkerPond(t, "Iceman", servertest.EnchantedStack(t, "minecraft:diamond_boots", "minecraft:frost_walker", 2))
	water := servertest.BlockState(t, "minecraft:water")

	// The step out onto the water.
	c.MoveToGround(4.5, 64, 80.5, true)
	disk := frostWalkerDisk(4.5, 64, 80.5, 4)
	// The glass on (2, 79) is no air above the water beneath it; the
	// disk's predicate leaves the cell alone.
	delete(disk, [3]int32{2, 63, 79})
	checkTheFrozenSetsAreEqual(t, srv, blockUpdatesAfter(t, c), disk)

	// The water the disk does not reach stays water: the cell a block
	// short of the radius's edge, one beyond it, and the one under the
	// glass.
	for _, cell := range [][3]int32{{0, 63, 80}, {9, 63, 80}, {2, 63, 79}} {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != water {
			t.Errorf("cell %v holds state %d, want the water left alone", cell, got)
		}
	}

	// A step east writes the bridge ahead: the cells the second disk
	// adds to the first, and nothing already frozen again.
	c.MoveToGround(6.5, 64, 80.5, true)
	second := frostWalkerDisk(6.5, 64, 80.5, 4)
	delete(second, [3]int32{2, 63, 79})
	for cell := range disk {
		delete(second, cell)
	}
	checkTheFrozenSetsAreEqual(t, srv, blockUpdatesAfter(t, c), second)
}

// TestTheFrozenIceOfAFrostWalkerStepMeltsOnSchedule checks the aging half
// the walk leaves behind: the ice a step froze over the pond runs the
// schedule the placement booked and melts back to plain water, on a bound
// well past the worst its rolls spend.
func TestTheFrozenIceOfAFrostWalkerStepMeltsOnSchedule(t *testing.T) {
	t.Parallel()
	srv, c := frostWalkerPond(t, "Thawer", servertest.EnchantedStack(t, "minecraft:diamond_boots", "minecraft:frost_walker", 2))
	frozen := servertest.BlockStateWith(t, "minecraft:frosted_ice", "age=0")

	c.MoveToGround(4.5, 64, 80.5, true)
	blockUpdatesAfter(t, c)
	if got := srv.BlockAt(4, 63, 80); got != frozen {
		t.Fatalf("the step left state %d under the feet, want the frosted ice of age zero", got)
	}
	// The light the ice reads is published by an asynchronous relight of the
	// pond; without waiting for it a loaded machine runs the whole bound
	// against stale light and the ice never ages.
	srv.WaitRelights()
	melted := false
	for range 1000 {
		srv.RunTick()
		name, _, _ := registry.BlockStateName(srv.BlockAt(4, 63, 80))
		if name == "minecraft:water" {
			melted = true
			break
		}
	}
	if !melted {
		t.Fatal("the frosted ice under the feet had not melted back to water within a thousand ticks")
	}
}

// TestUnenchantedBootsLeaveTheWaterAlone checks the gate from the other
// side: plain diamond boots walk the same pond without freezing anything,
// the wire staying quiet and the water staying water.
func TestUnenchantedBootsLeaveTheWaterAlone(t *testing.T) {
	t.Parallel()
	srv, c := frostWalkerPond(t, "Barefoot", servertest.ItemStack(t, "minecraft:diamond_boots", 1))
	water := servertest.BlockState(t, "minecraft:water")

	for _, step := range [][2]float64{{4.5, 80.5}, {6.5, 80.5}} {
		c.MoveToGround(step[0], 64, step[1], true)
		if updates := blockUpdatesAfter(t, c); len(updates) != 0 {
			t.Errorf("%d block updates arrived under unenchanted boots, want none", len(updates))
		}
	}
	for _, cell := range [][3]int32{{4, 63, 80}, {6, 63, 80}, {9, 63, 80}} {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != water {
			t.Errorf("cell %v holds state %d under unenchanted boots, want the water", cell, got)
		}
	}
}

// TestFrostWalkerBootsHoldTheHotFloorOff checks the enchantment's damage
// immunity: the sear of the magma block underfoot never lands through
// frost walker boots, and comes back the moment the boots come off.
func TestFrostWalkerBootsHoldTheHotFloorOff(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Soles")
	// A bar below the regeneration line, so any heal between the bites
	// would not blur them.
	srv.SetFood("Soles", 17)
	srv.GiveSlot("Soles", player.ArmorSlot+3, servertest.EnchantedStack(t, "minecraft:diamond_boots", "minecraft:frost_walker", 2))
	if !srv.SetBlock(20, 63, 50, servertest.BlockState(t, "minecraft:magma_block")) {
		t.Fatal("the magma's chunk was not loaded")
	}
	c.MoveToGround(20.5, 64, 50.5, true)
	c.Settle()

	tick(20)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived through frost walker boots, want the sear held off", n)
	}
	if got := srv.PlayerHealth("Soles"); got != 20 {
		t.Errorf("a body in frost walker boots stood down to %v health, want the whole twenty", got)
	}

	// Bare feet on the same stone: the first tick's sear lands, the
	// invulnerability window holding the rest back.
	srv.GiveSlot("Soles", player.ArmorSlot+3, protocol.ItemStack{})
	c.Settle()
	tick(1)
	if got := c.ExpectHealth(); got.Health != 19 {
		t.Errorf("after a tick on the magma barefoot: health=%v, want 19", got.Health)
	}
	c.ExpectDamageEvent(entityID, "minecraft:hot_floor", 0)
}

// seenFrozen reports whether the cell is among the updates and was frozen
// to the frosted ice of age zero.
func seenFrozen(t *testing.T, updates map[[3]int32][]uint16, cell [3]int32) bool {
	t.Helper()
	states, ok := updates[cell]
	return ok && len(states) == 1 && states[0] == servertest.BlockStateWith(t, "minecraft:frosted_ice", "age=0")
}
