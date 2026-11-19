package world

import (
	"testing"

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
