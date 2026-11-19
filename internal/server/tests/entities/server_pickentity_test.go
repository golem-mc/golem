package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"

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

// TestMiddleClickingAFilledFrameCopiesItsItem picks a frame that holds a
// diamond and reads the diamond, not the frame.
// vanilla: ItemFrame.getPickResult
func TestMiddleClickingAFilledFrameCopiesItsItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "creative", "Picker", "minecraft:pig_spawn_egg", 1)
	diamond := servertest.MustItemID(t, "minecraft:diamond")
	frame := srv.SpawnFilledFrameEntity(1, 64, 3, protocol.ItemStack{ItemID: diamond, Count: 1})
	c.Settle()
	got := servertest.PickedEntityStack(t, c, frame.ID)
	if got.ItemID != diamond || got.Count != 1 {
		t.Errorf("picked item %d x%d, want one diamond", got.ItemID, got.Count)
	}
}

// TestASurvivalPickOnlySelectsAnExistingStack picks a cow while a cow egg
// waits in the second hotbar slot, then a sheep with no egg to find: the
// first moves the selection, the second leaves it.
// vanilla: ServerGamePacketListenerImpl.tryPickItem
func TestASurvivalPickOnlySelectsAnExistingStack(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Picker", "minecraft:pig_spawn_egg", 1)
	srv.GiveSlot("Picker", player.HotbarSlot+1, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cow_spawn_egg"), Count: 1})
	cow := srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	sheep := srv.SpawnMob("minecraft:sheep", 2.5, 64, 1.5)
	c.Settle()
	selected := func(entity int32) int32 {
		c.PickEntity(entity)
		return protocol.NewReader(c.ExpectPlay(protocol.IDPlayHeldItem)).ReadVarInt()
	}
	if got := selected(cow); got != 1 {
		t.Errorf("selected slot %d after picking a cow, want 1", got)
	}
	if got := selected(sheep); got != 1 {
		t.Errorf("selected slot %d after picking a sheep, want 1: survival conjured a stack", got)
	}
}

// TestCtrlPickOnAMannequinPrintsItsProfile sends an include-data pick at a
// mannequin as a creative operator, and as a survival one, who gets nothing.
// vanilla: ServerGamePacketListenerImpl.handlePickItemFromEntity
func TestCtrlPickOnAMannequinPrintsItsProfile(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Picker")
	c.ChatCommand("summon minecraft:mannequin ~ ~ ~")
	id := awaitMannequin(t, c, srv).id

	c.ChatCommand("gamemode survival")
	servertest.RunTicks(t, srv, 2)
	c.PickEntityWithData(id)
	if got := c.SawSystemChatWithin(srv, 3, "commands.fetchprofile.entity.success"); got {
		t.Error("a survival operator was sent the mannequin's profile")
	}

	c.ChatCommand("gamemode creative")
	servertest.RunTicks(t, srv, 2)
	c.PickEntityWithData(id)
	if !c.SawSystemChatWithin(srv, 3, "commands.fetchprofile.entity.success") {
		t.Error("a creative operator's include-data pick on a mannequin sent no profile")
	}
}
