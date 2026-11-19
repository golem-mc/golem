package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestAVillagerKeepsItsDataAndOffersAcrossASave: VillagerData, the finalized
// flag and the Offers compound are written and read back, and the Xp a
// vanilla file keeps survives the save.
func TestAVillagerKeepsItsDataAndOffersAcrossASave(t *testing.T) {
	t.Parallel()
	offers := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"uses": nbt.Int(2), "maxUses": nbt.Int(12)},
	}}}
	file := nbt.Compound{
		"id":                    nbt.String("minecraft:villager"),
		"VillagerData":          nbt.Compound{"type": nbt.String("minecraft:snow"), "profession": nbt.String("minecraft:cleric"), "level": nbt.Int(3)},
		"VillagerDataFinalized": nbt.Byte(1),
		"Offers":                offers,
		"Xp":                    nbt.Int(40),
	}
	m := entity.NewMob(entity.TypeVillager, 1, 0.5, 64, 0.5, 1)
	ent := &mobEntity{mob: m}
	applyMobTag(m, ent, file)
	if m.Villager != (entity.VillagerData{Type: "minecraft:snow", Profession: "minecraft:cleric", Level: 3, Finalized: true}) {
		t.Fatalf("loaded villager data %+v", m.Villager)
	}
	tag := mobTag(ent)
	data, _ := nbt.Get[nbt.Compound](tag, "VillagerData")
	if p, _ := nbt.Get[nbt.String](data, "profession"); p != "minecraft:cleric" {
		t.Errorf("saved profession %q", p)
	}
	saved, ok := nbt.Get[nbt.Compound](tag, "Offers")
	if !ok {
		t.Fatal("Offers not written back")
	}
	list, _ := nbt.Get[nbt.List](saved, "Recipes")
	if len(list.Items) != 1 {
		t.Errorf("saved %d offers, want 1", len(list.Items))
	}
	if xp, _ := nbt.Get[nbt.Int](tag, "Xp"); xp != 40 {
		t.Errorf("saved Xp %d, want 40", xp)
	}
}

// TestAVillagerTellsItsDataInItsMetadata: the Villager Data sits at index 19,
// after the unhappy counter, not the zombie villager's 20.
func TestAVillagerTellsItsDataInItsMetadata(t *testing.T) {
	t.Parallel()
	m := entity.NewMob(entity.TypeVillager, 1, 0.5, 64, 0.5, 1)
	ent := &mobEntity{mob: m}
	was := mobMetadataStateOf(m)
	m.Villager = entity.VillagerData{Type: "minecraft:desert", Profession: "minecraft:farmer", Level: 2, Finalized: true}
	delta := mobMetadataDeltaPayload(ent, was)
	var want protocol.Buffer
	want.PutMetadata(19, protocol.MetadataVillagerData)
	want.PutVarInt(0) // desert
	want.PutVarInt(5) // farmer
	want.PutVarInt(2)
	want.PutMetadata(20, protocol.MetadataBoolean)
	want.PutBool(true)
	if !bytes.Contains(delta, want.Bytes()) {
		t.Errorf("metadata delta % x lacks % x", delta, want.Bytes())
	}
	if whole := mobMetadataPayload(ent); !bytes.Contains(whole, want.Bytes()[:2]) {
		t.Errorf("full metadata % x lacks the villager data", whole)
	}
}

// TestAZombieVillagerKeepsItsOffersAcrossASave: the Offers compound is read
// and written for the zombie villager as for the villager.
func TestAZombieVillagerKeepsItsOffersAcrossASave(t *testing.T) {
	t.Parallel()
	offers := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"uses": nbt.Int(2)},
	}}}
	m := entity.NewMob(entity.TypeZombieVillager, 1, 0.5, 64, 0.5, 1)
	ent := &mobEntity{mob: m}
	applyMobTag(m, ent, nbt.Compound{"id": nbt.String("minecraft:zombie_villager"), "Offers": offers})
	saved, ok := nbt.Get[nbt.Compound](mobTag(ent), "Offers")
	if !ok {
		t.Fatal("zombie villager lost its Offers")
	}
	if list, _ := nbt.Get[nbt.List](saved, "Recipes"); len(list.Items) != 1 {
		t.Errorf("saved %d offers, want 1", len(list.Items))
	}
}

func TestStructureVillagerFinalizesOnlyMissingTypeOnLoad(t *testing.T) {
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := world.ChunkPos{}
	w.Acquire(pos)
	defer w.Release(pos)
	for _, supplied := range []bool{false, true} {
		tag := nbt.Compound{"id": nbt.String("minecraft:villager"), "Age": nbt.Int(-24000), "AssignProfessionWhenSpawned": nbt.Byte(1), world.StructureFinalizeKey: nbt.Byte(1)}
		want := "minecraft:plains"
		if supplied {
			want = "minecraft:snow"
			tag["VillagerData"] = nbt.Compound{"type": nbt.String(want), "profession": nbt.String("minecraft:nitwit"), "level": nbt.Int(1)}
		}
		ent := mobOfTag(&Server{world: w}, w, tag)
		if ent == nil || ent.mob.Villager.Type != want || !ent.mob.Villager.Finalized || ent.mob.BabyAge != -24000 {
			t.Fatalf("structure load did not finalize type while preserving age: %v", ent)
		}
		saved := mobTag(ent)
		if !nbt.BoolField(saved, "AssignProfessionWhenSpawned") {
			t.Fatal("loading lost pending profession assignment")
		}
		if supplied && ent.mob.Villager.Profession != "minecraft:nitwit" {
			t.Fatal("finalization changed template profession")
		}
	}
}
