package world

import (
	"slices"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// reopenWorld saves w, closes it and opens the directory again with the chunks
// around the origin held and a fresh physics engine.
func reopenWorld(t *testing.T, w *World, dir string) (*World, *Physics) {
	t.Helper()
	return reopenWorldAround(t, w, dir, -1, 2)
}

// reopenWorldAround is reopenWorld holding chunks lo..hi on each axis.
func reopenWorldAround(t *testing.T, w *World, dir string, lo, hi int32) (*World, *Physics) {
	t.Helper()
	w.Save()
	w.Close()
	w2, err := NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	t.Cleanup(func() { w2.Close() })
	for cx := lo; cx <= hi; cx++ {
		for cz := lo; cz <= hi; cz++ {
			w2.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	return w2, NewPhysics(w2)
}

func newSavedWorld(t *testing.T) (*World, *Physics, string) {
	t.Helper()
	return newSavedWorldAround(t, -1, 2)
}

// newSavedWorldAround is newSavedWorld holding chunks lo..hi on each axis.
func newSavedWorldAround(t *testing.T, lo, hi int32) (*World, *Physics, string) {
	t.Helper()
	t.Parallel()
	dir := t.TempDir()
	w, err := NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	for cx := lo; cx <= hi; cx++ {
		for cz := lo; cz <= hi; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	return w, NewPhysics(w), dir
}

// TestRepeaterMidDelaySurvivesASaveAndReload stops the world with a repeater's
// tick queued and checks the reloaded world still turns it on.
func TestRepeaterMidDelaySurvivesASaveAndReload(t *testing.T) {
	w, p, dir := newSavedWorld(t)
	var clock int64
	lever := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "false"})
	wire := rsState(t, "minecraft:redstone_wire", map[string]string{"east": "side", "west": "side"})
	repeater := rsState(t, "minecraft:repeater", map[string]string{"facing": "east", "delay": "4", "powered": "false"})
	lamp := rsState(t, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	rsBuildLine(t, w, p, map[[3]int32]uint16{
		{16, 63, 9}: lever, {15, 63, 9}: wire, {14, 63, 9}: repeater,
		{13, 63, 9}: wire, {12, 63, 9}: wire, {11, 63, 9}: wire, {10, 63, 9}: lamp,
	})
	for clock < 10 {
		clock++
		p.Tick(clock)
	}
	powered := rsState(t, "minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "true"})
	old := w.BlockAt(16, 63, 9)
	w.SetBlock(16, 63, 9, powered)
	p.NotifyChanged(16, 63, 9, old)
	clock++
	p.Tick(clock)
	if _, props := rsNameAt(t, w, 14, 63, 9); props["powered"] != "false" {
		t.Fatalf("setup: the repeater is already on: %v", props)
	}
	if len(p.chunkTicks(ChunkPos{X: 0, Z: 0}).block) == 0 {
		t.Fatal("setup: the repeater has no tick queued")
	}

	w2, p2 := reopenWorld(t, w, dir)
	if _, props := rsNameAt(t, w2, 14, 63, 9); props["powered"] != "false" {
		t.Fatalf("the repeater is on after the reload: %v", props)
	}
	for range 12 {
		clock++
		p2.Tick(clock)
	}
	if _, props := rsNameAt(t, w2, 14, 63, 9); props["powered"] != "true" {
		t.Errorf("the repeater never turned on after the reload: %v", props)
	}
	if _, props := rsNameAt(t, w2, 10, 63, 9); props["lit"] != "true" {
		t.Errorf("the lamp never lit after the reload: %v", props)
	}
}

// TestHalfSpreadWaterContinuesAfterAReload saves the world with a water flow
// between two waves and checks it goes on spreading when loaded.
func TestHalfSpreadWaterContinuesAfterAReload(t *testing.T) {
	w, p, dir := newSavedWorld(t)
	var clock int64
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	for range 6 {
		clock++
		p.Tick(clock)
	}
	if got := len(p.chunkTicks(ChunkPos{X: 0, Z: 0}).fluid); got == 0 {
		t.Fatal("setup: the flow has no fluid tick queued")
	}
	w2, p2 := reopenWorld(t, w, dir)
	before := 0
	for x := int32(-8); x <= 8; x++ {
		if fluidHolding(w2.BlockAt(x, GroundLevel, 0)).kind != fluidNone {
			before++
		}
	}
	for range 40 {
		clock++
		p2.Tick(clock)
	}
	after := 0
	for x := int32(-8); x <= 8; x++ {
		if fluidHolding(w2.BlockAt(x, GroundLevel, 0)).kind != fluidNone {
			after++
		}
	}
	if after <= before {
		t.Errorf("the flow stopped at %d cells after the reload, want it to spread past %d", after, before)
	}
}

func TestWaterloggedScaffoldingKeepsBothTicksAfterReload(t *testing.T) {
	w, p, dir := newSavedWorld(t)
	state := scaffoldingTestState(t, map[string]string{"distance": "5", "waterlogged": "true"})
	setBlock(t, w, p, 8, GroundLevel, 8, state)

	w, p = reopenWorld(t, w, dir)
	p.Tick(0) // Adopt the saved delays before advancing game time.
	p.Tick(1)
	checkScaffold(t, w, 8, GroundLevel, 8, "0", "false", "stability after reload")
	for tick := int64(2); tick <= 4; tick++ {
		p.Tick(tick)
	}
	if got := w.BlockAt(9, GroundLevel, 8); got != airState {
		t.Errorf("outlet at tick four = %d, want air", got)
	}
	p.Tick(5)
	if got := w.BlockAt(9, GroundLevel, 8); got != waterByLevel[1] {
		t.Errorf("outlet at tick five = %d, want water level 1", got)
	}
}

// TestUnloadingAChunkDropsItsQueuedTicks checks a tick booked in a chunk does
// not stay on the queue once the chunk leaves memory.
func TestUnloadingAChunkDropsItsQueuedTicks(t *testing.T) {
	w, p, _ := newSavedWorld(t)
	t.Cleanup(func() { w.Close() })
	setBlock(t, w, p, 40, GroundLevel, 40, waterByLevel[0])
	pos := ChunkPos{X: 2, Z: 2}
	w.Acquire(pos)
	w.Release(pos)
	w.Release(pos)
	w.FlushSaves()
	if w.ChunkLoaded(pos) {
		t.Fatal("setup: the chunk is still loaded")
	}
	p.Tick(1)
	p.mu.Lock()
	n := len(p.queue)
	pending := len(p.pending)
	p.mu.Unlock()
	if n != 0 || pending != 0 {
		t.Errorf("queue holds %d ticks and %d bookings for the unloaded chunk, want none", n, pending)
	}
}

// TestPistonEventBookingTakesUnloadsFirst checks a piston event booked after a
// chunk unloaded discards the events that chunk held.
func TestPistonEventBookingTakesUnloadsFirst(t *testing.T) {
	w, p, _ := newSavedWorld(t)
	t.Cleanup(func() { w.Close() })
	p.schedulePistonEvent(40, GroundLevel, 40, 1, 0)
	key := blockKey{40, GroundLevel, 40}
	p.mu.Lock()
	_, booked := p.pendingEvent[key]
	p.mu.Unlock()
	if !booked {
		t.Fatal("setup: the event was not booked")
	}
	pos := ChunkPos{X: 2, Z: 2}
	w.Acquire(pos)
	w.Release(pos)
	w.Release(pos)
	w.FlushSaves()
	if w.ChunkLoaded(pos) {
		t.Fatal("setup: the chunk is still loaded")
	}
	p.schedulePistonEvent(0, GroundLevel, 0, 1, 0)
	p.mu.Lock()
	_, stale := p.pendingEvent[key]
	_, fresh := p.pendingEvent[blockKey{0, GroundLevel, 0}]
	p.mu.Unlock()
	if stale || !fresh {
		t.Errorf("stale event kept = %v, new event booked = %v; want false, true", stale, fresh)
	}
}

// fallsAfterReload removes the hold of a stalactite at (x, y, z), saves and
// reloads the world and returns how many stalactites fall within 10 ticks.
func fallsAfterReload(t *testing.T, waterlogged bool) int {
	t.Helper()
	w, p, dir := newSavedWorld(t)
	stone := w.BlockAt(0, GroundLevel-1, 0)
	state, ok := registry.FindState("minecraft:pointed_dripstone", map[string]string{
		"vertical_direction": "down", "thickness": "tip", "waterlogged": map[bool]string{false: "false", true: "true"}[waterlogged],
	})
	if !ok {
		t.Fatal("no stalactite state")
	}
	setBlock(t, w, p, 8, GroundLevel+2, 0, stone)
	setBlock(t, w, p, 8, GroundLevel+1, 0, state)
	setBlock(t, w, p, 8, GroundLevel+2, 0, airState)
	if !p.fallBooked[blockKey{8, GroundLevel + 1, 0}] {
		t.Fatal("setup: no fall booked")
	}
	_, p2 := reopenWorld(t, w, dir)
	falls := 0
	for clock := int64(1); clock <= 10; clock++ {
		_, f, _, _ := p2.Tick(clock)
		falls += len(f)
	}
	return falls
}

// BenchmarkChunkTicksDistant saves one chunk while n ticks wait in other
// chunks. "warm" repeats the save of an unchanged heap; "mutated" changes the
// heap before each of three consecutive saves, so the first scan and the
// index build run under the physics mutex as well.
func BenchmarkChunkTicksDistant(b *testing.B) {
	for _, mode := range []string{"warm", "mutated"} {
		for _, n := range []int{0, 4096, 65536} {
			b.Run(mode+"/"+strconv.Itoa(n), func(b *testing.B) {
				w := newTestWorld(b, NewFlatGenerator())
				b.Cleanup(func() { w.Close() })
				w.Acquire(ChunkPos{})
				p := NewPhysics(w)
				water := fluidTypeOf(fluidHolding(waterByLevel[0]))
				p.mu.Lock()
				p.insertTick(blockKey{0, 64, 0}, water, 200, tickPriorityNormal)
				for i := range int32(n) {
					p.insertTick(blockKey{16 + i%256, 64, i / 256}, water, 200, tickPriorityNormal)
				}
				p.mu.Unlock()
				save := func() {
					if got := p.chunkTicks(ChunkPos{}); len(got.fluid) != 1 || len(got.block) != 0 {
						b.Fatalf("got %d fluid, %d block entries, want one fluid", len(got.fluid), len(got.block))
					}
				}
				save()
				save()
				b.ReportAllocs()
				for b.Loop() {
					if mode == "mutated" {
						p.mu.Lock()
						p.pushTick(scheduledTick{pos: blockKey{16, 64, 0}, due: 0, seq: -1})
						p.popTick()
						p.mu.Unlock()
						save()
						save()
					}
					save()
				}
			})
		}
	}
}

// soilTickRow books, at z, a farmland tick at x=8 whose cell a dirt path then
// replaces, a path tick at VERY_HIGH at x=9, one at VERY_LOW (the piston
// landing's old priority) at x=10, and a plain one at x=11; all are due at 5.
func soilTickRow(w *World, p *Physics, z int32) {
	const y = GroundLevel
	w.SetBlock(8, y, z, registry.BlockFarmland.DefaultState())
	for x := int32(9); x <= 11; x++ {
		w.SetBlock(x, y, z, registry.BlockDirtPath.DefaultState())
	}
	p.mu.Lock()
	p.scheduleSoil(registry.BlockFarmland, 8, y, z, 5, tickPriorityNormal)
	p.scheduleSoil(registry.BlockDirtPath, 9, y, z, 5, tickPriorityVeryHigh)
	p.scheduleSoil(registry.BlockDirtPath, 10, y, z, 5, 2)
	p.scheduleSoil(registry.BlockDirtPath, 11, y, z, 5, tickPriorityNormal)
	p.mu.Unlock()
	w.SetBlock(8, y, z, registry.BlockDirtPath.DefaultState())
}

// wantBlocks reports each cell of row z at GroundLevel from x=8 that does not
// hold the block named.
func wantBlocks(t *testing.T, w *World, z int32, names ...string) {
	t.Helper()
	for i, want := range names {
		x := int32(8 + i)
		if got, _, _ := registry.BlockStateName(w.BlockAt(x, GroundLevel, z)); got != want {
			t.Errorf("block at (%d, %d) = %s, want %s", x, z, got, want)
		}
	}
}

// TestCopiedSoilTickKeepsItsBlockAndPriority copies soil ticks onto dirt paths
// and onto stone, and runs them: each keeps its booked type and priority, and
// dispatch checks the cell, so a path tick turns a path restored before it
// fires.
// vanilla: LevelTicks.copyAreaFrom, ServerLevel.tickBlock, TickPriority.VERY_LOW
func TestCopiedSoilTickKeepsItsBlockAndPriority(t *testing.T) {
	w, p := newOneChunkPhysics(t)
	const y = GroundLevel
	soilTickRow(w, p, 8)
	ticks := p.BlockTicksIn(8, y, 8, 11, y, 8)
	ids := map[int32]string{}
	for _, bt := range ticks {
		ids[bt.tick.pos.x] = bt.id
	}
	if ids[8] != "minecraft:farmland" || ids[9] != "minecraft:dirt_path" || ids[10] != "minecraft:dirt_path" ||
		ids[11] != "minecraft:dirt_path" {
		t.Errorf("copied ticks name %v, want farmland at 8 and dirt_path at 9 to 11", ids)
	}
	for x := int32(8); x <= 11; x++ {
		w.SetBlock(x, y, 10, registry.BlockDirtPath.DefaultState())
		w.SetBlock(x, y, 12, registry.BlockStone.DefaultState())
	}
	p.ScheduleBlockTicks(ticks, 0, 0, 2)
	p.ScheduleBlockTicks(ticks, 0, 0, 4)
	w.SetBlock(9, y, 12, registry.BlockDirtPath.DefaultState())
	p.mu.Lock()
	for _, tk := range p.queue {
		want, ok := map[blockKey]int{{9, y, 10}: tickPriorityVeryHigh, {10, y, 10}: 2}[tk.pos]
		if ok && tk.priority != want {
			t.Errorf("copied tick at %v has priority %d, want %d", tk.pos, tk.priority, want)
		}
	}
	p.mu.Unlock()
	for clock := int64(1); clock <= 5; clock++ {
		p.Tick(clock)
	}
	wantBlocks(t, w, 8, "minecraft:dirt_path", "minecraft:dirt", "minecraft:dirt", "minecraft:dirt")
	wantBlocks(t, w, 10, "minecraft:dirt_path", "minecraft:dirt", "minecraft:dirt", "minecraft:dirt")
	wantBlocks(t, w, 12, "minecraft:stone", "minecraft:dirt", "minecraft:stone", "minecraft:stone")
}

// TestSavedSoilTickKeepsItsBlockAndPriority saves soil ticks, one in a cell
// holding stone, and runs them after a reload, copying the VERY_LOW one: each
// keeps its booked type and priority, and a path back in the stone's cell
// before the tick fires turns to dirt.
// vanilla: SavedTick.loadTick, LevelChunkTicks.<init>, ServerLevel.tickBlock
func TestSavedSoilTickKeepsItsBlockAndPriority(t *testing.T) {
	w, p, dir := newSavedWorldAround(t, 0, 0)
	const y = GroundLevel
	soilTickRow(w, p, 8)
	w.SetBlock(11, y, 8, registry.BlockStone.DefaultState())

	w, p = reopenWorldAround(t, w, dir, 0, 0)
	p.Tick(0)
	w.SetBlock(11, y, 8, registry.BlockDirtPath.DefaultState())
	w.SetBlock(10, y, 10, registry.BlockDirtPath.DefaultState())
	ticks := p.BlockTicksIn(10, y, 8, 10, y, 8)
	if len(ticks) != 1 || ticks[0].tick.priority != 2 || ticks[0].id != "minecraft:dirt_path" {
		t.Fatalf("copied ticks = %+v, want one dirt path tick at priority 2", ticks)
	}
	p.ScheduleBlockTicks(ticks, 0, 0, 2)
	p.mu.Lock()
	for _, tk := range p.queue {
		if tk.pos == (blockKey{9, y, 8}) && tk.priority != tickPriorityVeryHigh {
			t.Errorf("reloaded path tick has priority %d, want %d", tk.priority, tickPriorityVeryHigh)
		}
	}
	p.mu.Unlock()
	for clock := int64(1); clock <= 5; clock++ {
		p.Tick(clock)
	}
	wantBlocks(t, w, 8, "minecraft:dirt_path", "minecraft:dirt", "minecraft:dirt", "minecraft:dirt")
	if got, _, _ := registry.BlockStateName(w.BlockAt(10, y, 10)); got != "minecraft:dirt" {
		t.Errorf("copied VERY_LOW path = %s, want minecraft:dirt", got)
	}
}

// TestBookedTickKeepsItsBlockWhenReplaced checks a generic tick booked for one
// block is not copied as another's after the cell changed.
func TestBookedTickKeepsItsBlockWhenReplaced(t *testing.T) {
	w, p := newOneChunkPhysics(t)
	const y = GroundLevel
	w.SetBlock(8, y, 8, registry.BlockStone.DefaultState())
	p.mu.Lock()
	p.insertTick(blockKey{8, y, 8}, 0, p.now+5, tickPriorityNormal)
	p.mu.Unlock()
	w.SetBlock(8, y, 8, registry.BlockDirtPath.DefaultState())
	ticks := p.BlockTicksIn(8, y, 8, 8, y, 8)
	if len(ticks) != 1 || ticks[0].id != registry.BlockStone.Name() {
		t.Fatalf("copied ticks = %v, want one booked for stone", ticks)
	}
	w.SetBlock(8, y, 10, registry.BlockDirtPath.DefaultState())
	p.ScheduleBlockTicks(ticks, 0, 0, 2)
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, tk := range p.queue {
		if tk.pos == (blockKey{8, y, 10}) {
			t.Errorf("a tick booked for stone was copied onto a dirt path: %+v", tk)
		}
	}
}

// TestTaggedTickKeepsItsBlockThroughCopyAndSave books a chorus flower's
// survival tick, puts a dirt path in its cell, copies the cell and reloads: the
// tick stays booked for the flower, so neither path takes it.
// vanilla: ScheduledTick.type, LevelTicks.copyAreaFrom, ServerLevel.tickBlock
func TestTaggedTickKeepsItsBlockThroughCopyAndSave(t *testing.T) {
	w, p, dir := newSavedWorldAround(t, 0, 0)
	const y = GroundLevel
	w.SetBlock(8, y, 8, registry.BlockChorusFlower.DefaultState())
	p.mu.Lock()
	p.scheduleSurvival(8, y, 8, p.now+1)
	p.mu.Unlock()
	w.SetBlock(8, y, 8, registry.BlockDirtPath.DefaultState())
	ticks := p.BlockTicksIn(8, y, 8, 8, y, 8)
	if len(ticks) != 1 || ticks[0].id != registry.BlockChorusFlower.Name() {
		t.Fatalf("copied ticks = %+v, want one booked for the chorus flower", ticks)
	}
	w.SetBlock(8, y, 10, registry.BlockDirtPath.DefaultState())
	p.ScheduleBlockTicks(ticks, 0, 0, 2)

	w, p = reopenWorldAround(t, w, dir, 0, 0)
	for clock := int64(0); clock <= 2; clock++ {
		p.Tick(clock)
	}
	for _, z := range []int32{8, 10} {
		if got, _, _ := registry.BlockStateName(w.BlockAt(8, y, z)); got != "minecraft:dirt_path" {
			t.Errorf("path at z=%d = %s, want minecraft:dirt_path", z, got)
		}
	}
}

// TestTiedTicksKeepBookingOrderAcrossReload books two visits with the same
// due and priority around heap reorderings and checks the file lists them in
// booking order (the order vanilla packs them in).
func TestTiedTicksKeepBookingOrderAcrossReload(t *testing.T) {
	w, p, dir := newSavedWorldAround(t, 0, 0)
	waterSource := fluidType(fluidWater)<<1 | 1
	// Booked out of coordinate order so a position sort cannot pass.
	p.mu.Lock()
	p.insertTick(blockKey{3, GroundLevel, 1}, waterSource, 100, 0) // A
	p.insertTick(blockKey{1, GroundLevel, 1}, waterSource, 50, 0)
	p.insertTick(blockKey{4, GroundLevel, 1}, waterSource, 100, 0) // B
	p.insertTick(blockKey{2, GroundLevel, 1}, waterSource, 25, 0)
	p.mu.Unlock()

	w, _ = reopenWorldAround(t, w, dir, 0, 0)
	var got []int32
	for _, st := range w.ticksOfChunk(ChunkPos{}).fluid {
		got = append(got, st.x)
	}
	if want := []int32{3, 1, 4, 2}; !slices.Equal(got, want) {
		t.Fatalf("reloaded fluid ticks x = %v, want %v", got, want)
	}
}

// TestTiedTicksDispatchInBookingOrderAfterReload books two sand falls due the
// same tick, with shorter fluid visits between them, and checks that after a
// save and reload the first-booked sand is the first to fall.
func TestTiedTicksDispatchInBookingOrderAfterReload(t *testing.T) {
	w, p, dir := newSavedWorldAround(t, 0, 0)
	sand, _ := registry.FindState("minecraft:sand", nil)
	waterSource := fluidType(fluidWater)<<1 | 1
	y := int32(GroundLevel + 4)
	// Booked second-to-first by x so a position sort cannot pass.
	setBlock(t, w, p, 9, y, 1, sand)
	p.mu.Lock()
	p.insertTick(blockKey{2, GroundLevel, 1}, waterSource, p.now+1, 0)
	p.mu.Unlock()
	setBlock(t, w, p, 3, y, 1, sand)
	p.mu.Lock()
	p.insertTick(blockKey{4, GroundLevel, 1}, waterSource, p.now+1, 0)
	p.mu.Unlock()

	_, p = reopenWorldAround(t, w, dir, 0, 0)
	var got []int32
	for clock := int64(1); clock <= 10 && len(got) < 2; clock++ {
		_, falls, _, _ := p.Tick(clock)
		for _, f := range falls {
			got = append(got, f.X)
		}
	}
	if len(got) != 2 || got[0] != 9 || got[1] != 3 {
		t.Fatalf("sand fell in x order %v, want [9 3]", got)
	}
}

// TestSavedSoilTickPriorityIsClamped adopts a path tick saved at p=5: it
// reads as the lowest vanilla priority, so a copy and a clear both see it.
// vanilla: TickPriority.byValue, LevelTicks.copyAreaFrom, LevelTicks.clearArea
func TestSavedSoilTickPriorityIsClamped(t *testing.T) {
	w, p := newOneChunkPhysics(t)
	const y = GroundLevel
	w.SetBlock(8, y, 8, registry.BlockDirtPath.DefaultState())
	p.mu.Lock()
	p.adoptBlockTick(8, y, 8, w.BlockAt(8, y, 8), registry.BlockDirtPath.Name(), p.now+1, 5)
	p.mu.Unlock()
	if ticks := p.BlockTicksIn(8, y, 8, 8, y, 8); len(ticks) != 1 || ticks[0].tick.priority != 3 {
		t.Fatalf("copied ticks = %+v, want one at priority 3", ticks)
	}
	p.ClearTicksIn(8, y, 8, 8, y, 8)
	p.Tick(p.now + 1)
	if got, _, _ := registry.BlockStateName(w.BlockAt(8, y, 8)); got != "minecraft:dirt_path" {
		t.Errorf("cleared path became %s, want minecraft:dirt_path", got)
	}
}

// TestSavedAirTickKeepsAirThroughCopyAndSave adopts a saved air tick, puts an
// uncovered path in its cell and checks the copy and the save still name air,
// so the path is not turned to dirt.
func TestSavedAirTickKeepsAirThroughCopyAndSave(t *testing.T) {
	w, p, dir := newSavedWorldAround(t, 0, 0)
	const y = GroundLevel
	p.mu.Lock()
	p.adoptBlockTick(8, y, 8, w.BlockAt(8, y, 8), registry.BlockAir.Name(), p.now+1, 0)
	p.mu.Unlock()
	w.SetBlock(8, y, 8, registry.BlockDirtPath.DefaultState())
	ticks := p.BlockTicksIn(8, y, 8, 8, y, 8)
	if len(ticks) != 1 || ticks[0].id != registry.BlockAir.Name() {
		t.Fatalf("copied ticks = %+v, want one booked for air", ticks)
	}
	w.SetBlock(8, y, 10, registry.BlockDirtPath.DefaultState())
	p.ScheduleBlockTicks(ticks, 0, 0, 2)

	w, p = reopenWorldAround(t, w, dir, 0, 0)
	for clock := int64(0); clock <= 2; clock++ {
		p.Tick(clock)
	}
	for _, z := range []int32{8, 10} {
		if got, _, _ := registry.BlockStateName(w.BlockAt(8, y, z)); got != "minecraft:dirt_path" {
			t.Errorf("path at z=%d = %s, want minecraft:dirt_path", z, got)
		}
	}
}

// cleanChunk marks the chunk at pos as saved, so ChunkDirty reads only what
// follows.
func cleanChunk(w *World, pos ChunkPos) {
	w.mu.Lock()
	loaded := w.chunks[pos]
	loaded.savedGen.Store(loaded.dirtyGen.Load())
	w.mu.Unlock()
}

// unloadChunk2 loads and releases chunk (2, 2), leaving it in the world's
// unloaded list.
func unloadChunk2(t *testing.T, w *World) ChunkPos {
	t.Helper()
	pos := ChunkPos{X: 2, Z: 2}
	w.Acquire(pos)
	w.Release(pos)
	w.FlushSaves()
	if w.ChunkLoaded(pos) {
		t.Fatal("setup: the chunk is still loaded")
	}
	return pos
}

// TestPrepareTickFlags checks the lock-free path marks the chunk dirty and
// takes nothing, and the pending flags follow the unload and loaded-tick lists.
func TestPrepareTickFlags(t *testing.T) {
	w, _, _ := newSavedWorldAround(t, 0, 0)
	t.Cleanup(func() { w.Close() })
	home := ChunkPos{}
	cleanChunk(w, home)
	if loaded, unloaded := w.prepareTick(0, 0, false, true); loaded != nil || unloaded != nil || w.ChunkDirty(home) {
		t.Fatalf("idle prepareTick(dirty=false) = %v, %v, dirty %v; want nil, nil, false", loaded, unloaded, w.ChunkDirty(home))
	}
	if loaded, unloaded := w.prepareTick(0, 0, true, true); loaded != nil || unloaded != nil {
		t.Fatalf("idle prepareTick = %v, %v; want nil, nil", loaded, unloaded)
	}
	if !w.ChunkDirty(home) {
		t.Error("the lock-free path left the chunk clean")
	}

	one := loadedTick{id: "minecraft:water", x: 1, delay: 1, fluid: true}
	two := one
	two.x, two.z = 40, 40 // chunk (2, 2)
	w.mu.Lock()
	w.queueLoadedTicks(&Chunk{loadedTicks: []loadedTick{one, two}})
	w.mu.Unlock()
	if !w.loadedTicksPending.Load() {
		t.Fatal("queueLoadedTicks left the flag clear")
	}
	cleanChunk(w, home)
	if loaded, _ := w.prepareTick(0, 0, true, false); loaded != nil || !w.loadedTicksPending.Load() || !w.ChunkDirty(home) {
		t.Errorf("takeLoaded=false took %v, flag %v, dirty %v; want nothing, set, dirty", loaded, w.loadedTicksPending.Load(), w.ChunkDirty(home))
	}
	w.mu.Lock()
	w.dropChunkTicks(ChunkPos{X: 2, Z: 2})
	w.mu.Unlock()
	if !w.loadedTicksPending.Load() {
		t.Error("dropping one chunk's ticks cleared the flag with another's still pending")
	}
	w.mu.Lock()
	w.dropChunkTicks(home)
	w.mu.Unlock()
	if w.loadedTicksPending.Load() {
		t.Error("flag still set after the last pending tick was dropped")
	}

	w.mu.Lock()
	w.queueLoadedTicks(&Chunk{loadedTicks: []loadedTick{one}})
	w.mu.Unlock()
	loaded, unloaded := w.prepareTick(0, 0, false, true)
	if len(loaded) != 1 || len(unloaded) != 2 {
		t.Fatalf("prepareTick took %d loaded, %d unloaded; want 1, 2", len(loaded), len(unloaded))
	}
	if w.loadedTicksPending.Load() || w.unloadsPending.Load() {
		t.Error("flags still set after the lists were taken")
	}
}

// TestBookTickTakesUnloadsAndLoadedTicks checks a fluid booking after a chunk
// unloaded discards that chunk's bookings, adopts the loaded fluid ticks before
// deduplicating, and marks its chunk dirty on the lock-free path.
func TestBookTickTakesUnloadsAndLoadedTicks(t *testing.T) {
	w, p, _ := newSavedWorldAround(t, 0, 0)
	t.Cleanup(func() { w.Close() })
	water := fluidTypeNamed("minecraft:water")
	home := ChunkPos{}
	cleanChunk(w, home)
	p.mu.Lock()
	p.bookTick(8, GroundLevel, 8, water, 5, tickPriorityNormal)
	p.mu.Unlock()
	if !w.ChunkDirty(home) {
		t.Error("a lock-free booking left its chunk clean")
	}

	// A saved water visit at (9, y, 9) is adopted before the booking of the
	// same cell, which then finds it queued.
	w.mu.Lock()
	w.queueLoadedTicks(&Chunk{loadedTicks: []loadedTick{{
		id: "minecraft:water", x: 9, y: GroundLevel, z: 9, delay: 3,
		fluid: true,
	}}})
	w.mu.Unlock()
	p.mu.Lock()
	p.bookTick(9, GroundLevel, 9, water, 20, tickPriorityNormal)
	queued := len(p.queue)
	_, adopted := p.fluidBooked[fluidBook{blockKey{9, GroundLevel, 9}, water}]
	p.mu.Unlock()
	if !adopted || queued != 2 {
		t.Errorf("adopted = %v, queued = %d; want true, 2 (the saved visit, not a duplicate)", adopted, queued)
	}

	// A chunk with a booking unloads; the next booking discards it.
	p.mu.Lock()
	p.bookTick(40, GroundLevel, 40, water, 5, tickPriorityNormal)
	p.mu.Unlock()
	unloadChunk2(t, w)
	p.mu.Lock()
	p.bookTick(1, GroundLevel, 1, water, 5, tickPriorityNormal)
	_, stale := p.fluidBooked[fluidBook{blockKey{40, GroundLevel, 40}, water}]
	p.mu.Unlock()
	if stale {
		t.Error("the unloaded chunk's booking survived the next booking")
	}
}

// TestScheduleBookedForTakesUnloads checks a booked-set booking discards an
// unloaded chunk's ticks, leaves the loaded ticks for a fluid booking and marks
// its chunk dirty on the lock-free path.
func TestScheduleBookedForTakesUnloads(t *testing.T) {
	w, p, _ := newSavedWorldAround(t, 0, 0)
	t.Cleanup(func() { w.Close() })
	home := ChunkPos{}
	cleanChunk(w, home)
	booked := map[blockKey]bool{}
	p.mu.Lock()
	p.scheduleBookedFor(booked, 8, GroundLevel, 8, 5, tickPriorityNormal, 1, registry.BlockStone)
	p.mu.Unlock()
	if !w.ChunkDirty(home) {
		t.Error("a lock-free booking left its chunk clean")
	}

	p.mu.Lock()
	p.scheduleBookedFor(booked, 40, GroundLevel, 40, 5, tickPriorityNormal, 1, registry.BlockStone)
	p.mu.Unlock()
	unloadChunk2(t, w)
	w.mu.Lock()
	w.queueLoadedTicks(&Chunk{loadedTicks: []loadedTick{{
		id: "minecraft:water", x: 9, y: GroundLevel, z: 9,
		fluid: true,
	}}})
	w.mu.Unlock()
	before := len(p.queue)
	p.mu.Lock()
	p.scheduleBookedFor(booked, 1, GroundLevel, 1, 5, tickPriorityNormal, 1, registry.BlockStone)
	after := len(p.queue)
	p.mu.Unlock()
	if w.unloadsPending.Load() {
		t.Error("the unload was not taken")
	}
	if !w.loadedTicksPending.Load() {
		t.Error("a block booking took the loaded ticks")
	}
	// The unloaded chunk's tick is gone and one new tick joined.
	if after != before {
		t.Errorf("queue %d -> %d; want the stale tick replaced by the new one", before, after)
	}
}

// TestDuplicateBookingLeavesChunkClean checks a booking that deduplicates does
// not flag its chunk for a save, and a new one does.
func TestDuplicateBookingLeavesChunkClean(t *testing.T) {
	w, p, _ := newSavedWorldAround(t, 0, 0)
	t.Cleanup(func() { w.Close() })
	water := fluidTypeNamed("minecraft:water")
	home := ChunkPos{}
	for _, book := range []func(){
		func() { p.bookTick(8, GroundLevel, 8, water, 5, tickPriorityNormal) },
		func() { p.bookTick(9, GroundLevel, 8, 0, 5, tickPriorityNormal) },
		func() { p.scheduleSurvival(10, GroundLevel, 8, 5) },
	} {
		p.mu.Lock()
		cleanChunk(w, home)
		book()
		dirty := w.ChunkDirty(home)
		cleanChunk(w, home)
		book()
		dup := w.ChunkDirty(home)
		p.mu.Unlock()
		if !dirty || dup {
			t.Errorf("new booking dirty %v, duplicate dirty %v; want true, false", dirty, dup)
		}
	}
}
