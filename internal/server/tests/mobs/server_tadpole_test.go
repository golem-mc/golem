package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestTadpoleBucketKeepsAgeLockAndHealth(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := summonSquidTagged(t, c, "minecraft:tadpole", fishType(t, "minecraft:tadpole"), pondCentreX, pondNorth, `{Age:1234,AgeLocked:1b,Health:4f}`)
	hotbarFish(t, srv, "minecraft:water_bucket")
	c.UseOn(id)
	c.Settle()
	srv.TickMobs(1)
	bucket := srv.InventorySlot("Striker", player.HotbarSlot)
	if bucket.ItemID != servertest.MustItemID(t, "minecraft:tadpole_bucket") {
		t.Fatalf("held item=%d", bucket.ItemID)
	}
	if _, ok := fishAt(srv, id); ok {
		t.Fatal("bucketed tadpole still exists")
	}
	c.MoveTo(playerX, 64, playerZ, 180, 60)
	c.Settle()
	servertest.UseHeld(c, 180, 60)
	c.Settle()
	srv.TickMobs(1)
	var released int32
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:tadpole" {
			released = m.ID
		}
	}
	if released == 0 {
		t.Fatal("bucket did not release tadpole")
	}
	if hp, _ := srv.MobHealth(released); hp != 4 {
		t.Fatalf("released health=%v", hp)
	}
	hotbarFish(t, srv, "minecraft:water_bucket")
	c.UseOn(released)
	c.Settle()
	tag := srv.InventorySlot("Striker", player.HotbarSlot).BucketEntityData()
	if age, ok := nbt.IntField(tag, "Age"); !ok || age != 1234 || !nbt.BoolField(tag, "AgeLocked") {
		t.Fatalf("released tadpole lost age: %v", tag)
	}
}

func TestTadpoleGrowsIntoFrog(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := summonSquidTagged(t, c, "minecraft:tadpole", fishType(t, "minecraft:tadpole"), pondCentreX, pondNorth, `{Age:23999,NoAI:1b,Silent:1b}`)
	srv.TickMobs(1)
	c.ExpectMobSpawn(fishType(t, "minecraft:frog"))
	if _, ok := fishAt(srv, id); ok {
		t.Fatal("grown tadpole still exists")
	}
	var frog int32
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:frog" {
			frog = m.ID
		}
	}
	if hp, _ := srv.MobHealth(frog); frog == 0 || hp != 10 {
		t.Fatalf("frog=%d health=%v", frog, hp)
	}
	// A second tick must leave one frog, not repeat the conversion.
	srv.TickMobs(1)
	if mobs := srv.Mobs(); len(mobs) != 1 || mobs[0].Kind != "minecraft:frog" {
		t.Fatalf("after conversion: %+v", mobs)
	}
}

func ageLockedIn(t *testing.T, c *servertest.Client, id int32) bool {
	t.Helper()
	for _, e := range c.AwaitMetadataFor(id) {
		if e.Index == protocol.MetadataIndexAgeLocked {
			return e.FlagVal
		}
	}
	t.Fatal("metadata carries no age lock")
	return false
}

func TestGoldenDandelionAgeLocksABabyCow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Milker", servertest.OpsListing(t, "Milker"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()
	dandelion := servertest.ItemIDOrDie(t, "minecraft:golden_dandelion")
	give := func() { srv.GiveSlot("Milker", player.HotbarSlot, protocol.ItemStack{ItemID: dandelion, Count: 2}) }
	give()
	c.Settle()
	c.ChatCommand(`summon minecraft:cow 5.5 64 5.5 {Age:-24000,NoAI:1b}`)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow).EntityID
	c.UseOn(cow)
	if !ageLockedIn(t, c, cow) {
		t.Fatal("clicked calf is not locked")
	}
	if held := srv.InventorySlot("Milker", player.HotbarSlot); held.Count != 1 {
		t.Fatalf("held count=%d, want 1", held.Count)
	}
	uuid, ok := srv.MobUUID(cow)
	if !ok {
		t.Fatal("no uuid for calf")
	}
	c.ChatCommand("data merge entity " + uuid + " {Age:-1}")
	c.Settle()
	srv.TickMobs(5)
	if m, _ := mobWithID(srv, cow); !m.Baby {
		t.Fatal("locked calf grew up")
	}
	srv.TickMobs(40)
	c.Settle()
	c.UseOn(cow)
	if ageLockedIn(t, c, cow) {
		t.Fatal("second click did not unlock")
	}
}
