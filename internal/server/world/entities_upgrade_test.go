package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// plantEntityRoot writes root as pos's entity record.
func plantEntityRoot(t *testing.T, w *World, pos ChunkPos, root nbt.Compound) {
	t.Helper()
	rf, err := w.entityRegion(regionCoords(pos))
	if err != nil {
		t.Fatal(err)
	}
	defer w.releaseEntityRegion(rf)
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := rf.writeChunk(pos, data); err != nil {
		t.Fatal(err)
	}
}

// TestOlderEntityFilesAreUpgradedOnLoad checks an entity file below 4301 has
// a saddled pig and a horse with a pig riding it moved to the saddle and
// equipment slots, one at 4301 is read as stored, and one below the chunk
// floor is refused.
// vanilla: SaddleEquipmentSlotFix, EquipmentFormatFix
func TestOlderEntityFilesAreUpgradedOnLoad(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	saddle := nbt.Compound{"id": nbt.String("minecraft:saddle"), "count": nbt.Int(1)}
	armor := nbt.Compound{"id": nbt.String("minecraft:iron_horse_armor"), "count": nbt.Int(1)}
	entities := func() nbt.List {
		return nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"id":              nbt.String("horse"),
			"SaddleItem":      saddle,
			"body_armor_item": armor,
			"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
				"id": nbt.String("minecraft:pig"), "Saddle": nbt.Byte(1),
			}}},
		}}}
	}
	plant := func(pos ChunkPos, version int32) {
		plantEntityRoot(t, w, pos, nbt.Compound{"DataVersion": nbt.Int(version), "Entities": entities()})
	}
	old, current, ancient := ChunkPos{X: 0, Z: 0}, ChunkPos{X: 1, Z: 0}, ChunkPos{X: 2, Z: 0}
	plant(old, 4189)
	plant(current, equipmentFormatVersion)
	plant(ancient, entityDataVersionFloor-1)

	got, _, err := w.LoadChunkEntities(old)
	if err != nil || len(got) != 1 {
		t.Fatalf("entities = %v, %v; want the horse", got, err)
	}
	chances := nbt.Compound{"saddle": nbt.Float(2)}
	wantPig := nbt.Compound{
		"id":           nbt.String("minecraft:pig"),
		"equipment":    nbt.Compound{"saddle": saddle},
		"drop_chances": chances,
	}
	want := nbt.Compound{
		"id":           nbt.String("horse"),
		"equipment":    nbt.Compound{"saddle": saddle, "body": armor},
		"drop_chances": chances,
		"Passengers":   nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{wantPig}},
	}
	if !nbt.Equal(got[0], want) {
		t.Errorf("the 4189 horse loaded as %v, want %v", got[0], want)
	}

	if err := w.SaveChunkEntities(old, got); err != nil {
		t.Fatalf("resaving upgraded entities: %v", err)
	}
	got, _, err = w.LoadChunkEntities(old)
	if err != nil || len(got) != 1 || !nbt.Equal(got[0], want) {
		t.Fatalf("resaved horse = %v, %v; want %v", got, err, want)
	}

	got, _, err = w.LoadChunkEntities(current)
	if err != nil || len(got) != 1 || !nbt.Equal(got[0], entities().Items[0]) {
		t.Errorf("the 4301 horse loaded as %v, %v; want it as stored", got, err)
	}
	if _, _, err := w.LoadChunkEntities(ancient); err == nil {
		t.Error("an entity file below the chunk floor loaded")
	}
	if err := w.SaveChunkEntities(ancient, nil); err != nil {
		t.Fatalf("saving over the refused chunk: %v", err)
	}
	if _, _, err := w.LoadChunkEntities(ancient); err == nil {
		t.Error("saving no entities over a refused entity file erased it")
	}
}

func TestMoveChunkEntitiesKeepsPre1165EntitiesAtTheirVersion(t *testing.T) {
	t.Parallel()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	pos := ChunkPos{X: 3, Z: 4}
	root := nbt.Compound{"DataVersion": nbt.Int(levelChunkFloor), "entities": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"id": nbt.String("minecraft:cow")}}}}
	if moved, err := w.moveChunkEntities(pos, root); err != nil || !moved {
		t.Fatalf("moveChunkEntities = %v, %v", moved, err)
	}
	if _, _, err := w.LoadChunkEntities(pos); err == nil {
		t.Error("entities below the floor loaded")
	}
	rf, err := w.entityRegion(regionCoords(pos))
	if err != nil {
		t.Fatal(err)
	}
	defer w.releaseEntityRegion(rf)
	data, err := rf.peekChunk(pos)
	if err != nil || data == nil {
		t.Fatalf("stored record = %v, %v", data != nil, err)
	}
	stored, err := decodeRoot(data)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := nbt.Get[nbt.Int](stored, "DataVersion"); v != levelChunkFloor {
		t.Errorf("stored DataVersion = %d, want %d", v, levelChunkFloor)
	}
}
