package network

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestAnOutOfRangeHandIsReadAsTheMainHand sends ids past the end of enums
// vanilla decodes by id rather than by readEnum: none costs the connection.
// Chat mode 3 wraps to full chat, Sign Update slot 5 falls to the back, and
// Use Item hand 2 falls to the main hand, which throws the held snowball.
// vanilla: ByIdMap.continuous, ChatVisiblity.BY_ID, SignTextSlot.BY_ID,
// InteractionHand.BY_ID
func TestAnOutOfRangeHandIsReadAsTheMainHand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Player")
	servertest.HoldTicks(t, srv)

	c.Send(protocol.IDPlayClientInformation, func(b *protocol.Buffer) {
		b.PutString("en_us")
		b.PutI8(10)
		b.PutVarInt(3) // chat mode, one past hidden: wraps to full
		b.PutBool(true)
		b.PutU8(0x7f)
		b.PutVarInt(2) // main hand, one past right: the left arm
		b.PutBool(false)
		b.PutBool(true)
		b.PutVarInt(5) // particle status: wraps to minimal
	})
	if got := c.ExpectMetadata(); got.MainHand != protocol.MainHandLeft {
		t.Errorf("main hand = %d, want main hand 2 read as left", got.MainHand)
	}
	// A hidden or system-only chat would not hear a player's message.
	c.Chat("still here")
	c.ExpectChat("Player", "still here")

	c.MoveTo(4.5, 64, 8.5, 180, 0)
	servertest.GiveHeldItem(c, "minecraft:oak_sign")
	c.Settle()
	c.PlaceBlock(4, 63, 7, protocol.FaceTop)
	if _, _, _, front := c.AwaitOpenSignEditor(); !front {
		t.Fatal("the placed sign's editor is for the back")
	}
	// Flush the placement's empty block entity data before writing the text.
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	c.Send(protocol.IDPlaySignUpdateIn, func(b *protocol.Buffer) {
		b.PutPosition(4, 64, 7)
		for _, line := range []string{"Back", "of", "the", "sign"} {
			b.PutString(line)
		}
		b.PutVarInt(5) // past front (1): the back
	})
	_, _, _, _, data := c.AwaitBlockEntityData()
	if got := signMessages(t, data, "back_text"); got != [4]string{"Back", "of", "the", "sign"} {
		t.Errorf("back text = %q, want the update's lines", got)
	}
	if got := signMessages(t, data, "front_text"); got != [4]string{} {
		t.Errorf("front text = %q, want it empty", got)
	}

	servertest.GiveHeldItem(c, "minecraft:snowball")
	c.Settle()
	c.UseItemRightClicks(2)
	if !servertest.ASpawnOfWithinTicks(c, srv, 20, protocol.EntityTypeSnowball) {
		t.Fatal("use item with hand 2 threw no snowball")
	}
	if srv.PlayerCount() != 1 {
		t.Errorf("player count = %d, want the sender still connected", srv.PlayerCount())
	}
}

// TestAnInteractWithHand2UsesTheMainHand: Interact with hand 2 uses the
// main hand's name tag on a pig, which then carries the tag's name.
// vanilla: InteractionHand.BY_ID
func TestAnInteractWithHand2UsesTheMainHand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Namer")
	srv.SpawnMob("minecraft:pig", 1.5, 64, 2.5)
	pig := c.ExpectMobSpawn(protocol.EntityTypePig)
	// Held, so a slow run never reaches the tab list's latency broadcast.
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Namer", player.HotbarSlot, protocol.ItemStack{
		ItemID: registry.MustItem("minecraft:name_tag"), Count: 1,
		CustomName: &protocol.Text{Text: "Wilbur"},
	})
	c.InteractAt(pig.EntityID, 2, 0.1, 0.9, 0.2)
	var meta []byte
	if !c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata || protocol.NewReader(data).ReadVarInt() != pig.EntityID {
			return false
		}
		meta = data
		return true
	}) {
		t.Fatal("no pig metadata within 20 ticks of an Interact with hand 2")
	}
	named := pig
	c.ReadMobMetadataInto(&named, meta)
	if !reflect.DeepEqual(named.Name, nbt.String("Wilbur")) {
		t.Errorf("the pig is named %v after an Interact with hand 2, want Wilbur", named.Name)
	}
	if srv.PlayerCount() != 1 {
		t.Errorf("player count = %d, want the sender still connected", srv.PlayerCount())
	}
}

// signMessages reads one side's four plain lines out of a sign's Block
// Entity Data NBT.
func signMessages(t *testing.T, data nbt.Compound, side string) (lines [4]string) {
	t.Helper()
	compound, ok := nbt.Get[nbt.Compound](data, side)
	if !ok {
		t.Fatalf("block entity NBT has no %s: %#v", side, data)
	}
	list, ok := nbt.Get[nbt.List](compound, "messages")
	if !ok || len(list.Items) != 4 {
		t.Fatalf("%s.messages = %#v, want four lines", side, compound["messages"])
	}
	for i, item := range list.Items {
		line, _ := item.(nbt.String)
		lines[i] = string(line)
	}
	return lines
}
