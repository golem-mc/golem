package commands

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
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

// vanilla: FallingBlockEntity.readAdditionalSaveData, causeFallDamage
func TestASummonedAnvilWithoutFallHurtAmountHurtsNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		amount string
		hurts  bool
	}{
		{name: "default"},
		{name: "explicit", amount: ",FallHurtAmount:2f", hurts: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
			servertest.HoldTicks(t, srv)

			c.ChatCommand(`summon minecraft:cow 7.5 64 7.5 {NoAI:1b}`)
			cow := c.ExpectMobSpawn(protocol.EntityTypeCow).EntityID
			health, ok := srv.MobHealth(cow)
			if !ok {
				t.Fatal("summoned cow is missing")
			}
			c.ChatCommand(`summon minecraft:falling_block 7.5 70 7.5 {BlockState:{id:"minecraft:anvil"}` + tc.amount + `}`)
			spawn := c.AwaitFallingSpawn()
			servertest.UntilTicks(t, srv, 40, "the anvil to land", func() bool { return srv.FallingCount() == 0 })
			c.AwaitFallingRemoval(spawn.EntityID)

			got, alive := srv.MobHealth(cow)
			if tc.hurts {
				if alive && got >= health {
					t.Errorf("explicit FallHurtAmount left cow health at %g, want less than %g", got, health)
				}
			} else {
				if !alive || got != health {
					t.Errorf("default FallHurtAmount left cow health at %g (present %t), want %g", got, alive, health)
				}
				if block := registry.BlockOf(srv.BlockAt(7, 64, 7)); block != registry.BlockAnvil {
					t.Errorf("landed block = %s, want an unchipped anvil", block.Name())
				}
			}
		})
	}
}

// TestASummonedFallingBlockOfAnUnknownBlockLandsAsSand names a block no
// state answers to in its tag: the fall still stands up, read as the sand
// vanilla's reader defaults an unreadable state to, and the success line
// names it after that sand.
// vanilla: FallingBlockEntity.readAdditionalSaveData, DEFAULT_BLOCK_STATE
func TestASummonedFallingBlockOfAnUnknownBlockLandsAsSand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}

	c.ChatCommand(`summon minecraft:falling_block 7.5 70 7.5 ` +
		`{UUID:[I;4,3,2,1],BlockState:{id:"minecraft:no_such_block"}}`)
	spawn := c.AwaitFallingSpawn()
	if spawn.X != 7.5 || spawn.Y != 70 || spawn.Z != 7.5 {
		t.Errorf("the falling block stands at (%g, %g, %g), want (7.5, 70, 7.5)",
			spawn.X, spawn.Y, spawn.Z)
	}
	if spawn.Data != int32(sand) {
		t.Errorf("a fall of an unknown block's spawn data = %d, want the sand state %d",
			spawn.Data, sand)
	}
	if got := srv.FallingCount(); got != 1 {
		t.Errorf("%d falling blocks stand, want the one", got)
	}

	// The success line names the fall after the sand it was read as.
	identity, _ := nbt.UUIDFromIntArray(nbt.IntArray{4, 3, 2, 1})
	display := protocol.Text{
		Translate: "entity.minecraft.falling_block_type",
		With:      []protocol.Text{{Translate: "block.minecraft.sand"}},
	}
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", protocol.Text{
		Translate: display.Translate,
		With:      display.With,
		Insertion: identity.String(),
		HoverEntity: &protocol.HoverEntity{
			Name: display,
			ID:   "minecraft:falling_block",
			UUID: [4]uint32{4, 3, 2, 1},
		},
	})
}

// TestASummonedFallingBlockIgnoresTheLegacyNameForm summons a falling block
// whose BlockState uses the pre-26.3 Name key: the codec reads only id, so
// the fall is sand, while the same block under id stays an anvil.
// vanilla: BlockState.CODEC, FallingBlockEntity.readAdditionalSaveData
func TestASummonedFallingBlockIgnoresTheLegacyNameForm(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	sand, _ := registry.FindState("minecraft:sand", nil)
	anvil, _ := registry.FindState("minecraft:anvil", map[string]string{"facing": "north"})

	c.ChatCommand(`summon minecraft:falling_block 7.5 70 7.5 {BlockState:{Name:"minecraft:anvil"},NoGravity:1b}`)
	if spawn := c.AwaitFallingSpawn(); spawn.Data != int32(sand) {
		t.Errorf("legacy Name spawn data = %d, want the sand state %d", spawn.Data, sand)
	}
	c.ChatCommand(`summon minecraft:falling_block 9.5 70 9.5 {BlockState:{id:"minecraft:anvil"},NoGravity:1b}`)
	if spawn := c.AwaitFallingSpawn(); spawn.Data != int32(anvil) {
		t.Errorf("id spawn data = %d, want the anvil state %d", spawn.Data, anvil)
	}
}

// TestASummonedFallingBlockReadsAnUnqualifiedBlockName names a block without
// a namespace in its tag: the fall is the block vanilla's resource location
// parse reads, not the sand an unreadable state falls back to.
// vanilla: Identifier.parse, FallingBlockEntity.readAdditionalSaveData
func TestASummonedFallingBlockReadsAnUnqualifiedBlockName(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}

	c.ChatCommand(`summon minecraft:falling_block 7.5 70 7.5 ` +
		`{UUID:[I;5,6,7,8],BlockState:{id:"stone"},NoGravity:1b}`)
	spawn := c.AwaitFallingSpawn()
	if spawn.Data != int32(stone) {
		t.Errorf("a fall named without a namespace is drawn as %d, want the stone state %d",
			spawn.Data, stone)
	}
	if got := srv.FallingCount(); got != 1 {
		t.Errorf("%d falling blocks stand, want the one", got)
	}

	identity, _ := nbt.UUIDFromIntArray(nbt.IntArray{5, 6, 7, 8})
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
			UUID: [4]uint32{5, 6, 7, 8},
		},
	})
}

// TestASummonedFallingBlockOfAirIsDiscardedAfterATick names air in its tag:
// the summon succeeds and stands the air fall up, drawn as the air state, and
// the body discards itself on its first tick, leaving nothing behind.
// vanilla: FallingBlockEntity.tick
func TestASummonedFallingBlockOfAirIsDiscardedAfterATick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	air, ok := registry.FindState("minecraft:air", nil)
	if !ok {
		t.Fatal("the registry has no air")
	}

	c.ChatCommand(`summon minecraft:falling_block 7.5 70 7.5 {UUID:[I;9,10,11,12],BlockState:{id:"air"}}`)
	spawn := c.AwaitFallingSpawn()
	if spawn.Data != int32(air) {
		t.Errorf("the air fall's spawn data = %d, want the air state %d", spawn.Data, air)
	}

	// The success line names the fall after its air block, the same way it
	// names any other block.
	identity, _ := nbt.UUIDFromIntArray(nbt.IntArray{9, 10, 11, 12})
	display := protocol.Text{
		Translate: "entity.minecraft.falling_block_type",
		With:      []protocol.Text{{Translate: "block.minecraft.air"}},
	}
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", protocol.Text{
		Translate: display.Translate,
		With:      display.With,
		Insertion: identity.String(),
		HoverEntity: &protocol.HoverEntity{
			Name: display,
			ID:   "minecraft:falling_block",
			UUID: [4]uint32{9, 10, 11, 12},
		},
	})

	c.AwaitFallingRemoval(spawn.EntityID)
	if got := srv.FallingCount(); got != 0 {
		t.Errorf("%d falling blocks stand, want the air fall discarded on its first tick", got)
	}
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

// TestASummonedArrowFamilyArrivesAsItsType summons each id of the arrow
// family — the plain shaft, the spectral shaft, the trident — each arriving
// as its own entity type, flying the motion its tag named.
func TestASummonedArrowFamilyArrivesAsItsType(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	for i, tc := range []struct {
		name       string
		entityType int32
	}{
		{"arrow", protocol.EntityTypeArrow},
		{"spectral_arrow", protocol.EntityTypeSpectralArrow},
		{"trident", protocol.EntityTypeTrident},
	} {
		x := 6.5 + float64(i)*4
		c.ChatCommand(fmt.Sprintf(`summon minecraft:%s %g 70 2.5 {Motion:[0.5d,0.0d,0.0d]}`, tc.name, x))
		arrow := c.AwaitSpawn(tc.entityType)
		if arrow.X != x || arrow.Y != 70 || arrow.Z != 2.5 {
			t.Errorf("the %s stands at (%g, %g, %g), want (%g, 70, 2.5)",
				tc.name, arrow.X, arrow.Y, arrow.Z, x)
		}
		if math.Abs(arrow.Vx-0.5) > 1e-4 || arrow.Vy != 0 || arrow.Vz != 0 {
			t.Errorf("the %s flies (%g, %g, %g), want the motion its tag named",
				tc.name, arrow.Vx, arrow.Vy, arrow.Vz)
		}
	}
	if got := srv.ArrowCount(); got != 3 {
		t.Errorf("%d arrows stand, want every kind summoned", got)
	}
}

// TestASummonedSpectralArrowsDurationSetsTheGlow checks the duration a
// spectral shaft saves and reads: a /summon's 600 ticks give the body it
// lands in thirty seconds of glow, and one with no Duration field reports
// the constructor's 200; removing the field, or replacing it with a nonnumber,
// reads that default again.
// vanilla: SpectralArrow.readAdditionalSaveData, doPostHurtEffects
func TestASummonedSpectralArrowsDurationSetsTheGlow(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))
	servertest.HoldTicks(t, srv)

	pig := srv.SpawnMob("minecraft:pig", 5.5, 64, 5.5)
	if !srv.SetMobNoAI(pig) {
		t.Fatal("the pig has no AI to freeze")
	}
	c.ChatCommand(`summon minecraft:spectral_arrow 5.5 67 5.5 {Duration:600,Motion:[0.0,-1.0,0.0]}`)
	c.AwaitSpawn(protocol.EntityTypeSpectralArrow)
	c.Drain()
	servertest.UntilTicks(t, srv, 100, "the spectral arrow's glowing", func() bool {
		_, _, ok := srv.MobEffectOf(pig, "minecraft:glowing")
		return ok
	})
	duration, amplifier, ok := srv.MobEffectOf(pig, "minecraft:glowing")
	if !ok || amplifier != 0 || duration < 590 || duration > 600 {
		t.Errorf("the spectral arrow left glowing for %d ticks at amplifier %d (present %v), want 590..600 ticks at 0",
			duration, amplifier, ok)
	}

	c.Drain()
	c.ChatCommand("summon minecraft:spectral_arrow 6.5 70 6.5")
	plain := c.AwaitSpawn(protocol.EntityTypeSpectralArrow)
	target := plain.Uuid.String()
	c.Drain()
	c.ChatCommand("data merge entity " + target + " {Duration:600}")
	c.ExpectFeedbackLine("commands.data.entity.modified")
	c.ChatCommand("data remove entity " + target + " Duration")
	c.ExpectFeedbackLine("commands.data.entity.modified")
	c.ChatCommand("data get entity " + target + " Duration")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:spectral_arrow", "entity.minecraft.spectral_arrow", plain.Uuid),
		servertest.NBTArg("200"))
	c.ChatCommand(`data merge entity ` + target + ` {Duration:"not a number"}`)
	c.ExpectFeedbackLine("commands.data.entity.modified")
	c.ChatCommand("data get entity " + target + " Duration")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:spectral_arrow", "entity.minecraft.spectral_arrow", plain.Uuid),
		servertest.NBTArg("200"))
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

// TestASummonedThrownBodyKeepsItsNoGravityFlag summons a snowball whose tag
// holds it up: the metadata the client reads carries the no-gravity flag
// beside the item slot, as the other kinds' payloads write it.
func TestASummonedThrownBodyKeepsItsNoGravityFlag(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:snowball 4.5 70 4.5 {NoGravity:1b}`)
	spawn := c.AwaitSpawn(protocol.EntityTypeSnowball)
	field := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexNoGravity)
	if field == nil || !field.FlagVal {
		t.Errorf("the summoned snowball's no-gravity flag = %v, want the one its tag named", field)
	}
}

// TestASummonedThrownBodyFallsBackToItsOwnItem summons a snowball whose tag
// carries an Item no codec reads: the body still stands up, drawn as the
// snowball vanilla's getDefaultItem falls back to.
// vanilla: ThrowableItemProjectile.readAdditionalSaveData, getDefaultItem
func TestASummonedThrownBodyFallsBackToItsOwnItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:snowball 4.5 70 4.5 {Item:{id:"minecraft:nope"}}`)
	spawn := c.AwaitSpawn(protocol.EntityTypeSnowball)
	field := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexItemSlot)
	if field == nil || field.SlotVal.ItemID != servertest.MustItem("minecraft:snowball") {
		t.Errorf("the summoned snowball is drawn as %v, want its own item", field)
	}
	if got := len(srv.ThrownInDimension(world.DimensionName)); got != 1 {
		t.Errorf("%d thrown bodies fly, want the one", got)
	}
}

// TestASummonedThrownBodyReadsAnUnqualifiedItemName carries an Item whose id
// is written without a namespace: the body is drawn as the item vanilla's
// resource location parse reads, not its kind's own default.
// vanilla: Identifier.parse, ThrowableItemProjectile.readAdditionalSaveData
func TestASummonedThrownBodyReadsAnUnqualifiedItemName(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:snowball 4.5 70 4.5 {Item:{id:"diamond",count:1}}`)
	spawn := c.AwaitSpawn(protocol.EntityTypeSnowball)
	field := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexItemSlot)
	if field == nil || field.SlotVal.ItemID != servertest.MustItem("minecraft:diamond") {
		t.Errorf("the summoned snowball is drawn as %v, want the diamond its tag named", field)
	}
	if got := len(srv.ThrownInDimension(world.DimensionName)); got != 1 {
		t.Errorf("%d thrown bodies fly, want the one", got)
	}
}

// TestASummonedThrownBodyNamedByItsTagShowsThatName summons a snowball whose
// tag names it: the success line shows the name, as a named mob's does.
// vanilla: Projectile.getDisplayName
func TestASummonedThrownBodyNamedByItsTagShowsThatName(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:snowball 4.5 70 4.5 {UUID:[I;1,2,3,4],CustomName:"Bob"}`)
	c.AwaitSpawn(protocol.EntityTypeSnowball)
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", namedDisplay("minecraft:snowball", "Bob", 4))
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

// TestASummonedItemKeepsANonPositiveHealth summons items whose tags name a
// health of zero and of below zero: each keeps the value it named rather
// than taking the default an absent key gets, so the item dies on the first
// hurt it takes.
// vanilla: ItemEntity.readAdditionalSaveData
func TestASummonedItemKeepsANonPositiveHealth(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))

	// The infinite pickup delay keeps the player's own body from taking the
	// pairs up before the world is read.
	c.ChatCommand(`summon minecraft:item 6.5 64 0.5 {Item:{id:"minecraft:stone",count:1},Health:0s,PickupDelay:32767s}`)
	c.ChatCommand(`summon minecraft:item 8.5 64 0.5 {Item:{id:"minecraft:stone",count:1},Health:-1s,PickupDelay:32767s}`)
	servertest.PollUntil(t, 2*time.Second, func() bool {
		return len(srv.ItemsInDimension(world.DimensionName)) == 2
	})

	healths := map[int]bool{}
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		healths[it.Health] = true
	}
	if !healths[0] || !healths[-1] {
		t.Errorf("the summoned items' healths = %v, want an explicit 0 and -1 kept", healths)
	}
}

// TestASummonedFallingBlockOfCaveOrVoidAirIsDiscarded summons falls whose
// block is cave_air and void_air: each is gone within its first ticks and
// places nothing, where the landing would have set that air in the cell.
// vanilla: FallingBlockEntity.tick
func TestASummonedFallingBlockOfCaveOrVoidAirIsDiscarded(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	air, ok := registry.FindState("minecraft:air", nil)
	if !ok {
		t.Fatal("the registry has no air")
	}
	for i, name := range []string{"cave_air", "void_air"} {
		x := int32(9 + 2*i)
		c.ChatCommand(fmt.Sprintf(`summon minecraft:falling_block %d.5 70 3.5 {BlockState:{id:"minecraft:%s"}}`, x, name))
		spawn := c.AwaitFallingSpawn()
		c.AwaitFallingRemoval(spawn.EntityID)
		if got := srv.FallingCount(); got != 0 {
			t.Errorf("%s: %d falling blocks stand, want none", name, got)
		}
		if got := srv.BlockAt(x, 64, 3); got != air {
			t.Errorf("%s: the cell below holds state %d, want the untouched air %d", name, got, air)
		}
	}
}

// TestASummonedItemFliesTheMotionItsTagNamed summons a stone tossed straight
// up: the spawn carries the tag's Motion to watchers, and the stone climbs on
// each of its first hand-driven ticks.
// vanilla: SummonCommand.createEntity
func TestASummonedItemFliesTheMotionItsTagNamed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:item 5.5 70 5.5 {Item:{id:"minecraft:stone"},Motion:[0.0d,1.0d,0.0d],PickupDelay:32767s}`)
	spawned := c.AwaitSpawn(protocol.EntityTypeItem)
	// The spawn's packed vector carries the motion to its own quantum.
	if spawned.Vx != 0 || math.Abs(spawned.Vy-1) > 1e-3 || spawned.Vz != 0 {
		t.Errorf("the item spawned flying (%g, %g, %g), want the (0, 1, 0) its tag named",
			spawned.Vx, spawned.Vy, spawned.Vz)
	}

	srv.StopTick()
	y := servertest.SoleItemY(t, srv)
	for tick := 1; tick <= 3; tick++ {
		srv.TickEntities(1)
		next := servertest.SoleItemY(t, srv)
		if next <= y {
			t.Fatalf("tick %d moved the item from y=%v to y=%v, want it climbing on the tossed motion", tick, y, next)
		}
		y = next
	}
}

// TestASummonedBodyZeroesAMotionAxisOverTen summons items with a Motion axis of
// 11 and of 10: the first spawns still on that axis, the second keeps it.
// vanilla: Entity.load
func TestASummonedBodyZeroesAMotionAxisOverTen(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:item 5.5 70 5.5 {Item:{id:"minecraft:stone"},Motion:[0.0d,11.0d,0.0d],PickupDelay:32767s}`)
	if got := c.AwaitSpawn(protocol.EntityTypeItem); got.Vy != 0 {
		t.Errorf("Motion y of 11 spawned an item flying at %g, want it zeroed", got.Vy)
	}
	c.ChatCommand(`summon minecraft:item 5.5 70 5.5 {Item:{id:"minecraft:stone"},Motion:[0.0d,10.0d,0.0d],PickupDelay:32767s}`)
	if got := c.AwaitSpawn(protocol.EntityTypeItem); math.Abs(got.Vy-10) > 1e-2 {
		t.Errorf("Motion y of 10 spawned an item flying at %g, want it kept", got.Vy)
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

// TestASummonedItemFrameReadsItsItemLikeTheStackCodec summons frames whose
// Item names the id without a namespace, omits the count, or writes a count
// outside 1..99; each holds one item.
// vanilla: ItemStack.MAP_CODEC
func TestASummonedItemFrameReadsItsItemLikeTheStackCodec(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	stone := servertest.MustItem("minecraft:stone")
	for i, tc := range []struct {
		item  string
		count int32 // 0 means no stack is held
	}{
		{`{id:"stone",count:1}`, 1},
		{`{id:"minecraft:stone"}`, 1},
		{`{id:"minecraft:stone",count:0}`, 1},
		{`{id:"minecraft:stone",count:100}`, 1},
	} {
		c.ChatCommand(fmt.Sprintf(`summon minecraft:item_frame %g 70 9.5 {Fixed:1b,Item:%s}`, 7.5+float64(i)*4, tc.item))
		spawn := c.AwaitSpawn(protocol.EntityTypeItemFrame)
		field := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexFrameItem)
		held := field != nil && field.SlotVal.ItemID == stone && field.SlotVal.Count > 0
		if tc.count == 0 && held || tc.count > 0 && (!held || field.SlotVal.Count != tc.count) {
			t.Errorf("Item %s holds %v, want count %d of stone (0 = empty)", tc.item, field, tc.count)
		}
	}
}

// TestASummonedArrowTakesATippedArrowItem summons an arrow whose item is a
// tipped arrow: it stands, wearing the potion's colour.
// vanilla: AbstractArrow.readAdditionalSaveData
func TestASummonedArrowTakesATippedArrowItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:arrow 4.5 70 4.5 {item:{id:"minecraft:tipped_arrow",count:1,components:{"minecraft:potion_contents":{potion:"minecraft:poison"}}}}`)
	c.AwaitSpawn(protocol.EntityTypeArrow)
	if got := srv.ArrowCount(); got != 1 {
		t.Errorf("%d arrows stand, want the tipped one", got)
	}
}

// TestASummonedFireballKeepsItsHurtingFields summons the three hurting
// projectiles with their saved fields and reads them back from the body.
// vanilla: AbstractHurtingProjectile.readAdditionalSaveData, LargeFireball.readAdditionalSaveData, WitherSkull.readAdditionalSaveData
func TestASummonedFireballKeepsItsHurtingFields(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	for i, tc := range []struct {
		command    string
		entityType int32
		key        string
		want       any
	}{
		{`small_fireball {acceleration_power:0.25d}`, protocol.EntityTypeSmallFireball, "acceleration_power", 0.25},
		{`fireball {ExplosionPower:3b}`, protocol.EntityTypeFireball, "ExplosionPower", uint8(3)},
		{`wither_skull {dangerous:1b}`, protocol.EntityTypeWitherSkull, "dangerous", uint8(1)},
	} {
		c.ChatCommand(fmt.Sprintf(`summon minecraft:%s`, strings.Replace(tc.command, " {", " 6.5 70 2.5 {", 1)))
		spawn := c.AwaitSpawn(tc.entityType)
		tag, _, _, _, _, ok := srv.ThrownTagOf(spawn.EntityID)
		if !ok {
			t.Fatalf("case %d: the body is not a thrown one", i)
		}
		var got any
		switch v := tag[tc.key].(type) {
		case nbt.Double:
			got = float64(v)
		case nbt.Byte:
			got = uint8(v)
		}
		if got != tc.want {
			t.Errorf("%s saves %s = %v, want %v", tc.command, tc.key, got, tc.want)
		}
	}
}

// awaitSummonSuccess reads the success line of a summon whose tag carried
// UUID:[I;1,2,3,last], naming the entity type shortName by its type name.
func awaitSummonSuccess(t *testing.T, c *servertest.Client, shortName string, last int32) {
	t.Helper()
	identity, _ := nbt.UUIDFromIntArray(nbt.IntArray{1, 2, 3, last})
	display := protocol.Text{Translate: "entity.minecraft." + shortName}
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", protocol.Text{
		Translate: display.Translate,
		Insertion: identity.String(),
		HoverEntity: &protocol.HoverEntity{
			Name: display,
			ID:   "minecraft:" + shortName,
			UUID: [4]uint32{1, 2, 3, uint32(last)},
		},
	})
}

// TestASummonStandsUpTheKindsASavedTagLoads summons each kind the server
// simulates but a /summon once refused: the command succeeds and the client
// is sent a spawn of the type at the position.
// vanilla: SummonCommand.createEntity, EntityType.loadEntityRecursive
func TestASummonStandsUpTheKindsASavedTagLoads(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	srv.SetBlock(9, 64, 9, servertest.BlockState(t, "minecraft:oak_fence"))

	fangsType, ok := registry.EntityTypeID("minecraft:evoker_fangs")
	if !ok {
		t.Fatal("the registry has no evoker fangs")
	}
	cases := []struct {
		name, tag string // tag: the fields after the UUID
		typ       int32
		x, y, z   float64
	}{
		{"end_crystal", ``, protocol.EntityTypeEndCrystal, 5.5, 64, 5.5},
		{"area_effect_cloud", `,Radius:2f,Duration:100`, protocol.EntityTypeAreaEffectCloud, 5.5, 64, 5.5},
		{"evoker_fangs", ``, fangsType, 5.5, 64, 5.5},
		{"firework_rocket", `,LifeTime:10`, protocol.EntityTypeFireworkRocket, 5.5, 64, 5.5},
		{"eye_of_ender", ``, protocol.EntityTypeEyeOfEnder, 5.5, 64, 5.5},
		{"shulker_bullet", ``, protocol.EntityTypeShulkerBullet, 5.5, 64, 5.5},
		{"llama_spit", ``, protocol.EntityTypeLlamaSpit, 5.5, 64, 5.5},
		{"wind_charge", ``, protocol.EntityTypeWindCharge, 5.5, 64, 5.5},
		{"leash_knot", ``, protocol.EntityTypeLeashKnot, 9.5, 64, 9.5},
	}
	for i, tc := range cases {
		// Each case's own UUID, as one live UUID cannot be summoned twice.
		tag := fmt.Sprintf("{UUID:[I;1,2,3,%d]%s}", i+10, tc.tag)
		c.ChatCommand(fmt.Sprintf("summon minecraft:%s %g %g %g %s", tc.name, tc.x, tc.y, tc.z, tag))
		spawn := c.AwaitSpawn(tc.typ)
		awaitSummonSuccess(t, c, tc.name, int32(i+10))
		if tc.name == "leash_knot" {
			// The knot hangs at its fence cell's corner.
			if spawn.X != 9 || spawn.Y != 64 || spawn.Z != 9 {
				t.Errorf("the knot stands at (%g, %g, %g), want its cell (9, 64, 9)", spawn.X, spawn.Y, spawn.Z)
			}
			continue
		}
		if tc.name != "area_effect_cloud" && (spawn.X != tc.x || spawn.Y != tc.y || spawn.Z != tc.z) {
			t.Errorf("%s stands at (%g, %g, %g), want (%g, %g, %g)", tc.name, spawn.X, spawn.Y, spawn.Z, tc.x, tc.y, tc.z)
		}
		if tc.name == "area_effect_cloud" {
			entries := c.AwaitMetadataFor(spawn.EntityID)
			if f := servertest.MetaOf(entries, protocol.MetadataIndexCloudRadius); f == nil || f.FloatVal != 2 {
				t.Errorf("the cloud's radius metadata = %v, want 2", f)
			}
		}
	}
}

// TestASummonedCrystalReadsShowBottom summons a crystal with ShowBottom
// off: its metadata hides the base.
// vanilla: EndCrystal.readAdditionalSaveData
func TestASummonedCrystalReadsShowBottom(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	c.ChatCommand(`summon minecraft:end_crystal 5.5 64 5.5 {ShowBottom:0b}`)
	spawn := c.AwaitSpawn(protocol.EntityTypeEndCrystal)
	entries := c.AwaitMetadataFor(spawn.EntityID)
	if f := servertest.MetaOf(entries, protocol.MetadataIndexEndCrystalShowBottom); f == nil || f.FlagVal {
		t.Errorf("the crystal's show-bottom metadata = %v, want false", f)
	}
}

// TestASummonedRocketBurstsAfterItsLifeTime summons a rocket of LifeTime 10
// and counts the ticks until it is gone.
// vanilla: FireworkRocketEntity.tick
func TestASummonedRocketBurstsAfterItsLifeTime(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:firework_rocket 5.5 64 5.5 {LifeTime:10}`)
	c.AwaitSpawn(protocol.EntityTypeFireworkRocket)
	if got := srv.FireworkCount(); got != 1 {
		t.Fatalf("%d rockets fly after the summon, want 1", got)
	}
	servertest.RunTicks(t, srv, 8)
	if got := srv.FireworkCount(); got != 1 {
		t.Errorf("the rocket is gone after 8 ticks of a 10-tick life")
	}
	servertest.UntilTicks(t, srv, 6, "the rocket to burst", func() bool { return srv.FireworkCount() == 0 })
}

// TestASummonedItemKeepsItsStacksComponents summons a damaged, enchanted
// sword and reads the stack off the item's metadata.
// vanilla: ItemEntity.readAdditionalSaveData
func TestASummonedItemKeepsItsStacksComponents(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	c.ChatCommand(`summon minecraft:item 5.5 64 5.5 {Item:{id:"minecraft:diamond_sword",count:1,components:{"minecraft:damage":7,"minecraft:enchantments":{"minecraft:sharpness":5}}}}`)
	spawn := c.AwaitSpawn(protocol.EntityTypeItem)
	entries := c.AwaitMetadataFor(spawn.EntityID)
	meta := servertest.MetaOf(entries, protocol.MetadataIndexItemSlot)
	if meta == nil {
		t.Fatal("the item carries no stack metadata")
	}
	id, _ := registry.EnchantmentID("minecraft:sharpness")
	if meta.SlotVal.Damage != 7 || meta.SlotVal.Enchantments[id] != 5 {
		t.Errorf("stack damage %d, enchantments %v; want damage 7, sharpness 5", meta.SlotVal.Damage, meta.SlotVal.Enchantments)
	}
}
