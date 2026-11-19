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
	t.Parallel()
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
