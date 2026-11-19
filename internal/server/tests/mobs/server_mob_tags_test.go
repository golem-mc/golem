package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"

	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
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
	fixtureKeptZombie  = servertest.FixtureUUID(-1670843907, 1874741199, -1690992589, 303831286)
	fixturePlainZombie = servertest.FixtureUUID(1022620492, -442479979, -1620027294, 657343806)
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

// TestALoadedLeftHandedMobIsShownLeftHanded loads the vanilla fixture's
// zombie saved with LeftHanded and checks its spawn metadata carries the
// bit of the mob flags byte a client draws the off-hand swing with; the
// plain zombie beside it carries none.
func TestALoadedLeftHandedMobIsShownLeftHanded(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	installMobFixture(t, cfg)

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000) // midnight, as the fixture was saved
	c := connect()
	c.Login("Drover")
	c.Configure()
	shown := c.JoinSeeingMobs("Drover", fixtureKeptZombie, fixturePlainZombie)

	left := servertest.MetaOf(shown[fixtureKeptZombie].Meta, protocol.MetadataIndexMobFlags)
	if left == nil || left.Typ != protocol.MetadataByte {
		t.Fatalf("the left-handed zombie's metadata carries no mob flags byte: %+v", left)
	}
	if left.ByteVal&protocol.EntityFlagLeftHanded == 0 {
		t.Errorf("the left-handed zombie's mob flags = %#02x, want the left-handed bit", left.ByteVal)
	}
	if right := servertest.MetaOf(shown[fixturePlainZombie].Meta, protocol.MetadataIndexMobFlags); right != nil && right.ByteVal&protocol.EntityFlagLeftHanded != 0 {
		t.Errorf("the plain zombie's mob flags = %#02x, want no left-handed bit", right.ByteVal)
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

// TestAPersistentMobIsNotDespawned carries the fixture's two zombies far
// from the player: the plain one despawns, the one saved with
// PersistenceRequired stays. vanilla: Mob.checkDespawn
func TestAPersistentMobIsNotDespawned(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	installMobFixture(t, cfg)

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000)
	c := connect()
	c.Login("Keeper")
	c.Configure()
	c.JoinSeeingMobs("Keeper", fixtureKeptZombie, fixturePlainZombie)

	release := srv.HoldChunk(200, 0)
	defer release()
	ids := map[uuid.UUID]int32{}
	for _, m := range srv.Mobs() {
		if m.Kind != "minecraft:zombie" {
			continue
		}
		s, _ := srv.MobUUID(m.ID)
		ids[uuid.MustParse(s)] = m.ID
	}
	kept, plain := ids[fixtureKeptZombie], ids[fixturePlainZombie]
	if kept == 0 || plain == 0 {
		t.Fatalf("the zombies loaded as %v, want both fixture zombies", ids)
	}
	srv.MoveMob(kept, 200.5, 0.5)
	srv.MoveMob(plain, 201.5, 1.5)

	alive := func(id int32) bool {
		for _, m := range srv.Mobs() {
			if m.ID == id {
				return true
			}
		}
		return false
	}
	servertest.UntilTicks(t, srv, 400, "condition", func() bool { return !alive(plain) })
	gone := srv.TicksRun()
	servertest.UntilTicks(t, srv, 400, "condition", func() bool { return srv.TicksRun() >= gone+10 })
	if !alive(kept) {
		t.Error("the zombie saved with PersistenceRequired despawned")
	}
}

// waitUntil waits for cond without failing the test when the ticks run out,
// answering whether it held — the wait a test that can carry on and try the
// next thing calls for. Each step runs a tick or two.
func waitUntil(t *testing.T, srv *server.Server, steps int, cond func() bool) bool {
	t.Helper()
	for range steps {
		if cond() {
			return true
		}
		servertest.RunTicks(t, srv, 1)
	}
	return cond()
}

// TestAChickenLaysTheEggOfItsVariant plants a grown hen of each variant and
// reads the egg each lays: the plain egg in the temperate plains, the brown
// one in the nether whose every biome the warm tag claims, the blue one in
// the end whose biome the cold tag claims — the item vanilla's
// gameplay/chicken_lay table pays each hen.
func TestAChickenLaysTheEggOfItsVariant(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Farmer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	lay := func(want string, plant func() int32) {
		t.Helper()
		id := plant()
		// Freeze the hen: the countdown below is wound on this ID, so one that
		// wanders off or stands back up under a new ID lays no egg the client
		// is shown.
		if !srv.SetMobNoAI(id) {
			t.Fatalf("the planted %s hen is not in the herd", want)
		}
		srv.SetEggTimer(id, 1)
		traffic := servertest.ReadThrownTraffic(c, srv, 40)
		egg := traffic.SpawnOfType(protocol.EntityTypeItem)
		if egg == nil {
			t.Fatalf("the hen laid no egg the client was shown, want a %s", want)
		}
		metas := traffic.MetasOf(egg.EntityID)
		if len(metas) == 0 {
			t.Fatal("the laid egg's metadata never said what it holds")
		}
		for _, m := range metas {
			if m.Count != 1 || m.Item != servertest.MustItem(want) {
				t.Errorf("the hen laid %d of item %d, want one %s", m.Count, m.Item, want)
			}
		}
	}

	lay("minecraft:egg", func() int32 {
		return srv.SpawnMob("minecraft:chicken", 3.5, 64, 4.5)
	})

	// The nether's own rock can fill the cell the hen is planted in, and its
	// arrival chunk is sent asynchronously: a hen left in either suffocates or
	// is saved away and stands back up under a new ID, so hold the chunk and
	// stand it on a stone floor in clear air.
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 8, 8))
	srv.SetBlockInDimension(world.NetherName, 8, 69, 8, servertest.MustState(t, "minecraft:stone", nil))
	srv.SetBlockInDimension(world.NetherName, 8, 70, 8, 0)
	srv.SetBlockInDimension(world.NetherName, 8, 71, 8, 0)
	srv.MovePlayerToDimension("Farmer", world.NetherName, 8, 70, 8)
	c.Settle()
	lay("minecraft:brown_egg", func() int32 {
		return srv.SpawnMobInDimension("minecraft:chicken", world.NetherName, 8.5, 70, 8.5)
	})

	arrival := world.EndArrival()
	// The generator bakes the arrival platform in, but its chunk is still sent
	// asynchronously, so hold it before the hen is planted.
	t.Cleanup(srv.HoldChunkInDimension(world.EndName, arrival.X, arrival.Z))
	srv.MovePlayerToDimension("Farmer", world.EndName,
		float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5)
	c.Settle()
	lay("minecraft:blue_egg", func() int32 {
		return srv.SpawnMobInDimension("minecraft:chicken", world.EndName,
			float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5)
	})
}

// TestABrownAndBlueEggFlyTheEggFlight throws the brown egg and the blue one
// the way the wire throws the plain egg, and reads the shapes the throw is
// made of off each: the spawn of the egg body, the item its metadata
// carries, and the survival hand one egg poorer.
func TestABrownAndBlueEggFlyTheEggFlight(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.SimulationDistance = 2
	srv, c, _ := servertest.JoinAs(t, cfg, "Thrower")

	for _, name := range []string{"minecraft:brown_egg", "minecraft:blue_egg"} {
		srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem(name), Count: 16})
		c.MoveTo(0.5, 64, 0.5, -90, 45)
		servertest.UseHeld(c, -90, 45)

		traffic := servertest.ReadThrownTraffic(c, srv, 40)
		egg := traffic.SpawnOfType(protocol.EntityTypeEgg)
		if egg == nil {
			t.Fatalf("no egg came into the world for the %s throw", name)
		}
		metas := traffic.MetasOf(egg.EntityID)
		if len(metas) == 0 {
			t.Fatalf("the %s's metadata never said what it holds", name)
		}
		for _, m := range metas {
			if m.Count != 1 || m.Item != servertest.MustItem(name) {
				t.Errorf("the %s's metadata carried %d of item %d, want one of itself", name, m.Count, m.Item)
			}
		}
		if left := srv.InventorySlot("Thrower", 36); left.Count != 15 {
			t.Errorf("the hotbar holds %d of the %s after the throw, want the fifteen one throw left", left.Count, name)
		}
	}
}

// TestAnEggHatchesTheChickItsVariantIs throws the brown egg whose dice
// hatch one chick and reads the variant the chick's metadata carries: the
// warm one the egg was, where a chick of the plain egg's temperate default
// would name none.
func TestAnEggHatchesTheChickItsVariantIs(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Hatcher")

	// A seed whose first roll of eight lands the hatch and whose next roll
	// of thirty-two passes: one chick, the second roll spent. The dice are
	// the body's own, and the throw's spread spent six draws of them before
	// the break's rolls asked, so the search draws those six first, the way
	// the server's throw does.
	var oneSeed int64 = -1
	for seed := int64(1); seed < 100000; seed++ {
		rng := rand.New(rand.NewSource(seed))
		for range 6 {
			rng.Float64()
		}
		if rng.Intn(8) == 0 && rng.Intn(32) != 0 {
			oneSeed = seed
			break
		}
	}
	if oneSeed < 0 {
		t.Fatal("no seed under a hundred thousand hatched one chick")
	}

	// The throw straight down at the thrower's feet, the way the flight
	// test throws, so the break is close behind the first pass.
	srv.ThrowEggItem("minecraft:brown_egg", 0.5, 64+1.62-0.1, 0.5, -90, 45, playerID, oneSeed)

	var (
		chickID int32
		entries []servertest.MetaEntry
	)
	c.ReadTicks(srv, 60, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			spawn := servertest.ReadThrownSpawn(c, data)
			if spawn.EntityType == protocol.EntityTypeChicken && chickID == 0 {
				chickID = spawn.EntityID
			}
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if id := r.ReadVarInt(); chickID == 0 || id != chickID {
				return false
			}
			es, ok := servertest.ReadMetadata(r)
			if !ok {
				c.T.Fatal("the chick's metadata carries a field this reader does not know")
			}
			entries = append(entries, es...)
		}
		return false
	})
	if chickID == 0 {
		t.Fatal("the egg's break hatched no chicken the client was shown")
	}
	m := servertest.MetaOf(entries, protocol.MetadataIndexChickenVariant)
	if m == nil {
		t.Fatal("the chick's metadata names no variant, want the warm one the egg carried")
	}
	if want := registry.MustID("minecraft:chicken_variant", "minecraft:warm"); m.Typ != protocol.MetadataChickenVariant || m.VarintVal != want {
		t.Errorf("the chick's variant field = type %d value %d, want type %d value %d",
			m.Typ, m.VarintVal, protocol.MetadataChickenVariant, want)
	}
}

// TestAHerdPicksTheBiomeEachAnimalStandsIn grows a herd, raises its ground
// a section and saves the chunk with the upper sections re-labelled the
// cold biome, then reads the variants the finalize leaves when the chunk's
// first-generation animals are run on the terrain read back: the snowy
// ground each animal stands on, not the sea-level section a chunk-centre
// read would ask.
func TestAHerdPicksTheBiomeEachAnimalStandsIn(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelSeed = "3"

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	srv.SetSpawning(false)
	srv.StartTick()

	// The mobs of one chunk, the herd the chunk's own wake stood up.
	herdIn := func(srv *server.Server, pos world.ChunkPos) []server.TestMob {
		var out []server.TestMob
		for _, m := range srv.Mobs() {
			if world.ChunkOf(m.X) == pos.X && world.ChunkOf(m.Z) == pos.Z {
				out = append(out, m)
			}
		}
		return out
	}

	// Hold chunks in turn until one grows a herd a variant carrier stands
	// in: the kind a chunk's dice draw is fixed, and a sheep names no
	// variant to read. The candidates are block coordinates, the form
	// HoldChunk takes.
	var herdAt world.ChunkPos
	count := 0
	for _, at := range [][2]int32{{16, -32}, {32, -32}} {
		pos := world.ChunkPos{X: world.ChunkOf(float64(at[0])), Z: world.ChunkOf(float64(at[1]))}
		release := srv.HoldChunk(at[0], at[1])
		if !waitUntil(t, srv, 200, func() bool { return len(herdIn(srv, pos)) > 0 }) {
			release()
			continue
		}
		kind := herdIn(srv, pos)[0].Kind
		if kind == "minecraft:chicken" || kind == "minecraft:cow" || kind == "minecraft:pig" {
			herdAt, count = pos, len(herdIn(srv, pos))
			break
		}
		release()
	}
	if count == 0 {
		t.Fatal("no candidate chunk grew a herd a variant carrier stands in")
	}
	// A grass plateau whose animals stand at y 80, in the sections the edit
	// below re-labels the cold biome (the zoomed read may reach y 76 too),
	// while the spawn gate's read at the chunk corner's top block stays in
	// the top section the chunk grew — the reads the server's
	// per-section biomes can only tell apart by the section they are read in.
	grass := servertest.BlockState(t, "minecraft:grass_block")
	for dx := range int32(16) {
		for dz := range int32(16) {
			srv.SetBlock(herdAt.X*16+dx, 79, herdAt.Z*16+dz, grass)
		}
	}
	// The chunk into the save: a generated chunk nothing changed is not
	// dirty, and the edit below needs its region file written.
	srv.TouchChunk(herdAt.X*16+8, herdAt.Z*16+8)
	srv.Shutdown()

	// The herd's entity file goes, so the restarted world stands up no herd
	// of its own.
	if err := os.Remove(servertest.EntityRegionPath(cfg.LevelName, herdAt)); err != nil {
		t.Fatalf("remove the herd's entity file: %v", err)
	}
	// The chunk's sections from y 64 to below the top re-labelled the cold
	// biome; the top section, where the spawn gate reads, left as it grew.
	servertest.RelabelChunkGround(t, cfg.LevelName, herdAt, 4, 18, "minecraft:snowy_plains")

	restarted := *cfg
	srv2, err := server.New(&restarted)
	if err != nil {
		t.Fatalf("restart the server: %v", err)
	}
	srv2.SetSpawning(false)
	srv2.StartTick()
	release := srv2.HoldChunk(herdAt.X*16+8, herdAt.Z*16+8)
	defer release()

	// The chunk read back grows no animals of its own, so the finalize is
	// asked here, on the terrain and biomes the chunk saved.
	grown := srv2.GrowNaturalMobsForTest(herdAt)
	srv2.Shutdown()

	// What the finalize left: every variant carrier of the chunk cold, the
	// snowy ground each stands on having said so, not the sea-level section a
	// chunk-centre read would ask.
	seen := 0
	for _, m := range grown {
		switch m.Kind {
		case "minecraft:chicken", "minecraft:cow", "minecraft:pig":
		default:
			continue
		}
		// The zoomed read is at (x-2)>>2 or one more, so it leaves the chunk,
		// which the neighbour keeps as it grew, outside local 2..13.
		if lx, lz := int(math.Floor(m.X))&15, int(math.Floor(m.Z))&15; lx < 2 || lx > 13 || lz < 2 || lz > 13 {
			continue
		}
		if m.Variant != "minecraft:cold" {
			t.Errorf("the %s at %v,%v finalized variant %q, want the snowy ground's minecraft:cold", m.Kind, m.X, m.Z, m.Variant)
		}
		seen++
	}
	if seen == 0 {
		t.Fatal("the chunk grew no variant carrier to read")
	}
}
