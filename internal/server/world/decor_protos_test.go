package world

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// netherWorld opens the nether of a world folder, the way the server does.
func netherWorld(t *testing.T, dir string, seed int64) *World {
	t.Helper()
	ow, err := NewWorld(dir, NewOverworldGenerator(seed))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ow.Close() })
	w := NewDimensionWorld(dir, true, NewNetherGenerator(seed))
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func acquireNetherRange(w *World, x0, z0, x1, z1 int32, out map[ChunkPos][]byte) {
	for cx := x0; cx <= x1; cx++ {
		for cz := z0; cz <= z1; cz++ {
			pos := ChunkPos{X: cx, Z: cz}
			c, _ := w.Acquire(pos)
			out[pos] = encodeWhole(c)
			w.Release(pos)
		}
	}
}

// TestNetherProtosSurviveARestart stops a world halfway through a range and
// goes on with a new one: the chunks must come out as in a run that never
// stopped, which they only do if the unfinished chunks were saved.
func TestNetherProtosSurviveARestart(t *testing.T) {
	t.Parallel()
	const seed = 0
	whole := map[ChunkPos][]byte{}
	acquireNetherRange(netherWorld(t, t.TempDir(), seed), -42, -16, -38, -12, whole)

	dir := t.TempDir()
	got := map[ChunkPos][]byte{}
	first := netherWorld(t, dir, seed)
	acquireNetherRange(first, -42, -16, -40, -12, got)
	first.Close()
	second := netherWorld(t, dir, seed)
	acquireNetherRange(second, -39, -16, -38, -12, got)

	for pos, want := range whole {
		if g, ok := got[pos]; ok && !bytes.Equal(g, want) {
			t.Errorf("chunk %v differs after a restart", pos)
		}
	}
}

// acquireFortressRange acquires a range like acquireNetherRange and records
// the chests and spawners of each chunk, which encodeWhole leaves out.
func acquireFortressRange(w *World, x0, z0, x1, z1 int32, blocks map[ChunkPos][]byte, entities map[ChunkPos]string) {
	for cx := x0; cx <= x1; cx++ {
		for cz := z0; cz <= z1; cz++ {
			pos := ChunkPos{X: cx, Z: cz}
			c, _ := w.Acquire(pos)
			blocks[pos] = encodeWhole(c)
			var list []string
			for k, be := range c.entities {
				list = append(list, fmt.Sprint(k, be.Kind, be.LootTable, be.LootTableSeed))
			}
			slices.Sort(list)
			entities[pos] = strings.Join(list, ";")
			w.Release(pos)
		}
	}
}

// TestNetherFortressSurvivesARestart does the same where a fortress is: its
// fences, chests and spawner live in the protos and their save format.
func TestNetherFortressSurvivesARestart(t *testing.T) {
	t.Parallel()
	const seed = 0
	whole, wholeBE := map[ChunkPos][]byte{}, map[ChunkPos]string{}
	acquireFortressRange(netherWorld(t, t.TempDir(), seed), 17, 0, 20, 5, whole, wholeBE)

	dir := t.TempDir()
	got, gotBE := map[ChunkPos][]byte{}, map[ChunkPos]string{}
	first := netherWorld(t, dir, seed)
	acquireFortressRange(first, 17, 0, 18, 5, got, gotBE)
	first.Close()
	acquireFortressRange(netherWorld(t, dir, seed), 19, 0, 20, 5, got, gotBE)

	withEntities := 0
	for pos, want := range whole {
		if !bytes.Equal(got[pos], want) {
			t.Errorf("chunk %v differs after a restart", pos)
		}
		if gotBE[pos] != wholeBE[pos] {
			t.Errorf("chunk %v: block entities %q after a restart, want %q", pos, gotBE[pos], wholeBE[pos])
		}
		if wholeBE[pos] != "" {
			withEntities++
		}
	}
	if withEntities == 0 {
		t.Error("the range has no fortress block entity, so the test checks nothing")
	}
}

func TestNetherProtoCodecRoundTrips(t *testing.T) {
	t.Parallel()
	p := &decorProto{decorated: true, final: true, finals: 0x1a5, sources: []posKey{{1, 40, 2}}, shapes: []posKey{{15, 77, 0}},
		entities: []protoEntity{{x: 3, y: 75, z: 4, lootSeed: -5}, {spawner: true, x: 1, y: 76, z: 2},
			{x: 9, y: 50, z: 8, tag: nbt.Compound{"id": nbt.String("minecraft:chest"), "LootTableSeed": nbt.Long(77)}}}}
	p.set(5, 70, 6, 1234)
	got, err := decodeDecorProto(appendDecorProto(nil, p))
	if err != nil {
		t.Fatal(err)
	}
	if !got.decorated || !got.final || got.finals != p.finals || !slices.Equal(got.sources, p.sources) ||
		!slices.Equal(got.shapes, p.shapes) || !slices.EqualFunc(got.entities, p.entities, func(a, b protoEntity) bool { return reflect.DeepEqual(a, b) }) {
		t.Errorf("decoded %+v, want %+v", got, p)
	}
	if s, ok := got.get(5, 70, 6); !ok || s != 1234 {
		t.Errorf("block read back as %d, %v", s, ok)
	}
}

// TestNetherDecorationFollowsRequestOrder checks that the chunks come out
// of the order they were asked for, as vanilla's do: features that reach
// over a border find what the chunk decorated before them placed.
func TestNetherDecorationFollowsRequestOrder(t *testing.T) {
	t.Parallel()
	// Order comparisons must generate both sides independently of fixtures.
	_, forward := generateNetherRangeWith(0, -42, -16, -37, -11)
	g := NewNetherGenerator(0)
	differ := 0
	for cx := int32(-37); cx >= -42; cx-- {
		for cz := int32(-11); cz >= -16; cz-- {
			c := newChunk(cx, cz)
			g.Generate(c)
			if !bytes.Equal(encodeWhole(c), encodeWhole(forward[ChunkPos{X: cx, Z: cz}])) {
				differ++
			}
		}
	}
	if differ == 0 {
		t.Error("two opposite request orders gave the same chunks")
	}
}

// TestNetherProtosStayBounded flies a line of chunks with a small cap and
// checks that the protos in memory stay near it, the rest being on disk.
func TestNetherProtosStayBounded(t *testing.T) {
	t.Parallel()
	w := netherWorld(t, t.TempDir(), 7)
	g := w.gen.(*NetherGenerator)
	g.claimAll()
	g.protos.init()
	g.protos.cap = 40
	g.releaseAll()
	for cx := range int32(30) {
		for cz := int32(-1); cz <= 1; cz += 2 {
			pos := ChunkPos{X: cx, Z: cz}
			w.Acquire(pos)
			w.Release(pos)
		}
	}
	mem, peak, spilled := g.ProtoStats()
	if peak > 40+decorRegionSpan*decorRegionSpan {
		t.Errorf("peak of %d protos in memory, cap 40", peak)
	}
	if spilled == 0 {
		t.Error("the line never filled the memory, so the cap went untested")
	}
	if mem+spilled > 7*64 {
		t.Errorf("%d protos kept for 60 requested chunks", mem+spilled)
	}
}

// TestNetherProtosAreDroppedWhenComplete generates a block of chunks and
// checks that the protos inside it are gone, from memory and from disk, once
// their eight neighbours are final, so only the border of the block keeps one.
func TestNetherProtosAreDroppedWhenComplete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w := netherWorld(t, dir, 5)
	g := w.gen.(*NetherGenerator)
	g.claimAll()
	g.protos.init()
	g.protos.cap = 20
	g.releaseAll()
	const n = 10
	for cx := range int32(n) {
		for cz := range int32(n) {
			pos := ChunkPos{X: cx, Z: cz}
			w.Acquire(pos)
			w.Release(pos)
		}
	}
	g.FlushProtos()
	mem, _, spilled := g.ProtoStats()
	// The chunks 1..n-2 have final neighbours all round; the others keep a
	// proto, as do the chunks around the block that its features wrote into.
	if left, max := mem+spilled, (n+4)*(n+4)-(n-2)*(n-2); left > max {
		t.Errorf("%d protos left, want at most %d", left, max)
	}
	g.claimAll()
	for x := int32(1); x <= n-2; x++ {
		for z := int32(1); z <= n-2; z++ {
			if g.protos.lookup(ChunkPos{X: x, Z: z}) != nil {
				t.Errorf("the proto of the complete chunk (%d, %d) is kept", x, z)
			}
		}
	}
	g.releaseAll()
	files, _ := os.ReadDir(filepath.Join(w.storage, "proto"))
	if len(files) != spilled {
		t.Errorf("%d proto files on disk, %d protos spilled", len(files), spilled)
	}
}

// TestNetherGenerateInParallel runs Generate from several goroutines at once,
// which is what chunk sends do; the race detector judges the locking.
func TestNetherGenerateInParallel(t *testing.T) {
	t.Parallel()
	g := NewNetherGenerator(3)
	var wg sync.WaitGroup
	for i := range int32(8) {
		wg.Go(func() { g.Generate(newChunk(i%4, i/4)) })
	}
	wg.Wait()
}

// TestDecorProtoLookupDuringSpill holds a trim in its file write: lookups go on,
// and a victim taken back stays in memory with no file left behind.
func TestDecorProtoLookupDuringSpill(t *testing.T) {
	var store decorProtoStore
	store.attach(t.TempDir())
	store.cap = 1
	victim, kept := ChunkPos{X: 0}, ChunkPos{X: 10}
	for _, pos := range []ChunkPos{victim, kept} {
		p := store.ensure(pos)
		p.set(0, 0, 0, stoneState)
		store.touch(p)
	}
	writing, unblock := make(chan struct{}), make(chan struct{})
	writeDecorProtoFile = func(name string, data []byte, perm os.FileMode) error {
		close(writing)
		<-unblock
		return os.WriteFile(name, data, perm)
	}
	t.Cleanup(func() { writeDecorProtoFile = os.WriteFile })
	var wg sync.WaitGroup
	wg.Go(func() { store.spillVictims(store.pick(func(pos ChunkPos) bool { return pos == kept })) })
	<-writing
	for _, pos := range []ChunkPos{victim, kept} {
		if p := store.lookup(pos); p == nil {
			t.Fatalf("proto %v lost during a spill", pos)
		} else if state, ok := p.get(0, 0, 0); !ok || state != stoneState {
			t.Errorf("proto %v lost its block during a spill", pos)
		}
	}
	close(unblock)
	wg.Wait()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.mem[victim] == nil {
		t.Error("victim taken back is not in memory")
	}
	if _, ok := store.spilled[victim]; ok {
		t.Error("victim taken back is recorded as spilled")
	}
	if _, err := os.Stat(store.path(victim)); !os.IsNotExist(err) {
		t.Errorf("victim taken back left its file: %v", err)
	}
}

// TestDecorProtoClaimDuringSpill registers a claim after a trim picked its
// victims and before it writes them: a proto the claim creates is no victim,
// and a victim it takes back keeps its writes.
func TestDecorProtoClaimDuringSpill(t *testing.T) {
	var g decorState
	g.protos.attach(t.TempDir())
	g.protos.cap = 1
	victim, fresh := ChunkPos{X: 0}, ChunkPos{X: 1}
	for _, pos := range []ChunkPos{victim, {X: 20}} {
		g.protos.touch(g.protos.ensure(pos))
	}
	writing, unblock := make(chan struct{}), make(chan struct{})
	var once sync.Once
	writeDecorProtoFile = func(name string, data []byte, perm os.FileMode) error {
		once.Do(func() { close(writing) })
		<-unblock
		return os.WriteFile(name, data, perm)
	}
	t.Cleanup(func() { writeDecorProtoFile = os.WriteFile })
	var wg sync.WaitGroup
	wg.Go(g.trim)
	<-writing
	g.claim(fresh)
	g.protos.ensure(fresh).set(0, 0, 0, stoneState)
	own := g.protos.ensure(victim)
	own.set(0, 0, 0, stoneState)
	own.final = true
	g.release(fresh)
	close(unblock)
	wg.Wait()
	for _, pos := range []ChunkPos{victim, fresh} {
		if g.protos.mem[pos] == nil {
			t.Errorf("proto %v the claim wrote into is not in memory", pos)
		} else if state, ok := g.protos.mem[pos].get(0, 0, 0); !ok || state != stoneState {
			t.Errorf("proto %v lost the claim's block", pos)
		}
		if _, ok := g.protos.spilled[pos]; ok {
			t.Errorf("proto %v the claim wrote into is recorded as spilled", pos)
		}
	}
	if !g.protos.final(victim) {
		t.Error("victim taken back lost final")
	}
}

// TestSpilledProtoFlagsNeedNoReadBack answers a spilled proto's flags and
// drops it once complete without reading it back, also after a restart.
func TestSpilledProtoFlagsNeedNoReadBack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var first decorProtoStore
	first.attach(dir)
	pos := ChunkPos{X: 3, Z: -2}
	p := first.ensure(pos)
	p.decorated = true
	p.set(0, 0, 0, stoneState)
	first.spill(pos)
	first.markFinal(pos)
	first.markFinal(ChunkPos{X: pos.X - 1, Z: pos.Z - 1})
	first.flush()
	var store decorProtoStore
	store.attach(dir)
	for _, s := range []*decorProtoStore{&first, &store} {
		if f, _ := s.flags(pos); !f.decorated || f.final || f.finals != 0b000010001 {
			t.Fatalf("spilled flags: decorated %v final %v finals %09b, want true false 000010001", f.decorated, f.final, f.finals)
		}
	}
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			store.markFinal(ChunkPos{X: pos.X + dx, Z: pos.Z + dz})
		}
	}
	if _, err := os.Stat(store.path(pos)); !os.IsNotExist(err) {
		t.Errorf("complete spilled proto kept its file: %v", err)
	}
	if _, ok := store.flags(pos); ok {
		t.Error("complete spilled proto still listed")
	}
	if n := store.readbacks.Load(); n != 0 {
		t.Errorf("readbacks = %d, want 0", n)
	}
}

// TestGeneratedSculkSensorHasItsBlockEntity pins a sculk sensor a seed-0 sculk
// patch places at (414, -38, -1763), which vanilla saves with a fresh block
// entity although the feature gave it no data.
// vanilla: WorldGenRegion.setBlock, LevelChunk.promotePendingBlockEntity
func TestGeneratedSculkSensorHasItsBlockEntity(t *testing.T) {
	t.Parallel()
	const x, y, z = 414, -38, -1763
	c := newChunk(x>>4, z>>4)
	NewOverworldGenerator(0).Generate(c)
	if got := c.BlockAt(x&15, y, z&15); registry.BlockOf(got) != registry.BlockSculkSensor {
		t.Fatalf("block at %d,%d,%d is %s, want a sculk sensor", x, y, z, registry.BlockOf(got).Name())
	}
	be := c.blockEntity(x, y, z)
	if be == nil {
		t.Fatal("the generated sculk sensor has no block entity")
	}
	if be.Kind != KindSculkSensor {
		t.Fatalf("block entity kind %s, want sculk_sensor", be.Kind)
	}
	// The fresh entity's fields, as vanilla's chunk saves them.
	want := nbt.Compound{
		"id": nbt.String("minecraft:sculk_sensor"), "x": nbt.Int(x), "y": nbt.Int(y), "z": nbt.Int(z),
		"components":               nbt.Compound{},
		"last_vibration_frequency": nbt.Int(0),
		"listener":                 nbt.Compound{"event_delay": nbt.Int(0), "selector": nbt.Compound{"tick": nbt.Long(-1)}},
	}
	if got := be.EncodeNBT(); !nbt.Equal(got, want) {
		t.Errorf("saved %v, want %v", got, want)
	}
}

func TestDecorWriteDropsReplacedBlockEntity(t *testing.T) {
	r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
	r.set(4, 20, 4, registry.BlockChest.DefaultState())
	r.placeBlockEntity(4, 20, 4, registry.BlockChest.DefaultState(), nbt.Compound{"id": nbt.String("minecraft:chest")})
	r.set(6, 20, 4, registry.BlockSpawner.DefaultState())
	r.placeBlockEntity(6, 20, 4, registry.BlockSpawner.DefaultState(), nbt.Compound{"id": nbt.String("minecraft:mob_spawner")})
	r.spawnStructureMob("minecraft:zombie", "zz", 4, 20, 4, false)

	r.setKnownShape(4, 20, 4, registry.BlockCobblestone.DefaultState())
	r.set(6, 20, 4, registry.BlockSpawner.DefaultState())

	var kept []nbt.Compound
	for _, e := range r.protoAt(4, 4).entities {
		kept = append(kept, e.tag)
	}
	if len(kept) != 2 || kept[0]["id"] != nbt.String("minecraft:mob_spawner") || kept[1]["id"] != nbt.String("minecraft:zombie") {
		t.Errorf("entities after overwriting the chest with cobblestone = %v, want the spawner and the mob", kept)
	}
}

// TestChangesFromReturnsHiveBees checks that a live placement hands back a bee
// nest's block entity with its bees, and a structure mob without its UUID.
func TestChangesFromReturnsHiveBees(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	center := w.chunkIfLoaded(ChunkPos{})
	r := newDecorRegion(&liveTerrain{w: w, chunks: map[ChunkPos]*shapedTerrain{}}, center, w.MinY(), w.Height())
	r.set(4, 70, 5, registry.BlockBeeNest.DefaultState())
	p := r.protoAt(4, 5)
	p.entities = append(p.entities, protoEntity{hive: true, x: 4, y: 70 - w.MinY(), z: 5, lootSeed: 2<<30 | 7<<10 | 3})
	r.spawnStructureMob("minecraft:witch", "w", 6, 70, 6, false)

	var extras PlacementExtras
	r.changesFrom(w, center, &extras)
	if len(extras.BlockEntities) != 1 {
		t.Fatalf("block entities %v, want the bee nest", extras.BlockEntities)
	}
	hive := extras.BlockEntities[0]
	if hive["id"] != nbt.String("minecraft:beehive") || hive["x"] != nbt.Int(4) || hive["y"] != nbt.Int(70) || hive["z"] != nbt.Int(5) {
		t.Errorf("hive %v, want minecraft:beehive at (4,70,5)", hive)
	}
	bees, _ := nbt.Get[nbt.List](hive, "bees")
	if len(bees.Items) != 2 || bees.Items[0].(nbt.Compound)["ticks_in_hive"] != nbt.Int(3) || bees.Items[1].(nbt.Compound)["ticks_in_hive"] != nbt.Int(7) {
		t.Errorf("bees %v, want two with ticks_in_hive 3 and 7", bees)
	}
	if len(extras.Entities) != 1 || extras.Entities[0].ID != "minecraft:witch" || extras.Entities[0].Pos != [3]float64{6.5, 70, 6.5} {
		t.Fatalf("entities %v, want the witch at (6.5,70,6.5)", extras.Entities)
	}
	if _, ok := extras.Entities[0].Tag["UUID"]; ok {
		t.Error("the witch keeps its generated UUID")
	}
}
