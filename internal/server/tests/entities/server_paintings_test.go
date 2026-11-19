package entities

// The painting tests: the HangingEntityItem's placement on a block face, and
// HangingEntity's life on the wall — the variant create picks among the
// largest that fit, the survives that stands the painting only while solid
// wall backs every cell of it and no other painting runs across it, the
// break a hit or the wall's loss lands, the item one not taken down in
// creative lays, and the save that carries it across a restart.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// paintingWallState is the stone the tests hang their paintings on.
var paintingWallState = registry.MustState("minecraft:stone", nil)

// expectedPaintings answers the variants create should be rolling among:
// those of the placeable tag that fit the wall the fit test names, cut down
// to the largest by area. It is Painting.create's own filtering, read off
// the same registry the server builds its candidates from.
func expectedPaintings(t *testing.T, fits func(width, height int32) bool) map[int32]bool {
	t.Helper()
	variants, ok := registry.Get("minecraft:painting_variant")
	if !ok {
		t.Fatal("the registry carries no painting variants")
	}
	placeable := map[string]bool{}
	for _, tagged := range registry.Tags {
		if tagged.Registry != "minecraft:painting_variant" {
			continue
		}
		for _, tag := range tagged.Tags {
			if tag.Name != "minecraft:placeable" {
				continue
			}
			for _, name := range tag.Entries {
				placeable[name] = true
			}
		}
	}
	fitting := map[int32]bool{}
	largest := int32(0)
	for _, name := range variants.Entries {
		if !placeable[name] {
			continue
		}
		id := variants.ID(name)
		if id < 0 || int(id) >= len(registry.PaintingVariants) {
			continue
		}
		dims := registry.PaintingVariants[id]
		if !fits(dims.Width, dims.Height) {
			continue
		}
		fitting[id] = true
		if area := dims.Width * dims.Height; area > largest {
			largest = area
		}
	}
	expected := map[int32]bool{}
	for id := range fitting {
		if dims := registry.PaintingVariants[id]; dims.Width*dims.Height == largest {
			expected[id] = true
		}
	}
	if len(expected) == 0 {
		t.Fatal("no painting fits the wall the test built")
	}
	return expected
}

// expectPaintingVariant checks the variant the wire carried against the set
// create leaves, and hands back the size it was hung at.
func expectPaintingVariant(t *testing.T, variant int32, expected map[int32]bool) (width, height int32) {
	t.Helper()
	if !expected[variant] {
		t.Errorf("painting variant = %d, want one of the largest that fit", variant)
	}
	dims := registry.PaintingVariants[variant]
	return dims.Width, dims.Height
}

// TestPaintingsHangOnAWallFace places a painting against a one-block-wide
// wall and reads what the watcher is told: the spawn carrying the hanging
// cell's corner and the clicked face's direction — north, whose body yaw is
// 180 degrees — and the metadata carrying that direction again with one of
// the variants create leaves. On a wall two tall and one wide only the 1x2s
// fit, and they are also the largest that do. The place sound reaches the
// watcher at the box's centre, and the flat faces of the same wall answer
// the use with none at all.
func TestPaintingsHangOnAWallFace(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	// A block north of the wall, looking at its north face, well within
	// the placement's reach.
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()

	// The north face of the lower wall block: the painting hangs at
	// (4, 64, 4), facing north.
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	if got.Pitch != 0 {
		t.Errorf("painting pitch angle = %v, want 0", got.Pitch)
	}
	const northYaw = 180 // degrees; north's body yaw, setDirection's 2D * 90
	if yaw := int32(math.Round(float64(got.Yaw))); yaw != northYaw {
		t.Errorf("painting yaw angle = %v, want north's %d degrees", got.Yaw, northYaw)
	}
	width, height := expectPaintingVariant(t, got.Variant, expectedPaintings(t, func(w, h int32) bool {
		return w <= 1 && h <= 2
	}))
	if width != 1 || height != 2 {
		t.Fatalf("the painting on the two-tall wall was hung %dx%d, want one of the 1x2s", width, height)
	}
	// The place sound, from the middle of the hung box: north means the
	// wall face at z = 4, the sliver against it, the height over the two
	// cells at 64 and 65.
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", 4.5, 65, 4.96875)
	if srv.PaintingCount() != 1 {
		t.Errorf("the server holds %d paintings, want the one that was hung", srv.PaintingCount())
	}

	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("painting ack sequence = %d, want 1", seq)
	}
	if echoes := actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth); echoes[0] != paintingWallState || echoes[1] != 0 {
		t.Errorf("tail echoes = %v, want the stone and the air", echoes)
	}

	// The top face of the wall answers the use with nothing at all: a
	// painting has no way to hang on a ceiling's.
	actor.PlaceBlock(4, 65, 5, protocol.FaceTop)
	servertest.HoldTicks(t, srv)
	if servertest.ASpawnOfWithinTicks(watcher, srv, 60, protocol.EntityTypePainting) {
		t.Error("a painting was hung on the flat face")
	}
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("top-face ack sequence = %d, want 1", seq)
	}
	if echoes := actor.ExpectUseEchoes(4, 65, 5, protocol.FaceTop); echoes[0] != paintingWallState || echoes[1] != 0 {
		t.Errorf("top-face echoes = %v, want the stone and the air", echoes)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(actorEntityID, "Actor")
}

// TestPaintingsPickTheLargestVariantsThatFit builds a wall four wide and
// three tall over the ground: the hanging cell then admits every size up to
// the 4x4s, and create keeps only those, the largest by area. A second
// placement on the same face finds nothing that fits — every candidate runs
// across the first — and consumes the click without a painting.
func TestPaintingsPickTheLargestVariantsThatFit(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 68, 3, 6)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	// Two blocks east of the wall, within reach of its middle, looking west.
	actor.MoveToGround(6.5, 64, 5.5, true)
	actor.Settle()

	// The east face of the wall's (4, 66, 5) block: the painting hangs at
	// (5, 66, 5) facing east, four wide toward the north and four tall
	// over the block the click landed on — the wall runs five tall above
	// the ground so every height has solid wall behind it, and the
	// hanging column in front of it is all air.
	actor.PlaceBlock(4, 66, 5, protocol.FaceEast)
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceEast, 5, 66, 5)
	width, height := expectPaintingVariant(t, got.Variant, expectedPaintings(t, func(w, h int32) bool {
		return w <= 4 && h <= 4
	}))
	if width != 4 || height != 4 {
		t.Fatalf("the painting on the wide wall was hung %dx%d, want one of the 4x4s", width, height)
	}

	// The wall is covered now: nothing else fits where this face hangs,
	// and the click is answered without a painting and without the item.
	actor.PlaceBlock(4, 66, 5, protocol.FaceEast)
	servertest.HoldTicks(t, srv)
	if servertest.ASpawnOfWithinTicks(watcher, srv, 60, protocol.EntityTypePainting) {
		t.Error("a second painting was hung over the first")
	}
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("second use ack sequence = %d, want 1", seq)
	}
	if srv.PaintingCount() != 1 {
		t.Errorf("the server holds %d paintings, want only the first", srv.PaintingCount())
	}
}

// TestPaintingsUseTheItemsVariantComponent hangs the variant a preset
// painting item carries rather than create's roll: a Kebab stack's
// minecraft:painting/variant component overrides the 4x4 create would have
// picked on the wide wall.
func TestPaintingsUseTheItemsVariantComponent(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 68, 3, 6)

	kebab := servertest.PaintingVariantID(t, "minecraft:kebab")
	if dims := registry.PaintingVariants[kebab]; dims.Width != 1 || dims.Height != 1 {
		t.Fatalf("kebab is %dx%d, want the 1x1 this test relies on", dims.Width, dims.Height)
	}
	actor.SetCreativeSlot(player.HotbarSlot, servertest.PaintingStack(t, kebab))
	actor.MoveToGround(6.5, 64, 5.5, true)
	actor.Settle()

	actor.PlaceBlock(4, 66, 5, protocol.FaceEast)
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceEast, 5, 66, 5)
	if got.Variant != kebab {
		t.Errorf("the hung variant = %d, want the item's kebab (%d)", got.Variant, kebab)
	}
	if srv.PaintingCount() != 1 {
		t.Errorf("the server holds %d paintings, want the one that was hung", srv.PaintingCount())
	}
}

// TestPaintingsRefuseAVariantThatDoesNotFit hangs a preset item whose
// variant is too large for the wall: the survives check fails, so nothing is
// placed even though create would have found a smaller one that fits.
func TestPaintingsRefuseAVariantThatDoesNotFit(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	// A one-wide, two-tall wall: every placeable variant that fits is a
	// 1x2, so the 4x4 the item names has no spot.
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	skull := servertest.PaintingVariantID(t, "minecraft:burning_skull")
	if dims := registry.PaintingVariants[skull]; dims.Width != 4 || dims.Height != 4 {
		t.Fatalf("burning_skull is %dx%d, want the 4x4 this test relies on", dims.Width, dims.Height)
	}
	actor.SetCreativeSlot(player.HotbarSlot, servertest.PaintingStack(t, skull))
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()

	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	servertest.HoldTicks(t, srv)
	if servertest.ASpawnOfWithinTicks(watcher, srv, 60, protocol.EntityTypePainting) {
		t.Error("a painting was hung although the item's variant does not fit")
	}
	if n := srv.PaintingCount(); n != 0 {
		t.Errorf("the server holds %d paintings, want none", n)
	}
}

// paintingStackWithData is a painting item carrying entity_data of a
// painting that names the variant.
func paintingStackWithData(t *testing.T, variant string) protocol.ItemStack {
	t.Helper()
	return protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1,
		Raw: []protocol.RawComponent{entityDataComponent(t, protocol.EntityTypePainting,
			nbt.Compound{"variant": nbt.String(variant)})}}
}

// TestPaintingsUseTheItemsEntityDataVariant hangs the variant a painting
// item's entity_data names instead of create's roll, which would have
// picked a larger one on the wide wall.
func TestPaintingsUseTheItemsEntityDataVariant(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 68, 3, 6)

	kebab := servertest.PaintingVariantID(t, "minecraft:kebab")
	actor.SetCreativeSlot(player.HotbarSlot, paintingStackWithData(t, "minecraft:kebab"))
	actor.MoveToGround(6.5, 64, 5.5, true)
	actor.Settle()

	actor.PlaceBlock(4, 66, 5, protocol.FaceEast)
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceEast, 5, 66, 5)
	if got.Variant != kebab {
		t.Errorf("the hung variant = %d, want the entity_data's kebab (%d)", got.Variant, kebab)
	}
	if srv.PaintingCount() != 1 {
		t.Errorf("the server holds %d paintings, want the one that was hung", srv.PaintingCount())
	}
}

// TestPaintingsRefuseAnEntityDataVariantThatDoesNotFit names a 4x4 variant
// in entity_data on a 1x2 wall: nothing is hung.
func TestPaintingsRefuseAnEntityDataVariantThatDoesNotFit(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, paintingStackWithData(t, "minecraft:burning_skull"))
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()

	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	servertest.HoldTicks(t, srv)
	if servertest.ASpawnOfWithinTicks(watcher, srv, 60, protocol.EntityTypePainting) {
		t.Error("a painting was hung although the entity_data's variant does not fit")
	}
	if n := srv.PaintingCount(); n != 0 {
		t.Errorf("the server holds %d paintings, want none", n)
	}
}

// TestAPaintingFallsWithItsSupport breaks the wall a 1x2 hangs on and reads
// what follows: the painting out of the world, the break sound, and its own
// item laid out of the frame toward the room — the loss-of-support break
// drops its item whatever hand broke the wall, the laid item not any
// player's to have skipped.
func TestAPaintingFallsWithItsSupport(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	expectPaintingVariant(t, got.Variant, expectedPaintings(t, func(w, h int32) bool {
		return w <= 1 && h <= 2
	}))
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", 4.5, 65, 4.96875)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	// The upper block of the wall goes, and the painting's own support
	// with it — a creative dig takes the stone out at once.
	servertest.HoldTicks(t, srv)
	actor.BreakBlock(4, 65, 5)
	actor.Settle()
	servertest.RunTicks(t, srv, 101)
	watcher.ExpectPaintingRemoval(t, got.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.break", 4.5, 65, 4.96875)
	if srv.PaintingCount() != 0 {
		t.Errorf("the server holds %d paintings after the wall fell, want none", srv.PaintingCount())
	}

	// The item it was laid down as: the drop out of the frame, north a
	// little, at the painting's own centre height.
	dropped := watcher.AwaitSpawn(protocol.EntityTypeItem)
	meta := watcher.AwaitMetadataFor(dropped.EntityID)
	item := servertest.MetaOf(meta, protocol.MetadataIndexItemSlot)
	if item == nil || item.Typ != protocol.MetadataSlot {
		t.Fatalf("the drop's metadata carries %+v, want the item slot", meta)
	}
	if got := item.SlotVal; got.ItemID != servertest.PaintingItem(t) || got.Count != 1 {
		t.Errorf("the drop holds %+v, want the painting item", got)
	}
}

// TestABlockPlacedIntoAPaintingTakesItDown hangs one, then builds into the
// cell it hangs in. A painting's sliver reaches a sixteenth of a block into
// the hanging cell off the wall, so the arriving stone collides with it and
// the painting comes down rather than hanging inside a block.
func TestABlockPlacedIntoAPaintingTakesItDown(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", 4.5, 65, 4.96875)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	// Stone in hand, against the wall's north face again: the stone takes
	// the hanging cell, and the painting cannot survive inside it. The
	// others see the held hand lighten first.
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: registry.MustItem("minecraft:stone"), Count: 1})
	if held := servertest.EquipEntry(watcher.ExpectEquipment(actorEntityID), 0); held.ItemID != registry.MustItem("minecraft:stone") {
		t.Fatalf("the held stack after the change = %d, want stone", held.ItemID)
	}
	servertest.HoldTicks(t, srv)
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.Settle()
	servertest.RunTicks(t, srv, 101)
	watcher.AwaitBlockState(4, 64, 4, registry.BlockStates["minecraft:stone"])
	watcher.ExpectPaintingRemoval(t, got.EntityID)
	if srv.PaintingCount() != 0 {
		t.Errorf("the server holds %d paintings after a stone was built into it, want none", srv.PaintingCount())
	}
}

// TestAPaintingBreaksByAHit swings at a hung painting. Any blow lands: the
// painting comes out of every view with its break sound — and a creative
// hand lays nothing behind it.
func TestAPaintingBreaksByAHit(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", 4.5, 65, 4.96875)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	actor.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) {
		b.PutVarInt(got.EntityID)
	})
	watcher.ExpectPaintingRemoval(t, got.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.break", 4.5, 65, 4.96875)
	if srv.PaintingCount() != 0 {
		t.Errorf("the server holds %d paintings after the blow, want none", srv.PaintingCount())
	}
	// Creative takes its painting down without laying the item.
	servertest.HoldTicks(t, srv)
	if servertest.ASpawnOfWithinTicks(watcher, srv, 60, protocol.EntityTypeItem) {
		t.Error("a creative blow laid the painting's item")
	}
}

// TestAPaintingSoundDoesNotReachAcrossTheWorld hangs a painting in front of
// one player and takes it down in front of them, while a second player stands
// well past the sixteen blocks a sound reaches: the place and the break are
// heard at the wall and nowhere else.
// vanilla: Painting.playPlacementSound, Painting.dropItem
func TestAPaintingSoundDoesNotReachAcrossTheWorld(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	far := connect().Join("Faraway")
	far.MoveTo(40.5, 64, 0.5, 0, 0)
	far.Settle()
	// The far player's arrival and its body are queued on the connections
	// already in the world; drop them before the reads below.
	watcher.Settle()
	actor.Settle()

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", 4.5, 65, 4.96875)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	actor.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) {
		b.PutVarInt(got.EntityID)
	})
	watcher.ExpectPaintingRemoval(t, got.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.break", 4.5, 65, 4.96875)

	heard := far.ReadTicksSkipping(srv, 40, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		r := protocol.NewReader(data)
		sound := r.ReadVarInt()
		if sound == 0 {
			r.ReadString(32767)
			return false
		}
		name, ok := servertest.SoundEventName(sound - 1)
		return ok && (name == "minecraft:entity.painting.place" || name == "minecraft:entity.painting.break")
	})
	if heard {
		t.Error("a player well past the painting's earshot heard its sound")
	}
}

// TestASurvivalBlowLaysThePaintingItemDown is TestAPaintingBreaksByAHit in
// survival, the mode whose drops reach the ground: the painting comes down
// and the item it was lands out of the frame.
func TestASurvivalBlowLaysThePaintingItemDown(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	// The survival hand spent its painting on the wall: the slot update
	// is the cost of it. The watcher hears the equipment lighten.
	actor.ExpectBlockAck()
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	expectPaintingVariant(t, got.Variant, expectedPaintings(t, func(w, h int32) bool {
		return w <= 1 && h <= 2
	}))
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", 4.5, 65, 4.96875)
	if held := servertest.EquipEntry(watcher.ExpectEquipment(actorEntityID), 0); !held.Empty() {
		t.Errorf("the held stack after the survival placement = %+v, want it spent", held)
	}

	actor.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) {
		b.PutVarInt(got.EntityID)
	})
	watcher.ExpectPaintingRemoval(t, got.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.break", 4.5, 65, 4.96875)
	dropped := watcher.AwaitSpawn(protocol.EntityTypeItem)
	meta := watcher.AwaitMetadataFor(dropped.EntityID)
	if item := servertest.MetaOf(meta, protocol.MetadataIndexItemSlot); item == nil ||
		item.SlotVal.ItemID != servertest.PaintingItem(t) || item.SlotVal.Count != 1 {
		t.Errorf("the blow's drop holds %+v, want the painting item", item)
	}
	// The item hops out of the wall with the toss the ItemEntity
	// constructor rolls: the 0.2 upward with a random nudge sideways, not
	// laid still at the painting's centre. North lays it the 0.15 toward
	// the room, its feet at the box centre.
	// vanilla: HangingEntity.spawnAtLocation, ItemEntity.<init>
	if math.Abs(dropped.Vy-0.2) > 0.01 {
		t.Errorf("the drop's upward velocity = %v, want 0.2", dropped.Vy)
	}
	if math.Abs(dropped.Vx) > 0.11 || math.Abs(dropped.Vz) > 0.11 {
		t.Errorf("the drop's sideways velocity = (%v, %v), want each within ±0.1", dropped.Vx, dropped.Vz)
	}
	if math.Abs(dropped.X-4.5) > 1e-9 || math.Abs(dropped.Z-(4.96875-0.15)) > 1e-9 {
		t.Errorf("the drop at (%v, %v), want (4.5, %v): the centre nudged 0.15 north",
			dropped.X, dropped.Z, 4.96875-0.15)
	}
	if math.Abs(dropped.Y-65) > 1e-9 {
		t.Errorf("the drop's feet at y %v, want the painting's centre 65", dropped.Y)
	}
}

// TestAPaintingSurvivesARestart hangs one, saves the world, and stands a
// server back up on it: the entity file holds the anchor cell, the facing
// and the variant the vanilla save writes, and the painting comes back at
// the same identity, with the same direction and the same variant.
func TestAPaintingSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	expectPaintingVariant(t, hung.Variant, expectedPaintings(t, func(w, h int32) bool {
		return w <= 1 && h <= 2
	}))
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", 4.5, 65, 4.96875)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(actorEntityID, "Actor")
	srv.FlushMobs()
	srv.Shutdown()

	// On disk the compound is the vanilla one: the facing as the legacy
	// 2D data value, the anchor cell as block_pos, and the variant by
	// name — the save a vanilla server would hand back.
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:painting", func(compound nbt.Compound) {
		identity, ok := nbt.Get[nbt.IntArray](compound, "UUID")
		if !ok || len(identity) != 4 {
			t.Fatal("the saved painting has no four-word UUID")
		}
		want := servertest.UuidWords(hung.Identity)
		for i := range want {
			if identity[i] != want[i] {
				t.Errorf("saved UUID = %v, want %v (the hung painting's)", identity, want)
				break
			}
		}
		if facing, ok := nbt.Get[nbt.Byte](compound, "facing"); !ok || facing != 2 {
			t.Errorf("facing = %v, %v; want 2 (north's 2D data value)", facing, ok)
		}
		pos, ok := nbt.Get[nbt.IntArray](compound, "block_pos")
		if !ok || len(pos) != 3 || pos[0] != 4 || pos[1] != 64 || pos[2] != 4 {
			t.Errorf("block_pos = %v, %v; want the hanging cell (4, 64, 4)", pos, ok)
		}
		name, ok := nbt.Get[nbt.String](compound, "variant")
		if !ok {
			t.Fatal("the saved painting has no variant")
		}
		variants, _ := registry.Get("minecraft:painting_variant")
		if got := variants.ID(string(name)); got != hung.Variant {
			t.Errorf("saved variant = %q (%d), want the one that was hung (%d)",
				string(name), got, hung.Variant)
		}
	})

	// A restarted server hangs it back where it was. Survival's join was
	// not made for hearing a painting that arrives with a world; the
	// capture the persistence tests use is.
	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypePainting: "painting",
	})
	back := servertest.CapturedOr(t, again, shown, "painting", protocol.EntityTypePainting)
	servertest.ExpectUUID(t, back.Uuid, hung.Identity, "painting")
	expectPaintingVariant(t, hungVariant(t, back), expectedPaintings(t, func(w, h int32) bool {
		return w <= 1 && h <= 2
	}))
	if back.X != 4 || back.Y != 64 || back.Z != 4 {
		t.Errorf("the restored painting hangs at (%v, %v, %v), want the cell corner (4, 64, 4)",
			back.X, back.Y, back.Z)
	}
	if got := directionOfPaintingMetadata(t, back); got != protocol.FaceNorth {
		t.Errorf("the restored painting faces %d, want north", got)
	}
	if againSrv.PaintingCount() != 1 {
		t.Errorf("the restarted server holds %d paintings, want the one that was saved", againSrv.PaintingCount())
	}
}

// hungVariant digs the variant out of what joinCapturing read, the registry
// ID plus one the metadata field travels as.
func hungVariant(t *testing.T, e servertest.CapturedEntity) int32 {
	t.Helper()
	field := servertest.MetaOf(e.Meta, protocol.MetadataIndexPaintingVariant)
	if field == nil || field.VarintVal <= 0 {
		t.Fatal("the restored painting's metadata carries no variant")
	}
	return field.VarintVal - 1
}

// directionOfPaintingMetadata digs the hanging direction out of what
// joinCapturing read.
func directionOfPaintingMetadata(t *testing.T, e servertest.CapturedEntity) int32 {
	t.Helper()
	field := servertest.MetaOf(e.Meta, protocol.MetadataIndexHangingDirection)
	if field == nil {
		t.Fatal("the restored painting's metadata carries no direction")
	}
	return field.VarintVal
}

// TestASurvivalBlowAtAPaintingSpendsNothing swings at a hung painting in
// survival: it comes down, and none of a blow's side effects follow — no
// cooldown, no sword wear, no attack sound. vanilla's skipAttackInteraction
// returns before Player.attack runs any of them.
func TestASurvivalBlowAtAPaintingSpendsNothing(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", 4.5, 65, 4.96875)
	if held := servertest.EquipEntry(watcher.ExpectEquipment(actorEntityID), 0); !held.Empty() {
		t.Fatalf("the held stack after the survival placement = %+v, want it spent", held)
	}

	// The sword the blow would waste: the client is not told of the slot, so
	// a wear's own correction is the equipment update the swing would send.
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{
		ItemID: registry.MustItem("minecraft:diamond_sword"), Count: 1,
	})

	actor.Attack(got.EntityID)
	watcher.ExpectPaintingRemoval(t, got.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.break", 4.5, 65, 4.96875)

	watcher.ExpectNoneWithinTicks(srv, 10,
		servertest.PacketIs(protocol.IDPlaySoundEffect, protocol.IDPlayEntityEquipment))
}

// TestAStrictFillThatNeverWakesStillTakesItsPaintingDown fills a painting's
// wall away with strict, which replaces the blocks without waking the
// neighbours: the periodic survives poll still takes the painting down.
func TestAStrictFillThatNeverWakesStillTakesItsPaintingDown(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))
	actor, _, _ := servertest.PaintingWatchers(t, connect)

	// A one-tall wall, so the only variant that fits is a 1x1.
	servertest.BuildPaintingWall(srv, 4, 64, 64, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	if n := srv.PaintingCount(); n != 1 {
		t.Fatalf("the server holds %d paintings, want the hung one", n)
	}

	servertest.HoldTicks(t, srv)
	actor.ChatCommand("fill 4 64 5 4 64 5 air strict")
	actor.ExpectPlay(protocol.IDPlaySystemChat)
	if n := srv.PaintingCount(); n != 1 {
		t.Fatalf("the strict fill woke the painting early: %d up, want 1", n)
	}

	servertest.WaitForTicks(t, srv, 115, func() bool { return srv.PaintingCount() == 0 })
}

// TestAnExplosionBreaksAPainting stands a primed TNT three and a half blocks
// from a hung painting: the blast's hurt reaches it and takes it down, the
// way ServerExplosion.hurtEntities reaches every body that does not ignore
// the explosion. The wall is obsidian, so it survives the blast and the
// painting can only have gone from the hurt.
func TestAnExplosionBreaksAPainting(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, _, _ := servertest.PaintingWatchers(t, connect)
	obsidian := registry.MustState("minecraft:obsidian", nil)
	srv.SetBlock(4, 64, 5, obsidian)
	srv.SetBlock(4, 65, 5, obsidian)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	if n := srv.PaintingCount(); n != 1 {
		t.Fatalf("the server holds %d paintings, want the hung one", n)
	}

	if id := srv.SpawnTNT(4.5, 64, 1.5, 1); id == 0 {
		t.Fatal("the TNT never stood up")
	}
	servertest.WaitForTicks(t, srv, 20, func() bool { return srv.PaintingCount() == 0 })
	if srv.BlockAt(4, 64, 5) != obsidian {
		t.Fatal("the blast destroyed the wall, so the painting's fall proves nothing")
	}
}

// TestAnExplosionSparesAPaintingWhenTheTNTIsInWater holds a blast off a
// painting while the primed TNT stands in water: BlockAttachedEntity.
// ignoreExplosion spares the painting because the explosion's direct source
// is wet, not because of anything in the painting's own cell. The wall is
// obsidian, so the painting's survival is the ignore's.
func TestAnExplosionSparesAPaintingWhenTheTNTIsInWater(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, _, _ := servertest.PaintingWatchers(t, connect)
	obsidian := registry.MustState("minecraft:obsidian", nil)
	srv.SetBlock(4, 64, 5, obsidian)
	srv.SetBlock(4, 65, 5, obsidian)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	if n := srv.PaintingCount(); n != 1 {
		t.Fatalf("the server holds %d paintings, want the hung one", n)
	}
	// The water goes where the TNT's feet stand, not where the painting
	// hangs.
	srv.SetBlock(4, 64, 1, registry.MustState("minecraft:water", nil))

	if id := srv.SpawnTNT(4.5, 64, 1.5, 1); id == 0 {
		t.Fatal("the TNT never stood up")
	}
	servertest.AwaitTicks(t, srv, 20)
	if n := srv.PaintingCount(); n != 1 {
		t.Errorf("the server holds %d paintings after the blast, want the spared one", n)
	}
}

// TestWaterInThePaintingsCellDoesNotSpareIt is the other side of the same
// rule: a dry TNT with water in the painting's own cell destroys it. The
// water is not what BlockAttachedEntity.ignoreExplosion reads. The wall is
// obsidian, so the painting can only have gone from the hurt.
func TestWaterInThePaintingsCellDoesNotSpareIt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, _, _ := servertest.PaintingWatchers(t, connect)
	obsidian := registry.MustState("minecraft:obsidian", nil)
	srv.SetBlock(4, 64, 5, obsidian)
	srv.SetBlock(4, 65, 5, obsidian)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	if n := srv.PaintingCount(); n != 1 {
		t.Fatalf("the server holds %d paintings, want the hung one", n)
	}
	srv.SetBlock(4, 64, 4, registry.MustState("minecraft:water", nil))

	if id := srv.SpawnTNT(4.5, 64, 1.5, 1); id == 0 {
		t.Fatal("the TNT never stood up")
	}
	servertest.WaitForTicks(t, srv, 20, func() bool { return srv.PaintingCount() == 0 })
	if srv.BlockAt(4, 64, 5) != obsidian {
		t.Fatal("the blast destroyed the wall, so the painting's fall proves nothing")
	}
}

// TestACreativePlayersTNTBreaksAPaintingWithoutDropping primes a TNT block
// with a creative hand beside a hung painting: the blast takes the painting
// down, and Painting.dropItem lays no item because the blast's indirect
// source — the player who lit the block — is a creative one.
func TestACreativePlayersTNTBreaksAPaintingWithoutDropping(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, _, _ := servertest.PaintingWatchers(t, connect)
	obsidian := registry.MustState("minecraft:obsidian", nil)
	srv.SetBlock(4, 64, 5, obsidian)
	srv.SetBlock(4, 65, 5, obsidian)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	if n := srv.PaintingCount(); n != 1 {
		t.Fatalf("the server holds %d paintings, want the hung one", n)
	}

	// A TNT block beside the painting, struck with the creative hand's
	// flint and steel, so the blast's cause is the player.
	tnt, ok := registry.FindState("minecraft:tnt", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:tnt")
	}
	srv.SetBlock(4, 64, 1, tnt)
	flint, ok := registry.ItemID("minecraft:flint_and_steel")
	if !ok {
		t.Fatal("the item registry has no minecraft:flint_and_steel")
	}
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: flint, Count: 1})
	servertest.HoldTicks(t, srv)
	actor.PlaceBlock(4, 64, 1, protocol.FaceTop)
	actor.Settle()

	servertest.WaitForTicks(t, srv, 120, func() bool { return srv.PaintingCount() == 0 })
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:painting" {
			t.Errorf("a creative player's TNT laid the painting item at (%v, %v, %v)", it.X, it.Y, it.Z)
		}
	}
}

// TestBreakingAPaintingWithEntityDropsOffDropsNothing breaks a painting by
// taking its wall with entity_drops off: it still goes, but Painting.dropItem
// returns before both the break sound and the item.
func TestBreakingAPaintingWithEntityDropsOffDropsNothing(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	srv.SetGameRule("entity_drops", false)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", 4.5, 65, 4.96875)
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	servertest.HoldTicks(t, srv)
	actor.BreakBlock(4, 65, 5)
	actor.Settle()

	// Read the whole window rather than stopping at the removal: the break
	// sound and the item follow it, so stopping early would miss them.
	removed, sound, dropped := false, false, false
	watcher.ReadTicks(srv, 101, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlayRemoveEntities:
			removed = true
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			if id, ok := registry.SoundEventID("minecraft:entity.painting.break"); ok && r.ReadVarInt() == id+1 {
				sound = true
			}
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItem {
				dropped = true
			}
		}
		return false
	})
	if !removed {
		t.Fatal("the painting's support went without the painting coming down")
	}
	if sound {
		t.Error("breaking a painting with entity_drops off played the break sound")
	}
	if dropped {
		t.Error("breaking a painting with entity_drops off laid the item")
	}
}

// TestAPaintingWithoutBlockPosOrAKnownVariantStillLoads covers the two save
// fallbacks: an unknown variant keeps the registry's first entry, and a
// missing block_pos falls back to Pos.
func TestAPaintingWithoutBlockPosOrAKnownVariantStillLoads(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 64, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", 4.5, 64.5, 4.96875)
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(actorEntityID, "Actor")
	srv.FlushMobs()
	srv.Shutdown()

	// Strip the anchor cell and break the variant name, the way a save from a
	// server that lost the field or the variant would read.
	chunk := world.ChunkPos{X: 0, Z: 0}
	editPaintingEntity(t, cfg.LevelName, chunk, func(compound nbt.Compound) {
		delete(compound, "block_pos")
		compound["variant"] = nbt.String("minecraft:no_such_painting")
	})

	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypePainting: "painting",
	})
	back := servertest.CapturedOr(t, again, shown, "painting", protocol.EntityTypePainting)
	if back.X != 4 || back.Y != 64 || back.Z != 4 {
		t.Errorf("the restored painting hangs at (%v, %v, %v), want the cell corner (4, 64, 4)",
			back.X, back.Y, back.Z)
	}
	if got := hungVariant(t, back); got != 0 {
		t.Errorf("the restored painting's variant = %d, want the registry's first (0)", got)
	}
	if againSrv.PaintingCount() != 1 {
		t.Errorf("the restarted server holds %d paintings, want the one that was saved", againSrv.PaintingCount())
	}
}

// editPaintingEntity rewrites the painting compound of a chunk's entity file,
// the region-file edit the persistence tests use.
func editPaintingEntity(t *testing.T, level string, pos world.ChunkPos, edit func(nbt.Compound)) {
	t.Helper()
	servertest.EditChunkRecord(t, servertest.EntityRegionPath(level, pos), pos, func(record []byte) []byte {
		zr, err := zlib.NewReader(bytes.NewReader(record[5:]))
		if err != nil {
			t.Fatalf("decompress the entities: %v", err)
		}
		data, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("read the entities: %v", err)
		}
		_, tag, err := nbt.Decode(data)
		if err != nil {
			t.Fatalf("parse the entities: %v", err)
		}
		root, ok := tag.(nbt.Compound)
		if !ok {
			t.Fatalf("entities root is a %s, want a compound", tag.Type())
		}
		entities, _ := nbt.Get[nbt.List](root, "Entities")
		for i, item := range entities.Items {
			compound, ok := item.(nbt.Compound)
			if !ok {
				continue
			}
			if id, _ := nbt.Get[nbt.String](compound, "id"); string(id) != "minecraft:painting" {
				continue
			}
			edit(compound)
			entities.Items[i] = compound
		}
		encoded, err := nbt.Encode("", root)
		if err != nil {
			t.Fatalf("re-encode the entities: %v", err)
		}
		var compressed bytes.Buffer
		w := zlib.NewWriter(&compressed)
		if _, err := w.Write(encoded); err != nil {
			t.Fatalf("compress the entities: %v", err)
		}
		w.Close()
		out := append([]byte{0, 0, 0, 0, 2}, compressed.Bytes()...)
		binary.BigEndian.PutUint32(out[:4], uint32(len(out)-4))
		return out
	})
}
