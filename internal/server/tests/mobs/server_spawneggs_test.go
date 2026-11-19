package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestASpawnEggPlacesItsMobWhereVanillaDoes uses a cow egg on blocks the way
// SpawnEggItem.useOn reads them: a block with no collision shape (grass) is
// spawned into, anything else is spawned beside on the clicked face, and the
// body drops onto what the cell holds (EntityType.getYOffset). One egg of
// the two is spent.
func TestASpawnEggPlacesItsMobWhereVanillaDoes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		block         string
		props         map[string]string
		face          int32
		clickY        int32
		wantX, wantY  float64
		wantZ         float64
		placeAtClickY bool
	}{
		{name: "into short grass", block: "minecraft:short_grass", face: protocol.FaceTop,
			clickY: 64, placeAtClickY: true, wantX: 2.5, wantY: 64, wantZ: 3.5},
		{name: "on top of stone", face: protocol.FaceTop,
			clickY: 63, wantX: 2.5, wantY: 64, wantZ: 3.5},
		{name: "on a bottom slab", block: "minecraft:stone_slab", props: map[string]string{"type": "bottom"},
			face: protocol.FaceTop, clickY: 64, placeAtClickY: true, wantX: 2.5, wantY: 64.5, wantZ: 3.5},
		{name: "beside a block's side", block: "minecraft:stone", face: protocol.FaceEast,
			clickY: 64, placeAtClickY: true, wantX: 3.5, wantY: 64, wantZ: 3.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.EggPlayer(t, "survival", "Rancher", "minecraft:cow_spawn_egg", 2)
			if tc.placeAtClickY {
				srv.SetBlock(2, tc.clickY, 3, servertest.MustState(t, tc.block, tc.props))
			}
			c.PlaceBlock(2, tc.clickY, 3, tc.face)
			cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
			if cow.X != tc.wantX || cow.Y != tc.wantY || cow.Z != tc.wantZ {
				t.Errorf("cow spawned at (%v, %v, %v), want (%v, %v, %v)",
					cow.X, cow.Y, cow.Z, tc.wantX, tc.wantY, tc.wantZ)
			}
			if cow.HasBaby {
				t.Error("an egg's cow spawned a baby")
			}
			c.ExpectBlockAck()
			if got := srv.InventorySlot("Rancher", player.HotbarSlot).Count; got != 1 {
				t.Errorf("eggs left = %d, want 1", got)
			}
		})
	}
}

// TestANamedSpawnEggNamesItsMob: the egg's custom_name is applied to the
// mob (Entity.applyComponentsFromItemStack), shown as its custom name field.
func TestANamedSpawnEggNamesItsMob(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Namer", "minecraft:pig_spawn_egg", 1)
	srv.GiveSlot("Namer", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:pig_spawn_egg"), Count: 1,
		CustomName: &protocol.Text{Text: "Wilbur"},
	})
	c.PlaceBlock(2, 63, 3, protocol.FaceTop)
	pig := c.AwaitSpawn(protocol.EntityTypePig)
	meta := c.AwaitMetadataFor(pig.EntityID)
	name := servertest.MetaOf(meta, protocol.MetadataIndexCustomName)
	if name == nil || name.TextVal == nil {
		t.Fatal("the pig carries no custom name field")
	}
	if text, ok := protocol.TextOf(name.TextVal); !ok || text.Text != "Wilbur" {
		t.Fatalf("pig's custom name field = %v, want Wilbur", name.TextVal)
	}
	if mobs := srv.Mobs(); len(mobs) != 1 || mobs[0].Name != "Wilbur" {
		t.Errorf("mobs = %+v, want one pig named Wilbur", mobs)
	}
}

// TestASpawnEggOnItsOwnAdultSpawnsABaby: a zombie egg used on a grown
// zombie spawns a baby zombie where the parent stands
// (SpawnEggItem.spawnOffspringFromSpawnEgg), spending the egg.
func TestASpawnEggOnItsOwnAdultSpawnsABaby(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Breeder", "minecraft:zombie_spawn_egg", 2)
	srv.StopTick()
	srv.SpawnMob("minecraft:zombie", 2.5, 64, 1.5)
	parent := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	if parent.Baby {
		t.Fatal("the planted zombie is a baby; plant it elsewhere")
	}
	c.UseOn(parent.EntityID)
	baby := c.AwaitSpawn(protocol.EntityTypeZombie)
	if flag := servertest.MetaOf(c.AwaitMetadataFor(baby.EntityID), protocol.MetadataIndexBaby); flag == nil || !flag.FlagVal {
		t.Error("the egg's zombie is not a baby")
	}
	if baby.X != parent.X || baby.Y != parent.Y || baby.Z != parent.Z {
		t.Errorf("baby at (%v, %v, %v), want the parent's (%v, %v, %v)",
			baby.X, baby.Y, baby.Z, parent.X, parent.Y, parent.Z)
	}
	servertest.UntilTicks(t, srv, 300, "condition", func() bool {
		return srv.InventorySlot("Breeder", player.HotbarSlot).Count == 1
	})
}

// TestASpawnEggOnAnotherKindDoesNothing: a cow egg on a creeper is no
// offspring (spawnsEntity fails), and the egg is kept.
func TestASpawnEggOnAnotherKindDoesNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Mixer", "minecraft:cow_spawn_egg", 1)
	srv.StopTick()
	srv.SpawnMob("minecraft:creeper", 2.5, 64, 1.5)
	creeper := c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	c.UseOn(creeper.EntityID)
	// A creeper egg on a creeper: Mob.setBaby does nothing, so no offspring.
	srv.GiveSlot("Mixer", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:creeper_spawn_egg"), Count: 1,
	})
	c.UseOn(creeper.EntityID)
	c.Settle()
	if n := srv.MobCount(); n != 1 {
		t.Errorf("mob count = %d, want the one creeper", n)
	}
	if got := srv.InventorySlot("Mixer", player.HotbarSlot).Count; got != 1 {
		t.Errorf("eggs left = %d, want 1", got)
	}
}

// TestAWanderingTraderEggOnATraderDoesNothing: a wandering trader can be a
// baby, but getBreedOffspring makes none, so the egg is kept.
func TestAWanderingTraderEggOnATraderDoesNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Trader", "minecraft:wandering_trader_spawn_egg", 1)
	srv.StopTick()
	srv.SpawnMob("minecraft:wandering_trader", 2.5, 64, 1.5)
	trader := c.ExpectMobSpawn(protocol.EntityTypeWanderingTrader)
	c.UseOn(trader.EntityID)
	c.Settle()
	if n := srv.MobCount(); n != 1 {
		t.Errorf("mob count = %d, want the one trader", n)
	}
	if got := srv.InventorySlot("Trader", player.HotbarSlot).Count; got != 1 {
		t.Errorf("eggs left = %d, want 1", got)
	}
}

// TestACreativeSpawnEggIsKept: a creative player's egg spawns its mob and
// stays in the hand (ItemStack.consume skips infinite materials).
func TestACreativeSpawnEggIsKept(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "creative", "Maker", "minecraft:cow_spawn_egg", 2)
	c.PlaceBlock(2, 63, 3, protocol.FaceTop)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ExpectBlockAck()
	if got := srv.InventorySlot("Maker", player.HotbarSlot).Count; got != 2 {
		t.Errorf("eggs left = %d, want the 2 creative keeps", got)
	}
}

// TestAMonsterEggInPeacefulIsRefused: EntityType.canSpawn refuses a monster
// in peaceful, and the egg is kept.
func TestAMonsterEggInPeacefulIsRefused(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Pacifist", "minecraft:zombie_spawn_egg", 1)
	srv.SetDifficulty(0)
	c.PlaceBlock(2, 63, 3, protocol.FaceTop)
	c.ExpectBlockAck()
	c.Settle()
	if n := srv.MobCount(); n != 0 {
		t.Errorf("mob count = %d, want none", n)
	}
	if got := srv.InventorySlot("Pacifist", player.HotbarSlot).Count; got != 1 {
		t.Errorf("eggs left = %d, want 1", got)
	}
}

// TestASpawnEggUsedAtWaterSpawnsInIt: Use Item with the look on a water
// source (SpawnEggItem.use's SOURCE_ONLY clip) spawns the mob in the
// water's cell, with no drop onto the floor.
func TestASpawnEggUsedAtWaterSpawnsInIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Swimmer", "minecraft:cow_spawn_egg", 1)
	srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"}))
	// From the eye (0.5, 65.62, 0.5) at the middle of the water cell.
	dx, dy, dz := 2.0, 64.5-65.62, 3.0
	yaw := float32(-math.Atan2(dx, dz) * 180 / math.Pi)
	pitch := float32(-math.Atan2(dy, math.Hypot(dx, dz)) * 180 / math.Pi)
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutVarInt(1)
		b.PutF32(yaw)
		b.PutF32(pitch)
	})
	cow := c.AwaitSpawn(protocol.EntityTypeCow)
	if cow.X != 2.5 || cow.Y != 64 || cow.Z != 3.5 {
		t.Errorf("cow spawned at (%v, %v, %v), want (2.5, 64, 3.5)", cow.X, cow.Y, cow.Z)
	}
	servertest.UntilTicks(t, srv, 300, "condition", func() bool {
		return srv.InventorySlot("Swimmer", player.HotbarSlot).Empty()
	})
}

// TestASpawnEggOnASpawner: with spawner_blocks_work off the egg is refused
// with vanilla's message and kept; with it on the egg's kind lands on the
// spawner's next spawn, told as the block entity data a Block Update rides
// with, and the egg is spent.
func TestASpawnEggOnASpawner(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Keeper", "minecraft:cow_spawn_egg", 2)
	srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:spawner", nil))
	srv.SetGameRule("spawner_blocks_work", false)
	c.PlaceBlock(2, 64, 3, protocol.FaceTop)
	c.ExpectSystemChat("advMode.notEnabled.spawner", false)
	c.ExpectBlockAck()

	srv.SetGameRule("spawner_blocks_work", true)
	c.PlaceBlock(2, 64, 3, protocol.FaceTop)
	x, y, z, beType, data := c.AwaitBlockEntityData()
	if x != 2 || y != 64 || z != 3 {
		t.Errorf("block entity data at (%d,%d,%d), want the spawner at (2,64,3)", x, y, z)
	}
	if beType != protocol.BlockEntitySpawner {
		t.Errorf("block entity type = %d, want %d", beType, protocol.BlockEntitySpawner)
	}
	if _, has := data["id"]; has {
		t.Errorf("the spawner's update tag carries an id: %#v", data)
	}
	spawn, ok := nbt.Get[nbt.Compound](data, "SpawnData")
	if !ok {
		t.Fatalf("block entity SpawnData = %#v, want the next spawn", data)
	}
	next, ok := nbt.Get[nbt.Compound](spawn, "entity")
	cow, ok2 := nbt.Get[nbt.String](next, "id")
	if !ok || !ok2 || cow != "minecraft:cow" {
		t.Fatalf("SpawnData.entity = %#v, want the cow", spawn)
	}
	// The numbers a fresh spawner keeps, BaseSpawner's own defaults.
	// The tag is read at the tick's flush, after the spawner's own tick took
	// one off Delay.
	if got, has := nbt.Get[nbt.Short](data, "Delay"); !has || got < 19 || got > 20 {
		t.Errorf("spawner Delay = %d/%v, want 19 or 20", got, has)
	}
	for name, want := range map[string]nbt.Short{
		"MinSpawnDelay": 200, "MaxSpawnDelay": 800, "SpawnCount": 4,
		"MaxNearbyEntities": 6, "RequiredPlayerRange": 16, "SpawnRange": 4,
	} {
		if got, has := nbt.Get[nbt.Short](data, name); !has || got != want {
			t.Errorf("spawner %s = %d/%v, want %d", name, got, has, want)
		}
	}
	if _, has := data["SpawnPotentials"]; has {
		t.Errorf("packet carries SpawnPotentials, want it dropped")
	}
	if n := srv.MobCount(); n != 0 {
		t.Errorf("mob count = %d, want none", n)
	}
	servertest.UntilTicks(t, srv, 300, "condition", func() bool {
		return srv.InventorySlot("Keeper", player.HotbarSlot).Count == 1
	})
}

// TestAMonsterEggOnASpawnerInPeacefulStaysSilent: useOn reads canSpawn
// before it reads the block it is on, so a zombie egg refused in peaceful
// says nothing even on a spawner with spawner_blocks_work off.
func TestAMonsterEggOnASpawnerInPeacefulStaysSilent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Pacifist", "minecraft:zombie_spawn_egg", 1)
	srv.SetDifficulty(0)
	srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:spawner", nil))
	srv.SetGameRule("spawner_blocks_work", false)
	c.PlaceBlock(2, 64, 3, protocol.FaceTop)
	if chats := c.SystemChatsBeforeSettle(); len(chats) != 0 {
		t.Errorf("%d system chats, want the silent canSpawn refusal", len(chats))
	}
	if n := srv.MobCount(); n != 0 {
		t.Errorf("mob count = %d, want none", n)
	}
	if got := srv.InventorySlot("Pacifist", player.HotbarSlot).Count; got != 1 {
		t.Errorf("eggs left = %d, want 1", got)
	}
}

// TestASpawnEggOnATrialSpawnerLeavesItInactive: TrialSpawner.overrideEntityToSpawn
// writes the block inactive, keeping its ominous flag. The block and its
// update tag go out at the tick's flush, after the spawner's own tick has
// moved it on to waiting_for_players, so the tag is the active one. No mob
// arrives, and the egg is spent.
// vanilla: ChunkHolder.broadcastChanges, TrialSpawnerBlockEntity.getUpdateTag
func TestASpawnEggOnATrialSpawnerLeavesItInactive(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Warden", "minecraft:cow_spawn_egg", 1)
	// The egg gives the spawner a spawn entity, so the next tick moves it from
	// inactive to waiting_for_players; held, the one tick the test runs does it
	// before the flush.
	servertest.HoldTicks(t, srv)
	srv.SetGameRule("spawner_blocks_work", true)
	srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:trial_spawner",
		map[string]string{"ominous": "true", "trial_spawner_state": "cooldown"}))
	c.PlaceBlock(2, 64, 3, protocol.FaceTop)
	// The block entity data goes out with the block at the tick's flush.
	c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	servertest.RunTicks(t, srv, 1)
	_, _, _, beType, data := c.AwaitBlockEntityData()
	if beType != protocol.BlockEntityTrialSpawner {
		t.Errorf("block entity type = %d, want %d", beType, protocol.BlockEntityTrialSpawner)
	}
	spawn, _ := nbt.Get[nbt.Compound](data, "spawn_data")
	entity, _ := nbt.Get[nbt.Compound](spawn, "entity")
	if id, _ := nbt.Get[nbt.String](entity, "id"); id != "minecraft:cow" {
		t.Errorf("block entity data = %#v, want the active update tag with the cow", data)
	}
	want := servertest.MustState(t, "minecraft:trial_spawner",
		map[string]string{"ominous": "true", "trial_spawner_state": "waiting_for_players"})
	if got := srv.LoadBlockAt(2, 64, 3); got != want {
		name, props, _ := registry.BlockStateName(got)
		t.Errorf("trial spawner left as %s %s, want ominous=true,trial_spawner_state=waiting_for_players", name, props)
	}
	if n := srv.MobCount(); n != 0 {
		t.Errorf("mob count = %d, want none", n)
	}
	servertest.UntilTicks(t, srv, 300, "condition", func() bool {
		return srv.InventorySlot("Warden", player.HotbarSlot).Empty()
	})
}

// TestASpawnEggsMobSpeaksAsItArrives: EntityType.spawn plays the mob's
// ambient sound once it is in the world (Mob.playAmbientSound), the cow its
// egg makes speaking at its spawn with its kind's own quiet voice.
func TestASpawnEggsMobSpeaksAsItArrives(t *testing.T) {
	t.Parallel()
	_, c := servertest.EggPlayer(t, "survival", "Rancher", "minecraft:cow_spawn_egg", 1)
	c.PlaceBlock(2, 63, 3, protocol.FaceTop)
	heard, spoke := false, false
	for id, data := range c.Stream() {
		switch id {
		case protocol.IDPlaySpawnEntity:
			spoke = true
		case protocol.IDPlaySoundEffect:
			name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
			// Either cow voice: the finalize's dice chose the sound
			// variant, and this test is the arrival's sound, not which.
			if name != "minecraft:entity.cow.ambient" && name != "minecraft:entity.cow_moody.ambient" {
				continue
			}
			heard = true
			if category != protocol.SoundCategoryNeutral {
				t.Errorf("ambient sound category = %d, want neutral", category)
			}
			if volume != 0.4 {
				t.Errorf("ambient volume = %v, want 0.4", volume)
			}
			if pitch < 0.79 || pitch > 1.21 {
				t.Errorf("ambient pitch = %v, want the cow's adult voice", pitch)
			}
			// The spawn at (2.5, 64, 3.5), on the wire as eighths.
			if x != 20 || y != 64*8 || z != 28 {
				t.Errorf("ambient sound at (%d,%d,%d), want (20,512,28)", x, y, z)
			}
		case protocol.IDPlayAckBlockChange:
			// useItemOnTail's ack is queued after the interaction's own
			// packets, so the egg's sound must already have gone out; the
			// ordinary ambient roll the mob runs later cannot stand in
			// for it.
			if !heard {
				t.Fatal("the block ack arrived before the egg's ambient sound")
			}
		}
		if heard && spoke {
			return
		}
	}
	t.Fatalf("spawn (%v) and ambient sound (%v) never arrived together", spoke, heard)
}

// farmVariantCases are the three farm animals with the entity type their
// babies spawn as, the variant serializer their metadata uses and the warm
// variant ID the nether picks for them.
var farmVariantCases = []struct {
	kind, egg   string
	entityType  int32
	variantType int32
	warm        int32
}{
	{"minecraft:cow", "minecraft:cow_spawn_egg", protocol.EntityTypeCow, protocol.MetadataCowVariant,
		registry.MustID("minecraft:cow_variant", "minecraft:warm")},
	{"minecraft:pig", "minecraft:pig_spawn_egg", protocol.EntityTypePig, protocol.MetadataPigVariant,
		registry.MustID("minecraft:pig_variant", "minecraft:warm")},
	{"minecraft:chicken", "minecraft:chicken_spawn_egg", protocol.EntityTypeChicken, protocol.MetadataChickenVariant,
		registry.MustID("minecraft:chicken_variant", "minecraft:warm")},
}

// variantFieldOf finds the farm animal variant entry an entity's metadata
// carries, or reports false where it carries none and the client's default
// stands.
func variantFieldOf(entries []servertest.MetaEntry, variantType int32) (int32, bool) {
	for _, e := range entries {
		if e.Typ == variantType {
			return e.VarintVal, true
		}
	}
	return 0, false
}

// netherEggPlayer joins a survival player into the nether holding count of
// the named spawn egg in the first hotbar slot, standing at the arrival.
func netherEggPlayer(t *testing.T, name, egg string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.Join(t, cfg, name)
	srv.MovePlayerToDimension(name, world.NetherName, 0.5, 64, 0.5)
	// Hold the arrival chunk for the test: its send is asynchronous, and a mob
	// planted in a chunk the player does not hold yet is saved away by the
	// next mob tick and stands back up under a new ID.
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))
	c.Settle()
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, egg), Count: 2})
	return srv, c
}

// TestASpawnedFarmAnimalCarriesItsBiomeVariant: a spawn egg's adult carries
// the variant finalizeSpawn picks from the biome it lands in — warm in the
// nether — and no variant field at all where the biome is not tagged warm or
// cold, the client's own temperate default.
func TestASpawnedFarmAnimalCarriesItsBiomeVariant(t *testing.T) {
	t.Parallel()
	for _, tc := range farmVariantCases {
		t.Run("warm/"+tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := netherEggPlayer(t, "Keeper", tc.egg)
			srv.SetBlockInDimension(world.NetherName, 1, 63, 1, servertest.MustState(t, "minecraft:stone", nil))
			c.PlaceBlock(1, 63, 1, protocol.FaceTop)
			spawn := c.AwaitSpawn(tc.entityType)
			got, ok := variantFieldOf(c.AwaitMetadataFor(spawn.EntityID), tc.variantType)
			if !ok || got != tc.warm {
				t.Errorf("the nether %s's variant = %d (carried %v), want the warm %d", tc.kind, got, ok, tc.warm)
			}
		})
		t.Run("temperate/"+tc.kind, func(t *testing.T) {
			t.Parallel()
			_, c := servertest.EggPlayer(t, "survival", "Keeper", tc.egg, 2)
			c.PlaceBlock(2, 63, 3, protocol.FaceTop)
			spawn := c.AwaitSpawn(tc.entityType)
			if got, ok := variantFieldOf(c.AwaitMetadataFor(spawn.EntityID), tc.variantType); ok {
				t.Errorf("the plains %s's variant = %d, want the temperate default left out", tc.kind, got)
			}
		})
	}
}

// TestABredFarmAnimalInheritsItsParentsVariant: an egg of a farm animal's own
// kind used on a grown one gives the baby the parent's variant
// (getBreedOffspring), so a cow bred in a warm biome carries the warm variant
// in its metadata rather than the constructor's temperate default.
func TestABredFarmAnimalInheritsItsParentsVariant(t *testing.T) {
	t.Parallel()
	for _, tc := range farmVariantCases {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := netherEggPlayer(t, "Breeder", tc.egg)
			// The nether's own ground can fill the cell the parent is planted
			// in. A body standing in a wall suffocates, the chicken's low eyes
			// soonest, so stand it on a stone floor in clear air, and freeze it
			// so it cannot wander out of the egg's reach.
			srv.SetBlockInDimension(world.NetherName, 2, 63, 2, servertest.MustState(t, "minecraft:stone", nil))
			srv.SetBlockInDimension(world.NetherName, 2, 64, 2, 0)
			srv.SetBlockInDimension(world.NetherName, 2, 65, 2, 0)
			parentID := srv.SpawnMobInDimension(tc.kind, world.NetherName, 2.5, 64, 2.5)
			if !srv.SetMobNoAI(parentID) {
				t.Fatalf("the planted %s is not in the herd", tc.kind)
			}
			parent := c.AwaitSpawn(tc.entityType)
			if got, ok := variantFieldOf(c.AwaitMetadataFor(parent.EntityID), tc.variantType); !ok || got != tc.warm {
				t.Fatalf("the parent's variant = %d (carried %v), want the warm %d", got, ok, tc.warm)
			}
			c.UseOn(parent.EntityID)
			baby := c.AwaitSpawn(tc.entityType)
			entries := c.AwaitMetadataFor(baby.EntityID)
			if flag := servertest.MetaOf(entries, protocol.MetadataIndexBaby); flag == nil || !flag.FlagVal {
				t.Fatalf("the egg's offspring (id %d, parent %d) is not a baby; metadata %+v", baby.EntityID, parent.EntityID, entries)
			}
			if got, ok := variantFieldOf(entries, tc.variantType); !ok || got != tc.warm {
				t.Errorf("the bred %s's variant = %d (carried %v), want the parent's warm %d", tc.kind, got, ok, tc.warm)
			}
		})
	}
}
