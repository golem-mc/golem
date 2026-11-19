package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The vanilla mob tags this server does not simulate. testdata/vanilla-mobs.mca
// is the overworld entity region a vanilla server saved (flat world,
// midnight) after these summons around chunk (0,0):
//
//	pig 4.5 64 4.5 {CustomName:"Bob",variant:"minecraft:cold",sound_variant:"minecraft:big"}
//	cow 8.5 64 4.5 {CustomName:{text:"Rose",color:"red",bold:true},CustomNameVisible:1b,
//	                PersistenceRequired:1b,variant:"minecraft:cold",sound_variant:"minecraft:moody"}
//	chicken 12.5 64 4.5 {variant:"minecraft:warm",sound_variant:"minecraft:picky",
//	                     Age:-20000,AgeLocked:1b,ForcedAge:-20000}
//	sheep 4.5 64 8.5 {InLove:400,Color:3b}
//	zombie 8.5 64 8.5 {PersistenceRequired:1b,LeftHanded:1b,CanPickUpLoot:1b}
//	zombie 12.5 64 8.5, skeleton 4.5 64 12.5 {AbsorptionAmount:4f}, cow 8.5 64 12.5
//
// and, unsummoned so their finalizeSpawn chose the variant, a cow, a pig and
// a chicken each in chunk (2,0) filled with snowy_plains and chunk (3,0)
// filled with desert: cold in the snow, warm in the desert.

// Fixture UUIDs, as the vanilla save wrote them.
var (
	fixtureBob         = servertest.FixtureUUID(-2010845121, -1162525990, -1968095534, -164160295)
	fixtureRose        = servertest.FixtureUUID(-872072145, 995117144, -1748266319, 205124063)
	fixtureLockedChick = servertest.FixtureUUID(209399171, -37862272, -1365244897, 1227401466)
	fixturePlainCow    = servertest.FixtureUUID(1852217840, 1057179854, -1713984039, 1098567494)
	fixtureSnowCow     = servertest.FixtureUUID(2021465165, -335066710, -1787013306, 954949591)
	fixtureDesertPig   = servertest.FixtureUUID(484562970, 36260949, -1435570117, 279320960)
	fixtureDesertChick = servertest.FixtureUUID(-64719359, 1381187848, -1684998856, 651547080)
	fixtureSnowChicken = servertest.FixtureUUID(1437166571, -1465692764, -1121146105, 2000754533)
)

const fixtureMobsInRegion = 14

// installMobFixture puts the vanilla entity region where the server reads
// the overworld's entities and returns its mobs by UUID.
func installMobFixture(t *testing.T, cfg *config.Config) map[uuid.UUID]nbt.Compound {
	t.Helper()
	cfg.LevelName = t.TempDir()
	data, err := os.ReadFile(filepath.Join("testdata", "vanilla-mobs.mca"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dir := servertest.OverworldEntities(cfg.LevelName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create entities dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "r.0.0.mca"), data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	mobs := servertest.SavedMobsByUUID(t, cfg.LevelName)
	if len(mobs) != fixtureMobsInRegion {
		t.Fatalf("the fixture holds %d mobs, want %d", len(mobs), fixtureMobsInRegion)
	}
	return mobs
}

// TestVanillaMobTagsSurviveALoadAndSave loads a vanilla entity region,
// shows the mobs' custom names and variants in their spawn metadata, and
// writes every tag back unchanged but the ones the simulation moves.
func TestVanillaMobTagsSurviveALoadAndSave(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	fixture := installMobFixture(t, cfg)

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000) // midnight, as the fixture was saved: nothing burns
	c := connect()
	c.Login("Drover")
	c.Configure()
	shown := c.JoinSeeingMobs("Drover", fixtureBob, fixtureRose, fixtureLockedChick,
		fixturePlainCow, fixtureSnowCow, fixtureDesertPig, fixtureDesertChick, fixtureSnowChicken)

	variantID := func(registryName, entry string) int32 { return registry.MustID(registryName, entry) }
	expectEntry := func(who string, e servertest.CapturedEntity, index uint8, typ int32, value int32) {
		t.Helper()
		m := servertest.MetaOf(e.Meta, index)
		switch {
		case m == nil:
			t.Errorf("%s's metadata has no field %d, want %d", who, index, value)
		case m.Typ != typ || m.VarintVal != value:
			t.Errorf("%s's field %d = type %d value %d, want type %d value %d", who, index, m.Typ, m.VarintVal, typ, value)
		}
	}
	bob := shown[fixtureBob]
	if m := servertest.MetaOf(bob.Meta, protocol.MetadataIndexCustomName); m == nil || !reflect.DeepEqual(m.TextVal, nbt.String("Bob")) {
		t.Errorf("Bob's custom name metadata = %+v, want the string Bob", m)
	}
	if m := servertest.MetaOf(bob.Meta, protocol.MetadataIndexCustomNameVisible); m != nil {
		t.Error("Bob's name is shown always, want only when looked at")
	}
	expectEntry("Bob", bob, protocol.MetadataIndexPigVariant, protocol.MetadataPigVariant, variantID("minecraft:pig_variant", "minecraft:cold"))
	expectEntry("Bob", bob, protocol.MetadataIndexPigSoundVariant, protocol.MetadataPigSoundVariant, variantID("minecraft:pig_sound_variant", "minecraft:big"))

	rose := shown[fixtureRose]
	if m := servertest.MetaOf(rose.Meta, protocol.MetadataIndexCustomName); m == nil || !reflect.DeepEqual(m.TextVal, fixture[fixtureRose]["CustomName"]) {
		t.Errorf("Rose's custom name metadata = %+v, want the saved component %v", m, fixture[fixtureRose]["CustomName"])
	}
	if m := servertest.MetaOf(rose.Meta, protocol.MetadataIndexCustomNameVisible); m == nil || !m.FlagVal {
		t.Error("Rose's name is not shown always, want CustomNameVisible's true")
	}
	expectEntry("Rose", rose, protocol.MetadataIndexCowVariant, protocol.MetadataCowVariant, variantID("minecraft:cow_variant", "minecraft:cold"))
	expectEntry("Rose", rose, protocol.MetadataIndexCowSoundVariant, protocol.MetadataCowSoundVariant, variantID("minecraft:cow_sound_variant", "minecraft:moody"))

	chick := shown[fixtureLockedChick]
	expectEntry("the locked chick", chick, protocol.MetadataIndexChickenVariant, protocol.MetadataChickenVariant, variantID("minecraft:chicken_variant", "minecraft:warm"))
	if m := servertest.MetaOf(chick.Meta, protocol.MetadataIndexAgeLocked); m == nil || !m.FlagVal {
		t.Error("the locked chick's metadata does not carry its age lock")
	}
	expectEntry("the snow cow", shown[fixtureSnowCow], protocol.MetadataIndexCowVariant, protocol.MetadataCowVariant, variantID("minecraft:cow_variant", "minecraft:cold"))
	expectEntry("the snow chicken", shown[fixtureSnowChicken], protocol.MetadataIndexChickenVariant, protocol.MetadataChickenVariant, variantID("minecraft:chicken_variant", "minecraft:cold"))
	expectEntry("the desert pig", shown[fixtureDesertPig], protocol.MetadataIndexPigVariant, protocol.MetadataPigVariant, variantID("minecraft:pig_variant", "minecraft:warm"))
	expectEntry("the desert chicken", shown[fixtureDesertChick], protocol.MetadataIndexChickenSoundVariant, protocol.MetadataChickenSoundVariant, variantID("minecraft:chicken_sound_variant", "minecraft:picky"))
	// The plain cow is temperate and classic, the defaults vanilla's spawn
	// metadata leaves out; and nobody named it.
	for _, index := range []uint8{protocol.MetadataIndexCowVariant, protocol.MetadataIndexCowSoundVariant, protocol.MetadataIndexCustomName} {
		if m := servertest.MetaOf(shown[fixturePlainCow].Meta, index); m != nil {
			t.Errorf("the plain cow's metadata carries field %d, a default", index)
		}
	}

	c.Disconnect()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	// What the simulation moves; everything else must come back as it was.
	moving := map[string]bool{
		"Pos": true, "Motion": true, "Rotation": true, "fall_distance": true,
		"OnGround": true, "Health": true, "Fire": true, "EggLayTime": true,
	}
	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)
	for id, want := range fixture {
		got, ok := saved[id]
		if !ok {
			t.Errorf("the %s %s was not saved back", want["id"], id)
			continue
		}
		for key, value := range want {
			if moving[key] {
				continue
			}
			if !reflect.DeepEqual(got[key], value) {
				t.Errorf("the %s %s saved %s = %v, want the loaded %v", want["id"], id, key, got[key], value)
			}
		}
		for key := range got {
			if key == "FreezingTime" && want["id"] == nbt.String("minecraft:skeleton") {
				// The fixture is a 26.2 save; this version writes the skeleton's
				// powder snow time, -1 out of the snow.
				// vanilla: ConversionTracker.addAdditionalSaveData
				if !reflect.DeepEqual(got[key], nbt.Int(-1)) {
					t.Errorf("the skeleton %s saved FreezingTime = %v, want -1", id, got[key])
				}
				continue
			}
			if _, ok := want[key]; !ok {
				t.Errorf("the %s %s saved %s, which the vanilla save did not have", want["id"], id, key)
			}
		}
	}
}

// TestANameTagNamesAMobForGood names a pig with a name tag: the name goes
// out in its metadata, the tag is used up, and the name and the persistence
// the tag gives are in the pig's saved compound and on its return.
func TestANameTagNamesAMobForGood(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Namer")

	srv.SpawnMob("minecraft:pig", 1.5, 64, 2.5)
	pig := c.ExpectMobSpawn(protocol.EntityTypePig)
	if pig.Name != nil {
		t.Fatalf("the fresh pig is named %v", pig.Name)
	}

	slot := player.HotbarSlot
	srv.GiveSlot("Namer", slot, protocol.ItemStack{
		ItemID: registry.MustItem("minecraft:name_tag"), Count: 1,
		CustomName: &protocol.Text{Text: "Wilbur"},
	})
	c.UseOn(pig.EntityID)
	named := pig
	c.ReadMobMetadataInto(&named, c.ExpectMobPacket(protocol.IDPlayEntityMetadata))
	if !reflect.DeepEqual(named.Name, nbt.String("Wilbur")) {
		t.Errorf("the name tag's metadata named the pig %v, want Wilbur", named.Name)
	}
	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.InventorySlot("Namer", slot).Empty() })

	c.Disconnect()
	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()
	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)
	if len(saved) != 1 {
		t.Fatalf("the world saved %d mobs, want the pig", len(saved))
	}
	for _, compound := range saved {
		if got := compound["CustomName"]; !reflect.DeepEqual(got, nbt.String("Wilbur")) {
			t.Errorf("saved CustomName = %v, want Wilbur", got)
		}
		if got := compound["PersistenceRequired"]; !reflect.DeepEqual(got, nbt.Byte(1)) {
			t.Errorf("saved PersistenceRequired = %v, want the name tag's 1b", got)
		}
	}

	second := connect()
	second.Login("Namer")
	second.Configure()
	back, ok := second.EnterWorldSeeingMob("Namer", protocol.EntityTypePig)
	if !ok {
		t.Fatal("the pig was not restored with its chunk")
	}
	if !reflect.DeepEqual(back.Name, nbt.String("Wilbur")) {
		t.Errorf("the returned pig is named %v, want Wilbur", back.Name)
	}
}
