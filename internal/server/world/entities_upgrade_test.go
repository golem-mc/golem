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

// TestFollowRangeBasesAreCorrectedBelow4187 checks the exact old bases of
// villagers, bees, allays, llamas, piglin brutes and wardens become the 4187
// ones, other bases stay, and a record at 4187 is untouched.
// vanilla: EntityAttributeBaseValueFix
func TestFollowRangeBasesAreCorrectedBelow4187(t *testing.T) {
	base := func(id string, saved float64, version int32) nbt.Tag {
		e := nbt.Compound{
			"id":         nbt.String(id),
			"Attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"Name": nbt.String("minecraft:generic.follow_range"), "Base": nbt.Double(saved)}}},
		}
		upgradeEntities(nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{e}}, version)
		list, _ := e["attributes"].(nbt.List)
		a, _ := nbt.CompoundView(list.Items[0])
		if a["id"] != nbt.String("minecraft:follow_range") {
			t.Fatalf("%s attribute = %v", id, a)
		}
		return a["base"]
	}
	for _, c := range []struct {
		id       string
		from, to float64
	}{
		{"minecraft:villager", 48, 16}, {"minecraft:bee", 48, 16}, {"minecraft:allay", 48, 16},
		{"minecraft:llama", 40, 16}, {"minecraft:piglin_brute", 16, 12}, {"minecraft:warden", 16, 24},
	} {
		if got := base(c.id, c.from, 3800); got != nbt.Double(c.to) {
			t.Errorf("%s base %v = %v, want %v", c.id, c.from, got, c.to)
		}
		if got := base(c.id, 20, 3800); got != nbt.Double(20) {
			t.Errorf("%s base 20 = %v, want it kept", c.id, got)
		}
	}
	warden := nbt.Compound{
		"id":         nbt.String("minecraft:warden"),
		"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"id": nbt.String("minecraft:follow_range"), "base": nbt.Double(16)}}},
	}
	upgradeEntityShapes(warden, 4187)
	if a, _ := nbt.CompoundView(warden["attributes"].(nbt.List).Items[0]); a["base"] != nbt.Double(16) {
		t.Errorf("warden at 4187 = %v, want 16", a["base"])
	}
}
