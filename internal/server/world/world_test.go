package world

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

// TestAGeneratedChunkIsSavedOnce checks that a chunk that was only generated
// is written on its first unload, and that reading it back leaves nothing for
// the next unload to write while nothing changes it.
// vanilla: LevelChunk(ServerLevel, ProtoChunk, PostLoadProcessor)
func TestAGeneratedChunkIsSavedOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())
	pos := ChunkPos{X: 2, Z: 3}

	_, _ = w.Acquire(pos)
	w.Release(pos)
	w.FlushSaves()
	rx, rz := regionCoords(pos)
	if _, err := os.Stat(regionFilePath(dimensionDir(dir, DimensionName), rx, rz)); err != nil {
		t.Fatalf("region file for a generated chunk: %v, want it written", err)
	}

	again, _ := w.Acquire(pos)
	if w.FreshlyGenerated(pos) {
		t.Error("the chunk was generated again rather than read back")
	}
	if got := again.BlockAt(0, worldMinY, 0); got != stoneState {
		t.Errorf("read-back stone = %d, want %d", got, stoneState)
	}
	if w.ChunkDirty(pos) {
		t.Error("a chunk read back unchanged is waiting for a save")
	}
	w.Release(pos)
}

// TestADroppedUntouchedChunkIsNotSaved checks SetDropUntouchedChunks: a chunk
// that was only generated is thrown away on unload rather than written, and
// coming back generates the same chunk again.
func TestADroppedUntouchedChunkIsNotSaved(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())
	w.SetDropUntouchedChunks(true)
	pos := ChunkPos{X: 2, Z: 3}

	_, _ = w.Acquire(pos)
	w.Release(pos)
	w.FlushSaves()
	rx, rz := regionCoords(pos)
	if _, err := os.Stat(regionFilePath(dimensionDir(dir, DimensionName), rx, rz)); !os.IsNotExist(err) {
		t.Errorf("region file for an unmodified chunk: %v, want not-exist", err)
	}

	again, _ := w.Acquire(pos)
	if got := again.BlockAt(0, worldMinY, 0); got != stoneState {
		t.Errorf("regenerated stone = %d, want %d", got, stoneState)
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

// TestSaveRetriesWhenABlockEntityLeavesMidSave checks that a chest broken
// while the save is stalled reading another block entity is not written as
// air still carrying the chest: the save reads the entities and the sections
// in two phases, so it rechecks the two agree and starts over when an entity
// left in between.
func TestSaveRetriesWhenABlockEntityLeavesMidSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())
	pos := ChunkPos{X: 0, Z: 0}

	chest := registry.BlockStates["minecraft:chest"]
	// Held for the whole test, so the chunk stays loaded under the save.
	_, _ = w.Acquire(pos)
	// The chest to break sits below the stall entity, so the save reads it
	// before it parks on the stall's lock.
	w.SetBlock(3, GroundLevel, 5, chest)
	w.SetBlock(9, GroundLevel+4, 2, chest)
	if w.EnsureBlockEntity(3, GroundLevel, 5, KindChest) == nil ||
		w.EnsureBlockEntity(9, GroundLevel+4, 2, KindChest) == nil {
		t.Fatal("no block entity came with the chests")
	}

	// The save parks inside encodeBlockEntities, which runs after the first
	// phase has read the chunk's entities, and stays parked while the stall's
	// lock is held: that frame on the save's stack is the sign the chest has
	// been read and the sections not yet. Only the save goroutine this test
	// started is matched — a parallel test's save parked in the same frame
	// would otherwise stand in for this one and let the chest leave before
	// the save has read anything. With the world lock down there, the chest
	// can leave in between, which is the state the save has to notice.
	saveDone := make(chan struct{})
	saveMark := make(chan string, 1)
	const stallTimeout = 20 * time.Second
	deadline := time.Now().Add(stallTimeout)
	stall := w.EnsureBlockEntity(9, GroundLevel+4, 2, KindChest)
	stall.Use(func(b *BlockEntity) {
		go func() {
			saveMark <- goroutineMark()
			defer close(saveDone)
			w.Save()
		}()
		mark := <-saveMark
		buf := make([]byte, 1<<20)
		for !goroutineSitsIn(buf[:runtime.Stack(buf, true)], mark, "world.encodeBlockEntities") {
			if time.Now().After(deadline) {
				n := runtime.Stack(buf, true)
				t.Fatalf("the save never reached its block entities:\n%s", buf[:n])
			}
			time.Sleep(time.Millisecond)
		}
		w.RemoveBlockEntity(3, GroundLevel, 5)
		w.SetBlock(3, GroundLevel, 5, airState)
	})

	select {
	case <-saveDone:
	case <-time.After(stallTimeout):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("the save never finished:\n%s", buf[:n])
	}
	w.Close()

	w2, _ := NewWorld(dir, NewFlatGenerator())
	defer w2.Close()
	w2.Acquire(pos)
	if got := w2.BlockAt(3, GroundLevel, 5); got != airState {
		t.Errorf("block where the chest was = %d, want air", got)
	}
	if be := w2.BlockEntity(3, GroundLevel, 5); be != nil {
		t.Error("the chest's block entity came back with a chunk that has air there")
	}
	if got := w2.BlockAt(9, GroundLevel+4, 2); got != chest {
		t.Errorf("the stall chest's block = %d, want the chest", got)
	}
	if w2.BlockEntity(9, GroundLevel+4, 2) == nil {
		t.Error("the stall chest's block entity did not come back")
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
	rf, err := openRegion(regionFilePath(dimensionDir(dir, DimensionName), rx, rz), defaultSaveFormat)
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

// TestWorldBiomeAtReadsTheMixedCells checks World.NoiseBiome against a mixed
// section: it names the cell the block's chunk-local coordinates fall in, with
// x&15 and z&15 wrapping the world coordinates of a chunk in the negative
// quadrant.
func TestWorldBiomeAtReadsTheMixedCells(t *testing.T) {
	t.Parallel()
	plains := registry.MustID("minecraft:worldgen/biome", "minecraft:plains")
	desert := registry.MustID("minecraft:worldgen/biome", "minecraft:desert")

	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{X: -1, Z: -2}
	c, _ := w.Acquire(pos)
	c.SetBiome(plains)
	c.sections[0].biomes = make([]int32, biomesPerSection)
	for i := range c.sections[0].biomes {
		c.sections[0].biomes[i] = plains
	}
	// The bottom section's topmost cell, local (12, 12, 12), is desert.
	c.sections[0].biomes[biomeCell(12, 12, 12)] = desert

	// The world coordinates of local (12, 12, 12) in chunk (-1, -2).
	if got := w.NoiseBiome(-16+12, worldMinY+12, -32+12).Name(); got != "minecraft:desert" {
		t.Errorf("biome at the desert cell = %q, want minecraft:desert", got)
	}
	if got := w.NoiseBiome(-16, worldMinY, -32).Name(); got != "minecraft:plains" {
		t.Errorf("biome at the plains cell = %q, want minecraft:plains", got)
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
	rf, err := openRegion(regionFilePath(dimensionDir(dir, DimensionName), rx, rz), defaultSaveFormat)
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

// TestTimeIsReadFromLevelDat checks that a level.dat a vanilla server wrote —
// with its own Time and DayTime — is read with the clock where it was left,
// which is what dropping a vanilla world into the server folder does.
func TestTimeIsReadFromLevelDat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"SpawnX":   nbt.Int(0),
		"SpawnY":   nbt.Int(GroundLevel),
		"SpawnZ":   nbt.Int(0),
		"GameType": nbt.Int(0),
		"Time":     nbt.Long(12345),
		"DayTime":  nbt.Long(6789),
	})

	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	if age, day := w.Time(); age != 12345 || day != 6789 {
		t.Errorf("time from level.dat = (%d, %d), want (12345, 6789)", age, day)
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

// TestRateSurvivesSaveAndReload checks that a clock rate set with
// SetClockRate is written on Save and read back on the next load, so a /time
// rate survives a restart.
func TestRateSurvivesSaveAndReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())
	w.SetClockRate(OverworldClock, 3)

	w.Save()
	w.Close()

	again, _ := NewWorld(dir, NewFlatGenerator())
	defer again.Close()
	if got := again.Rate(); got != 3 {
		t.Errorf("rate after reload = %v, want 3", got)
	}
	if again.Paused() {
		t.Error("clock is paused after reload, want running")
	}
}

// TestPausedSurvivesSaveAndReload checks that a paused clock is written on
// Save and read back on the next load, so a /time pause survives a restart —
// the clock stays paused rather than resetting to running.
func TestPausedSurvivesSaveAndReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())
	w.SetClockRate(OverworldClock, 2) // a non-default rate, so resume restores it
	w.SetClockPaused(OverworldClock, true)

	w.Save()
	w.Close()

	again, _ := NewWorld(dir, NewFlatGenerator())
	defer again.Close()
	if !again.Paused() {
		t.Error("clock is running after reload, want paused")
	}
	// The rate is kept so that resuming restores it, the way /time pause and
	// /time resume do in a single run.
	if got := again.Rate(); got != 2 {
		t.Errorf("rate after reload = %v, want 2 (kept so resume restores it)", got)
	}
}

// TestRateAndPausedFromLevelDat checks that a level.dat carrying ClockRate and
// ClockPaused is read with the clock in that state, which is what a restart
// picks up after a previous run wrote them.
func TestRateAndPausedFromLevelDat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"SpawnX":      nbt.Int(0),
		"SpawnY":      nbt.Int(GroundLevel),
		"SpawnZ":      nbt.Int(0),
		"GameType":    nbt.Int(0),
		"Time":        nbt.Long(0),
		"DayTime":     nbt.Long(0),
		"ClockRate":   nbt.Float(0.5),
		"ClockPaused": nbt.Byte(1),
	})

	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	if got := w.Rate(); got != 0.5 {
		t.Errorf("rate from level.dat = %v, want 0.5", got)
	}
	if !w.Paused() {
		t.Error("clock is running, want paused (ClockPaused=1)")
	}
}

// TestRateAndPausedDefaultForVanillaLevelDat checks that a level.dat a vanilla
// server wrote — without this server's ClockRate and ClockPaused fields —
// loads with the defaults of rate 1 and running, which is what a fresh vanilla
// world starts with. Dropping a vanilla world into the server folder should
// not pause or stall the clock.
func TestRateAndPausedDefaultForVanillaLevelDat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"SpawnX":   nbt.Int(0),
		"SpawnY":   nbt.Int(GroundLevel),
		"SpawnZ":   nbt.Int(0),
		"GameType": nbt.Int(0),
		"Time":     nbt.Long(0),
		"DayTime":  nbt.Long(0),
	})

	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	if got := w.Rate(); got != 1 {
		t.Errorf("rate from vanilla level.dat = %v, want 1 (default)", got)
	}
	if w.Paused() {
		t.Error("clock is paused, want running (default)")
	}
}

// TestSaveKeepsAMarkMadeWhileItEncodes checks that a change made while Save is
// encoding a chunk survives the save: putting an item in a chest mid-encode
// has to leave the chunk dirty, or the chest is written back the way the save
// read it and the item is gone when the world is loaded again.
func TestSaveKeepsAMarkMadeWhileItEncodes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	pos := ChunkPos{X: 0, Z: 0}
	_, _ = w.Acquire(pos)

	chestAt := [3]int32{3, GroundLevel, 5}
	barrelAt := [3]int32{1, GroundLevel + 4, 1}
	if !w.SetBlock(chestAt[0], chestAt[1], chestAt[2], rsState(t, "minecraft:chest", nil)) {
		t.Fatal("setup: the chest did not go in")
	}
	chest := w.EnsureBlockEntity(chestAt[0], chestAt[1], chestAt[2], KindChest)
	if !w.SetBlock(barrelAt[0], barrelAt[1], barrelAt[2], rsState(t, "minecraft:barrel", nil)) {
		t.Fatal("setup: the barrel did not go in")
	}
	w.EnsureBlockEntity(barrelAt[0], barrelAt[1], barrelAt[2], KindBarrel)
	item, _ := registry.ItemID("minecraft:dirt")
	chest.Use(func(b *BlockEntity) { b.Slots[0] = protocol.ItemStack{ItemID: item, Count: 1} })

	// Stall Save on the barrel's lock. The entities are encoded highest Y
	// last, so the barrel above the chest parks the save after it has read
	// the chest, the way a click mid-container holds one entity while the
	// save encodes the chunk.
	held := make(chan struct{})
	release := make(chan struct{})
	var unstick sync.Once
	go func() {
		w.BlockEntity(barrelAt[0], barrelAt[1], barrelAt[2]).Use(func(*BlockEntity) {
			close(held)
			<-release
		})
	}()
	<-held

	// The region file stays locked until the change has landed, so the save
	// cannot reach the flag-clearing step before the mark does.
	rf, err := w.region(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	rf.mu.Lock()
	var writeFile sync.Once

	saveMark := make(chan string, 1)
	saved := make(chan struct{})
	go func() {
		defer close(saved)
		saveMark <- goroutineMark()
		w.Save()
	}()
	// Runs before Close, which nils the region map a still-running save
	// would panic on; the defers registered after it run first.
	defer func() { <-saved }()
	defer writeFile.Do(func() { rf.mu.Unlock() })
	defer unstick.Do(func() { close(release) })
	waitSaveParked(t, <-saveMark)

	// The change runs while the save is parked on the barrel's lock, so it
	// lands after the encode has read the chest and before the save gets to
	// clear the chunk's flag.
	diamond, _ := registry.ItemID("minecraft:diamond")
	changed := make(chan struct{})
	marked := make(chan struct{})
	go func() {
		chest.Use(func(b *BlockEntity) { b.Slots[1] = protocol.ItemStack{ItemID: diamond, Count: 1} })
		close(changed)
		w.Touch(chestAt[0], chestAt[1], chestAt[2])
		close(marked)
	}()
	<-changed
	unstick.Do(func() { close(release) })
	<-marked
	writeFile.Do(func() { rf.mu.Unlock() })
	<-saved

	// The first save wrote the chest as it had read it; the second is the
	// one that has to carry the change.
	w.Save()
	w.Close()

	again, _ := NewWorld(dir, NewFlatGenerator())
	defer again.Close()
	_, _ = again.Acquire(pos)
	be := again.BlockEntity(chestAt[0], chestAt[1], chestAt[2])
	if be == nil {
		t.Fatal("the chest did not come back from the region file")
	}
	if !be.Slots[1].Equal(protocol.ItemStack{ItemID: diamond, Count: 1}) {
		t.Fatalf("the item put in the chest during the save was lost: chest slot 1 = %+v, want one diamond", be.Slots[1])
	}
}

// TestTheEndClockIsIndependent checks that the_end clock — the second clock
// the minecraft:world_clock registry has — advances by its own rate while
// paused and running on its own.
func TestTheEndClockIsIndependent(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	defer w.Close()

	if _, total, ok := w.ClockTime(TheEndClock); !ok || total != 0 {
		t.Fatalf("the_end clock = (%v, %d), want (true, 0)", ok, total)
	}

	// Both clocks advance by one while both run.
	w.TickTime()
	if _, day := w.Time(); day != 1 {
		t.Errorf("overworld clock after one tick = %d, want 1", day)
	}
	if _, total, _ := w.ClockTime(TheEndClock); total != 1 {
		t.Errorf("the_end clock after one tick = %d, want 1", total)
	}

	// Pausing the_end leaves the overworld's clock running, and the other
	// way around.
	w.SetClockPaused(TheEndClock, true)
	w.TickTime()
	if _, day := w.Time(); day != 2 {
		t.Errorf("overworld clock = %d, want 2 (the_end's pause holds only its own)", day)
	}
	if _, total, _ := w.ClockTime(TheEndClock); total != 1 {
		t.Errorf("the_end clock = %d, want 1 (paused)", total)
	}

	// Setting the_end's clock leaves the overworld's alone.
	w.SetClockTime(TheEndClock, 4502)
	if _, day := w.Time(); day != 2 {
		t.Errorf("overworld clock = %d, want 2", day)
	}
	if _, total, _ := w.ClockTime(TheEndClock); total != 4502 {
		t.Errorf("the_end clock = %d, want 4502", total)
	}

	// A clock name the registry does not have answers none.
	if _, _, ok := w.ClockTime("minecraft:nonsense"); ok {
		t.Error("minecraft:nonsense answered with clock state, want none")
	}
}

// TestMoveClockToMarker checks the jump /time set <marker> rides: forward
// to the next occurrence of the marker's ticks within its period — up to a
// marker ahead, wrapping to the one behind, and not at all when the clock
// already stands on it — never backwards, and dropping the fraction of a
// tick a slow rate was carrying.
// vanilla: ClockTimeMarker.resolveTimeToMoveTo
func TestMoveClockToMarker(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	defer w.Close()

	// A total below the marker moves up to it.
	w.SetClockTime(OverworldClock, 500)
	w.MoveClockToMarker(OverworldClock, 1000, 24000)
	if _, day := w.Time(); day != 1000 {
		t.Errorf("from 500 to the 1000 marker = %d, want 1000", day)
	}

	// A total past the marker wraps to its next occurrence.
	w.SetClockTime(OverworldClock, 30000)
	w.MoveClockToMarker(OverworldClock, 13000, 24000)
	if _, day := w.Time(); day != 37000 {
		t.Errorf("from 30000 to the 13000 marker = %d, want 37000", day)
	}

	// A total already on the marker does not move.
	w.SetClockTime(OverworldClock, 54000)
	if w.MoveClockToMarker(OverworldClock, 6000, 24000) {
		t.Error("move from 54000 onto the 6000 marker reported a move")
	}
	if _, day := w.Time(); day != 54000 {
		t.Errorf("from 54000 (on the 6000 marker) = %d, want 54000", day)
	}

	// The fraction a rate below one was part-way through does not ride.
	w.SetClockRate(OverworldClock, 0.5)
	w.TickTime()
	if got := w.clock(OverworldClock).fractional; got != 0.5 {
		t.Errorf("fractional after a half-rate tick = %g, want 0.5", got)
	}
	w.MoveClockToMarker(OverworldClock, 1000, 24000)
	if got := w.clock(OverworldClock).fractional; got != 0 {
		t.Errorf("fractional after the jump = %g, want 0", got)
	}

	// A clock name the registry does not have is left alone.
	_, day := w.Time()
	w.MoveClockToMarker("minecraft:nonsense", 1000, 24000)
	if _, after := w.Time(); after != day {
		t.Errorf("nonsense jump moved the overworld clock: %d, want %d", after, day)
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

// TestClocksDefaultForAVanillaWorld checks that a world a vanilla
// server wrote — no world_clocks.dat, and a level.dat without a DayTime —
// loads with both clocks at the fresh-world defaults: total 0, rate 1,
// running.
func TestClocksDefaultForAVanillaWorld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"SpawnX":   nbt.Int(0),
		"SpawnY":   nbt.Int(GroundLevel),
		"SpawnZ":   nbt.Int(0),
		"GameType": nbt.Int(0),
		"Time":     nbt.Long(0),
	})

	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	for _, name := range []string{OverworldClock, TheEndClock} {
		if _, total, ok := w.ClockTime(name); !ok || total != 0 {
			t.Fatalf("%s clock = (%v, %d), want (true, 0)", name, ok, total)
		}
		if w.clock(name).rate != 1 {
			t.Errorf("%s rate = %v, want 1 (default)", name, w.clock(name).rate)
		}
		if w.clock(name).paused {
			t.Errorf("%s clock is paused, want running (default)", name)
		}
	}
}

// TestTouchEntityMarksALiveEntityAndRefusesAGoneOne checks the contract the
// dispense visit depends on: TouchEntity marks the chunk of an entity the
// world still holds at its own position — the contents-changed form of
// Touch — and refuses a removed or unloaded one, marking nothing. Both
// happen in one world-lock hold, which is what makes it safe to call under
// a block entity's lock.
func TestTouchEntityMarksALiveEntityAndRefusesAGoneOne(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	defer w.Close()
	pos := ChunkPos{X: 0, Z: 0}
	_, _ = w.Acquire(pos)

	chestAt := [3]int32{3, GroundLevel, 5}
	if !w.SetBlock(chestAt[0], chestAt[1], chestAt[2], rsState(t, "minecraft:chest", nil)) {
		t.Fatal("setup: the chest did not go in")
	}
	be := w.EnsureBlockEntity(chestAt[0], chestAt[1], chestAt[2], KindChest)
	if be == nil {
		t.Fatal("setup: no block entity came with the chest")
	}

	// dirtyGen counts the chunk's marks; TouchEntity on the live chest is
	// one more, and the dirty flag stands.
	mark := func() (gen uint64, dirty bool) {
		w.mu.Lock()
		defer w.mu.Unlock()
		l := w.chunks[pos]
		return l.dirtyGen, l.dirty
	}
	before, _ := mark()
	if !w.TouchEntity(be) {
		t.Fatal("TouchEntity refused the chest the world still holds")
	}
	if gen, dirty := mark(); gen != before+1 || !dirty {
		t.Errorf("after TouchEntity: dirtyGen = %d (want %d), dirty = %v", gen, before+1, dirty)
	}

	// A removed entity gets no mark: removing it was the last one, and
	// TouchEntity must not order anything after it.
	w.RemoveBlockEntity(chestAt[0], chestAt[1], chestAt[2])
	after, _ := mark()
	if w.TouchEntity(be) {
		t.Error("TouchEntity vouched for the chest after its removal")
	}
	if gen, _ := mark(); gen != after {
		t.Errorf("the refused TouchEntity marked the chunk anyway: dirtyGen = %d, want %d", gen, after)
	}

	// An unloaded chunk is refused the same way.
	w.Release(pos)
	w.FlushSaves()
	if w.TouchEntity(be) {
		t.Error("TouchEntity vouched for the chest of an unloaded chunk")
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

// TestAReleaseSaveDropsNoChangeMadeWhileItRan checks that a released chunk
// leaves the world only once its save holds every mark made while it ran: a
// block changed after the save's read must ride out in a later generation
// rather than be dropped with the stale bytes.
func TestAReleaseSaveDropsNoChangeMadeWhileItRan(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	defer w.Close()
	pos := ChunkPos{X: 0, Z: 0}
	_, _ = w.Acquire(pos)

	// The stone the save reads, and the air the change puts in its place
	// while the save is parked past its read.
	stoneAt := [3]int32{3, GroundLevel, 5}
	if !w.SetBlock(stoneAt[0], stoneAt[1], stoneAt[2], rsState(t, "minecraft:stone", nil)) {
		t.Fatal("setup: the stone did not go in")
	}

	// The leaving holder is gone, the way a Release says so with its users
	// count; the save itself runs here as Release's I/O task runs it, from
	// the generation the chunk stands at.
	w.mu.Lock()
	w.chunks[pos].users.Store(0)
	chunk := w.chunks[pos].chunk
	gen := w.chunks[pos].dirtyGen
	w.mu.Unlock()

	// The save parks inside its region write, which is past its read of the
	// chunk and short of the bytes landing.
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

	// The change lands under the world's lock while the save's read is
	// already behind it; the mark must keep the chunk from being dropped
	// with the stale bytes.
	if !w.SetBlock(stoneAt[0], stoneAt[1], stoneAt[2], airState) {
		t.Fatal("the change did not land while the chunk was being saved")
	}
	rf.mu.Unlock()
	<-saved

	if got := w.LoadedChunks(); got != 0 {
		t.Fatalf("loaded chunks = %d, want 0 once the released chunk is saved out", got)
	}
	_, _ = w.Acquire(pos)
	if got := w.BlockAt(stoneAt[0], stoneAt[1], stoneAt[2]); got != airState {
		t.Fatalf("the release save dropped a change made while it ran: block = %d, want air", got)
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

// TestSaveDoesNotClearAReacquiredChunksMark checks that Save clears only the
// chunk it listed, not one loaded in its place while the save ran: a chunk
// released and acquired again mid-save starts its mark count over, and one
// mark on the fresh chunk makes it match the recorded generation, which must
// not be mistaken for the listed chunk's.
func TestSaveDoesNotClearAReacquiredChunksMark(t *testing.T) {
	t.Parallel()
	// The save walks its chunk map in random order, so the race shows in
	// about half the rounds; enough rounds that a regression cannot slip
	// through.
	for attempt := range 25 {
		dir := t.TempDir()
		w, _ := NewWorld(dir, NewFlatGenerator())
		a := ChunkPos{X: 0, Z: 0}  // region (0,0)
		b := ChunkPos{X: 32, Z: 0} // region (1,0), a file of its own
		_, _ = w.Acquire(a)
		_, _ = w.Acquire(b)
		oldAt := [3]int32{5, GroundLevel + 1, 5}
		markAt := [3]int32{7, GroundLevel + 1, 7}
		if !w.SetBlock(oldAt[0], oldAt[1], oldAt[2], rsState(t, "minecraft:stone", nil)) {
			t.Fatal("setup: the block in the first chunk did not go in")
		}
		if !w.SetBlock(b.X*16+3, GroundLevel+1, 3, rsState(t, "minecraft:stone", nil)) {
			t.Fatal("setup: the block in the second chunk did not go in")
		}

		// Park the save inside the second chunk's region write by holding
		// that file's lock, then release the first chunk and load it again
		// while the save sits there, marking the fresh chunk once.
		rf, err := w.region(1, 0)
		if err != nil {
			t.Fatal(err)
		}
		rf.mu.Lock()
		saveDone := make(chan struct{})
		go func() {
			defer close(saveDone)
			w.Save()
		}()
		w.Release(a)
		w.FlushSaves()
		_, _ = w.Acquire(a)
		if !w.SetBlock(markAt[0], markAt[1], markAt[2], rsState(t, "minecraft:stone", nil)) {
			t.Fatal("setup: the block in the reacquired chunk did not go in")
		}

		rf.mu.Unlock()
		<-saveDone

		// The save wrote the chunk it listed; an edit made on the chunk
		// loaded in its place has to reach the file through the next save.
		w.Save()
		w.Close()

		again, _ := NewWorld(dir, NewFlatGenerator())
		_, _ = again.Acquire(a)
		if got := again.BlockAt(markAt[0], markAt[1], markAt[2]); got != rsState(t, "minecraft:stone", nil) {
			t.Fatalf("the edit made on the reacquired chunk was lost by the save (attempt %d)", attempt)
		}
		again.Close()
	}
}
