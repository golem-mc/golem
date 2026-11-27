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

// TestPre1205HorsesLlamasDonkeysAndWolvesAreUpgradedOnLoad checks a record at
// 3700 has its horse armor moved to the body slot, a llama's carpet likewise,
// a donkey's chest slots shifted down by two and a wolf's health doubled.
// vanilla: HorseBodyArmorItemFix, ChestedHorsesInventoryZeroIndexingFix, FixWolfHealth
func TestPre1205HorsesLlamasDonkeysAndWolvesAreUpgradedOnLoad(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	stack := func(id string) nbt.Compound { return nbt.Compound{"id": nbt.String(id), "Count": nbt.Byte(1)} }
	list := func(items ...nbt.Tag) nbt.List { return nbt.List{Elem: items[0].Type(), Items: items} }
	slotted := func(slot int8, id string) nbt.Tag {
		s := stack(id)
		s["Slot"] = nbt.Byte(slot)
		return s
	}
	armor := stack("minecraft:iron_horse_armor")
	horse := nbt.Compound{
		"id":               nbt.String("horse"),
		"ArmorItem":        armor,
		"ArmorItems":       list(nbt.Compound{}, nbt.Compound{}, stack("minecraft:iron_horse_armor"), nbt.Compound{}),
		"ArmorDropChances": list(nbt.Float(0.085), nbt.Float(0.085), nbt.Float(0.085), nbt.Float(0.085)),
	}
	llama := nbt.Compound{"id": nbt.String("minecraft:llama"), "DecorItem": stack("minecraft:white_carpet")}
	donkey := nbt.Compound{
		"id":    nbt.String("minecraft:donkey"),
		"Items": list(slotted(2, "minecraft:stick"), slotted(16, "minecraft:apple")),
	}
	wolf := nbt.Compound{
		"id":     nbt.String("minecraft:wolf"),
		"Health": nbt.Float(15),
		"Attributes": list(nbt.Compound{
			"Name": nbt.String("minecraft:generic.max_health"), "Base": nbt.Double(20),
		}),
	}
	plantEntityRoot(t, w, ChunkPos{}, nbt.Compound{
		"DataVersion": nbt.Int(3700),
		"Entities":    list(horse, llama, donkey, wolf),
	})
	got, _, err := w.LoadChunkEntities(ChunkPos{})
	if err != nil || len(got) != 4 {
		t.Fatalf("entities = %v, %v", got, err)
	}
	view := func(tag nbt.Tag) nbt.Compound {
		c, ok := nbt.CompoundView(tag)
		if !ok {
			t.Fatalf("%v is not a compound", tag)
		}
		return c
	}
	equipment := func(e nbt.Tag) nbt.Compound { return view(view(e)["equipment"]) }
	if eq := equipment(got[0]); eq["body"] == nil || eq["chest"] != nil {
		t.Errorf("horse equipment = %v, want body armor and no chest item", eq)
	}
	if eq := equipment(got[1]); eq["body"] == nil {
		t.Errorf("llama equipment = %v, want a body carpet", eq)
	}
	items, _ := nbt.Get[nbt.List](view(got[2]), "Items")
	for i, want := range []nbt.Byte{0, 14} {
		if s, _ := nbt.Get[nbt.Byte](view(items.Items[i]), "Slot"); s != want {
			t.Errorf("donkey item %d slot = %d, want %d", i, s, want)
		}
	}
	e := view(got[3])
	if h, _ := nbt.Get[nbt.Float](e, "Health"); h != 30 {
		t.Errorf("wolf Health = %v, want 30", h)
	}
	attributes, _ := nbt.Get[nbt.List](e, "attributes")
	if len(attributes.Items) != 1 || nbt.DoubleFieldOr(view(attributes.Items[0]), "base", 0) != 40 {
		t.Errorf("wolf attributes = %v, want max health base 40", attributes)
	}
}
