package world

import (
	"crypto/sha256"
	"fmt"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"os"
	"strings"
	"testing"
)

func TestMansionStartsMatchVanilla(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/mansion_starts.txt")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var seed int64
		var x, z int32
		var count int
		var want string
		if _, err := fmt.Sscan(line, &seed, &x, &z, &count, &want); err != nil {
			t.Fatal(err)
		}
		g := NewOverworldGenerator(seed)
		set := levelgen.StructureSetNamed("minecraft:woodland_mansions")
		if !set.IsStructureChunk(seed, x, z, nil) {
			t.Fatal("start outside placement grid")
		}
		s := g.growStructure("minecraft:mansion", x, z)
		if s == nil {
			t.Fatalf("seed %d: no mansion", seed)
		}
		var children []nbt.Tag
		for _, p := range s.pieces {
			children = append(children, p.nbt())
		}
		enc, _ := nbt.Encode("", structureStartNBT(s.def.Name, x, z, children))
		if got := fmt.Sprintf("%x", sha256.Sum256(enc)); got != want || len(children) != count {
			t.Errorf("seed %d: %d pieces, digest %s; vanilla %d, %s", seed, len(children), got, count, want)
		}
		g.DisableStructures()
		if len(g.structureStartsIn(x, z)) != 0 {
			t.Fatal("disabled structures grew mansion")
		}
		checked++
	}
	if checked != 2 {
		t.Fatalf("checked %d starts, want 2", checked)
	}
}

func TestMansionMarkersAndFoundation(t *testing.T) {
	r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
	r.bindProtos(ChunkPos{})
	p := &mansionPiece{Rotation: 1, Mirror: 2}
	for i, name := range []string{"ChestWest", "Mage", "Warrior", "Group of Allays"} {
		p.marker(r, TemplateBlock{Pos: [3]int32{int32(2 + i*2), 120, 8}, NBT: nbt.Compound{"metadata": nbt.String(name)}})
	}
	r.set(12, 120, 12, registry.BlockDarkOakPlanks.DefaultState())
	r.set(12, 116, 12, registry.BlockStone.DefaultState())
	s := &structureStart{pieces: []structurePiece{&testPiece{jbox{12, 120, 12, 12, 120, 12}}}}
	mansionStructure{}.afterPlace(r, jbox{0, worldMinY, 0, 15, 319, 15}, s)
	for y := int32(117); y < 120; y++ {
		if registry.BlockOf(r.get(12, y, 12)) != registry.BlockCobblestone {
			t.Fatalf("foundation gap at %d", y)
		}
	}
	if registry.BlockOf(r.get(12, 116, 12)) != registry.BlockStone || !r.src.isAir(r.get(13, 119, 12)) {
		t.Fatal("foundation replaced ground or escaped piece")
	}
	// Pending markers must survive saving a proto before the final chunk handoff.
	own := r.protoAt(0, 0)
	saved, err := decodeDecorProto(appendDecorProto(nil, own))
	if err != nil {
		t.Fatal(err)
	}
	r.finishOverworld(saved)
	c := r.center
	if registry.StateValue(c.BlockAt(2, 120, 8), registry.PropFacing) != "north" {
		t.Fatal("marker chest was mirrored")
	}
	be := c.blockEntity(2, 120, 8)
	if be == nil || be.LootTable != "minecraft:chests/woodland_mansion" || be.LootTableSeed == 0 {
		t.Fatal("missing chest loot")
	}
	counts := map[nbt.String]int{}
	for _, e := range c.generatedEntities {
		id, _ := nbt.Get[nbt.String](e, "id")
		counts[id]++
		if _, ok := nbt.UUIDField(e, "UUID"); !ok {
			t.Fatal("mob has no UUID")
		}
		if !nbt.BoolField(e, "PersistenceRequired") {
			t.Fatal("mob can despawn")
		}
		if !nbt.BoolField(e, StructureFinalizeKey) {
			t.Fatal("structure mob is not marked for finalization")
		}
	}
	if counts["minecraft:evoker"] != 1 || counts["minecraft:vindicator"] != 1 || counts["minecraft:allay"] < 1 || counts["minecraft:allay"] > 3 {
		t.Fatalf("mobs %v", counts)
	}
}

func BenchmarkMansionChunkGenerate(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		NewOverworldGenerator(0).Generate(newChunk(-221, -52))
	}
}
