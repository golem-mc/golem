package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestASetblockTagFillsAModelledEntity checks the command tag that lands on a
// container: the entry the world builds from it stands the entity up with
// what the tag carried, and the compound read back is the entity's own.
func TestASetblockTagFillsAModelledEntity(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)
	chest, ok := registry.FindState("minecraft:chest", nil)
	if !ok {
		t.Fatal("the registry has no chest")
	}
	w.SetBlock(0, GroundLevel, 0, chest)
	tag := nbt.Compound{"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"Slot": nbt.Byte(3), "id": nbt.String("minecraft:diamond"), "count": nbt.Int(2)},
	}}}
	kind, modelled := w.SetBlockEntityTag(0, GroundLevel, 0, "minecraft:chest", tag)
	if !modelled || kind != KindChest {
		t.Fatalf("SetBlockEntityTag = (%q, %v), want (%q, true)", kind, modelled, KindChest)
	}
	be := w.BlockEntity(0, GroundLevel, 0)
	if be == nil {
		t.Fatal("the chest has no block entity after its tag landed")
	}
	be.Use(func(b *BlockEntity) {
		if b.Kind != KindChest {
			t.Errorf("entity kind = %q, want %q", b.Kind, KindChest)
		}
		if got := b.Slots[3]; got.ItemID == 0 || got.Count != 2 {
			t.Errorf("slot 3 = %+v, want two diamonds", got)
		}
	})
	compound, ok := w.BlockEntityTag(0, GroundLevel, 0)
	if !ok {
		t.Fatal("BlockEntityTag found nothing where the chest stands")
	}
	items, ok := nbt.Get[nbt.List](compound, "Items")
	if !ok || len(items.Items) != 1 {
		t.Fatalf("the compound's Items = %v, want the one stack the tag carried", items)
	}
}

// TestACarriedTagSurvivesASaveRoundTrip checks the compound a command carried
// reaching the next load the way it went in, which is what keeps a tag for a
// kind this server does not model from thinning out on disk.
func TestACarriedTagSurvivesASaveRoundTrip(t *testing.T) {
	t.Parallel()
	c := flatChunk(0, 0)
	c.setCarriedBlockEntity(nbt.Compound{
		"id":               nbt.String("minecraft:trial_spawner"),
		"x":                nbt.Int(5),
		"y":                nbt.Int(GroundLevel),
		"z":                nbt.Int(9),
		"cooldown_ends_at": nbt.Long(900000000),
	})
	root, err := encodeChunkNBT(c)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("nbt.Encode: %v", err)
	}
	_, tag, err := nbt.Decode(data)
	if err != nil {
		t.Fatalf("nbt.Decode: %v", err)
	}
	loaded, err := decodeChunkNBT(tag.(nbt.Compound))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	compound, ok := loaded.carriedBlockEntityAt(5, GroundLevel, 9)
	if !ok {
		t.Fatal("the carried tag did not survive the save")
	}
	if got, _ := nbt.Get[nbt.Long](compound, "cooldown_ends_at"); got != 900000000 {
		t.Errorf("round-tripped cooldown_ends_at = %d, want 900000000", got)
	}
}

// TestARetagLoadsIntoTheEntityThatIsThere checks the tag a setblock gives a
// block that already stood as asked: it loads whole into the entity the
// position already has instead of standing a new one up, and a field the
// tag does not name resets to the fresh entity's own value.
// vanilla: CommandSetBlock, BlockEntity.loadWithComponents
func TestARetagLoadsIntoTheEntityThatIsThere(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)
	spawner, ok := registry.FindState("minecraft:spawner", nil)
	if !ok {
		t.Fatal("the registry has no spawner")
	}
	w.SetBlock(0, GroundLevel, 0, spawner)
	first := nbt.Compound{"SpawnCount": nbt.Int(5)}
	if _, ok := w.SetBlockEntityTag(0, GroundLevel, 0, "minecraft:mob_spawner", first); !ok {
		t.Fatal("the spawner's first tag did not land")
	}
	before := w.BlockEntity(0, GroundLevel, 0)
	if before == nil {
		t.Fatal("the spawner has no block entity after its first tag")
	}

	// A second tag that names another field and nothing else: the entity
	// is the one that was there, loaded whole from the new tag, and the
	// count the first tag set is back at the fresh spawner's default.
	second := nbt.Compound{"MaxNearbyEntities": nbt.Int(9)}
	kind, ok := w.SetBlockEntityTag(0, GroundLevel, 0, "minecraft:mob_spawner", second)
	if !ok || kind != KindSpawner {
		t.Fatalf("SetBlockEntityTag = (%q, %v), want (%q, true)", kind, ok, KindSpawner)
	}
	after := w.BlockEntity(0, GroundLevel, 0)
	if after != before {
		t.Error("the retag stood a new block entity up")
	}
	after.Use(func(b *BlockEntity) {
		if got := b.SpawnCount; got != 4 {
			t.Errorf("SpawnCount = %d, want 4, the fresh spawner's own", got)
		}
		if got := b.MaxNearbyEntities; got != 9 {
			t.Errorf("MaxNearbyEntities = %d, want 9, what the second tag carried", got)
		}
	})
}
