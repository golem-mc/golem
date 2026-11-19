package blocks

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The player head's profile: the minecraft:profile component names the skin a
// head shows, and the block entity's loot table copies it back onto the item
// the head drops. vanilla: SkullBlockEntity.collectImplicitComponents,
// the player_head loot table's copy_components.

// profileValue builds the wire value a stack's minecraft:profile component
// carries: the complete profile shape, the UUID and name, no property list,
// and an empty skin patch.
// vanilla: ResolvableProfile.STREAM_CODEC
func profileValue(t *testing.T, id uuid.UUID, name string) []byte {
	t.Helper()
	var value protocol.Buffer
	value.PutBool(true)
	value.PutUUID(id)
	value.PutString(name)
	value.PutVarInt(0)   // properties
	value.PutBool(false) // body texture
	value.PutBool(false) // cape texture
	value.PutBool(false) // elytra texture
	value.PutBool(false) // slim model
	return value.Bytes()
}

// headWithProfile is a player head stack carrying the profile of the named
// player, the stack a client sends for a skinned head.
func headWithProfile(t *testing.T, id uuid.UUID, name string) protocol.ItemStack {
	t.Helper()
	itemID, ok := registry.ItemID("minecraft:player_head")
	if !ok {
		t.Fatal("the registry has no minecraft:player_head")
	}
	return protocol.ItemStack{ItemID: itemID, Count: 1,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentProfile, Data: profileValue(t, id, name)},
		}}
}

// profileOf returns the profile component bytes the stack carries, nil for a
// stack without one.
func profileOf(stack protocol.ItemStack) []byte {
	for _, raw := range stack.Raw {
		if raw.Type == protocol.DataComponentProfile {
			return raw.Data
		}
	}
	return nil
}

// TestBreakingAPlayerHeadDropsItWithItsProfile places a player head carrying
// a profile and breaks it in survival: the dropped item's components carry
// the same profile, so the skin is not lost with the block.
func TestBreakingAPlayerHeadDropsItWithItsProfile(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Miner")

	const x, y, z = int32(2), int32(64), int32(2)
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	head := headWithProfile(t, id, "Notch")
	srv.GiveSlot("Miner", player.HotbarSlot, head)
	c.MoveStep(0.5, 64, 0.5, true)
	c.PlaceBlock(x, y-1, z, protocol.FaceTop)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("acknowledged sequence = %d, want 1", seq)
	}
	c.SettleAndCollect(func(int32, []byte) {})

	c.BreakBlock(x, y, z)
	_, drop := c.WaitBreakSettled()
	if got := profileOf(drop.Stack); !bytes.Equal(got, profileValue(t, id, "Notch")) {
		t.Errorf("the dropped head's profile = %x, want the placed head's %x",
			got, profileValue(t, id, "Notch"))
	}
}

// TestAPlacedPlayerHeadKeepsItsProfile places a head, breaks it, and places
// the drop again: the second break still drops the profile, so placement
// stored what the first break carried away.
func TestAPlacedPlayerHeadKeepsItsProfile(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Miner")

	id := uuid.MustParse("66666666-7777-8888-9999-000000000000")
	head := headWithProfile(t, id, "Notch")
	srv.GiveSlot("Miner", player.HotbarSlot, head)
	c.MoveStep(0.5, 64, 0.5, true)
	c.PlaceBlock(2, 63, 2, protocol.FaceTop)
	c.ExpectBlockAck()
	c.SettleAndCollect(func(int32, []byte) {})

	c.BreakBlock(2, 64, 2)
	_, drop := c.WaitBreakSettled()
	if profileOf(drop.Stack) == nil {
		t.Fatal("the first break dropped a head with no profile")
	}

	// Put the dropped head back in hand and place it in the same hole.
	srv.GiveSlot("Miner", player.HotbarSlot, drop.Stack)
	c.PlaceBlock(2, 63, 2, protocol.FaceTop)
	c.ExpectBlockAck()
	c.SettleAndCollect(func(int32, []byte) {})

	c.BreakBlock(2, 64, 2)
	_, again := c.WaitBreakSettled()
	if got := profileOf(again.Stack); !bytes.Equal(got, profileOf(drop.Stack)) {
		t.Errorf("the re-placed head's profile = %x, want the placed head's %x",
			got, profileOf(drop.Stack))
	}
}
