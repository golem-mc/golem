package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The redstone engine's black-box checks: a lever, its wire, the lamp at the
// end of it and the torch inverting behind, all driven through the same
// NotifyChanged and Tick the server drives, with the state ids the registry
// hands out.

func rsState(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state for %s %v", name, props)
	}
	return state
}

func rsNameAt(t *testing.T, w *World, x, y, z int32) (string, map[string]string) {
	t.Helper()
	state := w.BlockAt(x, y, z)
	name, props, ok := registry.BlockStateName(state)
	if !ok {
		t.Fatalf("block at (%d, %d, %d) = %d is not a named block", x, y, z, state)
	}
	return name, registry.ParseStateProps(props)
}

// rsBuild lays the test circuit on the flat ground: a stone floor is already
// there at y=62 with air from y=63 up. Everything sits on the floor line
// z=9, x from 10 to 20.
func rsBuildLine(t *testing.T, w *World, p *Physics, states map[[3]int32]uint16) {
	t.Helper()
	for pos, state := range states {
		setBlock(t, w, p, pos[0], pos[1], pos[2], state)
	}
}

func TestNeighborSignalKeepsStrongerLaterSource(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	const x, y, z = 8, 80, 8
	weak := rsState(t, "minecraft:daylight_detector", map[string]string{"power": "7"})
	stronger := rsState(t, "minecraft:daylight_detector", map[string]string{"power": "11"})
	w.SetBlock(x, y-1, z, weak)
	if got := p.NeighborSignal(x, y, z); got != 7 {
		t.Fatalf("power from below = %d, want 7", got)
	}
	w.SetBlock(x+1, y, z, stronger)
	if got := p.NeighborSignal(x, y, z); got != 11 {
		t.Fatalf("power with a stronger source to the east = %d, want 11", got)
	}
}

// TestLeverLightsWireAndLampCascades pulls a lever at the head of a wire
// line and follows the power fall off block by block until the lamp lights.
func TestLeverLightsWireAndLampCascades(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	// Ground row: lever on stone at (20), wire 19..11, lamp at 10, all on
	// the floor y=63.
	lever := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "false"})
	wire := rsState(t, "minecraft:redstone_wire", map[string]string{"east": "side", "west": "side"})
	lamp := rsState(t, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	rsBuildLine(t, w, p, map[[3]int32]uint16{
		{20, 63, 9}: lever,
		{19, 63, 9}: wire, {18, 63, 9}: wire, {17, 63, 9}: wire,
		{16, 63, 9}: wire, {15, 63, 9}: wire, {14, 63, 9}: wire,
		{13, 63, 9}: wire, {12, 63, 9}: wire, {11, 63, 9}: wire,
		{10, 63, 9}: lamp,
	})
	for clock < 30 {
		clock++
		p.Tick(clock)
	}

	// Pull the lever: the wire lights from the head outward one block a
	// tick, and the lamp waits for the wire's last step.
	powered := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "true"})
	old := w.BlockAt(20, 63, 9)
	w.SetBlock(20, 63, 9, powered)
	p.NotifyChanged(20, 63, 9, old)

	// One tick later the whole line holds its graded powers: the fill
	// is one synchronous cascade in vanilla, the far wires weaker by
	// their distance, the lamp lit by the power that still reaches it.
	clock++
	p.Tick(clock)
	for x := int32(11); x <= 19; x++ {
		level := int(15 - (19 - x))
		_, props := rsNameAt(t, w, x, 63, 9)
		if got := props["power"]; got != itoa(level) {
			t.Fatalf("wire at x=%d has power %s, want %d", x, got, level)
		}
	}
	// The lamp lights on the tick the power reaches it.
	_, lampProps := rsNameAt(t, w, 10, 63, 9)
	if lampProps["lit"] != "true" {
		t.Fatalf("lamp never lit: %v", lampProps)
	}
}

// TestLeverOffDecaysWireStepWise turns the lever off and watches the wire's
// power fall by one a tick, the lamp going dark four ticks after its wire.
func TestLeverOffDecaysWireStepWise(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	lever := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "false"})
	wire := rsState(t, "minecraft:redstone_wire", map[string]string{"east": "side", "west": "side"})
	lamp := rsState(t, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	rsBuildLine(t, w, p, map[[3]int32]uint16{
		{20, 63, 9}: lever,
		{19, 63, 9}: wire, {18, 63, 9}: wire, {17, 63, 9}: wire,
		{16, 63, 9}: wire, {15, 63, 9}: wire, {14, 63, 9}: wire,
		{13, 63, 9}: wire, {12, 63, 9}: wire, {11, 63, 9}: wire,
		{10, 63, 9}: lamp,
	})
	// Light the whole circuit and let it settle.
	powered := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "true"})
	old := w.BlockAt(20, 63, 9)
	w.SetBlock(20, 63, 9, powered)
	p.NotifyChanged(20, 63, 9, old)
	for clock < 40 {
		clock++
		p.Tick(clock)
	}
	_, lampProps := rsNameAt(t, w, 10, 63, 9)
	if lampProps["lit"] != "true" {
		t.Fatalf("setup: lamp never lit: %v", lampProps)
	}

	// Off: the whole line drops in the one cascade, and the lamp goes
	// dark on its four-tick off delay, not with the wires.
	old = w.BlockAt(20, 63, 9)
	w.SetBlock(20, 63, 9, lever)
	p.NotifyChanged(20, 63, 9, old)
	clock++
	p.Tick(clock)
	for x := int32(11); x <= 19; x++ {
		_, props := rsNameAt(t, w, x, 63, 9)
		if got := props["power"]; got != "0" {
			t.Fatalf("wire at x=%d has power %s after the lever went off, want 0", x, got)
		}
	}
	_, lampProps = rsNameAt(t, w, 10, 63, 9)
	if lampProps["lit"] != "true" {
		t.Fatalf("lamp went dark with its wire, want the 4-tick off delay: %v", lampProps)
	}
	for tick := 1; tick <= 4; tick++ {
		clock++
		p.Tick(clock)
	}
	_, lampProps = rsNameAt(t, w, 10, 63, 9)
	if lampProps["lit"] != "false" {
		t.Fatalf("lamp never went dark: %v", lampProps)
	}
}

// TestTorchInvertsAndTimesItsTurn checks the wall torch's rhythm: a signal
// at the block it sits on turns it off two ticks later, the loss turns it
// back on two ticks after that, and its output is 15 at its own cell.
func TestTorchInvertsAndTimesItsTurn(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	// A standing torch on the ground at (12), with a wire beside it on
	// the ground at (11): the torch's weak signal reaches the wire at its
	// own level, and the block it stands on is its input, not its output.
	torch := rsState(t, "minecraft:redstone_torch", nil)
	rsBuildLine(t, w, p, map[[3]int32]uint16{
		{12, 63, 9}: torch,
		{11, 63, 9}: rsState(t, "minecraft:redstone_wire", map[string]string{"east": "side", "north": "side", "south": "side", "west": "side"}),
	})
	for clock < 10 {
		clock++
		p.Tick(clock)
	}
	// The wire beside the lit torch holds power 15.
	if _, props := rsNameAt(t, w, 11, 63, 9); props["power"] != "15" {
		t.Fatalf("wire beside a lit torch has power %v, want 15", props)
	}

	// Power the stone under the torch: the torch goes dark two ticks
	// later, and the wire above goes with it.
	setBlock(t, w, p, 12, 62, 9, rsState(t, "minecraft:redstone_block", nil))
	clock++
	p.Tick(clock)
	if _, props := rsNameAt(t, w, 12, 63, 9); props["lit"] != "true" {
		t.Fatalf("torch went dark on the tick of the change, want the 2-tick delay: %v", props)
	}
	clock++
	p.Tick(clock)
	if _, props := rsNameAt(t, w, 12, 63, 9); props["lit"] != "false" {
		t.Fatalf("torch did not go dark on the second tick: %v", props)
	}
	if _, props := rsNameAt(t, w, 11, 63, 9); props["power"] != "0" {
		t.Fatalf("wire beside a dark torch still powered: %v", props)
	}

	// Take the block's power away: the torch comes back two ticks later.
	setBlock(t, w, p, 12, 62, 9, w.BlockAt(11, 62, 9))
	clock++
	p.Tick(clock)
	if _, props := rsNameAt(t, w, 12, 63, 9); props["lit"] != "false" {
		t.Fatalf("torch relit on the tick of the change, want the 2-tick delay: %v", props)
	}
	clock++
	p.Tick(clock)
	if _, props := rsNameAt(t, w, 12, 63, 9); props["lit"] != "true" {
		t.Fatalf("torch did not relight on the second tick: %v", props)
	}
}

// TestRepeaterDelaysAndLocks walks a repeater's delay count and its lock by
// a second repeater aimed at its side.
func TestRepeaterDelaysAndLocks(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	// Lever at (16) feeding wire, repeater at (14) facing east (its input
	// side toward the wire), wire 13..11, lamp at 10. The repeater reads
	// its input at FACING and emits out the back.
	lever := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "false"})
	wire := rsState(t, "minecraft:redstone_wire", map[string]string{"east": "side", "west": "side"})
	repeater := rsState(t, "minecraft:repeater", map[string]string{"facing": "east", "delay": "1", "powered": "false"})
	lamp := rsState(t, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	rsBuildLine(t, w, p, map[[3]int32]uint16{
		{16, 63, 9}: lever,
		{15, 63, 9}: wire,
		{14, 63, 9}: repeater,
		{13, 63, 9}: wire, {12, 63, 9}: wire, {11, 63, 9}: wire,
		{10, 63, 9}: lamp,
	})
	for clock < 10 {
		clock++
		p.Tick(clock)
	}
	powered := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "true"})
	old := w.BlockAt(16, 63, 9)
	w.SetBlock(16, 63, 9, powered)
	p.NotifyChanged(16, 63, 9, old)

	// The repeater turns on only after its delay of two game ticks past
	// the wire reaching it.
	sawOff := false
	var turnedOn int
	for tick := 1; tick <= 12; tick++ {
		clock++
		p.Tick(clock)
		if testing.Verbose() {
			_, wp := rsNameAt(t, w, 15, 63, 9)
			_, rp := rsNameAt(t, w, 14, 63, 9)
			t.Logf("tick %d wire15=%v rep=%v", tick, wp["power"], rp)
		}
		_, props := rsNameAt(t, w, 14, 63, 9)
		if props["powered"] == "false" {
			sawOff = true
		} else if turnedOn == 0 {
			turnedOn = tick
		}
	}
	if !sawOff || turnedOn == 0 {
		t.Fatalf("repeater did not turn on in its own time: turnedOn=%d sawOff=%v", turnedOn, sawOff)
	}
	// The output side lit only after the delay, never before tick 2.
	if turnedOn < 2 {
		t.Fatalf("repeater turned on at tick %d, before its delay could pass", turnedOn)
	}
	_, lampProps := rsNameAt(t, w, 10, 63, 9)
	if lampProps["lit"] != "true" {
		t.Fatalf("lamp never lit through the repeater: %v", lampProps)
	}

	// A repeater aimed at the first one's side holds it on through the
	// lever going off, and lets go two ticks after the lock lifts.
	// A repeater south of the first one, facing south so its back — the
	// output — points north into the first repeater's side, fed straight
	// from a redstone block behind it.
	locker := rsState(t, "minecraft:repeater", map[string]string{"facing": "south", "delay": "1", "powered": "false"})
	// The repeater goes in first: a repeater placed beside an already
	// powered source is not updated until a neighbour changes.
	setBlock(t, w, p, 14, 63, 10, locker)
	setBlock(t, w, p, 14, 63, 11, rsState(t, "minecraft:redstone_block", nil))
	for tick := 1; tick <= 6; tick++ {
		clock++
		p.Tick(clock)
	}
	if _, props := rsNameAt(t, w, 14, 63, 10); props["powered"] != "true" {
		t.Fatalf("locker did not power up: %v", props)
	}
	// Off at the lever: the locked repeater holds.
	old = w.BlockAt(16, 63, 9)
	w.SetBlock(16, 63, 9, lever)
	p.NotifyChanged(16, 63, 9, old)
	for tick := 1; tick <= 8; tick++ {
		clock++
		p.Tick(clock)
	}
	if _, props := rsNameAt(t, w, 14, 63, 9); props["powered"] != "true" {
		t.Fatalf("locked repeater did not hold: %v", props)
	}
	if _, props := rsNameAt(t, w, 10, 63, 9); props["lit"] != "true" {
		t.Fatalf("lamp went out behind a locked repeater: %v", props)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	return digits
}

// comparatorCircuit lays the line every comparator test walks: a lever and
// its feed wire to the east of a comparator whose facing points east, the
// comparator's block entity created the way the server's placement makes
// one, and the output wire with a lamp on the west. It returns the world,
// the physics and the run's own clock.
func comparatorCircuit(t *testing.T, mode string) (*World, *Physics, *int64) {
	t.Helper()
	w, p := newPhysicsWorld(t)
	clock := int64(0)
	lever := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "false"})
	wire := rsState(t, "minecraft:redstone_wire", map[string]string{"east": "side", "west": "side"})
	comparator := rsState(t, "minecraft:comparator", map[string]string{"facing": "east", "mode": mode, "powered": "false"})
	lamp := rsState(t, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	rsBuildLine(t, w, p, map[[3]int32]uint16{
		{16, 63, 9}: lever,
		{15, 63, 9}: wire,
		{14, 63, 9}: comparator,
		{13, 63, 9}: wire,
		{12, 63, 9}: lamp,
	})
	w.EnsureBlockEntity(14, 63, 9, KindComparator)
	for clock < 10 {
		clock++
		p.Tick(clock)
	}
	return w, p, &clock
}

// comparatorOutputAt reads the strength a comparator block entity keeps.
func comparatorOutputAt(t *testing.T, w *World, x, y, z int32) int {
	t.Helper()
	be := w.BlockEntity(x, y, z)
	if be == nil || be.Kind != KindComparator {
		t.Fatalf("no comparator block entity at (%d, %d, %d)", x, y, z)
	}
	out := 0
	be.Use(func(b *BlockEntity) { out = b.Output })
	return out
}

// comparatorProps reads a comparator's parsed properties.
func comparatorProps(t *testing.T, w *World, x, y, z int32) map[string]string {
	t.Helper()
	return propsAt(t, w, x, y, z, "minecraft:comparator")
}

// observerProps reads an observer's parsed properties.
func observerProps(t *testing.T, w *World, x, y, z int32) map[string]string {
	t.Helper()
	return propsAt(t, w, x, y, z, "minecraft:observer")
}

// propsAt reads a block's parsed properties and fails when the block is
// not the one named.
func propsAt(t *testing.T, w *World, x, y, z int32, want string) map[string]string {
	t.Helper()
	name, props := rsNameAt(t, w, x, y, z)
	if name != want {
		t.Fatalf("block at (%d, %d, %d) is %s, want %s", x, y, z, name, want)
	}
	return props
}

// railCartsStub is a RailCartSource for the tests: a fixed answer to the
// detector rail's one question.
type railCartsStub struct{ crossing bool }

func (r railCartsStub) CartCrossing(Box) bool { return r.crossing }

func (r railCartsStub) CartContainerSignal(Box) (int, bool) { return 0, false }

// TestComparatorGradedInput gives the comparator a weaker input — a wire
// three blocks along from its lever — and checks the output strength and
// the output wire both carry the graded level, the behaviour the old
// fifteen-or-nothing answer could not have.
func TestComparatorGradedInput(t *testing.T) {
	w, p := newPhysicsWorld(t)
	clock := int64(0)
	lever := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "false"})
	wire := rsState(t, "minecraft:redstone_wire", map[string]string{"east": "side", "west": "side"})
	comparator := rsState(t, "minecraft:comparator", map[string]string{"facing": "east", "mode": "compare", "powered": "false"})
	rsBuildLine(t, w, p, map[[3]int32]uint16{
		{18, 63, 9}: lever,
		{17, 63, 9}: wire, {16, 63, 9}: wire, {15, 63, 9}: wire,
		{14, 63, 9}: comparator,
		{13, 63, 9}: wire,
	})
	w.EnsureBlockEntity(14, 63, 9, KindComparator)
	for clock < 10 {
		clock++
		p.Tick(clock)
	}
	powered := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "true"})
	old := w.BlockAt(18, 63, 9)
	w.SetBlock(18, 63, 9, powered)
	p.NotifyChanged(18, 63, 9, old)
	for tick := 1; tick <= 6; tick++ {
		clock++
		p.Tick(clock)
	}
	// The wire at 15 holds 15 minus its distance from the lever's wire
	// head at 17: power 13.
	_, inProps := rsNameAt(t, w, 15, 63, 9)
	if inProps["power"] != "13" {
		t.Fatalf("setup: input wire holds power %v, want 13", inProps)
	}
	if out := comparatorOutputAt(t, w, 14, 63, 9); out != 13 {
		t.Fatalf("comparator output %d, want the input's 13", out)
	}
	if _, props := rsNameAt(t, w, 13, 63, 9); props["power"] != "13" {
		t.Fatalf("output wire has power %v, want the comparator's 13", props)
	}
}

// TestComparatorSubtractAndCompareCycle puts a side input next to the
// comparator, one weaker than the front input, and walks the mode cycle by
// hand: subtract holds back the difference, compare passes the front
// strength, and the click the cycle plays rides out as an effect.
func TestComparatorSubtractAndCompareCycle(t *testing.T) {
	w, p, clock := comparatorCircuit(t, "subtract")
	// A redstone block south of the comparator is the side input, its
	// fifteen matching the lever's fifteen in front.
	rsBuildLine(t, w, p, map[[3]int32]uint16{
		{14, 63, 10}: rsState(t, "minecraft:redstone_block", nil),
	})
	powered := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "true"})
	old := w.BlockAt(16, 63, 9)
	w.SetBlock(16, 63, 9, powered)
	p.NotifyChanged(16, 63, 9, old)
	for tick := 1; tick <= 6; tick++ {
		*clock++
		p.Tick(*clock)
	}
	// Subtract 15 from 15: the output is zero and the comparator stays
	// dark even though its input is full.
	if props := comparatorProps(t, w, 14, 63, 9); props["powered"] != "false" {
		t.Fatalf("subtract-mode comparator with equal inputs powered up: %v", props)
	}
	if out := comparatorOutputAt(t, w, 14, 63, 9); out != 0 {
		t.Fatalf("subtract output %d, want 0 for 15-15", out)
	}

	// The click cycles to compare, where equal inputs pass: the output
	// turns on to the full fifteen.
	used, changes, effects, _ := p.UseBlock(14, 63, 9, "")
	if !used {
		t.Fatalf("the comparator did not consume the use")
	}
	if len(changes) == 0 {
		t.Fatalf("the mode cycle wrote no block change")
	}
	clicked := false
	for _, e := range effects {
		if e.SoundID.Name() == "minecraft:block.comparator.click" {
			clicked = true
		}
	}
	if !clicked {
		t.Fatalf("the mode cycle played no click: %v", effects)
	}
	if props := comparatorProps(t, w, 14, 63, 9); props["mode"] != "compare" || props["powered"] != "true" {
		t.Fatalf("after the cycle to compare the comparator is %v", props)
	}
	if out := comparatorOutputAt(t, w, 14, 63, 9); out != 15 {
		t.Fatalf("compare output %d, want 15", out)
	}
	if _, props := rsNameAt(t, w, 13, 63, 9); props["power"] != "15" {
		t.Fatalf("output wire has power %v after the cycle, want 15", props)
	}
}

// TestComparatorReadsChestFullness checks the analog read: a chest in the
// input cell reads out its fullness directly, and the same chest buried
// behind a stone wall reads through the wall — the second-cell look the
// vanilla getInputSignal makes when the input block is a redstone
// conductor. Fourteen full stacks in twenty-seven slots read out as eight,
// the vanilla formula's floor(14/27*14)+1, and the output wire carries
// that strength on. Nothing wakes a comparator on its own: the check rides
// a neighbour change beside it.
func TestComparatorReadsChestFullness(t *testing.T) {
	w, p, clock := comparatorCircuit(t, "compare")
	dirt, _ := registry.ItemID("minecraft:dirt")
	wake := func() {
		// Any change beside the comparator books its two-tick visit. The
		// cell beside it starts as the floor's stone, so the first wake
		// puts cobblestone there and the next one takes it back out.
		old := w.BlockAt(14, 63, 8)
		var next uint16
		if old == airState {
			next = rsState(t, "minecraft:cobblestone", nil)
		}
		if !w.SetBlock(14, 63, 8, next) {
			t.Fatalf("setup: the wake stone did not go in")
		}
		p.NotifyChanged(14, 63, 8, old)
		for tick := 1; tick <= 4; tick++ {
			*clock++
			p.Tick(*clock)
		}
	}
	fillChest := func(x, y, z int32) {
		be := w.EnsureBlockEntity(x, y, z, KindChest)
		be.Use(func(b *BlockEntity) {
			for slot := range 14 {
				b.Slots[slot] = protocol.ItemStack{ItemID: dirt, Count: 64}
			}
		})
	}

	// The chest sits directly in the input cell first.
	old := w.BlockAt(15, 63, 9)
	if !w.SetBlock(15, 63, 9, rsState(t, "minecraft:chest", nil)) {
		t.Fatalf("setup: the chest did not go in")
	}
	p.NotifyChanged(15, 63, 9, old)
	fillChest(15, 63, 9)
	wake()
	if out := comparatorOutputAt(t, w, 14, 63, 9); out != 8 {
		t.Fatalf("comparator read %d off the chest, want 8", out)
	}
	if props := comparatorProps(t, w, 14, 63, 9); props["powered"] != "true" {
		t.Fatalf("comparator never powered on the chest's fullness: %v", props)
	}
	if _, props := rsNameAt(t, w, 13, 63, 9); props["power"] != "8" {
		t.Fatalf("output wire has power %v, want the chest's 8", props)
	}

	// The chest goes behind a stone wall one block further on: the same
	// fullness reads through the conductor, at the same strength.
	old = w.BlockAt(15, 63, 9)
	if !w.SetBlock(15, 63, 9, rsState(t, "minecraft:stone", nil)) {
		t.Fatalf("setup: the wall did not go in")
	}
	p.NotifyChanged(15, 63, 9, old)
	old = w.BlockAt(16, 63, 9)
	if !w.SetBlock(16, 63, 9, rsState(t, "minecraft:chest", nil)) {
		t.Fatalf("setup: the buried chest did not go in")
	}
	p.NotifyChanged(16, 63, 9, old)
	fillChest(16, 63, 9)
	wake()
	if out := comparatorOutputAt(t, w, 14, 63, 9); out != 8 {
		t.Fatalf("comparator read %d through the wall, want the chest's 8", out)
	}
	if _, props := rsNameAt(t, w, 13, 63, 9); props["power"] != "8" {
		t.Fatalf("output wire has power %v through the wall, want 8", props)
	}
}

// observerCircuit lays an observer facing west at (14, 63, 9), watching the
// cell to its west and feeding its output wire eastward to a lamp. The
// watched cell starts as the flat world's own stone.
func observerCircuit(t *testing.T) (*World, *Physics, *int64) {
	t.Helper()
	w, p := newPhysicsWorld(t)
	clock := int64(0)
	observer := rsState(t, "minecraft:observer", map[string]string{"facing": "west", "powered": "false"})
	wire := rsState(t, "minecraft:redstone_wire", map[string]string{"east": "side", "west": "side"})
	lamp := rsState(t, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	rsBuildLine(t, w, p, map[[3]int32]uint16{
		{14, 63, 9}: observer,
		{15, 63, 9}: wire,
		{16, 63, 9}: lamp,
	})
	for clock < 10 {
		clock++
		p.Tick(clock)
	}
	return w, p, &clock
}

// TestObserverPulsesOnWatchedChange puts a block into the cell the observer
// watches and follows the two-tick pulse: powered on the second tick after
// the change, off two ticks later, the output wire rising and falling with
// it and the lamp lighting on the rise.
func TestObserverPulsesOnWatchedChange(t *testing.T) {
	w, p, clock := observerCircuit(t)
	// A change at the watched cell: cobblestone where the floor's stone
	// sat. The pulse books on the change itself.
	old := w.BlockAt(13, 63, 9)
	if !w.SetBlock(13, 63, 9, rsState(t, "minecraft:cobblestone", nil)) {
		t.Fatalf("setup: the watched change did not take")
	}
	p.NotifyChanged(13, 63, 9, old)

	sawOn, sawOff := false, false
	onTick, offTick := 0, 0
	for tick := 1; tick <= 8; tick++ {
		*clock++
		p.Tick(*clock)
		props := observerProps(t, w, 14, 63, 9)
		if props["powered"] == "true" {
			sawOn = true
			if onTick == 0 {
				onTick = tick
			}
		} else if sawOn {
			sawOff = true
			offTick = tick
			break
		}
	}
	if !sawOn || !sawOff {
		t.Fatalf("observer never pulsed: on=%v(%d) off=%v(%d)", sawOn, onTick, sawOff, offTick)
	}
	if onTick != 2 {
		t.Fatalf("observer turned on at tick %d, want the two-tick detection", onTick)
	}
	if offTick != 4 {
		t.Fatalf("observer turned off at tick %d, want two ticks after the rise", offTick)
	}
	// While it pulsed the output wire held fifteen; now it is dark.
	if _, props := rsNameAt(t, w, 15, 63, 9); props["power"] != "0" {
		t.Fatalf("output wire still powered after the pulse: %v", props)
	}
	// The lamp keeps its light through the four-tick off delay.
	_, lampProps := rsNameAt(t, w, 16, 63, 9)
	if lampProps["lit"] != "true" {
		t.Fatalf("lamp went dark with the pulse, want the off delay: %v", lampProps)
	}
	for tick := 1; tick <= 4; tick++ {
		*clock++
		p.Tick(*clock)
	}
	_, lampProps = rsNameAt(t, w, 16, 63, 9)
	if lampProps["lit"] != "false" {
		t.Fatalf("lamp never went dark: %v", lampProps)
	}
}

// touchStub is the tests' Toucher: the same living and all-entity counts
// for every cell the engine asks about, mutable so a test can take whatever
// stood on the plate away again.
type touchStub struct {
	living, all, arrows int
	presser             EventSource
}

func (t *touchStub) Touching(x, y, z int32) (int, int) {
	return t.living, t.all
}

func (t *touchStub) ArrowsIn(box Box) int {
	return t.arrows
}

func (t *touchStub) Presser(x, y, z int32) EventSource { return t.presser }

func (t *touchStub) Pressing(box Box) int {
	return t.all
}

// TestWeightedLightPlateCountsItems puts one item entity on a light weighted
// pressure plate: the plate carries the one-entity signal in POWER and the
// lamp beside it lights, and with the item gone the plate re-looks on its
// ten-tick press time and lets the lamp go dark again.
func TestWeightedLightPlateCountsItems(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	touch := &touchStub{all: 1}
	p.SetToucher(touch)
	plate := rsState(t, "minecraft:light_weighted_pressure_plate", map[string]string{"power": "0"})
	lamp := rsState(t, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	rsBuildLine(t, w, p, map[[3]int32]uint16{
		{12, 63, 9}: plate,
		{11, 63, 9}: lamp,
	})
	for clock < 5 {
		clock++
		p.Tick(clock)
	}

	// The item's sweep presses the plate on its first pass.
	var changes []BlockChange
	var effects []Effect
	var primes []TNTPrime
	p.EntityInsideCells(12, 63, 9, 12, 63, 9, 63, true, &changes, &effects, &primes)
	_, plateProps := rsNameAt(t, w, 12, 63, 9)
	if got := plateProps["power"]; got != "1" {
		t.Fatalf("light plate with one item carries power %q, want 1", got)
	}
	_, lampProps := rsNameAt(t, w, 11, 63, 9)
	if lampProps["lit"] != "true" {
		t.Fatalf("lamp never lit: %v", lampProps)
	}

	// Gone: the plate looks again ten ticks after the press, not the
	// ordinary twenty, and the lamp follows its four-tick off delay.
	touch.all = 0
	for tick := 1; tick <= 10; tick++ {
		clock++
		p.Tick(clock)
	}
	_, plateProps = rsNameAt(t, w, 12, 63, 9)
	if got := plateProps["power"]; got != "0" {
		t.Fatalf("plate still carries power %q ten ticks after the item left, want 0", got)
	}
	for tick := 1; tick <= 4; tick++ {
		clock++
		p.Tick(clock)
	}
	_, lampProps = rsNameAt(t, w, 11, 63, 9)
	if lampProps["lit"] != "false" {
		t.Fatalf("lamp never went dark: %v", lampProps)
	}
}

// TestLeverStronglyPowersTheBlockTheLampHangsUnder pulls a lever on a stone
// block with the lamp beneath it: the lever strongly powers the stone and the
// lamp, whose only neighbour is the stone, reads that strong power through
// the conductor and lights. vanilla: SignalGetter.getSignal/getDirectSignalTo.
func TestLeverStronglyPowersTheBlockTheLampHangsUnder(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	setBlock(t, w, p, 12, 64, 9, rsState(t, "minecraft:stone", nil))
	setBlock(t, w, p, 12, 63, 9, rsState(t, "minecraft:redstone_lamp", map[string]string{"lit": "false"}))
	setBlock(t, w, p, 12, 65, 9, rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "false"}))
	for clock < 10 {
		clock++
		p.Tick(clock)
	}
	if _, props := rsNameAt(t, w, 12, 63, 9); props["lit"] != "false" {
		t.Fatalf("lamp lit before the lever was pulled: %v", props)
	}

	if used, _, _, _ := p.UseBlock(12, 65, 9, "north"); !used {
		t.Fatalf("the lever did not answer the use")
	}
	for range 4 {
		clock++
		p.Tick(clock)
	}
	if _, props := rsNameAt(t, w, 12, 63, 9); props["lit"] != "true" {
		t.Fatalf("lamp under the strongly powered stone never lit: %v", props)
	}
}

// TestBurntOutTorchRelightsAfterTheRestartDelay burns a torch out by toggling
// the block under it and checks that it lights again once the 160-tick restart
// delay has passed: the tick drops a torch's toggles older than sixty ticks
// before it reads them, so by then the unlit branch sees a clear record.
// vanilla: RedstoneTorchBlock.tick
func TestBurntOutTorchRelightsAfterTheRestartDelay(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	stone := rsState(t, "minecraft:stone", nil)
	powered := rsState(t, "minecraft:redstone_block", nil)
	setBlock(t, w, p, 12, 63, 9, rsState(t, "minecraft:redstone_torch", nil))
	for clock < 10 {
		clock++
		p.Tick(clock)
	}
	// Three ticks of power and three of stone per cycle: eight off-toggles
	// inside sixty ticks burn the torch out.
	for range 10 {
		setBlock(t, w, p, 12, 62, 9, powered)
		for range 3 {
			clock++
			p.Tick(clock)
		}
		setBlock(t, w, p, 12, 62, 9, stone)
		for range 3 {
			clock++
			p.Tick(clock)
		}
	}
	if _, props := rsNameAt(t, w, 12, 63, 9); props["lit"] != "false" {
		t.Fatalf("setup: the torch did not burn out: %v", props)
	}
	// Unpowered and dark, it waits out the restart delay and then relights.
	for clock < 500 {
		clock++
		p.Tick(clock)
	}
	if _, props := rsNameAt(t, w, 12, 63, 9); props["lit"] != "true" {
		t.Fatalf("the burnt-out torch never relit: %v", props)
	}
}
