package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
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
