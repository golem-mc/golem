package commands

import (
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The /summon of the kinds the simulation already runs beyond the mobs: the
// falling block by the BlockState its tag named, the arrow and the two
// thrown bodies with the Motion theirs named, the painting by its facing and
// variant, and the boats the vanilla ids name — each stood up where the command
// points through the tag a saved body of the kind reads, and each seen
// arriving as the spawn packet of its kind.

// TestASummonStandsAFallingBlockUpFromItsBlockStateTag summons a falling
// block whose tag names stone and holds it up with NoGravity: the body
// arrives where the command says, drawn as the block state ID riding its
// spawn's data field, held up as the pairing's metadata says, wearing the
// UUID its tag named, and the success line names it after its block.
// vanilla: SummonCommand.createEntity, FallingBlockEntity.getTypeName
func TestASummonStandsAFallingBlockUpFromItsBlockStateTag(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}

	c.ChatCommand(`summon minecraft:falling_block 7.5 70 7.5 ` +
		`{UUID:[I;1,2,3,4],BlockState:{id:"minecraft:stone"},NoGravity:1b}`)
	spawn := c.AwaitFallingSpawn()
	if spawn.X != 7.5 || spawn.Y != 70 || spawn.Z != 7.5 {
		t.Errorf("the falling block stands at (%g, %g, %g), want (7.5, 70, 7.5)",
			spawn.X, spawn.Y, spawn.Z)
	}
	if spawn.Data != int32(stone) {
		t.Errorf("the falling block's spawn data = %d, want the stone state %d", spawn.Data, stone)
	}
	identity, _ := nbt.UUIDFromIntArray(nbt.IntArray{1, 2, 3, 4})
	if spawn.Uuid != identity {
		t.Errorf("the falling block's UUID = %s, want the tag's %s", spawn.Uuid, identity)
	}
	if field := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexNoGravity); field == nil || !field.FlagVal {
		t.Error("the falling block's metadata carried no NoGravity flag, want the one its tag named")
	}
	if got := srv.FallingCount(); got != 1 {
		t.Errorf("%d falling blocks stand, want the one", got)
	}

	// The success line names what stood up after its block, the way a fall's
	// display name is its block's.
	display := protocol.Text{
		Translate: "entity.minecraft.falling_block_type",
		With:      []protocol.Text{{Translate: "block.minecraft.stone"}},
	}
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", protocol.Text{
		Translate: display.Translate,
		With:      display.With,
		Insertion: identity.String(),
		HoverEntity: &protocol.HoverEntity{
			Name: display,
			ID:   "minecraft:falling_block",
			UUID: [4]uint32{1, 2, 3, 4},
		},
	})
}

// TestABareFallingBlockSummonLandsAsSand summons a falling block with no
// tag: the fresh fall is sand, and the body falls where it was stood and
// lands as the sand block the floor takes.
// vanilla: FallingBlockEntity's DEFAULT_BLOCK_STATE
func TestABareFallingBlockSummonLandsAsSand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}

	c.ChatCommand("summon minecraft:falling_block 9.5 70 3.5")
	spawn := c.AwaitFallingSpawn()
	if spawn.X != 9.5 || spawn.Y != 70 || spawn.Z != 3.5 {
		t.Errorf("the falling block stands at (%g, %g, %g), want (9.5, 70, 3.5)",
			spawn.X, spawn.Y, spawn.Z)
	}
	if spawn.Data != int32(sand) {
		t.Errorf("a bare falling block's spawn data = %d, want the sand state %d", spawn.Data, sand)
	}
	servertest.WaitForBlock(t, srv, 9, 64, 3, func(s uint16) bool { return s == sand })
	c.AwaitFallingRemoval(spawn.EntityID)
}

// TestASummonedArrowArrivesOnTheMotionItsTagNamed summons an arrow with the
// motion its tag named: the shaft arrives where the command says, flying that
// motion, ownerless.
func TestASummonedArrowArrivesOnTheMotionItsTagNamed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:arrow 4.5 70 4.5 {Motion:[0.5d,0.0d,0.0d]}`)
	arrow := c.AwaitSpawn(protocol.EntityTypeArrow)
	if arrow.X != 4.5 || arrow.Y != 70 || arrow.Z != 4.5 {
		t.Errorf("the arrow stands at (%g, %g, %g), want (4.5, 70, 4.5)",
			arrow.X, arrow.Y, arrow.Z)
	}
	// The spawn's packed vector carries the motion to its own quantum, so
	// the nearest fraction is near enough.
	if math.Abs(arrow.Vx-0.5) > 1e-4 || arrow.Vy != 0 || arrow.Vz != 0 {
		t.Errorf("the arrow flies (%g, %g, %g), want the motion its tag named (0.5, 0, 0)",
			arrow.Vx, arrow.Vy, arrow.Vz)
	}
	if got := srv.ArrowCount(); got != 1 {
		t.Errorf("%d arrows stand, want the one", got)
	}
}

// TestASummonedItemFrameHangsWhereItsTagSaid summons each frame type, held
// up by the Fixed flag the tag names: each arrives as its own entity type
// where the command points.
// vanilla: SummonCommand.createEntity, ItemFrame.readAdditionalSaveData
func TestASummonedItemFrameHangsWhereItsTagSaid(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	for i, tc := range []struct {
		name       string
		entityType int32
	}{
		{"item_frame", protocol.EntityTypeItemFrame},
		{"glow_item_frame", protocol.EntityTypeGlowItemFrame},
	} {
		x := 7.5 + float64(i)*4
		c.ChatCommand(fmt.Sprintf(`summon minecraft:%s %g 70 9.5 {Fixed:1b}`, tc.name, x))
		spawn := c.AwaitSpawn(tc.entityType)
		// The spawn names the frame's centre, which for the default
		// downward facing sits near the cell's ceiling; the cell is what
		// the command position floors to.
		cx, cy, cz := int32(math.Floor(spawn.X)), int32(math.Floor(spawn.Y)), int32(math.Floor(spawn.Z))
		if cx != int32(x) || cy != 70 || cz != 9 {
			t.Errorf("the %s hangs in the cell (%d, %d, %d), want (%d, 70, 9)",
				tc.name, cx, cy, cz, int32(x))
		}
	}
	if got := srv.FrameCount(); got != 2 {
		t.Errorf("%d frames hang, want every kind summoned", got)
	}
}

// TestASummonedThrownBodiesArriveAsTheirKind summons each summonable thrown
// body with the motion its tag named: each arrives as its own entity type,
// flying that motion, and drawn as the stack of its kind the item slot its
// metadata carries says.
func TestASummonedThrownBodiesArriveAsTheirKind(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	// Frozen, the bodies hang where they were summoned: flying, the first
	// would land and break before the last is summoned on a slow machine.
	srv.SetTickFrozen(true)

	for i, tc := range []struct {
		name       string
		item       int32
		entityType int32
	}{
		{"snowball", servertest.MustItem("minecraft:snowball"), protocol.EntityTypeSnowball},
		{"egg", servertest.MustItem("minecraft:egg"), protocol.EntityTypeEgg},
		{"ender_pearl", servertest.MustItem("minecraft:ender_pearl"), protocol.EntityTypeEnderPearl},
		{"splash_potion", servertest.MustItem("minecraft:splash_potion"), protocol.EntityTypeSplashPotion},
		{"lingering_potion", servertest.MustItem("minecraft:lingering_potion"), protocol.EntityTypeLingeringPotion},
		{"experience_bottle", servertest.MustItem("minecraft:experience_bottle"), protocol.EntityTypeExperienceBottle},
	} {
		x := 6.5 + float64(i)*4
		c.ChatCommand(fmt.Sprintf(`summon minecraft:%s %g 70 2.5 {Motion:[0.5d,0.0d,0.0d]}`, tc.name, x))
		spawn := c.AwaitSpawn(tc.entityType)
		if spawn.X != x || spawn.Y != 70 || spawn.Z != 2.5 {
			t.Errorf("the %s stands at (%g, %g, %g), want (%g, 70, 2.5)",
				tc.name, spawn.X, spawn.Y, spawn.Z, x)
		}
		// The spawn's packed vector carries the motion to its own quantum,
		// so the nearest fraction is near enough.
		if math.Abs(spawn.Vx-0.5) > 1e-4 || spawn.Vy != 0 || spawn.Vz != 0 {
			t.Errorf("the %s flies (%g, %g, %g), want the motion its tag named (0.5, 0, 0)",
				tc.name, spawn.Vx, spawn.Vy, spawn.Vz)
		}
		field := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexItemSlot)
		if field == nil || field.SlotVal.ItemID != tc.item || field.SlotVal.Count != 1 {
			t.Errorf("the summoned %s is drawn as %v, want one of item %d",
				tc.name, field, tc.item)
		}
	}
	if got := len(srv.ThrownInDimension(world.DimensionName)); got != 6 {
		t.Errorf("%d thrown bodies fly, want every kind summoned", got)
	}
}

// TestASummonedPaintingHangsByItsFacingAndVariant summons a kebab facing
// north before a stone wall, its tag carrying another block_pos besides: the
// painting hangs in the cell the command's position floors to — the tag's
// block_pos never moves it — and the metadata says which way it faces and
// what it is.
// vanilla: Painting.readAdditionalSaveData, BlockAttachedEntity.readAdditionalSaveData
func TestASummonedPaintingHangsByItsFacingAndVariant(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	// The wall the kebab's one cell hangs on, south of the anchor cell.
	srv.SetBlock(5, 64, 1, servertest.BlockState(t, "minecraft:stone"))

	c.ChatCommand(`summon minecraft:painting 5.5 64 0.5 ` +
		`{block_pos:[I;20,64,20],facing:2b,variant:"minecraft:kebab"}`)
	painting := c.AwaitSpawn(protocol.EntityTypePainting)
	if painting.X != 5 || painting.Y != 64 || painting.Z != 0 {
		t.Errorf("the painting hangs at (%g, %g, %g), want the cell (5, 64, 0)",
			painting.X, painting.Y, painting.Z)
	}
	entries := c.AwaitMetadataFor(painting.EntityID)
	if field := servertest.MetaOf(entries, protocol.MetadataIndexHangingDirection); field == nil || field.VarintVal != protocol.FaceNorth {
		t.Errorf("the summoned painting's direction = %v, want north", field)
	}
	if field := servertest.MetaOf(entries, protocol.MetadataIndexPaintingVariant); field == nil ||
		field.VarintVal != registry.MustID("minecraft:painting_variant", "minecraft:kebab")+1 {
		t.Errorf("the summoned painting's variant = %v, want the kebab",
			field)
	}
	// A hung painting stays put: the periodic survives poll keeps taking it
	// for the wall behind it.
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.PaintingCount() == 1 })
}

// TestASummonStandsEveryBoatKindUp summons every id of the boats family the
// item tag names bare, the chest boats among the plain hulls: each
// arrives as its own entity type, where the command says.
func TestASummonStandsEveryBoatKindUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	var kinds []string
	for _, tagged := range registry.BuiltinTags {
		if tagged.Registry != "minecraft:item" {
			continue
		}
		for _, tag := range tagged.Tags {
			if tag.Name != "minecraft:boats" {
				continue
			}
			for _, id := range tag.IDs {
				name, ok := registry.ItemName(id)
				if !ok {
					t.Fatalf("the boats tag names item %d, which the registry does not know", id)
				}
				kinds = append(kinds, name)
			}
		}
	}
	if len(kinds) == 0 {
		t.Fatal("the boats tag names no boats")
	}
	sort.Strings(kinds)

	for i, kind := range kinds {
		// Spaced to keep the last hull inside the range a body is tracked at.
		x := 2.5 + float64(i)*2.5
		c.ChatCommand(fmt.Sprintf("summon %s %.1f 64.5 2.5", kind, x))
		want, ok := registry.EntityTypeID(kind)
		if !ok {
			t.Fatalf("the boats tag names %s, which the entity registry does not know", kind)
		}
		boat := c.AwaitSpawn(want)
		if boat.X != x || boat.Y != 64.5 || boat.Z != 2.5 {
			t.Errorf("the %s stands at (%g, %g, %g), want (%g, 64.5, 2.5)",
				kind, boat.X, boat.Y, boat.Z, x)
		}
	}
	if got := srv.BoatCount(); got != len(kinds) {
		t.Errorf("%d boats stand, want one of each of the %d kinds", got, len(kinds))
	}
}

// TestASummonedItemKeepsItsAgeOwnerAndHealth summons an item whose compound
// names the age, the health and the owner a saved item reads: the item stands
// with all three, and its age is the unlimited-lifetime marker, so it is
// still there long past the despawn age.
// vanilla: ItemEntity.readAdditionalSaveData, ItemEntity.tick
func TestASummonedItemKeepsItsAgeOwnerAndHealth(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))

	owner := protocol.OfflineUUID("Keeper")
	words := servertest.UuidWords(owner)
	c.ChatCommand(fmt.Sprintf(
		`summon minecraft:item 6.5 64 0.5 `+
			`{Item:{id:"minecraft:stone",count:1},Age:-32768s,Health:20s,Owner:[I;%d,%d,%d,%d],PickupDelay:32767s}`,
		words[0], words[1], words[2], words[3]))
	c.AwaitSpawn(protocol.EntityTypeItem)

	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("%d items stand, want the one summoned", len(items))
	}
	it := items[0]
	if it.Age != math.MinInt16 {
		t.Errorf("the summoned item's age = %d, want the marker %d", it.Age, math.MinInt16)
	}
	if it.Health != 20 {
		t.Errorf("the summoned item's health = %d, want the 20 its tag named", it.Health)
	}
	if it.Owner != owner.String() {
		t.Errorf("the summoned item's owner = %q, want %q", it.Owner, owner.String())
	}

	// The marker holds the age still, so the item outlives the despawn age
	// many times over.
	srv.StopTick()
	srv.TickEntities(6001)
	items = srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("%d items stand after 6001 ticks, want the unlimited-lifetime one still there", len(items))
	}
	if items[0].Age != math.MinInt16 {
		t.Errorf("the item's age after 6001 ticks = %d, want it held at %d", items[0].Age, math.MinInt16)
	}
}

// TestASummonedTNTAndOrbFlyTheMotionTheirTagNamed summons a primed TNT and an
// experience orb tossed straight up: each climbs on its first ticks.
// vanilla: SummonCommand.createEntity
func TestASummonedTNTAndOrbFlyTheMotionTheirTagNamed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	// Frozen, the two hold their launch until the ticks below: the climb
	// is spent in about twenty ticks, which the waits for the spawns could
	// otherwise run.
	srv.SetTickFrozen(true)

	c.ChatCommand(`summon minecraft:tnt 5.5 70 5.5 {fuse:200s,Motion:[0.0d,1.0d,0.0d]}`)
	if got := c.AwaitSpawn(protocol.EntityTypeTnt); math.Abs(got.Vy-1) > 1e-3 {
		t.Errorf("the TNT spawned flying %g up, want the 1 its tag named", got.Vy)
	}
	c.ChatCommand(`summon minecraft:experience_orb 8.5 70 8.5 {Value:5s,Motion:[0.0d,1.0d,0.0d]}`)
	orb := c.AwaitSpawn(protocol.EntityTypeExperienceOrb)

	srv.StopTick()
	tntY := servertest.SoleTNTY(t, srv)
	_, orbY, _, ok := srv.OrbPos(orb.EntityID)
	if !ok {
		t.Fatal("the summoned orb is not standing")
	}
	for tick := 1; tick <= 3; tick++ {
		srv.TickEntities(1)
		nextTNT := servertest.SoleTNTY(t, srv)
		_, nextOrb, _, _ := srv.OrbPos(orb.EntityID)
		if nextTNT <= tntY || nextOrb <= orbY {
			t.Fatalf("tick %d moved the TNT from y=%v to %v and the orb from y=%v to %v, want both climbing", tick, tntY, nextTNT, orbY, nextOrb)
		}
		tntY, orbY = nextTNT, nextOrb
	}
}
