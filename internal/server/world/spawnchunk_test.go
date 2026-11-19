package world

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestSpawnChunkLiveReads(t *testing.T) {
	t.Run("collision", testSpawnCollisionReads)
	t.Run("cache-lifecycle", testSpawnCollisionLifecycle)
	w := newTestWorld(t, NewFlatGenerator())
	origin := ChunkPos{X: -1}
	if _, ok := w.HoldSpawnChunk(origin); ok {
		t.Fatal("held an unloaded chunk")
	}
	w.Acquire(origin)
	defer w.Release(origin)
	s, ok := w.HoldSpawnChunk(origin)
	if !ok {
		t.Fatal("missing origin")
	}
	if s.HasFortresses() {
		t.Fatal("flat generator has fortresses")
	}
	if got := w.chunks[origin].users.Load(); got != 2 {
		t.Fatalf("users=%d, want 2", got)
	}
	check := func() {
		t.Helper()
		s.ResetSpawnStep()
		for _, x := range []int32{-17, -16, -1, 0, 15, 16, 63 * 16} {
			for _, y := range []int32{w.MinY() - 1, w.MinY(), GroundLevel - 1, GroundLevel, 319, 320} {
				if got, want := s.InitialSpawnBlocked(x, y, 0), registry.IsRedstoneConductorState(w.BlockAt(x, y, 0)); got != want {
					t.Fatalf("collision(%d,%d)=%t, want %t", x, y, got, want)
				}
				if got, want := s.BlockAt(x, y, 0), w.BlockAt(x, y, 0); got != want {
					t.Fatalf("block(%d,%d)=%d, want %d", x, y, got, want)
				}
				if got, want := s.Biome(x, y, 0), w.Biome(x, y, 0); got != want {
					t.Fatalf("biome(%d,%d)=%v, want %v", x, y, got, want)
				}
				if got, want := s.ConductorAt(x, y, 0), w.ConductorAt(x, y, 0); got != want {
					t.Fatalf("conductor(%d,%d)=%v, want %v", x, y, got, want)
				}
			}
			if got, want := s.SurfaceHeight(x, 0), w.SurfaceHeight(x, 0); got != want {
				t.Fatalf("height(%d)=%d, want %d", x, got, want)
			}
			if got, want := s.ChunkLoadedAt(x, 0), w.ChunkLoadedAt(x, 0); got != want {
				t.Fatalf("loaded(%d)=%t, want %t", x, got, want)
			}
		}
	}
	check()
	for _, pos := range []ChunkPos{{}, {X: 1}, {X: 63}} {
		w.Acquire(pos)
		defer w.Release(pos)
		check()
	}
	for _, x := range []int32{-16, -1, 0, 16} {
		for _, state := range []uint16{registry.BlockStone.DefaultState(), registry.BlockAir.DefaultState()} {
			w.SetBlock(x, 90, 0, state)
			check()
		}
	}
	for _, b := range []registry.Biome{registry.BiomeSwamp, registry.BiomePlains} {
		s.ResetSpawnStep()
		s.Biome(-8, 64, 8)
		_, _, ok := w.FillBiomes(BiomeBox{MinX: -16, MaxX: -1, MinY: 48, MaxY: 80, MinZ: 0, MaxZ: 15}, int32(b), nil)
		if !ok {
			t.Fatal("fill failed")
		}
		s.ResetSpawnStep()
		if got := s.Biome(-8, 64, 8); got != b {
			t.Fatalf("step retained biome %v, want %v", got, b)
		}
	}
	s.Close()
	if got := w.chunks[origin].users.Load(); got != 1 {
		t.Fatalf("released users=%d, want 1", got)
	}
}

// assertSpawnCollisionWord checks every initialized pair of the word against
// live blocks; an unqueried pair may still be zero.
func assertSpawnCollisionWord(t *testing.T, sec *section, first int) {
	t.Helper()
	cache := sec.spawn.Load()
	if cache == nil {
		t.Fatal("missing collision cache")
	}
	var want uint64
	for j := range 32 {
		pair := uint64(1)
		if registry.IsRedstoneConductorState(sec.at(first + j)) {
			pair = 2
		}
		want |= pair << (2 * j)
	}
	if cache == &spawnAll || cache == &spawnNone {
		pair := uint64(1)
		if cache == &spawnAll {
			pair = 2
		}
		if want != pair*0x5555555555555555 {
			t.Fatalf("singleton disagrees with group %d: %016x", first/32, want)
		}
		return
	}
	got := cache[first/32].Load()
	for j := range 32 {
		if pair := got >> (2 * j) & 3; pair != 0 && pair != want>>(2*j)&3 {
			t.Fatalf("group %d cell %d: %d, want %d", first/32, j, pair, want>>(2*j)&3)
		}
	}
}

// spawnCollisionKnown counts the initialized pairs of a word.
func spawnCollisionKnown(word uint64) int {
	n := 0
	for j := range 32 {
		if word>>(2*j)&3 != 0 {
			n++
		}
	}
	return n
}

func testSpawnCollisionReads(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{X: -1, Z: -1}
	w.Acquire(pos)
	s, ok := w.HoldSpawnChunk(pos)
	if !ok {
		t.Fatal("missing origin")
	}
	check := func(x, y, z int32) {
		t.Helper()
		if got, want := s.InitialSpawnBlocked(x, y, z), registry.IsRedstoneConductorState(w.BlockAt(x, y, z)); got != want {
			t.Fatalf("collision(%d,%d,%d)=%t, want %t", x, y, z, got, want)
		}
	}
	for _, y := range []int32{worldMinY - 1, worldMinY, GroundLevel - 1, 80, 319, 320} {
		check(-16, y, -16)
	}
	air, stone := registry.BlockAir.DefaultState(), registry.BlockStone.DefaultState()
	sec := &s.origin.chunk.sections[(80-worldMinY)/16]
	if sec.blocks.Load() != nil {
		t.Fatal("uniform air allocated storage")
	}
	w.SetBlock(-16, 80, -16, air)
	if sec.blocks.Load() != nil {
		t.Fatal("unchanged write invalidated summary")
	}
	for _, state := range []uint16{registry.BlockOakStairs.DefaultState(), stone, air, registry.BlockIronBars.DefaultState()} {
		w.SetBlock(-16, 80, -16, state)
		check(-16, 80, -16)
	}
	if registry.IsRedstoneConductorState(registry.BlockOakStairs.DefaultState()) || CollisionTop(registry.BlockOakStairs.DefaultState()) != 1 {
		t.Fatal("partial top-one fixture")
	}
	// Uniform and indirect all-top sections, including a partial shape.
	solidY := int32(worldMinY)
	solid := &s.origin.chunk.sections[0]
	check(-16, solidY, -16)
	if solid.blocks.Load() != nil {
		t.Fatal("uniform full section allocated storage")
	}
	w.SetBlock(-16, solidY, -16, registry.BlockDirt.DefaultState())
	check(-16, solidY, -16)
	if solid.spawn.Load() != &spawnAll {
		t.Fatal("all-top palette summary")
	}
	w.SetBlock(-16, solidY, -16, air)
	check(-16, solidY, -16)
	if solid.spawn.Load() == nil || solid.spawn.Load() == &spawnAll || solid.spawn.Load() == &spawnNone {
		t.Fatal("mixed palette summary")
	}
	for _, state := range []uint16{registry.BlockTorch.DefaultState(), registry.BlockOakSlab.DefaultState()} {
		w.SetBlock(-16, 112, -16, state)
		check(-16, 112, -16)
		if s.origin.chunk.sections[(112-worldMinY)/16].spawn.Load() != &spawnNone {
			t.Fatal("all-none palette summary")
		}
	}
	// Every palette slot and width, preserving the section cache through resizes.
	// Recheck every written cell after each quiescent write.
	for i := range 300 {
		x, y, z := int32(-16+(i&15)), int32(80+i/256), int32(-16+((i/16)&15))
		w.mu.Lock()
		sec.set(i, uint16(i), true)
		w.mu.Unlock()
		check(x, y, z)
		for j := 0; j <= i; j++ {
			check(-16+int32(j&15), 80+int32(j/256), -16+int32((j/16)&15))
		}
		cache := sec.spawn.Load()
		w.mu.Lock()
		sec.set(i, uint16(i), true)
		sec.set(4095, uint16(i), true)
		w.mu.Unlock()
		if sec.spawn.Load() != cache {
			t.Fatal("mixed cell write replaced cache")
		}
		check(-1, 95, -1)
	}
	if sec.blocks.Load().bits != directBits || sec.spawn.Load() == nil {
		t.Fatal("direct layout must retain section cache")
	}
	// Concurrent readers race cache construction and locked palette writes.
	w.mu.Lock()
	s.origin.chunk.sections[(96-worldMinY)/16].set(4095, stone, true)
	w.mu.Unlock()
	var readers sync.WaitGroup
	done := make(chan struct{})
	started := make(chan struct{}, 2)
	for range 2 {
		readers.Go(func() {
			started <- struct{}{}
			for {
				select {
				case <-done:
					return
				default:
					if !s.InitialSpawnBlocked(-1, 111, -1) || s.InitialSpawnBlocked(-2, 111, -1) {
						t.Error("concurrent read changed an untouched predicate")
						return
					}
					for i := range 400 {
						s.InitialSpawnBlocked(-16+int32(i&15), 96+int32(i/256), -16+int32((i/16)&15))
					}
				}
			}
		})
	}
	for range 2 {
		<-started
	}
	for pass := range 2 {
		for i := range 400 {
			w.mu.Lock()
			s.origin.chunk.sections[(96-worldMinY)/16].set(i, uint16((i+pass*13)%400), true)
			w.mu.Unlock()
		}
	}
	close(done)
	readers.Wait()
	for i := range 400 {
		check(-16+int32(i&15), 96+int32(i/256), -16+int32((i/16)&15))
	}
	s.Close()
	w.Release(pos)
	w.FlushSaves()
	w.Acquire(pos)
	s, ok = w.HoldSpawnChunk(pos)
	if !ok {
		t.Fatal("reload missing")
	}
	defer func() { s.Close(); w.Release(pos) }()
	sec = &s.origin.chunk.sections[(80-worldMinY)/16]
	if sec.spawn.Load() != nil {
		t.Fatal("decoded section retained summary")
	}
	check(-16, 80, -16)
	// A new section's bulk fill also drops its lazy classification.
	var fresh section
	fresh.set(0, stone, false)
	fresh.setAll(air)
	fresh.spawn.Store(&spawnAll)
	fresh.setAll(air)
	if fresh.blocks.Load() != nil || fresh.spawn.Load() != nil {
		t.Fatal("setAll retained summary")
	}
}

func testSpawnCollisionLifecycle(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	c := newChunk(-1, -1)
	s := SpawnChunk{World: w, origin: &loadedChunk{pos: ChunkPos{X: -1, Z: -1}, chunk: c}}
	sec := &c.sections[(80-worldMinY)/16]
	air, stone := registry.BlockAir.DefaultState(), registry.BlockStone.DefaultState()
	stairs := registry.BlockDirt.DefaultState()
	check := func(i int) {
		t.Helper()
		x, y, z := int32(-16+(i&15)), int32(80+i/256), int32(-16+((i/16)&15))
		if got, want := s.InitialSpawnBlocked(x, y, z), registry.IsRedstoneConductorState(c.sharedBlockAt(x&15, y, z&15)); got != want {
			t.Fatalf("cell %d: %t want %t", i, got, want)
		}
	}
	check(0)
	if sec.spawn.Load() != &spawnNone {
		t.Fatal("uniform none singleton")
	}
	sec.set(31, registry.BlockTorch.DefaultState(), false)
	if sec.spawn.Load() != &spawnNone {
		t.Fatal("same predicate invalidated singleton")
	}
	sec.set(31, stone, false)
	if sec.spawn.Load() != nil {
		t.Fatal("singleton predicate transition not invalidated")
	}
	check(31)
	cache := sec.spawn.Load()
	if cache == nil || cache == &spawnAll || cache == &spawnNone {
		t.Fatal("missing mixed cache")
	}
	for i := range cache {
		if (cache[i].Load() != 0) != (i == 0) {
			t.Fatalf("eager word %d", i)
		}
	}
	if n := spawnCollisionKnown(cache[0].Load()); n != 1 {
		t.Fatalf("one cold query initialized %d cells, want 1", n)
	}
	first := cache[0].Load()
	sec.set(31, stairs, false)
	if cache[0].Load() != first || sec.spawn.Load() != cache {
		t.Fatal("same top changed mixed word")
	}
	sec.set(32, stone, false)
	sec.set(4095, stone, false)
	if cache[1].Load() != 0 || cache[127].Load() != 0 {
		t.Fatal("write initialized a cold word")
	}
	check(32)
	check(4095)
	sec.set(31, air, false)
	sec.set(32, air, false)
	sec.set(4095, air, false)
	for _, i := range []int{0, 31, 32, 33, 4095} {
		check(i)
	}
	if sec.spawn.Load() != cache {
		t.Fatal("mixed cache replaced")
	}
	// Each palette growth must fill a previously untouched group completely.
	width, growth := 0, 0
	for state := range uint16(300) {
		sec.set(256+int(state), state, false)
		if b := sec.blocks.Load(); b.bits != width {
			width = b.bits
			start := 1024 + growth*32
			growth++
			if cache[start/32].Load() != 0 {
				t.Fatalf("%d-bit group already initialized", width)
			}
			check(start + 31)
			assertSpawnCollisionWord(t, sec, start)
			other := stone
			if registry.IsRedstoneConductorState(sec.at(start + 31)) {
				other = air
			}
			sec.set(start+31, other, false)
			check(start + 31)
			assertSpawnCollisionWord(t, sec, start)
		}
	}
	if growth != 6 || width != directBits {
		t.Fatalf("visited %d layouts ending at %d bits", growth, width)
	}
	sec.setAll(stone)
	if sec.spawn.Load() != nil {
		t.Fatal("setAll retained cache")
	}
	check(31)
	if sec.spawn.Load() != &spawnAll {
		t.Fatal("uniform all singleton")
	}
	sec.set(31, stairs, false)
	if sec.spawn.Load() != &spawnAll {
		t.Fatal("same predicate invalidated all singleton")
	}
	sec.spawn.Store(nil)
	check(31)
	if sec.spawn.Load() != &spawnAll {
		t.Fatal("all-top indirect used entries")
	}
	sec.set(32, air, false)
	if sec.spawn.Load() != nil {
		t.Fatal("all singleton transition")
	}
	check(32)
	// Decode replaces an already queried section, including a nonuniform layout.
	palette := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{blockPaletteEntry(air), blockPaletteEntry(stone)}}
	indices := make([]uint16, blocksPerSection)
	indices[4095] = 1
	if err := decodeSectionBlocks(sec, nbt.Compound{"palette": palette, "data": packLongs(indices, 4)}); err != nil {
		t.Fatal(err)
	}
	if sec.spawn.Load() != nil {
		t.Fatal("decode retained cache")
	}
	check(31)
	check(4095)
	// Terrain copies and restored destinations start without a cache.
	saved := &shapedTerrain{}
	saved.keep(c, true)
	copyChunk := newChunk(-1, -1)
	copyChunk.sections[(80-worldMinY)/16].spawn.Store(&spawnAll)
	saved.restore(copyChunk)
	copied := &copyChunk.sections[(80-worldMinY)/16]
	if copied.spawn.Load() != nil {
		t.Fatal("terrain transplanted cache")
	}
	copied.set(4095, air, false)
	check(4095)
	if copied.at(4095) != air || sec.at(4095) != stone {
		t.Fatal("terrain copy shares blocks")
	}
	saved.restore(c)
	if sec.spawn.Load() != nil {
		t.Fatal("restore retained cache")
	}
	check(4095)
	// Proto replacement exercises both uniform and packed paths.
	var states [256]uint16
	states[terrain.Stone] = stone
	proto := terrain.NewProtoChunk(80, 16, terrain.Stone)
	for i := range blocksPerSection {
		proto.Set(int32(i&15), 80+int32(i/256), int32((i/16)&15), terrain.Stone)
	}
	proto.Set(0, 80, 0, terrain.Air)
	c.fillFromProto(proto, &states, false)
	if sec.spawn.Load() != nil {
		t.Fatal("proto retained cache")
	}
	check(0)
	check(4095)
	uniform := terrain.NewProtoChunk(80, 16, terrain.Stone)
	for i := range blocksPerSection {
		uniform.Set(int32(i&15), 80+int32(i/256), int32((i/16)&15), terrain.Stone)
	}
	c.fillFromProto(uniform, &states, false)
	if sec.spawn.Load() != nil {
		t.Fatal("uniform proto retained cache")
	}
	check(0)
	// A recycled lighting view borrows blocks but never their section cache.
	view := newChunk(-1, -1)
	view.sections[(80-worldMinY)/16].spawn.Store(&spawnNone)
	view.viewOf(c)
	if view.sections[(80-worldMinY)/16].spawn.Load() != nil {
		t.Fatal("view retained cache")
	}
	shadow := &lightShadow{w: &World{chunks: make(map[ChunkPos]*loadedChunk)}, views: []*loadedChunk{{chunk: view}}}
	view.sections[(80-worldMinY)/16].spawn.Store(&spawnAll)
	shadow.release(true)
	if view.sections[(80-worldMinY)/16].spawn.Load() != nil {
		t.Fatal("release retained cache")
	}
}

// A nonconducting block whose collision top reaches 1 does not reject the
// initial spawn position, on the held and the fallback read alike.
func TestInitialSpawnStairsNotBlocked(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	origin, other := ChunkPos{X: 0, Z: 0}, ChunkPos{X: 1, Z: 0}
	w.Acquire(origin)
	defer w.Release(origin)
	w.Acquire(other)
	defer w.Release(other)
	s, ok := w.HoldSpawnChunk(origin)
	if !ok {
		t.Fatal("missing origin")
	}
	stairs := registry.BlockOakStairs.DefaultState()
	if CollisionTop(stairs) != 1 {
		t.Fatal("fixture must reach the top of the cell")
	}
	for _, x := range []int32{1, 17} {
		w.SetBlock(x, 80, 1, stairs)
		w.SetBlock(x, 81, 1, registry.BlockStone.DefaultState())
		if s.InitialSpawnBlocked(x, 80, 1) {
			t.Fatalf("x=%d: stairs rejected", x)
		}
		if !s.InitialSpawnBlocked(x, 81, 1) {
			t.Fatalf("x=%d: conductor accepted", x)
		}
	}
}

// A slab's initial-spawn answer follows its type: only the double slab is a
// full block, and the warm cache follows each write.
func TestInitialSpawnDoubleSlabBlocked(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	origin, other := ChunkPos{X: 0, Z: 0}, ChunkPos{X: 1, Z: 0}
	w.Acquire(origin)
	defer w.Release(origin)
	w.Acquire(other)
	defer w.Release(other)
	s, ok := w.HoldSpawnChunk(origin)
	if !ok {
		t.Fatal("missing origin")
	}
	slab := func(typ string) uint16 {
		return registry.MustState("minecraft:oak_slab", map[string]string{"type": typ})
	}
	for _, x := range []int32{1, 17} {
		for i, step := range []struct {
			typ     string
			blocked bool
		}{{"bottom", false}, {"double", true}, {"bottom", false}} {
			w.SetBlock(x, 80, 1, slab(step.typ))
			if got := s.InitialSpawnBlocked(x, 80, 1); got != step.blocked {
				t.Fatalf("x=%d step %d (%s): blocked=%v", x, i, step.typ, got)
			}
			// Warm the cache for the next write.
			s.InitialSpawnBlocked(x, 81, 1)
		}
	}
}

func TestSpawnBiomeClasses(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	positions := []ChunkPos{{X: -1, Z: -1}, {X: -1}, {Z: -1}, {}}
	for _, pos := range positions {
		w.Acquire(pos)
		defer w.Release(pos)
	}
	s, ok := w.HoldSpawnChunk(ChunkPos{})
	if !ok {
		t.Fatal("missing origin")
	}
	defer s.Close()
	var identity, folded, single [registry.BiomeCount]registry.Biome
	for b := range registry.Biome(registry.BiomeCount) {
		identity[b], folded[b], single[b] = b, b/4*4, registry.BiomePlains
	}
	tables := map[string]*[registry.BiomeCount]registry.Biome{"identity": &identity, "folded": &folded, "single": &single}
	check := func(label string) {
		t.Helper()
		for name, reps := range tables {
			for x := int32(-18); x <= 18; x++ {
				for z := int32(-18); z <= 18; z += 3 {
					for _, y := range []int32{-80, -64, -33, -32, -1, 0, 15, 16, 63, 64, 65, 95, 96, 319, 320, 340} {
						want := w.Biome(x, y, z)
						if want >= 0 && want < registry.BiomeCount {
							want = reps[want]
						}
						s.ResetSpawnStep()
						if got := s.SpawnBiome(x, y, z, reps); got != want {
							t.Fatalf("%s/%s (%d,%d,%d) = %v, want %v", label, name, x, y, z, got, want)
						}
					}
				}
			}
		}
	}
	check("flat")
	origin := w.chunks[ChunkPos{}].chunk
	w.mu.Lock()
	for i, y := range []int32{-64, -33, 63, 64, 95, 319} {
		origin.SetCellBiome(0, y, 0, int32(registry.Biome(i*5+1)))
		origin.SetCellBiome(12, y, 12, int32(registry.Biome(i*5+2)))
	}
	w.mu.Unlock()
	check("mixed cells")
	// A sink edits the raw cells between steps; the next read must see it.
	for _, b := range []registry.Biome{registry.BiomeSwamp, registry.BiomeTaiga, registry.NoBiome} {
		if _, _, ok := w.FillBiomes(BiomeBox{MinX: 0, MaxX: 15, MinY: 48, MaxY: 80, MinZ: 0, MaxZ: 15}, int32(b), nil); !ok {
			t.Fatal("fill failed")
		}
		check("fill")
	}
	w.Release(ChunkPos{X: -1, Z: -1})
	w.FlushSaves()
	check("neighbour unloaded")
	w.Acquire(ChunkPos{X: -1, Z: -1})
	check("neighbour reloaded")
}
