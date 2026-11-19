package world

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestShipwreckContinuationAfterRestart(t *testing.T) {
	t.Parallel()
	const id = "minecraft:shipwreck_beached"
	// The vanilla fixture pins this beached, rotated wreck across chunk boundaries.
	newGenerator := func() *OverworldGenerator {
		g := NewOverworldGenerator(0)
		g.placed = nil
		g.structures.types = map[string]structureType{id: shipwreckStructure{beached: true}}
		return g
	}
	whole, first := newGenerator(), newGenerator()
	dir := t.TempDir()
	first.OpenProtos(dir)
	for _, g := range []*OverworldGenerator{whole, first} {
		g.DecorateInOrder([]ChunkPos{{14, 8}})
	}
	start := first.structureStartsIn(14, 8)[0]
	saved := start.pieces[0].nbt()
	if !nbt.BoolField(saved, "height_adjusted") {
		t.Fatal("first chunk did not adjust height")
	}
	first.FlushProtos()
	resumed := newGenerator()
	resumed.OpenProtos(dir)
	restored := resumed.structureStartsIn(14, 8)[0]
	if !reflect.DeepEqual(restored.pieces[0].nbt(), saved) {
		t.Fatalf("restored TPY/height_adjusted and piece: got %v, want %v", restored.pieces[0].nbt(), saved)
	}
	// Generate a previously undecorated piece chunk after reopening. This also
	// catches consuming a second beach-height roll before the chest loot seeds.
	box := start.pieces[0].bounds()
	checked := 0
	for x := box.minX >> 4; x <= box.maxX>>4; x++ {
		for z := box.minZ >> 4; z <= box.maxZ>>4; z++ {
			if x == 14 && z == 8 {
				continue
			}
			want, got := newChunk(x, z), newChunk(x, z)
			whole.Generate(want)
			resumed.Generate(got)
			if !bytes.Equal(encodeWhole(got), encodeWhole(want)) || !reflect.DeepEqual(got.entities, want.entities) {
				t.Fatalf("continuation chunk %d,%d differs in blocks or chest data", x, z)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("wreck did not cross a chunk boundary")
	}
	if !reflect.DeepEqual(restored.pieces[0].nbt(), whole.structureStartsIn(14, 8)[0].pieces[0].nbt()) {
		t.Fatal("continued piece position or height_adjusted differs")
	}
	// A second flush must retain starts restored at open, even with no new placement.
	resumed.FlushProtos()
	idle := newGenerator()
	idle.OpenProtos(dir)
	idle.FlushProtos()
	again := newGenerator()
	again.OpenProtos(dir)
	if !reflect.DeepEqual(again.structureStartsIn(14, 8)[0].pieces[0].nbt(), saved) {
		t.Fatal("second restart lost adjusted start")
	}
}
