package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestPre1165EntityFixes(t *testing.T) {
	t.Parallel()
	list := func(items ...nbt.Tag) nbt.List { return nbt.List{Elem: nbt.TypeCompound, Items: items} }
	run := func(version int32, e nbt.Compound) nbt.Compound {
		upgradeEntities(list(e), version)
		return e
	}
	most, least := nbt.Long(0x0123456789abcdef), nbt.Long(0x0fedcba987654321)
	want := nbt.IntArray{0x01234567, -0x76543211, 0x0fedcba9, -0x789abcdf}
	if got := uuidInts(uint64(most), uint64(least)); !nbt.Equal(got, want) {
		t.Fatalf("uuidInts = %v, want %v", got, want)
	}

	zombie := nbt.Compound{
		"id": nbt.String("minecraft:zombie_pigman"), "UUIDMost": most, "UUIDLeast": least,
		"HurtBy": nbt.String("01234567-89ab-cdef-0fed-cba987654321"),
		"Attributes": list(nbt.Compound{"Name": nbt.String("generic.maxHealth"), "Base": nbt.Double(40),
			"Modifiers": list(nbt.Compound{"UUIDMost": most, "UUIDLeast": least})}),
	}
	upgradePre1165Entity(zombie, 2225) // the later steps rename Attributes
	if zombie["id"] != nbt.String("minecraft:zombified_piglin") || !nbt.Equal(zombie["UUID"], want) || zombie["UUIDMost"] != nil {
		t.Errorf("piglin id or UUID: %v", zombie)
	}
	if !nbt.Equal(zombie["HurtBy"], want) {
		t.Errorf("HurtBy = %v", zombie["HurtBy"])
	}
	attr, _ := nbt.CompoundView(zombie["Attributes"].(nbt.List).Items[0])
	mod, _ := nbt.CompoundView(attr["Modifiers"].(nbt.List).Items[0])
	if attr["Name"] != nbt.String("minecraft:generic.max_health") || !nbt.Equal(mod["UUID"], want) {
		t.Errorf("attribute = %v", attr)
	}

	arrow := run(2510, nbt.Compound{"id": nbt.String("minecraft:arrow"), "OwnerUUIDMost": most, "OwnerUUIDLeast": least})
	if !nbt.Equal(arrow["Owner"], want) || arrow["OwnerUUID"] != nil {
		t.Errorf("arrow owner: %v", arrow)
	}
	wolf := run(2513, nbt.Compound{"id": nbt.String("minecraft:wolf"), "OwnerUUID": nbt.String("01234567-89ab-cdef-0fed-cba987654321")})
	if !nbt.Equal(wolf["Owner"], want) {
		t.Errorf("wolf owner: %v", wolf)
	}
	fox := run(2513, nbt.Compound{"id": nbt.String("minecraft:fox"), "TrustedUUIDs": list(nbt.Compound{"M": most, "L": least})})
	if trusted, _ := fox["Trusted"].(nbt.List); len(trusted.Items) != 1 || !nbt.Equal(trusted.Items[0], want) || fox["TrustedUUIDs"] != nil {
		t.Errorf("fox: %v", fox)
	}

	villager := nbt.Compound{
		"id":         nbt.String("minecraft:villager"),
		"Brain":      nbt.Compound{"memories": nbt.Compound{"minecraft:home": nbt.Compound{"pos": nbt.IntArray{1, 2, 3}}}},
		"Attributes": list(nbt.Compound{"Name": nbt.String("generic.follow_range"), "Base": nbt.Double(16)}),
	}
	upgradePre1165Entity(villager, 2504)
	memories, _ := nbt.CompoundView(villager["Brain"].(nbt.Compound)["memories"])
	if home, _ := nbt.CompoundView(memories["minecraft:home"]); home["value"] == nil {
		t.Errorf("memory not wrapped: %v", memories)
	}
	if a, _ := nbt.CompoundView(villager["Attributes"].(nbt.List).Items[0]); a["Base"] != nbt.Double(48) {
		t.Errorf("follow range = %v", a["Base"])
	}
	if s := run(2528, nbt.Compound{"id": nbt.String("minecraft:strider"), "NoGravity": nbt.Byte(1)}); s["NoGravity"] != nbt.Byte(0) {
		t.Errorf("strider NoGravity = %v", s["NoGravity"])
	}
	shulker := run(2534, nbt.Compound{"id": nbt.String("minecraft:shulker"), "Rotation": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(200), nbt.Float(0)}}})
	if r, _ := shulker["Rotation"].(nbt.List); r.Items[0] != nbt.Float(20) {
		t.Errorf("shulker rotation = %v", r)
	}
	if run(2586, nbt.Compound{"id": nbt.String("minecraft:shulker"), "Rotation": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(200)}}})["Rotation"].(nbt.List).Items[0] != nbt.Float(200) {
		t.Error("a 1.16.5 shulker was rotated")
	}
}

func TestMoveChunkEntitiesUpgradesA1_15Chunk(t *testing.T) {
	t.Parallel()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	pos := ChunkPos{X: 3, Z: 4}
	pig := nbt.Compound{"id": nbt.String("minecraft:pig"), "UUIDMost": nbt.Long(1), "UUIDLeast": nbt.Long(2)}
	root := nbt.Compound{"DataVersion": nbt.Int(levelChunkFloor), "entities": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{pig}}}
	if moved, err := w.moveChunkEntities(pos, root); err != nil || !moved {
		t.Fatalf("moveChunkEntities = %v, %v", moved, err)
	}
	got, _, err := w.LoadChunkEntities(pos)
	if err != nil || len(got) != 1 {
		t.Fatalf("entities = %v, %v", got, err)
	}
	if c, _ := nbt.CompoundView(got[0]); !nbt.Equal(c["UUID"], nbt.IntArray{0, 1, 0, 2}) {
		t.Errorf("entities = %v, %v; want the pig with an int array UUID", got, err)
	}
}
