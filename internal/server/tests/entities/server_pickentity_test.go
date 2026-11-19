package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestMiddleClickingAnEntityPicksItsItem sends Pick Item From Entity at each
// kind of body in reach and reads the item that lands in a creative hand.
// vanilla: Entity.getPickResult
func TestMiddleClickingAnEntityPicksItsItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "creative", "Picker", "minecraft:pig_spawn_egg", 1)
	bodies := []struct {
		name, item string
		id         int32
	}{
		{"a cow", "minecraft:cow_spawn_egg", srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)},
		{"an armor stand", "minecraft:armor_stand", srv.SpawnArmorStand(2.5, 64, 1.5)},
		{"a birch boat", "minecraft:birch_boat", srv.SpawnBoat(2.5, 64, 2.5, "minecraft:birch_boat")},
		{"a minecart", "minecraft:minecart", srv.SpawnMinecart(0.5, 64, 2.5)},
		{"an item frame", "minecraft:item_frame", srv.SpawnFrameEntity(1, 64, 3).ID},
		{"a glow item frame", "minecraft:glow_item_frame", srv.SpawnGlowFrameEntity(2, 64, 3).ID},
		{"an end crystal", "minecraft:end_crystal", srv.SpawnEndCrystal(-1.5, 64, 0.5)},
		{"a leash knot", "minecraft:lead", srv.SpawnKnotEntity(-1, 64, 1).ID},
		{"a painting", "minecraft:painting", srv.SpawnPaintingEntity(1, 66, 3, 0).ID},
	}
	c.Settle()
	for _, b := range bodies {
		if b.id == 0 {
			t.Fatalf("%s did not spawn", b.name)
		}
		got := servertest.PickedEntityStack(t, c, b.id)
		if got.ItemID != servertest.MustItemID(t, b.item) || got.Count != 1 {
			t.Errorf("%s picked item %d x%d, want %s", b.name, got.ItemID, got.Count, b.item)
		}
	}
}

// TestAPickOutOfReachGivesNothing sends a pick at a cow far away, then one at
// a sheep beside the player: the first answer is the sheep's egg.
// vanilla: ServerGamePacketListenerImpl.handlePickItemFromEntity
func TestAPickOutOfReachGivesNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "creative", "Picker", "minecraft:pig_spawn_egg", 1)
	far := srv.SpawnMob("minecraft:cow", 40.5, 64, 0.5)
	near := srv.SpawnMob("minecraft:sheep", 2.5, 64, 0.5)
	c.Settle()
	c.PickEntity(far)
	got := servertest.PickedEntityStack(t, c, near)
	if got.ItemID != servertest.MustItemID(t, "minecraft:sheep_spawn_egg") {
		t.Errorf("picked item %d, want the sheep egg: the cow out of reach answered", got.ItemID)
	}
}
