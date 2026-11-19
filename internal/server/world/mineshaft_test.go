package world

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func mineshaftGenerator() *OverworldGenerator {
	g := NewOverworldGenerator(0)
	g.structures.types = map[string]structureType{"minecraft:mineshaft": mineshaftStructure{}}
	return g
}

// TestMineshaftGrowsARoomThenConnectedPieces checks the start's shape: a room
// first, every piece within the 80-block reach and below sea level.
func TestMineshaftGrowsARoomThenConnectedPieces(t *testing.T) {
	t.Parallel()
	g := mineshaftGenerator()
	s := firstStart(t, g, 60)
	room, ok := s.pieces[0].(*mineshaftPiece)
	if !ok || room.kind != msRoom {
		t.Fatalf("first piece is %T", s.pieces[0])
	}
	if len(s.pieces) < 2 {
		t.Fatalf("a start of %d pieces", len(s.pieces))
	}
	for _, sp := range s.pieces {
		p := sp.(*mineshaftPiece)
		if absInt32(p.box.minX-room.box.minX) > 90 || absInt32(p.box.minZ-room.box.minZ) > 90 {
			t.Errorf("piece %v strays from the room %v", p.box, room.box)
		}
		if p.box.maxY >= SeaLevel-msSeaOffset {
			t.Errorf("piece %v is not below sea level", p.box)
		}
	}
	c := room.nbt()
	if id, _ := nbt.Get[nbt.String](c, "id"); id != "minecraft:msroom" {
		t.Errorf("room saved as %v", c)
	}
}

// TestMineshaftIsPlacedIntoItsChunks generates the chunks around a start and
// finds the cave air, planks and fences it places.
func TestMineshaftIsPlacedIntoItsChunks(t *testing.T) {
	t.Parallel()
	g := mineshaftGenerator()
	s := firstStart(t, g, 60)
	planks := registry.BlockOf(registry.BlockStates["minecraft:oak_planks"])
	counts := map[registry.Block]int{}
	for dx := range int32(2) {
		c := newChunk(s.chunkX+dx, s.chunkZ)
		g.Generate(c)
		for y := int32(worldMinY); y < 63; y++ {
			for x := range int32(16) {
				for z := range int32(16) {
					counts[registry.BlockOf(c.BlockAt(x, y, z))]++
				}
			}
		}
	}
	if counts[registry.BlockCaveAir] == 0 || counts[planks] == 0 {
		t.Errorf("cave air %d, planks %d", counts[registry.BlockCaveAir], counts[planks])
	}
}

// TestMineshaftSpawnerEntityIsOnItsBlock places a cave spider spawner below
// y=0 and finds its block entity on the spawner block.
func TestMineshaftSpawnerEntityIsOnItsBlock(t *testing.T) {
	t.Parallel()
	g := mineshaftGenerator()
	c := newChunk(0, 0)
	r := newDecorRegion(g, c, worldMinY, worldHeight)
	r.nx, r.nz = 0, 0
	var x, y, z int32 = 5, -51, 4
	spawner := mineshaftBlocks().spawner
	r.set(x, y, z, spawner)
	r.placeBlockEntity(x, y, z, spawner, nbt.Compound{
		"id":        nbt.String(KindSpawner.Name()),
		"SpawnData": nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:cave_spider")}},
	})
	r.finishOverworld(r.protoAt(x, z))
	be := c.blockEntity(x, y, z)
	if be == nil || be.Kind != KindSpawner {
		t.Fatalf("no spawner block entity at %d,%d,%d: %v", x, y, z, be)
	}
	if got := c.BlockAt(x, y, z); got != spawner {
		t.Errorf("block %d, want the spawner %d", got, spawner)
	}
}

// TestMineshaftChestMinecartIsGeneratedWithItsLoot stands a cart in a proto and
// finds it, with its loot table, among the generated entities of the chunk.
func TestMineshaftChestMinecartIsGeneratedWithItsLoot(t *testing.T) {
	t.Parallel()
	g := mineshaftGenerator()
	c := newChunk(2, -3)
	r := newDecorRegion(g, c, worldMinY, worldHeight)
	r.nx, r.nz = 2, -3
	var x, y, z int32 = 2*16 + 5, -40, -3*16 + 4
	r.set(x, y, z, airState)
	r.set(x, y-1, z, registry.BlockStates["minecraft:stone"])
	rng := levelgen.NewLegacyRandom(7)
	wantRNG := levelgen.NewLegacyRandom(7)
	wantRNG.NextBoolean() // rail orientation
	wantSeed := wantRNG.NextLong()
	w := mineshaftWriter{r: r, g: g, chunk: jbox{minX: 32, minY: worldMinY, minZ: -48, maxX: 47, maxY: 319, maxZ: -33}, p: &mineshaftPiece{}, st: mineshaftBlocks(), rng: rng}
	w.chestMinecart(x, y, z)
	p := r.protoAt(x, z)
	r.finishOverworld(p)
	got := c.generatedEntities
	if len(got) != 1 {
		t.Fatalf("%d generated entities, want 1", len(got))
	}
	pos, _ := got[0]["Pos"].(nbt.List)
	if len(pos.Items) != 3 || pos.Items[1] != nbt.Double(-39.5) {
		t.Errorf("Pos %v, want y -39.5", pos.Items)
	}
	if got[0]["LootTable"] != nbt.String("minecraft:chests/abandoned_mineshaft") {
		t.Errorf("loot table %v", got[0]["LootTable"])
	}
	if seed, _ := nbt.LongValue(got[0]["LootTableSeed"]); seed != wantSeed {
		t.Errorf("loot seed %d, want %d", seed, wantSeed)
	}
	rot, _ := got[0]["Rotation"].(nbt.List)
	if rot.Elem != nbt.TypeFloat || len(rot.Items) != 2 || rot.Items[0] != nbt.Float(0) || rot.Items[1] != nbt.Float(0) {
		t.Errorf("Rotation %v, want [0f, 0f]", got[0]["Rotation"])
	}
	if got[0]["FlippedRotation"] != nbt.Byte(0) {
		t.Errorf("FlippedRotation %v, want 0b", got[0]["FlippedRotation"])
	}
	if c.blockEntity(x, y, z) != nil {
		t.Error("the cart became a block entity")
	}
}

// TestMineshaftPendingCartsStayWithTheirChunks keeps two carts per chunk
// across 1025 pending chunks, including both carts of each late chunk.
func TestMineshaftPendingCartsStayWithTheirChunks(t *testing.T) {
	g := NewOverworldGenerator(3)
	chunks := make([]*Chunk, 1025)
	for i := range chunks {
		c := newChunk(int32(i), 0)
		chunks[i] = c
		for x := range int32(2) {
			_, cart := protoEntity{x: x, tag: nbt.Compound{"id": nbt.String("minecraft:chest_minecart")}}.resolve(c.x, c.z, worldMinY, g.seed)
			c.generatedEntities = append(c.generatedEntities, cart)
		}
	}
	for i, c := range chunks {
		if len(c.generatedEntities) != 2 {
			t.Fatalf("chunk %d has %d carts, want 2", i, len(c.generatedEntities))
		}
		for j, tag := range c.generatedEntities {
			pos := tag["Pos"].(nbt.List)
			if pos.Items[0] != nbt.Double(float64(i*16+j)+0.5) {
				t.Fatalf("chunk %d cart %d: position %v", i, j, pos)
			}
		}
	}
}

// TestMineshaftCartsSavedBeforeWake keeps both carts when terrain reaches disk
// without a server entity wake, then loads that terrain without new carts.
func TestMineshaftCartsSavedBeforeWake(t *testing.T) {
	dir := t.TempDir()
	pos := ChunkPos{X: -19, Z: 10}
	w, err := NewWorld(dir, NewOverworldGenerator(3))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Acquire(pos)
	if got := len(w.chunks[pos].chunk.generatedEntities); got != 2 {
		t.Fatalf("generated %d carts, want 2", got)
	}
	// An entity write failure must keep terrain unsaved and carts available.
	if err := os.MkdirAll(w.storage, 0755); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(w.storage, "entities")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	loaded := w.chunks[pos]
	if _, err := w.saveLoaded(pos, loaded, loaded.dirtyGen); err == nil {
		t.Fatal("terrain save succeeded despite the blocked entity directory")
	}
	if c, err := w.loadChunk(pos); err != nil || c != nil {
		t.Fatalf("terrain reached disk before entities: chunk %v, error %v", c, err)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	w.Release(pos)
	w.FlushSaves()
	w.Acquire(pos)
	defer w.Release(pos)
	if got := len(w.chunks[pos].chunk.generatedEntities); got != 0 {
		t.Fatalf("reloaded chunk generated %d new carts", got)
	}
	tags, has, err := w.LoadChunkEntities(pos)
	if err != nil || !has || len(tags) != 2 {
		t.Fatalf("saved carts %v, entry %v, error %v", tags, has, err)
	}
	first, firstOK := nbt.CompoundView(tags[0])
	second, secondOK := nbt.CompoundView(tags[1])
	firstID, firstHasID := nbt.UUIDField(first, "UUID")
	secondID, secondHasID := nbt.UUIDField(second, "UUID")
	if !firstOK || !secondOK || !firstHasID || !secondHasID {
		t.Fatal("saved carts are missing compound UUIDs")
	}
	if firstID == secondID {
		t.Fatal("two carts have the same UUID")
	}
}

// TestMineshaftCartHandoffCannotRegenerateRemovedCarts consumes the wake's
// tags, then saves an empty entity list without resurrecting the starting carts.
func TestMineshaftCartHandoffCannotRegenerateRemovedCarts(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewOverworldGenerator(3))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := ChunkPos{X: -19, Z: 10}
	w.Acquire(pos)
	if got := len(w.TakeGeneratedEntities(pos)); got != 2 {
		t.Fatalf("wake got %d carts, want 2", got)
	}
	if got := len(w.TakeGeneratedEntities(pos)); got != 0 {
		t.Fatalf("second wake got %d carts", got)
	}
	if err := w.SaveChunkEntities(pos, nil); err != nil {
		t.Fatal(err)
	}
	w.Release(pos)
	w.FlushSaves()
	tags, _, err := w.LoadChunkEntities(pos)
	if err != nil || len(tags) != 0 {
		t.Fatalf("removed carts regenerated: %v, error %v", tags, err)
	}
}
