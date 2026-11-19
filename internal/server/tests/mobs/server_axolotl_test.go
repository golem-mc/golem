package mobs

import (
	"github.com/golem-mc/golem/nbt"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestAxolotlBucketKeepsVariantAgeAndHealth(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	kind := fishType(t, "minecraft:axolotl")
	id := summonSquidTagged(t, c, "minecraft:axolotl", kind, pondCentreX, pondNorth, `{Variant:4,Age:-1200,AgeLocked:1b,Health:8f}`)
	hotbarFish(t, srv, "minecraft:water_bucket")
	c.UseOn(id)
	c.Settle()
	srv.TickMobs(1)
	bucket := srv.InventorySlot("Striker", player.HotbarSlot)
	if bucket.ItemID != servertest.MustItemID(t, "minecraft:axolotl_bucket") {
		t.Fatalf("held item=%d, want axolotl bucket", bucket.ItemID)
	}
	if v, ok := bucket.AxolotlVariant(); !ok || v != 4 {
		t.Fatalf("bucket variant=%d (%v)", v, ok)
	}
	if _, ok := fishAt(srv, id); ok {
		t.Fatal("bucketed axolotl still exists")
	}
	c.MoveTo(playerX, 64, playerZ, 180, 60)
	c.Settle()
	servertest.UseHeld(c, 180, 60)
	c.Settle()
	srv.TickMobs(1)
	var released int32
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:axolotl" {
			released = m.ID
		}
	}
	if released == 0 {
		t.Fatal("bucket did not release axolotl")
	}
	if hp, _ := srv.MobHealth(released); hp != 8 {
		t.Fatalf("released health=%v", hp)
	}
	// Scoop it again to read the released body's bucket data through the public protocol.
	hotbarFish(t, srv, "minecraft:water_bucket")
	c.UseOn(released)
	c.Settle()
	bucket = srv.InventorySlot("Striker", player.HotbarSlot)
	if v, ok := bucket.AxolotlVariant(); !ok || v != 4 {
		t.Fatalf("released variant=%d (%v)", v, ok)
	}
	tag := bucket.BucketEntityData()
	if age, ok := nbt.IntField(tag, "Age"); !ok || age != -1200 || !nbt.BoolField(tag, "AgeLocked") {
		t.Fatalf("released axolotl lost age: %v", tag)
	}
}

func TestAxolotlFoodReturnsWaterBucket(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := summonSquid(t, c, "minecraft:axolotl", fishType(t, "minecraft:axolotl"), pondCentreX, pondNorth)
	hotbarFish(t, srv, "minecraft:tropical_fish_bucket")
	c.UseOn(id)
	c.Settle()
	if held := srv.InventorySlot("Striker", player.HotbarSlot); held.ItemID != servertest.MustItemID(t, "minecraft:water_bucket") || held.Count != 1 {
		t.Fatalf("fed hand=%+v, want water bucket", held)
	}
}
