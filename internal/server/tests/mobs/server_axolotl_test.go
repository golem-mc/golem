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

// TestCreativeAxolotlFeedingAddsOneWaterBucket: a creative player keeps the
// fish bucket and gets a water bucket only while the inventory holds none.
// vanilla: Axolotl.usePlayerItem, ItemUtils.createFilledResult
func TestCreativeAxolotlFeedingAddsOneWaterBucket(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	c.ChatCommand("gamemode creative")
	c.Settle()
	kind := fishType(t, "minecraft:axolotl")
	first := summonSquid(t, c, "minecraft:axolotl", kind, pondCentreX-1, pondNorth)
	second := summonSquid(t, c, "minecraft:axolotl", kind, pondCentreX+1, pondNorth)
	hotbarFish(t, srv, "minecraft:tropical_fish_bucket")
	for _, id := range []int32{first, second} {
		c.UseOn(id)
		c.Settle()
	}
	water := servertest.MustItemID(t, "minecraft:water_bucket")
	buckets := 0
	for slot := 0; slot <= player.OffHandSlot; slot++ {
		if s := srv.InventorySlot("Striker", slot); s.ItemID == water {
			buckets += int(s.Count)
		}
	}
	if buckets != 1 {
		t.Errorf("two creative feedings left %d water buckets, want 1", buckets)
	}
	if held := srv.InventorySlot("Striker", player.HotbarSlot); held.ItemID != servertest.MustItemID(t, "minecraft:tropical_fish_bucket") {
		t.Errorf("hand=%+v, want the tropical fish bucket a creative feeding keeps", held)
	}
}

func TestAxolotlSupportsItsTargetsKiller(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	const name = "Striker"
	axolotl := srv.SpawnMob("minecraft:axolotl", pondCentreX-2, 62, pondNorth)
	c.ExpectMobSpawn(fishType(t, "minecraft:axolotl"))
	prey := summonSquidTagged(t, c, "minecraft:cod", fishType(t, "minecraft:cod"), pondCentreX, pondNorth, `{NoAI:1b}`)
	// Acquire a water target before the player intervenes, with enough space
	// that it cannot be eaten while we wait for the sensor.
	for range 40 {
		srv.TickMobs(1)
		if srv.MobTargetID(axolotl) == prey {
			break
		}
	}
	if srv.MobTargetID(axolotl) != prey {
		t.Fatal("axolotl did not target cod")
	}
	srv.GiveEffect(name, "minecraft:mining_fatigue", 6000, 2, true)
	hotbarFish(t, srv, "minecraft:netherite_sword")
	srv.SetMobHealth(prey, 1)
	// Wind the player's swing without moving the pond's inhabitants.
	for range 20 {
		srv.TickSurvival()
	}
	c.Attack(prey)
	c.Settle()
	if hp, _ := srv.MobHealth(prey); hp > 0 {
		t.Fatalf("cod survived final blow: %v", hp)
	}
	for range 4 {
		srv.TickMobs(1)
	}
	if duration, amp, ok := srv.PlayerEffect(name, "minecraft:regeneration"); !ok || duration != 100 || amp != 0 {
		t.Fatalf("support regeneration=%d/%d (%v)", duration, amp, ok)
	}
	if _, _, ok := srv.PlayerEffect(name, "minecraft:mining_fatigue"); ok {
		t.Fatal("support retained mining fatigue")
	}
	if !srv.AdvancementObtained(name, "minecraft:husbandry/kill_axolotl_target", "kill_axolotl_target") {
		t.Fatal("support did not obtain The Healing Power of Friendship")
	}
}
