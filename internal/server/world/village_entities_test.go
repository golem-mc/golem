package world

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// One village-centre chunk is enough to exercise villagers, a golem and a cat.
var villageEntityChunk = sync.OnceValue(func() *Chunk {
	c := newChunk(16, 58)
	NewOverworldGenerator(0).Generate(c)
	return c
})

func TestVillageTemplateEntitiesReachTheirChunk(t *testing.T) {
	c := villageEntityChunk()
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, tag := range c.generatedEntities {
		id, _ := nbt.Get[nbt.String](tag, "id")
		counts[string(id)]++
		pos, _ := nbt.Get[nbt.List](tag, "Pos")
		x, z := float64(pos.Items[0].(nbt.Double)), float64(pos.Items[2].(nbt.Double))
		if ChunkOf(x) != c.x || ChunkOf(z) != c.z {
			t.Fatalf("entity at %v belongs to another chunk", pos)
		}
		uid := fmt.Sprint(tag["UUID"])
		if seen[uid] {
			t.Fatal("duplicate template UUID")
		}
		seen[uid] = true
		if id == "minecraft:villager" && !nbt.BoolField(tag, "AssignProfessionWhenSpawned") {
			t.Fatal("village villager was not finalized for a structure spawn")
		}
	}
	want := map[string]int{"minecraft:villager": 2, "minecraft:iron_golem": 1, "minecraft:cat": 1}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("entities %v, want %v", counts, want)
	}
}

func TestJigsawEntitiesRotateClipAndSurviveProtoSaving(t *testing.T) {
	original := nbt.Compound{"id": nbt.String("minecraft:villager"), "Age": nbt.Int(-24000),
		"UUID": nbt.IntArray{1, 2, 3, 4}, "Rotation": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(30), nbt.Float(5)}}}
	tpl := &jigsawTemplate{id: "test:villagers", Template: &Template{
		Palettes: [][]TemplateBlock{{}},
		Entities: []TemplateEntity{
			{Pos: [3]float64{0.5, 1, 0.5}, BlockPos: [3]int32{0, 1, 0}, NBT: original},
			{Pos: [3]float64{16.5, 1, 0.5}, BlockPos: [3]int32{16, 1, 0}, NBT: original},
		},
	}}
	snapshot := nbt.Copy(original)
	for rot := RotateNone; rot <= RotateCounterclockwise90; rot++ {
		src := newGoldenTerrain()
		r := &decorRegion{src: src, center: newChunk(-1, -1), minY: worldMinY, height: worldHeight}
		r.bindProtos(ChunkPos{-1, -1})
		p := &jigsawPiece{pos: [3]int32{-8, -32, -8}, rotation: rot, element: &jigsawElement{template: tpl}}
		p.place(r, jbox{-16, worldMinY, -16, -1, 319, -1}, p.pos)
		proto := r.protoAt(-8, -8)
		if len(proto.entities) != 1 {
			t.Fatalf("rotation %d: got %d entities, want 1", rot, len(proto.entities))
		}
		before := proto.entities[0]
		tag := before.tag
		if tag["Age"] != nbt.Int(-24000) || reflect.DeepEqual(tag["UUID"], original["UUID"]) {
			t.Fatalf("template fields or identity lost: %v", tag)
		}
		if before.y != -31-worldMinY {
			t.Fatalf("proto stored world Y instead of relative Y: %d", before.y)
		}
		rotation, _ := nbt.Get[nbt.List](tag, "Rotation")
		if float32(rotation.Items[0].(nbt.Float)) != turnedYaw(30, MirrorNone, rot) || rotation.Items[1] != nbt.Float(5) {
			t.Fatalf("rotation %d: facing %v", rot, rotation)
		}
		// A second generation uses a fresh region, not a second placement in the same proto.
		replay := &decorRegion{src: newGoldenTerrain(), center: newChunk(-1, -1), minY: worldMinY, height: worldHeight}
		replay.bindProtos(ChunkPos{-1, -1})
		p.place(replay, jbox{-16, worldMinY, -16, -1, 319, -1}, p.pos)
		if !reflect.DeepEqual(before.tag, replay.protoAt(-8, -8).entities[0].tag) {
			t.Fatal("regeneration changed entity identity")
		}
		saved, err := decodeDecorProto(appendDecorProto(nil, proto))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(saved.entities, proto.entities) {
			t.Fatal("entity changed across proto save")
		}
		r.finishOverworld(saved)
		if len(r.center.generatedEntities) != 1 || !reflect.DeepEqual(r.center.generatedEntities[0], tag) {
			t.Fatal("finishing chunk did not transfer its entity")
		}
	}
	if !reflect.DeepEqual(original, snapshot) {
		t.Fatal("placement mutated shared template NBT")
	}
}

// villageHandoffGenerator reuses the expensive generation fixture for save/wake cases.
type villageHandoffGenerator struct{ *FlatGenerator }

func (g villageHandoffGenerator) Generate(c *Chunk) {
	g.FlatGenerator.Generate(c)
	if c.x == 16 && c.z == 58 {
		for _, tag := range villageEntityChunk().generatedEntities {
			c.generatedEntities = append(c.generatedEntities, nbt.Copy(tag).(nbt.Compound))
		}
	}
}

func TestVillageEntitiesSaveBeforeWakeAndNeverRegenerateAfterRemoval(t *testing.T) {
	dir := t.TempDir()
	open := func() *World {
		w, err := NewWorld(dir, villageHandoffGenerator{NewFlatGenerator()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = w.Close() })
		return w
	}
	pos := ChunkPos{16, 58}
	w := open()
	w.Acquire(pos)
	w.Release(pos)
	w.FlushSaves()
	w.Close()
	w = open()
	w.Acquire(pos)
	tags, has, err := w.LoadChunkEntities(pos)
	if err != nil || !has || len(tags) != 4 {
		t.Fatalf("saved village: %d tags, entry %v, error %v", len(tags), has, err)
	}
	for i, tag := range tags {
		if !nbt.Equal(tag, villageEntityChunk().generatedEntities[i]) {
			t.Fatal("save before wake lost template data")
		}
	}
	if len(w.TakeGeneratedEntities(pos)) != 0 {
		t.Fatal("saved chunk regenerated its villagers")
	}
	if err := w.SaveChunkEntities(pos, nil); err != nil {
		t.Fatal(err)
	}
	w.Release(pos)
	w.FlushSaves()
	w.Close()
	w = open()
	w.Acquire(pos)
	defer w.Release(pos)
	tags, _, err = w.LoadChunkEntities(pos)
	if err != nil || len(tags) != 0 || len(w.TakeGeneratedEntities(pos)) != 0 {
		t.Fatal("removed village entities resurrected")
	}
}

func TestVillageEntityHandoffRunsOnce(t *testing.T) {
	w, err := NewWorld(t.TempDir(), villageHandoffGenerator{NewFlatGenerator()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := ChunkPos{16, 58}
	w.Acquire(pos)
	defer w.Release(pos)
	if got := len(w.TakeGeneratedEntities(pos)); got != 4 {
		t.Fatalf("wake got %d entities, want 4", got)
	}
	if len(w.TakeGeneratedEntities(pos)) != 0 {
		t.Fatal("a second wake got template entities")
	}
}

func TestJigsawListElementsKeepDistinctEntityIdentities(t *testing.T) {
	r := &decorRegion{src: newGoldenTerrain(), center: newChunk(0, 0), minY: worldMinY, height: worldHeight}
	r.bindProtos(ChunkPos{})
	el := &jigsawElement{template: &jigsawTemplate{id: "test:repeated", Template: &Template{
		Palettes: [][]TemplateBlock{{}},
		Entities: []TemplateEntity{{Pos: [3]float64{0.5, 0, 0.5}, NBT: nbt.Compound{"id": nbt.String("minecraft:villager")}}},
	}}}
	p := &jigsawPiece{element: &jigsawElement{list: []*jigsawElement{el, el}}, pos: [3]int32{1, 64, 1}}
	p.place(r, jbox{0, worldMinY, 0, 15, 319, 15}, p.pos)
	got := r.protoAt(1, 1).entities
	if len(got) != 2 || reflect.DeepEqual(got[0].tag["UUID"], got[1].tag["UUID"]) {
		t.Fatal("repeated list elements lost an entity or reused its UUID")
	}
}
