package world

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// reopenWorld saves w, closes it and opens the directory again with the chunks
// around the origin held and a fresh physics engine.
func reopenWorld(t *testing.T, w *World, dir string) (*World, *Physics) {
	t.Helper()
	w.Save()
	w.Close()
	w2, err := NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	t.Cleanup(func() { w2.Close() })
	for cx := int32(-1); cx <= 2; cx++ {
		for cz := int32(-1); cz <= 2; cz++ {
			w2.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	return w2, NewPhysics(w2)
}

func newSavedWorld(t *testing.T) (*World, *Physics, string) {
	t.Helper()
	t.Parallel()
	dir := t.TempDir()
	w, err := NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	for cx := int32(-1); cx <= 2; cx++ {
		for cz := int32(-1); cz <= 2; cz++ {
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

// TestChunkWritesPendingTicksToItsFile reads the saved chunk back as NBT.
func TestChunkWritesPendingTicksToItsFile(t *testing.T) {
	w, p, _ := newSavedWorld(t)
	t.Cleanup(func() { w.Close() })
	setBlock(t, w, p, 3, GroundLevel, 3, waterByLevel[0])
	ticks := w.ticksOfChunk(ChunkPos{})
	if len(ticks.fluid) != 1 || ticks.fluid[0].id != "minecraft:water" {
		t.Fatalf("fluid ticks = %+v, want one for minecraft:water", ticks.fluid)
	}
	c, _ := w.Acquire(ChunkPos{})
	root, err := encodeChunkNBTWith(c, nil, ticks)
	if err != nil {
		t.Fatal(err)
	}
	if list, ok := nbt.Get[nbt.List](root, "fluid_ticks"); !ok || len(list.Items) != 1 {
		t.Errorf("fluid_ticks = %v, %v; want one entry", list, ok)
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

// TestBookedStalactiteFallSurvivesAReload checks a fall booked before a save
// still happens afterwards, plain and waterlogged.
func TestBookedStalactiteFallSurvivesAReload(t *testing.T) {
	for _, waterlogged := range []bool{false, true} {
		t.Run(fmt.Sprintf("waterlogged=%v", waterlogged), func(t *testing.T) {
			if got := fallsAfterReload(t, waterlogged); got != 1 {
				t.Errorf("%d stalactite falls after the reload, want 1", got)
			}
		})
	}
}

// TestLoadedFluidTickNeedsTheSameFluid checks a saved water tick is not taken
// for a cell that holds lava now.
func TestLoadedFluidTickNeedsTheSameFluid(t *testing.T) {
	w, p, _ := newSavedWorld(t)
	t.Cleanup(func() { w.Close() })
	setBlock(t, w, p, 3, GroundLevel, 3, lavaByLevel[0])
	p.mu.Lock()
	p.queue, p.pending = nil, map[blockKey]bool{}
	p.mu.Unlock()
	w.mu.Lock()
	w.pendingLoaded = []loadedTick{{id: "minecraft:water", x: 3, y: GroundLevel, z: 3, fluid: true}}
	w.mu.Unlock()
	p.mu.Lock()
	p.adoptLoadedTicks()
	n := len(p.queue)
	p.mu.Unlock()
	if n != 0 {
		t.Errorf("a water tick was queued for a lava cell: %d entries", n)
	}
}

// TestChunkLoadDropsTicksOutsideIt checks entries positioned in another chunk
// are not kept.
func TestChunkLoadDropsTicksOutsideIt(t *testing.T) {
	t.Parallel()
	c := flatChunk(0, 0)
	root, err := encodeChunkNBTWith(c, nil, chunkTicks{block: []savedTick{
		{id: "minecraft:stone", x: 1, y: 70, z: 1},
		{id: "minecraft:stone", x: 40, y: 70, z: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeChunkNBT(root)
	if err != nil || got == nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.loadedTicks) != 1 || got.loadedTicks[0].x != 1 {
		t.Errorf("loaded ticks = %+v, want only the one inside the chunk", got.loadedTicks)
	}
}

// TestBookingATickMarksItsChunkForASave checks a tick booked in a saved chunk
// makes the next save write it.
func TestBookingATickMarksItsChunkForASave(t *testing.T) {
	w, p, _ := newSavedWorld(t)
	t.Cleanup(func() { w.Close() })
	w.Save()
	pos := ChunkPos{}
	w.mu.RLock()
	dirty := w.chunks[pos].dirty
	w.mu.RUnlock()
	if dirty {
		t.Fatal("setup: the chunk is dirty after a save")
	}
	p.schedule(3, GroundLevel, 3, p.now+5)
	w.mu.RLock()
	dirty = w.chunks[pos].dirty
	w.mu.RUnlock()
	if !dirty {
		t.Error("booking a tick left the chunk clean")
	}
}
