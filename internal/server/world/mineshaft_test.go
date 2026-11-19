package world

import (
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
