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
