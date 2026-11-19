package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestZombieNautilusSpawnsWithReadableVariant(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Reader", servertest.OpsListing(t, "Reader"))
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:zombie_nautilus", 3.5, 64, 0.5)
	typeID, _ := registry.EntityTypeID("minecraft:zombie_nautilus")
	got := c.ExpectMobSpawn(int32(typeID))
	if got.EntityID != id || got.Health != 15 {
		t.Fatalf("spawn = %+v, want entity %d with 15 health", got, id)
	}
	tag := entityCompound(t, c, "@e[type=minecraft:zombie_nautilus,limit=1]")
	if variant, _ := nbt.Get[nbt.String](tag, "variant"); variant != "minecraft:temperate" {
		t.Fatalf("saved variant = %v", tag["variant"])
	}
	c.ChatCommand("data merge entity @e[type=minecraft:zombie_nautilus,limit=1] {variant:\"minecraft:warm\"}")
	c.SettleAndCollect(func(packet int32, data []byte) {
		if packet == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == id {
			c.ReadMobMetadataInto(&got, data)
		}
	})
	variants, _ := registry.Get("minecraft:zombie_nautilus_variant")
	if got.Variant != variants.ID("minecraft:warm") {
		t.Fatalf("variant metadata = %d", got.Variant)
	}
	tag = entityCompound(t, c, "@e[type=minecraft:zombie_nautilus,limit=1]")
	if variant, _ := nbt.Get[nbt.String](tag, "variant"); variant != "minecraft:warm" {
		t.Fatalf("merged variant = %v", tag["variant"])
	}
}

func TestNaturalTridentDrownedRollsZombieNautilusJockey(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	// Both seeds produce adult trident holders; their jockey rolls straddle 0.5.
	for _, tc := range []struct {
		seed  int64
		rides bool
	}{{41, true}, {46, false}} {
		id := srv.SpawnMobSeeded("minecraft:drowned", 5.5, 64, 0.5, tc.seed, true)
		if hand := srv.MobHand(id); registry.Item(hand.ItemID) != registry.ItemTrident {
			t.Fatalf("seed %d: hand = %+v", tc.seed, hand)
		}
		drownedType, _ := registry.EntityTypeID("minecraft:drowned")
		if got := c.ExpectMobSpawn(int32(drownedType)); got.EntityID != id {
			t.Fatalf("drowned spawn = %d, want %d", got.EntityID, id)
		}
		vehicle := srv.MobVehicleID(id)
		if (vehicle != 0) != tc.rides {
			t.Fatalf("seed %d: vehicle = %d, rides want %v", tc.seed, vehicle, tc.rides)
		}
		if !tc.rides {
			continue
		}
		typeID, _ := registry.EntityTypeID("minecraft:zombie_nautilus")
		if got := c.ExpectMobSpawn(int32(typeID)); got.EntityID != vehicle {
			t.Fatalf("vehicle spawn = %d, want %d", got.EntityID, vehicle)
		}
		linked := false
		c.SettleAndCollect(func(packet int32, data []byte) {
			if packet == protocol.IDPlaySetPassengers {
				v, riders := servertest.ReadPassengers(c, data)
				if v == vehicle && len(riders) == 1 && riders[0] == id {
					linked = true
				}
			}
		})
		if !linked {
			t.Fatal("missing vehicle's Set Passengers link")
		}
	}
	// The same winning seed is never eligible when summoned.
	if id := srv.SpawnMobSeeded("minecraft:drowned", 7.5, 64, 0.5, 41); srv.MobVehicleID(id) != 0 {
		t.Fatal("summoned drowned rolled a nautilus")
	}
}
