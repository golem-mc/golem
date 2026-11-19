package blocks

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
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

// noteBlockSoundOf returns the identifier the stack's note_block_sound
// component carries, "" for a stack without one.
func noteBlockSoundOf(t *testing.T, stack protocol.ItemStack) string {
	t.Helper()
	for _, raw := range stack.Raw {
		if raw.Type == protocol.DataComponentNoteBlockSound {
			return protocol.NewReader(raw.Data).ReadString(32767)
		}
	}
	return ""
}

// TestABrokenHeadKeepsItsNoteBlockSound sets a head the vanilla way, its note
// block sound as the block entity tag, and breaks it: the loot table's
// copy_components reads that tag back onto the drop.
// vanilla: SkullBlockEntity.collectImplicitComponents
func TestABrokenHeadKeepsItsNoteBlockSound(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.SurvivalConfig(), "Miner", servertest.OpsListing(t, "Miner"))

	const x, y, z = int32(4), int32(64), int32(4)
	servertest.Setblock(t, c, x, y, z, `minecraft:player_head{note_block_sound:"minecraft:block.note_block.pling"}`)
	c.Settle()

	c.MoveStep(3.5, 64, 3.5, true)
	c.BreakBlock(x, y, z)
	_, drop := c.WaitBreakSettled()
	if got := noteBlockSoundOf(t, drop.Stack); got != "minecraft:block.note_block.pling" {
		t.Errorf("the dropped head's note block sound = %q, want minecraft:block.note_block.pling", got)
	}
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

// TestAVanillaFormatProfileOnAPlacedHeadSurvivesTheBreak sets a head the
// vanilla way, with its profile compound as the block entity tag, and checks
// the break reads that compound: the block entity keeps the profile under
// "profile", and the drop carries the same name-only profile back.
// vanilla: SkullBlockEntity.collectImplicitComponents
func TestAVanillaFormatProfileOnAPlacedHeadSurvivesTheBreak(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Miner", servertest.OpsListing(t, "Miner"))

	const x, y, z = int32(4), int32(64), int32(4)
	servertest.Setblock(t, c, x, y, z, `minecraft:player_head{profile:{name:"Notch"}}`)

	tag, ok := srv.BlockEntityTagAt(x, y, z)
	if !ok {
		t.Fatal("the set head kept no block entity")
	}
	if _, has := tag["profile"]; !has {
		t.Errorf("the head's block entity = %v, want a profile under \"profile\"", tag)
	}
	if _, stale := tag["golem:profile"]; stale {
		t.Error("the head kept the private profile key instead of the vanilla one")
	}
	c.Settle()

	c.MoveStep(3.5, 64, 3.5, true)
	c.BreakBlock(x, y, z)
	_, drop := c.WaitBreakSettled()
	if got := profileOf(drop.Stack); !bytes.Equal(got, servertest.PartialProfileValue("Notch")) {
		t.Errorf("the dropped head's profile = %x, want %x", got, servertest.PartialProfileValue("Notch"))
	}
}

// TestANamedMobHeadDropsItsName names a mob head with a custom name and
// breaks it: the name survives on the drop, the loot table's copy_components
// reading it back from the block entity the placement kept it in.
func TestANamedMobHeadDropsItsName(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Miner")

	name := protocol.Text{Text: "Bob"}
	itemID, ok := registry.ItemID("minecraft:zombie_head")
	if !ok {
		t.Fatal("the registry has no minecraft:zombie_head")
	}
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{
		ItemID: itemID, Count: 1, CustomName: &name,
	})
	c.MoveStep(0.5, 64, 0.5, true)
	c.PlaceBlock(2, 63, 2, protocol.FaceTop)
	c.ExpectBlockAck()
	c.SettleAndCollect(func(int32, []byte) {})

	tag, ok := srv.BlockEntityTagAt(2, 64, 2)
	if !ok {
		t.Fatal("the placed head kept no block entity")
	}
	if _, has := tag["custom_name"]; !has {
		t.Errorf("the head's block entity = %v, want the name under \"custom_name\"", tag)
	}
	if _, stale := tag["CustomName"]; stale {
		t.Error("the head stored its name under \"CustomName\" instead of the skull's \"custom_name\"")
	}

	c.BreakBlock(2, 64, 2)
	_, drop := c.WaitBreakSettled()
	if drop.Stack.CustomName == nil || drop.Stack.CustomName.Text != "Bob" {
		t.Errorf("the dropped head's name = %v, want Bob", drop.Stack.CustomName)
	}
}

// TestAProfileSurvivesABlast places a head carrying a profile and blows it
// up: the block the blast destroys still drops the profile, the break reading
// the carried compound before the removal takes it away.
func TestAProfileSurvivesABlast(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Miner")

	const x, y, z = int32(8), int32(64), int32(8)
	id := uuid.MustParse("12121212-3434-5656-7878-909090909090")
	srv.GiveSlot("Miner", player.HotbarSlot, headWithProfile(t, id, "Notch"))
	c.MoveStep(7.5, 64, 7.5, true)
	c.PlaceBlock(x, y-1, z, protocol.FaceTop)
	c.ExpectBlockAck()
	c.SettleAndCollect(func(int32, []byte) {})

	// Step clear so the drop is out of reach and stays on the ground.
	c.MoveStep(0.5, 64, 0.5, true)
	srv.SpawnTNT(10.5, 64, 8.5, 5)

	var drop protocol.ItemStack
	servertest.WaitFor(t, srv, 200, func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:player_head" {
				drop = it.Stack
				return true
			}
		}
		return false
	})
	if got := profileOf(drop); !bytes.Equal(got, profileValue(t, id, "Notch")) {
		t.Errorf("the blasted head's profile = %x, want the placed head's %x",
			got, profileValue(t, id, "Notch"))
	}
}

// TestAPistonPushDestroysAHeadWithItsProfile pushes a stone block into a
// player head the piston destroys on push: the destruction the engine brings
// about drops the profile the head kept.
func TestAPistonPushDestroysAHeadWithItsProfile(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Miner", servertest.OpsListing(t, "Miner"))

	servertest.Setblock(t, c, 2, 64, 0, "minecraft:piston[facing=east]")
	servertest.Setblock(t, c, 3, 64, 0, "minecraft:stone")
	servertest.Setblock(t, c, 4, 64, 0, `minecraft:player_head{profile:{name:"Notch"}}`)

	// Step clear so the drop is out of reach and stays on the ground, then
	// power the piston from behind.
	c.MoveStep(0.5, 64, -6.5, true)
	servertest.Setblock(t, c, 2, 64, 1, "minecraft:redstone_block")

	var drop protocol.ItemStack
	servertest.WaitFor(t, srv, 200, func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:player_head" {
				drop = it.Stack
				return true
			}
		}
		return false
	})
	if got := profileOf(drop); !bytes.Equal(got, servertest.PartialProfileValue("Notch")) {
		t.Errorf("the pushed head's profile = %x, want %x", got, servertest.PartialProfileValue("Notch"))
	}
}

// TestABrokenWallHeadDropsItsProfile sets a wall head with a profile and
// breaks it: the wall variant keeps its carried block entity the same way.
func TestABrokenWallHeadDropsItsProfile(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.SurvivalConfig(), "Miner", servertest.OpsListing(t, "Miner"))

	const x, y, z = int32(4), int32(64), int32(2)
	servertest.Setblock(t, c, x, y, z, `minecraft:player_wall_head[facing=north]{profile:{name:"Notch"}}`)
	c.Settle()

	c.MoveStep(3.5, 64, 1.5, true)
	c.BreakBlock(x, y, z)
	_, drop := c.WaitBreakSettled()
	if got := profileOf(drop.Stack); !bytes.Equal(got, servertest.PartialProfileValue("Notch")) {
		t.Errorf("the dropped wall head's profile = %x, want %x", got, servertest.PartialProfileValue("Notch"))
	}
}
