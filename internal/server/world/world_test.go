package world

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestWorldGeneratesChunksOnDemand(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	if got := w.LoadedChunks(); got != 0 {
		t.Fatalf("loaded chunks = %d, want 0 before anyone is in the world", got)
	}

	pos := ChunkPos{X: 1, Z: -3}
	first, _ := w.Acquire(pos)
	if first.x != pos.X || first.z != pos.Z {
		t.Errorf("chunk at (%d, %d) = (%d, %d)", pos.X, pos.Z, first.x, first.z)
	}
	if len(first.encoded) == 0 {
		t.Error("chunk was not encoded when it was generated")
	}

	// A chunk somebody already has is handed out as it is, rather than
	// generated a second time.
	if second, _ := w.Acquire(pos); second != first {
		t.Error("acquiring a loaded chunk generated it again")
	}
	if got := w.LoadedChunks(); got != 1 {
		t.Errorf("loaded chunks = %d, want 1", got)
	}

	w.Release(pos)
	if got := w.LoadedChunks(); got != 1 {
		t.Errorf("loaded chunks = %d, want 1 while a player still has the chunk", got)
	}
	w.Release(pos)
	// The last release saves the generated chunk before it leaves memory.
	w.FlushSaves()
	if got := w.LoadedChunks(); got != 0 {
		t.Errorf("loaded chunks = %d, want 0 once nobody has the chunk", got)
	}

	// Coming back reads the same chunk back.
	again, _ := w.Acquire(pos)
	if !bytes.Equal(again.encoded, first.encoded) {
		t.Error("chunk read back differently the second time")
	}

	// Giving up a chunk that is not loaded is not worth a panic.
	w.Release(ChunkPos{X: 100, Z: 100})
}

// TestBlockChangeSurvivesReleaseAndReload checks that a block a player broke is
// written to the region file when the chunk is unloaded, and read back when it
// is loaded again — which is what makes a change outlive a walk away and back,
// and a server restart.
func TestBlockChangeSurvivesReleaseAndReload(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{X: 0, Z: 0}

	_, _ = w.Acquire(pos)
	w.SetBlock(5, GroundLevel-1, 3, airState) // dig a hole, mark the chunk dirty
	w.Release(pos)
	w.FlushSaves() // wait for the async save to finish

	if got := w.LoadedChunks(); got != 0 {
		t.Fatalf("loaded chunks = %d, want 0 after releasing a dirty chunk", got)
	}

	// Re-acquiring loads the chunk from disk, with the hole still in it.
	again, _ := w.Acquire(pos)
	if got := again.BlockAt(5, GroundLevel-1, 3); got != airState {
		t.Errorf("block after reload = %d, want %d (air)", got, airState)
	}
	// And the rest of the chunk is still stone below ground level.
	if got := again.BlockAt(0, worldMinY, 0); got != stoneState {
		t.Errorf("stone at bottom = %d, want %d", got, stoneState)
	}
	w.Release(pos)
}

// TestWorldSavesOnShutdown checks that a chunk still dirty in memory when the
// server shuts down is written to its region file by Save.
func TestWorldSavesOnShutdown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())
	pos := ChunkPos{X: -1, Z: 4}

	_, _ = w.Acquire(pos)
	w.SetBlock(2, worldMinY, 2, stoneState) // mark dirty
	// The chunk is still loaded (not released), so Save is what a shutdown
	// does: writes it while it is in memory.
	w.Save()
	w.Close()

	// A new world pointed at the same directory should load the chunk.
	w2, _ := NewWorld(dir, NewFlatGenerator())
	defer w2.Close()
	loaded, _ := w2.Acquire(pos)
	if got := loaded.BlockAt(2, worldMinY, 2); got != stoneState {
		t.Errorf("block after restart = %d, want %d (stone)", got, stoneState)
	}
}

// TestWorldLoadsVanillaChunk checks that a chunk written in the vanilla on-disk
// NBT format — named block palette, per-section biomes — is loaded with the
// right blocks, which is what dropping a vanilla world into the server folder
// does.
func TestWorldLoadsVanillaChunk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pos := ChunkPos{X: 7, Z: -2}

	// Build a chunk NBT compound: a single section with a two-entry block
	// palette (stone and dirt) and a packed data array, plus a biome palette.
	bottomY := int32(worldMinY / sectionSize)
	stone := nbt.Compound{"Name": nbt.String("minecraft:stone")}
	dirt := nbt.Compound{"Name": nbt.String("minecraft:dirt")}
	// 4096 entries at 4 bits each: 256 longs. Fill with 0 (stone) except
	// block (0, 0, 0) which is 1 (dirt).
	perLong := 64 / minIndirectBits
	longs := make(nbt.LongArray, blocksPerSection/perLong)
	longs[0] = 1 // entry 0 = dirt, the rest = stone (0)

	section := nbt.Compound{
		"Y": nbt.Byte(bottomY),
		"block_states": nbt.Compound{
			"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{stone, dirt}},
			"data":    longs,
		},
		"biomes": nbt.Compound{
			"palette": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:plains")}},
		},
	}
	root := nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"xPos":        nbt.Int(pos.X),
		"yPos":        nbt.Int(bottomY),
		"zPos":        nbt.Int(pos.Z),
		"Status":      nbt.String("minecraft:full"),
		"sections":    nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{section}},
	}

	// Write it to the region file.
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("nbt.Encode: %v", err)
	}
	rx, rz := regionCoords(pos)
	rf, err := openRegion(regionFilePath(DimensionDir(dir, DimensionName), rx, rz), defaultSaveFormat)
	if err != nil {
		t.Fatalf("openRegion: %v", err)
	}
	if err := rf.writeChunk(pos, data); err != nil {
		t.Fatalf("writeChunk: %v", err)
	}
	rf.close()

	// The world should load it and hand back the right blocks.
	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	c, _ := w.Acquire(pos)

	// Block (0, worldMinY, 0) should be dirt; everything else in that
	// section should be stone.
	if got := c.BlockAt(0, worldMinY, 0); got != registry.BlockStates["minecraft:dirt"] {
		t.Errorf("block at (0,%d,0) = %d, want dirt", got, worldMinY)
	}
	if got := c.BlockAt(1, worldMinY, 0); got != registry.BlockStates["minecraft:stone"] {
		t.Errorf("block at (1,%d,0) = %d, want stone", got, worldMinY)
	}
	// Above this section should be air (no sections were defined for it).
	if got := c.BlockAt(0, worldMinY+sectionSize, 0); got != airState {
		t.Errorf("block above the section = %d, want air", got)
	}
	w.Release(pos)
}

// TestWorldRegeneratesUngeneratedChunks checks that a chunk in a region file
// whose Status is not "minecraft:full" — a chunk a vanilla world visited but
// never finished generating — is regenerated rather than loaded as-is. Without
// this, the edge of a vanilla world would be full of empty chunks with no
// terrain instead of generated ones.
func TestWorldRegeneratesUngeneratedChunks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pos := ChunkPos{X: 3, Z: 4}

	// A chunk that was carved but never finished: it has a Status of
	// "minecraft:carved" and no sections at all, the shape an edge chunk in
	// a vanilla world takes.
	root := nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"xPos":        nbt.Int(pos.X),
		"zPos":        nbt.Int(pos.Z),
		"Status":      nbt.String("minecraft:carved"),
		"sections":    nbt.List{Elem: nbt.TypeCompound},
	}
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("nbt.Encode: %v", err)
	}
	rx, rz := regionCoords(pos)
	rf, err := openRegion(regionFilePath(DimensionDir(dir, DimensionName), rx, rz), defaultSaveFormat)
	if err != nil {
		t.Fatalf("openRegion: %v", err)
	}
	if err := rf.writeChunk(pos, data); err != nil {
		t.Fatalf("writeChunk: %v", err)
	}
	rf.close()

	// The world should not load the carved chunk; it should generate a fresh
	// one with the flat generator's stone.
	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	c, _ := w.Acquire(pos)
	if got := c.BlockAt(0, worldMinY, 0); got != stoneState {
		t.Errorf("block in ungenerated chunk = %d, want %d (stone from generation, not the air the file held)", got, stoneState)
	}
	w.Release(pos)
}

// TestTimeAdvancesAndWraps checks that TickTime advances WorldAge by one per
// call and the overworld clock's total ticks by one, growing without bound
// the way the vanilla server's ServerClockManager does — the client wraps at
// a full day for display.
func TestTimeAdvancesAndWraps(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	defer w.Close()

	// A new world starts at the dawn of day zero.
	if age, day := w.Time(); age != 0 || day != 0 {
		t.Fatalf("new world time = (%d, %d), want (0, 0)", age, day)
	}

	// Advancing 24000 ticks is one full day: WorldAge is 24000 and the
	// clock's total ticks is 24000 (unwrapped — the client wraps for display).
	for range ticksPerDay {
		w.TickTime()
	}
	if age, day := w.Time(); age != ticksPerDay || day != ticksPerDay {
		t.Errorf("after one day time = (%d, %d), want (%d, %d)", age, day, ticksPerDay, ticksPerDay)
	}

	// One more tick: both grow by one.
	w.TickTime()
	if age, day := w.Time(); age != ticksPerDay+1 || day != ticksPerDay+1 {
		t.Errorf("after one tick into day two time = (%d, %d), want (%d, %d)", age, day, ticksPerDay+1, ticksPerDay+1)
	}
}

// TestTimeSurvivesSaveAndReload checks that the world's clock is written to
// level.dat on Save and read back on the next load, so a restart picks up
// where the last server left off.
func TestTimeSurvivesSaveAndReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())

	// Advance the clock a bit, then save.
	for range 100 {
		w.TickTime()
	}
	w.Save()
	w.Close()

	again, _ := NewWorld(dir, NewFlatGenerator())
	defer again.Close()
	if age, day := again.Time(); age != 100 || day != 100 {
		t.Errorf("time after reload = (%d, %d), want (100, 100)", age, day)
	}
}

// TestClocksSurviveSaveAndReload checks that both clocks' totals, rates and
// paused flags are written to data/minecraft/world_clocks.dat on Save and
// read back on the next load, and that the level.dat the same save writes
// carries none of the clock — a vanilla level.dat keeps no field of it.
func TestClocksSurviveSaveAndReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())
	w.SetClockTime(OverworldClock, 986)
	w.SetClockRate(OverworldClock, 2)
	w.SetClockTime(TheEndClock, 4502)
	w.SetClockRate(TheEndClock, 0.5)
	w.SetClockPaused(TheEndClock, true)

	w.Save()
	w.Close()

	if _, err := os.Stat(filepath.Join(dir, worldClocksFile)); err != nil {
		t.Fatalf("world_clocks.dat was not written: %v", err)
	}
	level, err := LoadLevel(dir)
	if err != nil {
		t.Fatalf("LoadLevel: %v", err)
	}
	if _, ok := nbt.Get[nbt.Long](level.raw, "DayTime"); ok {
		t.Error("level.dat carries a DayTime, want the clock out of it")
	}
	if _, ok := nbt.Get[nbt.Float](level.raw, "ClockRate"); ok {
		t.Error("level.dat carries a ClockRate, want the clock out of it")
	}

	again, _ := NewWorld(dir, NewFlatGenerator())
	defer again.Close()
	if _, day := again.Time(); day != 986 {
		t.Errorf("overworld clock after reload = %d, want 986", day)
	}
	if got := again.Rate(); got != 2 {
		t.Errorf("overworld rate after reload = %v, want 2", got)
	}
	if _, total, _ := again.ClockTime(TheEndClock); total != 4502 {
		t.Errorf("the_end clock after reload = %d, want 4502", total)
	}
	if got := again.clock(TheEndClock).rate; got != 0.5 {
		t.Errorf("the_end rate after reload = %v, want 0.5", got)
	}
	if !again.clock(TheEndClock).paused {
		t.Error("the_end clock is running after reload, want paused")
	}
}

// TestClocksLoadVanillaWorldClocksDat checks that a world_clocks.dat in the
// vanilla shape — one compound per clock under the clock's wire name, its
// total_ticks, rate and paused inside — is read as the state it says, in the
// file a vanilla server keeps both clocks in.
func TestClocksLoadVanillaWorldClocksDat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, worldClocksFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	encoded, err := nbt.Encode("", nbt.Compound{
		"data": nbt.Compound{
			OverworldClock: nbt.Compound{
				"total_ticks": nbt.Long(123),
				"rate":        nbt.Float(1),
				"paused":      nbt.Byte(0),
			},
			TheEndClock: nbt.Compound{
				"total_ticks": nbt.Long(4502),
				"rate":        nbt.Float(0.5),
				"paused":      nbt.Byte(1),
			},
		},
	})
	if err != nil {
		t.Fatalf("encode world_clocks.dat: %v", err)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(encoded); err != nil {
		t.Fatalf("compress world_clocks.dat: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("compress world_clocks.dat: %v", err)
	}
	if err := os.WriteFile(path, compressed.Bytes(), 0o644); err != nil {
		t.Fatalf("write world_clocks.dat: %v", err)
	}

	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	if _, day := w.Time(); day != 123 {
		t.Errorf("overworld clock = %d, want 123", day)
	}
	if w.clock(OverworldClock).paused {
		t.Error("overworld clock is paused, want running")
	}
	if _, total, _ := w.ClockTime(TheEndClock); total != 4502 {
		t.Errorf("the_end clock = %d, want 4502", total)
	}
	if got := w.clock(TheEndClock).rate; got != 0.5 {
		t.Errorf("the_end rate = %v, want 0.5", got)
	}
	if !w.clock(TheEndClock).paused {
		t.Error("the_end clock is running, want paused")
	}
}

// TestAReleaseSaveRacingAGiveBackSavesTheChange checks the release's side of
// the dispense racing an unload: the release save reads the container while
// the taken item is gone from its slot, and the give-back a dispense then
// lands under the entity's lock — marked by TouchEntity in the same hold —
// has to reach the region file before the chunk leaves the world. The
// region file's lock parks the save between its read of the slots and its
// write, so the parked bytes are stale; dropping the chunk with them would
// lose the give-back.
func TestAReleaseSaveRacingAGiveBackSavesTheChange(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	defer w.Close()
	pos := ChunkPos{X: 0, Z: 0}
	_, _ = w.Acquire(pos)

	dispenserAt := [3]int32{3, GroundLevel, 5}
	if !w.SetBlock(dispenserAt[0], dispenserAt[1], dispenserAt[2], rsState(t, "minecraft:dispenser", nil)) {
		t.Fatal("setup: the dispenser block did not go in")
	}
	be := w.EnsureBlockEntity(dispenserAt[0], dispenserAt[1], dispenserAt[2], KindDispenser)
	if be == nil {
		t.Fatal("setup: no block entity came with the dispenser")
	}
	dirt, _ := registry.ItemID("minecraft:dirt")
	be.Use(func(b *BlockEntity) { b.Slots[0] = protocol.ItemStack{ItemID: dirt, Count: 1} })

	// The visit takes the item out before the act; the release save that
	// follows reads the slot already empty.
	be.Use(func(b *BlockEntity) { b.Slots[0] = protocol.ItemStack{} })

	// The leaving holder is gone, the way a Release says so with its users
	// count; the save itself runs here as Release's I/O task runs it, from
	// the generation the chunk stands at.
	w.mu.Lock()
	w.chunks[pos].users.Store(0)
	chunk := w.chunks[pos].chunk
	gen := w.chunks[pos].dirtyGen
	w.mu.Unlock()

	// The save parks inside its region write, which is past its read of
	// the block entities and short of the bytes landing.
	rx, rz := regionCoords(pos)
	rf, err := w.region(rx, rz)
	if err != nil {
		t.Fatal(err)
	}
	rf.mu.Lock()
	saveMark := make(chan string, 1)
	saved := make(chan struct{})
	go func() {
		saveMark <- goroutineMark()
		defer close(saved)
		w.unloadSaved(pos, chunk, gen)
	}()
	mark := <-saveMark
	buf := make([]byte, 1<<22)
	deadline := time.Now().Add(20 * time.Second)
	for !goroutineSitsIn(buf[:runtime.Stack(buf, true)], mark, "world.(*regionFile).writeChunk(") {
		if time.Now().After(deadline) {
			n := runtime.Stack(buf, true)
			t.Fatalf("the release save never reached its write:\n%s", buf[:n])
		}
		time.Sleep(time.Millisecond)
	}

	// The give-back lands under the entity's lock after the save's read,
	// the shape a dispense's commit gives it: TouchEntity first, and the
	// slot only when the entity is still the world's.
	be.Use(func(b *BlockEntity) {
		if w.TouchEntity(b) {
			b.Slots[0] = protocol.ItemStack{ItemID: dirt, Count: 1}
		}
	})
	rf.mu.Unlock()
	<-saved

	// The chunk left the world with the give-back in its bytes; loading it
	// back is the proof the re-save happened rather than the stale delete.
	if got := w.LoadedChunks(); got != 0 {
		t.Fatalf("loaded chunks = %d, want 0 once the released chunk is saved out", got)
	}
	_, _ = w.Acquire(pos)
	loaded := w.BlockEntity(dispenserAt[0], dispenserAt[1], dispenserAt[2])
	if loaded == nil {
		t.Fatal("the dispenser did not come back from the region file")
	}
	if !loaded.Slots[0].Equal(protocol.ItemStack{ItemID: dirt, Count: 1}) {
		t.Fatalf("the give-back raced the release save away: slot 0 = %+v, want one dirt", loaded.Slots[0])
	}
}

func TestBlockWritesOutsideBuildHeight(t *testing.T) {
	t.Parallel()
	plate := registry.BlockLightWeightedPressurePlate.DefaultState()
	cases := []struct {
		name  string
		world func(t *testing.T) *World
	}{
		{"overworld", func(t *testing.T) *World { return newTestWorld(t, NewFlatGenerator()) }},
		{"nether", func(t *testing.T) *World {
			w := NewDimensionWorld(t.TempDir(), true, NewFlatGenerator())
			t.Cleanup(w.FlushSaves)
			return w
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := tc.world(t)
			pos := ChunkPos{X: 0, Z: 0}
			_, _ = w.Acquire(pos)
			defer w.Release(pos)
			// Save clears dirty so any write bookkeeping afterwards stands out.
			w.Save()

			w.mu.RLock()
			loaded := w.chunks[pos]
			writesBefore := loaded.chunk.writes
			genBefore := loaded.containerGen.Load()
			w.mu.RUnlock()

			outOfBounds := []int32{w.MinY() - 1, w.MaxY()}
			w.JournalStart()
			for _, y := range outOfBounds {
				before := w.BlockAt(1, y, 2)
				if old, changed := w.SwapBlock(1, y, 2, plate); old != 0 || changed {
					t.Errorf("SwapBlock(1, %d, 2) = (%d, %v), want (0, false)", y, old, changed)
				}
				if w.SetBlock(1, y, 2, plate) {
					t.Errorf("SetBlock(1, %d, 2) = true, want false", y)
				}
				if got := w.BlockAt(1, y, 2); got != before {
					t.Errorf("BlockAt(1, %d, 2) = %d, want unchanged %d", y, got, before)
				}
			}
			if cells, _, _ := w.JournalStop(); len(cells) != 0 {
				t.Errorf("journal recorded %d out-of-bounds writes, want 0", len(cells))
			}

			batch := []BlockChange{
				{X: 2, Y: w.MinY() - 1, Z: 3, State: plate},
				{X: 2, Y: w.MaxY(), Z: 3, State: stoneState},
			}
			if applied := w.SetBlocks(batch); len(applied) != 0 {
				t.Errorf("SetBlocks out of bounds applied %d entries, want 0", len(applied))
			}
			if got := w.LoadedTouchBlocks(); got != 0 {
				t.Errorf("LoadedTouchBlocks = %d, want 0", got)
			}
			if w.ChunkDirty(pos) {
				t.Error("chunk marked dirty by out-of-bounds writes")
			}
			w.mu.RLock()
			if loaded.chunk.stale {
				t.Error("chunk marked stale by out-of-bounds writes")
			}
			if loaded.chunk.writes != writesBefore {
				t.Errorf("chunk writes = %d, want %d", loaded.chunk.writes, writesBefore)
			}
			if got := loaded.containerGen.Load(); got != genBefore {
				t.Errorf("containerGen = %d, want %d", got, genBefore)
			}
			w.mu.RUnlock()

			// A mixed batch applies only the in-bounds write.
			inY := w.MaxY() - 1
			mixed := []BlockChange{
				{X: 4, Y: w.MinY() - 1, Z: 5, State: stoneState},
				{X: 4, Y: inY, Z: 5, State: stoneState},
				{X: 4, Y: w.MaxY(), Z: 5, State: stoneState},
			}
			applied := w.SetBlocks(mixed)
			if len(applied) != 1 || applied[0].Y != inY {
				t.Fatalf("mixed SetBlocks applied %+v, want only y=%d", applied, inY)
			}
			if got := w.BlockAt(4, inY, 5); got != stoneState {
				t.Errorf("BlockAt(4, %d, 5) = %d, want stone", inY, got)
			}
			if !w.ChunkDirty(pos) {
				t.Error("chunk not marked dirty by in-bounds write")
			}
		})
	}
}

// newTestWorld makes a world in a fresh temporary directory that waits for
// its queued chunk saves before the directory is removed: a released chunk
// is saved asynchronously, and the test's own cleanup would otherwise race
// the write ("TempDir RemoveAll cleanup: directory not empty"). The flush is
// registered after the directory, so it runs first.
func newTestWorld(t testing.TB, gen Generator) *World {
	t.Helper()
	w, err := NewWorld(t.TempDir(), gen)
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	t.Cleanup(w.FlushSaves)
	return w
}

// encodeWhole encodes every section of the chunk, the full-height form the
// overworld's wire band is and the smaller dimensions' bands are not.
func encodeWhole(c *Chunk) []byte {
	return c.encodeChunkSections(0, len(c.sections))
}

// goroutineMark is the mark a stack dump opens the calling goroutine's block
// with — "goroutine N ", the trailing space keeping it from matching a
// longer id.
func goroutineMark() string {
	buf := make([]byte, 128)
	line, _, _ := strings.Cut(string(buf[:runtime.Stack(buf, false)]), "\n")
	mark, _, _ := strings.Cut(line, " [")
	return mark + " "
}

// goroutineSitsIn reports whether the dump's block for the marked goroutine
// alone contains frame: the block runs from its mark to the next goroutine's,
// so a parallel test's business in the same frames does not stand in for this
// test's own.
func goroutineSitsIn(dump []byte, mark, frame string) bool {
	start := bytes.Index(dump, []byte(mark))
	if start < 0 {
		return false
	}
	block := dump[start:]
	if end := bytes.Index(block, []byte("\ngoroutine ")); end >= 0 {
		block = block[:end]
	}
	return bytes.Contains(block, []byte(frame))
}

// waitSaveParked waits until the marked save is parked on a block entity's
// lock inside its chunk save, which is after the entities sorted before the
// stall's have been read. It fails if Save finishes without ever parking.
func waitSaveParked(t *testing.T, mark string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	buf := make([]byte, 1<<20)
	for {
		dump := buf[:runtime.Stack(buf, true)]
		if goroutineSitsIn(dump, mark, "world.(*BlockEntity).Use(") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the save never parked on a block entity:\n%s", dump)
		}
		time.Sleep(time.Millisecond)
	}
}
