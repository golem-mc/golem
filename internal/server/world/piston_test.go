package world

import (
	"fmt"
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

// TestOneTickPulseLeavesBlockOut is the other leave-it-out path: the
// retraction arrives one game tick after the extend, while the ride still
// blocks the cell the pull would start at - the resolver refuses, the head
// goes, and the block stays out just the same.
func TestOneTickPulseLeavesBlockOut(t *testing.T) {
	w, p, blocks := pistonRow(t, true)
	clock := pistonSetup(t, w, p, blocks, true, map[int32]uint16{11: blocks["dirt"]}, nil)
	pistonPowerOn(t, w, p, blocks, &clock)
	// The power off straight after the extend: the retraction drains the
	// next tick, while the ride is still travelling.
	setBlock(t, w, p, 10, 64, 8, airState)
	clock++
	p.Tick(clock)

	// The head's cell went; the pushed block's ride is still out.
	wantRow(t, w, map[int32]string{11: "minecraft:air"})
	for clock < 5 {
		clock++
		p.Tick(clock)
	}
	if name, props := rsNameAt(t, w, 10, 64, 9); name != "minecraft:sticky_piston" || props["extended"] != "false" {
		t.Fatalf("piston cell after one-tick pulse: %s %v", name, props)
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

// TestPistonBreaksCandlesAndPots extends a piston at each candle and pot kind:
// the block breaks where it stands with its loot roll instead of moving.
func TestPistonBreaksCandlesAndPots(t *testing.T) {
	for _, name := range []string{"minecraft:candle", "minecraft:red_candle", "minecraft:candle_cake", "minecraft:red_candle_cake", "minecraft:flower_pot", "minecraft:potted_poppy", "minecraft:potted_flowering_azalea_bush"} {
		t.Run(name, func(t *testing.T) {
			w, p, blocks := pistonRow(t, false)
			state := rsState(t, name, nil)
			clock := pistonSetup(t, w, p, blocks, false, map[int32]uint16{11: state}, nil)
			setBlock(t, w, p, 10, 64, 8, blocks["power"])
			clock++
			_, _, _, tickEffects := p.Tick(clock)
			dropped := false
			for _, e := range tickEffects {
				if e.BreakDrop == state && e.X == 11 && e.Y == 64 && e.Z == 9 {
					dropped = true
				}
			}
			if !dropped {
				t.Fatalf("%s did not break with its loot roll: %v", name, tickEffects)
			}
			for clock < 4 {
				clock++
				p.Tick(clock)
			}
			wantRow(t, w, map[int32]string{10: "minecraft:piston", 11: "minecraft:piston_head", 12: "minecraft:air"})
		})
	}
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

// TestPistonLimitCountsStickyChain checks the resolver's limit the way the
// sticky chains count it: a slime at the head with eleven plain blocks
// behind is twelve to the move and runs.
func TestPistonLimitCountsStickyChain(t *testing.T) {
	w, p, blocks := pistonRow(t, true)
	states := map[int32]uint16{11: blocks["slime"]}
	dig := []int32{11}
	for x := int32(12); x <= 22; x++ {
		states[x] = blocks["dirt"]
		dig = append(dig, x)
	}
	clock := pistonSetup(t, w, p, blocks, true, states, dig)
	pistonPowerOn(t, w, p, blocks, &clock)
	if name, _ := rsNameAt(t, w, 11, 64, 9); name != "minecraft:moving_piston" {
		t.Fatalf("twelve-cell move refused: %s at the head", name)
	}
}

// TestPistonHeadDropsItsBaseWhenBroken breaks the head of a landed piston:
// the head's removal pass takes the extended base behind it down, the base
// breaking with the drop the piston's loot rolls.
func TestPistonHeadDropsItsBaseWhenBroken(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	clock := pistonSetup(t, w, p, blocks, false, map[int32]uint16{11: blocks["dirt"]}, nil)
	pistonPowerOn(t, w, p, blocks, &clock)
	for clock < 4 {
		clock++
		p.Tick(clock)
	}
	if name, _ := rsNameAt(t, w, 11, 64, 9); name != "minecraft:piston_head" {
		t.Fatalf("head never landed: %s", name)
	}
	// Break the head: the removal pass takes the extended base behind it
	// down, the piston coming off with the drop its loot rolls.
	old := w.BlockAt(11, 64, 9)
	w.SetBlock(11, 64, 9, airState)
	_, effects, _ := p.NotifyChanged(11, 64, 9, old)
	broke := false
	for _, e := range effects {
		if e.BreakDrop == blocks["pistonOut"] && e.X == 10 && e.Y == 64 && e.Z == 9 {
			broke = true
		}
	}
	if !broke {
		t.Fatalf("the base behind the head did not break: %v", effects)
	}
	if name, _ := rsNameAt(t, w, 10, 64, 9); name != "minecraft:air" {
		t.Fatalf("base cell after the head broke: %s", name)
	}
	wantRow(t, w, map[int32]string{12: "minecraft:dirt"})
}

// TestPistonRetractionTakesTravellingHeadIn is the retraction of a piston
// whose head is still the ride of an extend that ran the tick before: the
// event takes the ride in where it stands - the head never lands - and the
// plain piston's cell rides home.
func TestPistonRetractionTakesTravellingHeadIn(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	clock := pistonSetup(t, w, p, blocks, false, map[int32]uint16{11: blocks["dirt"]}, nil)
	pistonPowerOn(t, w, p, blocks, &clock)
	// The power off one game tick after the extend: the head's ride is
	// still travelling, the retraction finishes it where it stands and the
	// cell it would have landed in goes to air.
	pistonPowerOff(t, w, p, &clock)
	wantRow(t, w, map[int32]string{11: "minecraft:air"})
	for clock < 6 {
		clock++
		p.Tick(clock)
	}
	if name, props := rsNameAt(t, w, 10, 64, 9); name != "minecraft:piston" || props["extended"] != "false" {
		t.Fatalf("piston cell after the ride-in: %s %v", name, props)
	}
	wantRow(t, w, map[int32]string{11: "minecraft:air", 12: "minecraft:dirt"})
}

// TestAMovedObserverPulsesWhereItLands rides an observer out on a sticky
// piston's slime: the landing's own shape offer books the pulse two game
// ticks out, and nothing else does.
// vanilla: ObserverBlock.updateShape, Block.updateFromNeighbourShapes
func TestAMovedObserverPulsesWhereItLands(t *testing.T) {
	w, p, blocks := pistonRow(t, true)
	observer := rsState(t, "minecraft:observer", map[string]string{"facing": "up", "powered": "false"})
	clock := pistonSetup(t, w, p, blocks, true, map[int32]uint16{11: blocks["slime"]}, []int32{11})
	setBlock(t, w, p, 11, 65, 9, observer)

	pistonPowerOn(t, w, p, blocks, &clock)
	// The rides went out at the power's tick; the observer travels unpowered.
	for clock < 4 {
		clock++
		p.Tick(clock)
	}
	if name, props := rsNameAt(t, w, 12, 65, 9); name != "minecraft:observer" || props["powered"] != "false" {
		t.Fatalf("the observer at its landing: %s %v, want the carried-unpowered state", name, props)
	}

	// The landing booked the pulse: powered on two game ticks later, and
	// off again two after that.
	clock++
	p.Tick(clock)
	if props := observerProps(t, w, 12, 65, 9); props["powered"] != "false" {
		t.Fatalf("observer pulsed too early: %v", props)
	}
	clock++
	p.Tick(clock)
	if props := observerProps(t, w, 12, 65, 9); props["powered"] != "true" {
		t.Fatalf("the moved observer never pulsed: %v", props)
	}
	for clock < 8 {
		clock++
		p.Tick(clock)
	}
	if props := observerProps(t, w, 12, 65, 9); props["powered"] != "false" {
		t.Fatalf("observer stayed on past its pulse: %v", props)
	}
}

// TestAnObserverEnginePumpsOnItsMovedObserver runs the flying machine's core
// on its own: the moved observer's landing stops the pulse it left with and
// books the next, so the piston keeps cycling.
func TestAnObserverEnginePumpsOnItsMovedObserver(t *testing.T) {
	w, p, blocks := pistonRow(t, true)
	observer := rsState(t, "minecraft:observer", map[string]string{"facing": "east", "powered": "false"})
	clock := pistonSetup(t, w, p, blocks, true, map[int32]uint16{11: blocks["slime"]}, []int32{11})
	setBlock(t, w, p, 11, 65, 9, observer)

	// The kick: a block in the watched cell books the pulse whose back
	// powers the piston, so the ride leaves with the observer powered.
	setBlock(t, w, p, 12, 65, 9, blocks["grass"])
	for clock < 5 {
		clock++
		p.Tick(clock)
	}
	if name, props := rsNameAt(t, w, 12, 65, 9); name != "minecraft:observer" {
		t.Fatalf("the observer never rode out: %s %v", name, props)
	}
	// It landed carrying the pulse it left with, whose off-edge stayed
	// behind, so the landing stops it.
	if props := observerProps(t, w, 12, 65, 9); props["powered"] != "false" {
		t.Fatalf("observer landed still powered, want the landing to stop the pulse: %v", props)
	}

	for clock < 8 {
		clock++
		p.Tick(clock)
	}
	if name, _ := rsNameAt(t, w, 11, 65, 9); name != "minecraft:observer" {
		t.Fatalf("the engine never came home: %s at the observer's cell", name)
	}
	if props := observerProps(t, w, 11, 65, 9); props["powered"] != "false" {
		t.Fatalf("observer came home powered: %v", props)
	}

	// The home landing books the next pulse, and the machine pumps again.
	for clock < 10 {
		clock++
		p.Tick(clock)
	}
	if name, props := rsNameAt(t, w, 10, 64, 9); name != "minecraft:sticky_piston" || props["extended"] != "true" {
		t.Fatalf("the engine did not extend on its own again: %s %v", name, props)
	}
	for clock < 17 {
		clock++
		p.Tick(clock)
	}
	if name, props := rsNameAt(t, w, 10, 64, 9); name != "minecraft:sticky_piston" || props["extended"] != "true" {
		t.Fatalf("the engine stopped pumping: %s %v", name, props)
	}
}

// TestForcedLandingPulsesTheMovedObserverOnTime is the sticky retraction's
// forced landing of an extending observer's ride: the landing's own shape
// offer books the pulse two game ticks out, not the ride's queued landing
// visit firing against the block just written.
// vanilla: PistonMovingBlockEntity.finalTick, ObserverBlock.updateShape
func TestForcedLandingPulsesTheMovedObserverOnTime(t *testing.T) {
	w, p, blocks := pistonRow(t, true)
	observer := rsState(t, "minecraft:observer", map[string]string{"facing": "up", "powered": "false"})
	clock := pistonSetup(t, w, p, blocks, true, map[int32]uint16{11: observer}, []int32{11})

	// The power on and off one game tick apart: the retraction finishes the
	// observer's extending ride where it stands.
	setBlock(t, w, p, 10, 64, 8, blocks["power"])
	clock++
	p.Tick(clock)
	setBlock(t, w, p, 10, 64, 8, airState)
	clock++
	p.Tick(clock)

	if name, props := rsNameAt(t, w, 12, 64, 9); name != "minecraft:observer" || props["powered"] != "false" {
		t.Fatalf("the forced landing: %s %v, want the carried-unpowered observer", name, props)
	}
	clock++
	p.Tick(clock)
	if props := observerProps(t, w, 12, 64, 9); props["powered"] != "false" {
		t.Fatalf("the ride's stale landing visit pulsed the observer a tick early: %v", props)
	}
	clock++
	p.Tick(clock)
	if props := observerProps(t, w, 12, 64, 9); props["powered"] != "true" {
		t.Fatalf("the forced landing never booked the pulse: %v", props)
	}
}

// TestALandedHeadWithoutItsBaseGoes is a head whose base was removed while it
// was still riding: the landing's shape pass finds no base to fit and takes
// the head down, where it used to land floating.
// vanilla: PistonHeadBlock.updateShape
func TestALandedHeadWithoutItsBaseGoes(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	clock := pistonSetup(t, w, p, blocks, false, nil, nil)

	setBlock(t, w, p, 10, 64, 8, blocks["power"])
	clock++
	p.Tick(clock)
	if name, _ := rsNameAt(t, w, 11, 64, 9); name != "minecraft:moving_piston" {
		t.Fatalf("the head never rode out: %s", name)
	}
	// Break the base while the head travels.
	old := w.BlockAt(10, 64, 9)
	w.SetBlock(10, 64, 9, airState)
	p.NotifyChanged(10, 64, 9, old)

	// The head lands two game ticks after the ride and finds no base.
	for clock < 4 {
		clock++
		p.Tick(clock)
	}
	if name, _ := rsNameAt(t, w, 11, 64, 9); name != "minecraft:air" {
		t.Fatalf("the landed head without its base: %s, want air", name)
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

// TestAMovingPistonRideShapeFollowsFacingHeadAndBase covers the vertical
// facing, the source head with its short flag, and the pulled base.
func TestAMovingPistonRideShapeFollowsFacingHeadAndBase(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	ride := movingPistonStateOf(dirUp, false)
	w.SetBlock(12, 64, 9, ride)
	p.pistonRideCreate([3]int32{12, 64, 9}, ride, blocks["dirt"], dirUp, true, false)
	be := w.BlockEntity(12, 64, 9)
	be.Use(func(b *BlockEntity) { b.RideProgress = 0.5 })
	if boxes := w.MovingPistonBoxes(12, 64, 9); len(boxes) != 1 || boxes[0].MinY != -0.5 || boxes[0].MinX != 0 {
		t.Errorf("vertical extend: %+v, want one box from y -0.5", boxes)
	}

	// A source head: short until the last quarter of an extension.
	ride = movingPistonStateOf(dirEast, true)
	w.SetBlock(13, 64, 9, ride)
	p.pistonRideCreate([3]int32{13, 64, 9}, ride, blocks["dirt"], dirEast, true, true)
	be = w.BlockEntity(13, 64, 9)
	armLen := func(progress float64) float64 {
		be.Use(func(b *BlockEntity) { b.RideProgress = progress })
		lo, hi := 2.0, -2.0
		for _, c := range w.MovingPistonBoxes(13, 64, 9) {
			lo, hi = min(lo, c.MinX), max(hi, c.MaxX)
		}
		return hi - lo
	}
	if short, full := armLen(0.5), armLen(0.9); !(short < full) {
		t.Errorf("head extent short %v, full %v: want the short head to be shorter", short, full)
	}

	// A pulled piston: its base stays at the cell while the head draws in.
	ride = movingPistonStateOf(dirEast, false)
	w.SetBlock(14, 64, 9, ride)
	p.pistonRideCreate([3]int32{14, 64, 9}, ride, blocks["piston"], dirEast, false, true)
	be = w.BlockEntity(14, 64, 9)
	be.Use(func(b *BlockEntity) { b.RideProgress = 0.5 })
	boxes := w.MovingPistonBoxes(14, 64, 9)
	base := CollisionBoxes(pistonStateOf(false, dirEast, true))
	if len(boxes) <= len(base) || boxes[0] != base[0] {
		t.Errorf("pulled piston: %+v, want the base %+v first, then the head", boxes, base)
	}
}

// TestBoxCollidesReadsAMovingPistonsRide checks the box query sees the ride
// where the state alone has no shape.
func TestBoxCollidesReadsAMovingPistonsRide(t *testing.T) {
	w, p, blocks := pistonRow(t, false)
	ride := movingPistonStateOf(dirEast, false)
	w.SetBlock(12, 64, 9, ride)
	p.pistonRideCreate([3]int32{12, 64, 9}, ride, blocks["dirt"], dirEast, true, false)
	be := w.BlockEntity(12, 64, 9)
	be.Use(func(b *BlockEntity) { b.RideProgress = 0.5 })
	// The carried dirt spans x 11.5 to 12.5 at progress 0.5.
	if !w.BoxCollides(12.1, 64.1, 9.1, 12.4, 64.9, 9.9) {
		t.Error("a box inside the carried block's span does not collide")
	}
	if w.BoxCollides(12.6, 64.1, 9.1, 12.9, 64.9, 9.9) {
		t.Error("a box in the vacated half of the cell collides")
	}
}

// TestPistonExtendsIntoAirAtTheTopLayer puts an up-facing piston under the
// last layer: the head fits into the air at y=319, and a block there stops it.
func TestPistonExtendsIntoAirAtTheTopLayer(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint("blocked=", blocked), func(t *testing.T) {
			w, p, blocks := pistonRow(t, false)
			setBlock(t, w, p, 10, 318, 9, rsState(t, "minecraft:piston", map[string]string{"facing": "up", "extended": "false"}))
			if blocked {
				setBlock(t, w, p, 10, 319, 9, blocks["dirt"])
			}
			var clock int64 = 1
			p.Tick(clock)
			setBlock(t, w, p, 10, 318, 8, blocks["power"])
			for range 3 {
				clock++
				p.Tick(clock)
			}
			name, props := rsNameAt(t, w, 10, 318, 9)
			top, _ := rsNameAt(t, w, 10, 319, 9)
			if blocked {
				if props["extended"] != "false" || top != "minecraft:dirt" {
					t.Fatalf("blocked piston: %s %v, top %s", name, props, top)
				}
			} else if props["extended"] != "true" || top != "minecraft:piston_head" {
				t.Fatalf("free piston: %s %v, top %s", name, props, top)
			}
		})
	}
}
