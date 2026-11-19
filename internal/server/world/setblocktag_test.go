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

// TestASetblockTagIsCarriedForAnUnmodelledEntity checks the tag that lands on
// a block entity the server does not model: it is held as the compound it is,
// nothing stands in its place, and a second tag takes the first one's place.
func TestASetblockTagIsCarriedForAnUnmodelledEntity(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)
	spawner, ok := registry.FindState("minecraft:trial_spawner", nil)
	if !ok {
		t.Fatal("the registry has no trial spawner")
	}
	w.SetBlock(0, GroundLevel, 0, spawner)
	tag := nbt.Compound{"cooldown_ends_at": nbt.Long(900000000)}
	kind, modelled := w.SetBlockEntityTag(0, GroundLevel, 0, "minecraft:trial_spawner", tag)
	if modelled || kind != KindNone {
		t.Errorf("SetBlockEntityTag = (%q, %v), want unmodelled", kind, modelled)
	}
	if be := w.BlockEntity(0, GroundLevel, 0); be != nil {
		t.Errorf("the spawner grew a live entity of kind %q", be.Kind)
	}
	compound, ok := w.BlockEntityTag(0, GroundLevel, 0)
	if !ok {
		t.Fatal("the spawner's tag was not carried")
	}
	if got, _ := nbt.Get[nbt.Long](compound, "cooldown_ends_at"); got != 900000000 {
		t.Errorf("carried cooldown_ends_at = %d, want 900000000", got)
	}
	// A tag the entry built names the entity, so the carried compound reads
	// back as the spawner's own.
	if got, _ := nbt.Get[nbt.String](compound, "id"); got != "minecraft:trial_spawner" {
		t.Errorf("carried id = %q, want %q", got, "minecraft:trial_spawner")
	}
	// A second tag takes the first one's place rather than standing beside it.
	again := nbt.Compound{"cooldown_ends_at": nbt.Long(7)}
	if _, modelled := w.SetBlockEntityTag(0, GroundLevel, 0, "minecraft:trial_spawner", again); modelled {
		t.Error("the second tag was modelled")
	}
	compound, ok = w.BlockEntityTag(0, GroundLevel, 0)
	if !ok {
		t.Fatal("the second tag was not carried")
	}
	if got, _ := nbt.Get[nbt.Long](compound, "cooldown_ends_at"); got != 7 {
		t.Errorf("cooldown_ends_at = %d after the second tag, want 7", got)
	}
	// The save codec writes an empty components compound for every spawner.
	for key := range compound {
		switch key {
		case "id", "x", "y", "z", "components", "cooldown_ends_at":
		default:
			t.Errorf("the second tag left field %q behind", key)
		}
	}
}

// TestABlockChangeTakesItsCarriedEntityWithIt checks the carried compound of
// a position going when the block there does, so a saved chunk does not hold
// an entity for a block that has none.
func TestABlockChangeTakesItsCarriedEntityWithIt(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)
	spawner, ok := registry.FindState("minecraft:trial_spawner", nil)
	if !ok {
		t.Fatal("the registry has no trial spawner")
	}
	w.SetBlock(0, GroundLevel, 0, spawner)
	if _, modelled := w.SetBlockEntityTag(0, GroundLevel, 0, "minecraft:trial_spawner",
		nbt.Compound{"cooldown_ends_at": nbt.Long(900000000)}); modelled {
		t.Fatal("the spawner's tag was modelled")
	}
	w.RemoveBlockEntity(0, GroundLevel, 0)
	if _, ok := w.BlockEntityTag(0, GroundLevel, 0); ok {
		t.Error("the carried compound outlived the block it belonged to")
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

// TestARetagBlanksTheSignSidesItDoesNotName checks a setblock tag landing on
// a sign in place: a side or the wax the tag leaves out blanks, the way
// vanilla's loadAdditional reads a missing front_text, back_text and is_waxed
// over the entity that was there, rather than keeping the old text.
// vanilla: SignBlockEntity.loadAdditional
func TestARetagBlanksTheSignSidesItDoesNotName(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)
	sign, ok := registry.FindState("minecraft:oak_sign", nil)
	if !ok {
		t.Fatal("the registry has no oak sign")
	}
	w.SetBlock(0, GroundLevel, 0, sign)
	first := nbt.Compound{
		"front_text": nbt.Compound{
			"messages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
				nbt.String("a"), nbt.String(""), nbt.String(""), nbt.String(""),
			}},
			"color":            nbt.String("blue"),
			"has_glowing_text": nbt.Byte(1),
		},
		"back_text": nbt.Compound{
			"messages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
				nbt.String("b"), nbt.String(""), nbt.String(""), nbt.String(""),
			}},
		},
	}
	if _, ok := w.SetBlockEntityTag(0, GroundLevel, 0, "minecraft:sign", first); !ok {
		t.Fatal("the sign's first tag did not land")
	}
	before := w.BlockEntity(0, GroundLevel, 0)
	if before == nil {
		t.Fatal("the sign has no block entity after its first tag")
	}

	// A second tag naming only the wax: both sides are blank and black
	// again, the colour and glow included, and the wax is what it carried.
	second := nbt.Compound{"is_waxed": nbt.Byte(1)}
	kind, ok := w.SetBlockEntityTag(0, GroundLevel, 0, "minecraft:sign", second)
	if !ok || kind != KindSign {
		t.Fatalf("SetBlockEntityTag = (%q, %v), want (%q, true)", kind, ok, KindSign)
	}
	after := w.BlockEntity(0, GroundLevel, 0)
	if after != before {
		t.Error("the retag stood a new block entity up")
	}
	after.Use(func(b *BlockEntity) {
		if got := b.SignFront.Messages; got != [4]string{} {
			t.Errorf("front messages = %q, want blank", got)
		}
		if b.SignFront.Color != "black" || b.SignFront.Glowing {
			t.Errorf("front colour/glow = %q %v, want black and not glowing", b.SignFront.Color, b.SignFront.Glowing)
		}
		if got := b.SignBack.Messages; got != [4]string{} {
			t.Errorf("back messages = %q, want blank", got)
		}
		if !b.Waxed {
			t.Error("Waxed = false, want true, what the second tag carried")
		}
	})
}
