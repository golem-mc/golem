package world

import (
	"testing"
)

// The piston engine's black-box checks: a piston on the flat ground, its
// power read through the redstone block the tests place beside it, the moves
// driven through the same NotifyChanged and Tick the server drives, and the
// landed blocks read back out of the world. The flat ground's stone tops out
// at y=63, so everything stands on the floor line y=64, z=9.

func pistonRow(t *testing.T, sticky bool) (*World, *Physics, map[string]uint16) {
	t.Helper()
	w, p := newPhysicsWorld(t)
	blocks := map[string]uint16{}
	if sticky {
		blocks["piston"] = rsState(t, "minecraft:sticky_piston", map[string]string{"facing": "east", "extended": "false"})
		blocks["pistonOut"] = rsState(t, "minecraft:sticky_piston", map[string]string{"facing": "east", "extended": "true"})
		blocks["head"] = rsState(t, "minecraft:piston_head", map[string]string{"facing": "east", "short": "false", "type": "sticky"})
	} else {
		blocks["piston"] = rsState(t, "minecraft:piston", map[string]string{"facing": "east", "extended": "false"})
		blocks["pistonOut"] = rsState(t, "minecraft:piston", map[string]string{"facing": "east", "extended": "true"})
		blocks["head"] = rsState(t, "minecraft:piston_head", map[string]string{"facing": "east", "short": "false", "type": "normal"})
	}
	blocks["dirt"] = rsState(t, "minecraft:dirt", nil)
	blocks["slime"] = rsState(t, "minecraft:slime_block", nil)
	blocks["honey"] = rsState(t, "minecraft:honey_block", nil)
	blocks["grass"] = rsState(t, "minecraft:short_grass", nil)
	blocks["obsidian"] = rsState(t, "minecraft:obsidian", nil)
	blocks["power"] = rsState(t, "minecraft:redstone_block", nil)
	return w, p, blocks
}

// pistonRowCells reads the row the tests build on: x from 9 to 16 on the
// floor line y=64, z=9.
func pistonRowCells(t *testing.T, w *World) map[int32]string {
	t.Helper()
	out := map[int32]string{}
	for x := int32(9); x <= 16; x++ {
		name, _ := rsNameAt(t, w, x, 64, 9)
		out[x] = name
	}
	return out
}

func wantRow(t *testing.T, w *World, want map[int32]string) {
	t.Helper()
	got := pistonRowCells(t, w)
	for x, name := range want {
		if got[x] != name {
			t.Fatalf("cell x=%d holds %s, want %s (row: %v)", x, got[x], name, got)
		}
	}
}

// pistonSetup lays the piston and its row and runs the tick that settles
// them, returning the clock the moves stage against. The dig list names the
// columns whose ground cell the test clears first - a sticky chain sitting
// on the stone floor drags the floor with it, and the twelve-block move the
// test wants has to stand over air.
func pistonSetup(t *testing.T, w *World, p *Physics, blocks map[string]uint16, sticky bool, states map[int32]uint16, dig []int32) int64 {
	t.Helper()
	setBlock(t, w, p, 10, 64, 9, blocks["piston"])
	for _, x := range dig {
		setBlock(t, w, p, x, 63, 9, airState)
	}
	for x, state := range states {
		setBlock(t, w, p, x, 64, 9, state)
	}
	var clock int64
	clock++
	p.Tick(clock)
	return clock
}

// pistonPowerOn lays the power beside the piston and lets one tick carry the
// move: the block event the power queues drains the same game tick the
// placement's wake ran in.
func pistonPowerOn(t *testing.T, w *World, p *Physics, blocks map[string]uint16, clock *int64) {
	t.Helper()
	setBlock(t, w, p, 10, 64, 8, blocks["power"])
	*clock++
	p.Tick(*clock)
}

// pistonPowerOff takes the power away and lets one tick carry the
// retraction the losing of power queues.
func pistonPowerOff(t *testing.T, w *World, p *Physics, clock *int64) {
	t.Helper()
	setBlock(t, w, p, 10, 64, 8, airState)
	*clock++
	p.Tick(*clock)
}

// TestPistonExtendsAndPushes powers a piston with a block in front and
// follows the ride out and the landing home: the head rides out one cell and
// the pushed block rides two, and two game ticks after the move both have
// landed.
func TestPistonExtendsAndPushes(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	clock := pistonSetup(t, w, p, blocks, false, map[int32]uint16{11: blocks["dirt"]}, nil)
	pistonPowerOn(t, w, p, blocks, &clock)

	// The move ran in the tick the power arrived: the piston's own cell is
	// the extended piston, the head's ride sits in front and the dirt's
	// ride one further, the cell the dirt left is air.
	if name, props := rsNameAt(t, w, 10, 64, 9); name != "minecraft:piston" || props["extended"] != "true" {
		t.Fatalf("piston cell after extend: %s %v", name, props)
	}
	wantRow(t, w, map[int32]string{10: "minecraft:piston", 11: "minecraft:moving_piston", 12: "minecraft:moving_piston"})

	// The landings run the game tick the rides finish on, two after the
	// move.
	created := clock
	for clock < created+2 {
		clock++
		p.Tick(clock)
	}
	wantRow(t, w, map[int32]string{10: "minecraft:piston", 11: "minecraft:piston_head", 12: "minecraft:dirt"})
	if _, props := rsNameAt(t, w, 11, 64, 9); props["facing"] != "east" {
		t.Fatalf("head cell faces %v", props)
	}
}

// TestPistonRetractsPlain extends a plain piston and takes the power away:
// the head goes with the retraction - the plain piston's head is removed,
// not carried - and the piston's own cell rides back to the unextended
// piston two game ticks after the event.
func TestPistonRetractsPlain(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	clock := pistonSetup(t, w, p, blocks, false, map[int32]uint16{11: blocks["dirt"]}, nil)
	pistonPowerOn(t, w, p, blocks, &clock)
	for clock < 4 {
		clock++
		p.Tick(clock)
	}
	wantRow(t, w, map[int32]string{10: "minecraft:piston", 11: "minecraft:piston_head", 12: "minecraft:dirt"})

	pistonPowerOff(t, w, p, &clock)
	// The head is taken the tick the event runs.
	wantRow(t, w, map[int32]string{11: "minecraft:air", 12: "minecraft:dirt"})
	for clock < 7 {
		clock++
		p.Tick(clock)
	}
	if name, props := rsNameAt(t, w, 10, 64, 9); name != "minecraft:piston" || props["extended"] != "false" {
		t.Fatalf("piston cell after retract: %s %v", name, props)
	}
	wantRow(t, w, map[int32]string{11: "minecraft:air", 12: "minecraft:dirt"})
}

// TestStickyPullsBlockBack extends a sticky piston with a block in front and
// retracts it: the block the head pushed comes home with the head's pull.
func TestStickyPullsBlockBack(t *testing.T) {
	w, p, blocks := pistonRow(t, true)
	clock := pistonSetup(t, w, p, blocks, true, map[int32]uint16{11: blocks["dirt"]}, nil)
	pistonPowerOn(t, w, p, blocks, &clock)
	for clock < 4 {
		clock++
		p.Tick(clock)
	}
	wantRow(t, w, map[int32]string{10: "minecraft:sticky_piston", 11: "minecraft:piston_head", 12: "minecraft:dirt"})

	pistonPowerOff(t, w, p, &clock)
	for clock < 7 {
		clock++
		p.Tick(clock)
	}
	if name, props := rsNameAt(t, w, 10, 64, 9); name != "minecraft:sticky_piston" || props["extended"] != "false" {
		t.Fatalf("piston cell after pull: %s %v", name, props)
	}
	// The pulled block sits where the head stood; the cell it was pushed to
	// is air again.
	wantRow(t, w, map[int32]string{11: "minecraft:dirt", 12: "minecraft:air"})
}

// TestZeroTickPistonLeavesBlockOut is the zero-tick: the power comes on one
// game tick and goes the next, the retraction arriving the game tick the
// pushed block's ride is still out - the vanilla abort, the type-2 event -
// and the ride is finished where it stands: the block stays out, the head
// goes and the piston comes home unextended.
func TestZeroTickPistonLeavesBlockOut(t *testing.T) {
	w, p, blocks := pistonRow(t, true)
	clock := pistonSetup(t, w, p, blocks, true, map[int32]uint16{11: blocks["dirt"]}, nil)
	pistonPowerOn(t, w, p, blocks, &clock)
	clock++
	p.Tick(clock)

	// The power off queues the retraction for the tick the ride's landing
	// visit also falls due on - and the event drains ahead of it.
	setBlock(t, w, p, 10, 64, 8, airState)
	clock++
	p.Tick(clock)

	// The abort ran: the pushed block landed where the ride carried it and
	// stays there; the head's ride went the same way, leaving air.
	wantRow(t, w, map[int32]string{11: "minecraft:air", 12: "minecraft:dirt"})
	// The piston's own retraction ride lands two game ticks after the
	// event.
	for clock < 6 {
		clock++
		p.Tick(clock)
	}
	if name, props := rsNameAt(t, w, 10, 64, 9); name != "minecraft:sticky_piston" || props["extended"] != "false" {
		t.Fatalf("piston cell after zero tick: %s %v", name, props)
	}
	wantRow(t, w, map[int32]string{11: "minecraft:air", 12: "minecraft:dirt"})
}

// TestPistonBreaksDestroyOnPush extends a piston at tall grass: the grass is
// the destroy-on-push kind, the extend breaks it where it stands - the loot
// rolls once - and the head takes its place.
func TestPistonBreaksDestroyOnPush(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	clock := pistonSetup(t, w, p, blocks, false, map[int32]uint16{11: blocks["grass"]}, nil)

	setBlock(t, w, p, 10, 64, 8, blocks["power"])
	clock++
	changes, _, _, tickEffects := p.Tick(clock)

	if name, _ := rsNameAt(t, w, 11, 64, 9); name != "minecraft:moving_piston" {
		t.Fatalf("head ride cell: %s", name)
	}
	dropped := false
	for _, e := range tickEffects {
		if e.BreakDrop == blocks["grass"] && e.X == 11 && e.Y == 64 && e.Z == 9 {
			dropped = true
		}
	}
	if !dropped {
		t.Fatalf("the grass broke without its loot roll: %v", tickEffects)
	}
	if len(changes) == 0 {
		t.Fatalf("the extend recorded no block changes")
	}
	for clock < 4 {
		clock++
		p.Tick(clock)
	}
	wantRow(t, w, map[int32]string{10: "minecraft:piston", 11: "minecraft:piston_head", 12: "minecraft:air"})
}

// TestPistonRefusesImmovable powers a piston at obsidian: the move the
// resolver refuses never queues, the piston holds its ground under power.
func TestPistonRefusesImmovable(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	clock := pistonSetup(t, w, p, blocks, false, map[int32]uint16{11: blocks["obsidian"]}, nil)
	pistonPowerOn(t, w, p, blocks, &clock)
	if name, props := rsNameAt(t, w, 10, 64, 9); name != "minecraft:piston" || props["extended"] != "false" {
		t.Fatalf("piston extended into obsidian: %s %v", name, props)
	}
	wantRow(t, w, map[int32]string{11: "minecraft:obsidian"})
}

// TestPistonQuasiConnectivity powers the piston through the block above it -
// a redstone block beside that block, no direct line to the piston - and
// gives the piston the block update its quasi-connected reading waits for by
// placing a stone beside it.
func TestPistonQuasiConnectivity(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	var clock int64
	setBlock(t, w, p, 10, 64, 9, blocks["piston"])
	// The stone the quasi reading looks through, the power beside it.
	setBlock(t, w, p, 10, 65, 9, rsState(t, "minecraft:stone", nil))
	setBlock(t, w, p, 10, 65, 8, blocks["power"])
	clock++
	p.Tick(clock)
	// Nothing yet: the piston has had no block update to read on.
	wantRow(t, w, map[int32]string{10: "minecraft:piston"})

	// The update: a stone placed beside the piston wakes it, and the
	// quasi-connected power the cell above carries runs the extend.
	setBlock(t, w, p, 9, 64, 9, rsState(t, "minecraft:stone", nil))
	clock++
	p.Tick(clock)
	wantRow(t, w, map[int32]string{10: "minecraft:piston", 11: "minecraft:moving_piston"})
	for clock < 4 {
		clock++
		p.Tick(clock)
	}
	wantRow(t, w, map[int32]string{10: "minecraft:piston", 11: "minecraft:piston_head"})
}

// TestSlimeDragsWhatItTouches retracts a sticky piston whose head pushed a
// slime block with a plain block behind it: the slime comes home and drags
// the block it touches with it.
func TestSlimeDragsWhatItTouches(t *testing.T) {
	w, p, blocks := pistonRow(t, true)
	clock := pistonSetup(t, w, p, blocks, true, map[int32]uint16{11: blocks["slime"], 12: blocks["dirt"]}, []int32{11, 12})
	pistonPowerOn(t, w, p, blocks, &clock)
	for clock < 4 {
		clock++
		p.Tick(clock)
	}
	wantRow(t, w, map[int32]string{10: "minecraft:sticky_piston", 11: "minecraft:piston_head", 12: "minecraft:slime_block", 13: "minecraft:dirt"})

	pistonPowerOff(t, w, p, &clock)
	for clock < 7 {
		clock++
		p.Tick(clock)
	}
	wantRow(t, w, map[int32]string{10: "minecraft:sticky_piston", 11: "minecraft:slime_block", 12: "minecraft:dirt", 13: "minecraft:air"})
}

// TestHoneyRefusesSlime retracts a sticky piston whose head pushed a honey
// block with a slime block behind it: the two refuse each other, the pull
// takes the honey home and the slime stays where the extend put it.
func TestHoneyRefusesSlime(t *testing.T) {
	w, p, blocks := pistonRow(t, true)
	clock := pistonSetup(t, w, p, blocks, true, map[int32]uint16{11: blocks["honey"], 12: blocks["slime"]}, []int32{11, 12})
	pistonPowerOn(t, w, p, blocks, &clock)
	for clock < 4 {
		clock++
		p.Tick(clock)
	}
	wantRow(t, w, map[int32]string{11: "minecraft:piston_head", 12: "minecraft:honey_block", 13: "minecraft:slime_block"})

	pistonPowerOff(t, w, p, &clock)
	for clock < 7 {
		clock++
		p.Tick(clock)
	}
	wantRow(t, w, map[int32]string{10: "minecraft:sticky_piston", 11: "minecraft:honey_block", 12: "minecraft:air", 13: "minecraft:slime_block"})
}

// TestPistonLimitTwelveBlocks extends a piston at a line of thirteen dirt:
// the resolver carries twelve and refuses the thirteenth, the move never
// queues, and nothing moves.
func TestPistonLimitTwelveBlocks(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	states := map[int32]uint16{}
	for x := int32(11); x <= 23; x++ {
		states[x] = blocks["dirt"]
	}
	clock := pistonSetup(t, w, p, blocks, false, states, nil)
	pistonPowerOn(t, w, p, blocks, &clock)
	if name, props := rsNameAt(t, w, 10, 64, 9); name != "minecraft:piston" || props["extended"] != "false" {
		t.Fatalf("thirteen-deep line moved: %s %v", name, props)
	}
	for x := int32(11); x <= 23; x++ {
		if name, _ := rsNameAt(t, w, x, 64, 9); name != "minecraft:dirt" {
			t.Fatalf("thirteen-deep line moved at x=%d: %s", x, name)
		}
	}
}

// TestAMovingPistonCollidesAsItsCarriedBlock reads the ride's shape: the
// carried block shifted back along the facing by what is left to travel.
func TestAMovingPistonCollidesAsItsCarriedBlock(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	ride := movingPistonStateOf(dirEast, false)
	w.SetBlock(12, 64, 9, ride)
	p.pistonRideCreate([3]int32{12, 64, 9}, ride, blocks["dirt"], dirEast, true, false)
	be := w.BlockEntity(12, 64, 9)
	for progress, want := range map[float64]float64{0: -1, 0.5: -0.5, 1: 0} {
		be.Use(func(b *BlockEntity) { b.RideProgress = progress })
		boxes := w.MovingPistonBoxes(12, 64, 9)
		if len(boxes) != 1 || boxes[0].MinX != want || boxes[0].MaxX != want+1 {
			t.Errorf("progress %v: boxes %+v, want one unit box at x %v", progress, boxes, want)
		}
	}
	if boxes := w.MovingPistonBoxes(13, 64, 9); boxes != nil {
		t.Errorf("a cell with no ride has boxes %+v", boxes)
	}
}
