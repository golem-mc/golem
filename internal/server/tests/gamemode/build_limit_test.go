package gamemode

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

func buildLimitLine(key, limit string) []byte {
	var want protocol.Buffer
	want.PutText(protocol.Text{
		Translate: key,
		With:      []protocol.Text{{Text: limit}},
		Color:     protocol.ColorRed,
	})
	want.PutBool(true) // over the hotbar
	return want.Bytes()
}

// TestARefusedBlockAtTheBuildLimitSendsTheActionBarLine checks a block item
// clicked against the top face at the world's top and the bottom face at its
// floor: nothing is placed and the placer hears build.tooHigh or build.tooLow.
// vanilla: ServerGamePacketListenerImpl.handleUseItemOn
func TestARefusedBlockAtTheBuildLimitSendsTheActionBarLine(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	stoneState := registry.BlockStates["minecraft:stone"]

	for _, tc := range []struct {
		desc       string
		item       string
		adventure  bool
		y, face    int32
		key, limit string
	}{
		{"top", "minecraft:stone", false, 319, protocol.FaceTop, "build.tooHigh", "319"},
		{"bottom", "minecraft:stone", false, -64, protocol.FaceBottom, "build.tooLow", "-64"},
		{"lily pad", "minecraft:lily_pad", false, 319, protocol.FaceTop, "build.tooHigh", "319"},
		{"adventure", "minecraft:stone", true, 319, protocol.FaceTop, "build.tooHigh", "319"},
	} {
		if tc.adventure {
			c.ChatCommand("gamemode adventure Builder")
			servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.PlayerGameMode("Builder") == protocol.GameModeAdventure })
			c.Settle()
		}
		id, _ := registry.ItemID(tc.item)
		c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})
		servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.SetBlock(6, tc.y, 8, stoneState) || srv.BlockAt(6, tc.y, 8) == stoneState })
		c.FacePlayer(6.5, float64(tc.y), 10.5, 0, 0)
		c.PlaceBlock(6, tc.y, 8, tc.face)
		data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySystemChat)
		if !ok || !bytes.Equal(data, buildLimitLine(tc.key, tc.limit)) {
			t.Errorf("%s: line = % X (heard %v), want % X", tc.desc, data, ok, buildLimitLine(tc.key, tc.limit))
		}
	}
}

// TestAClickOutsideTheWorldHeightSendsTheLimitLine checks a click on a block
// above the top: refused with the line and no block-update echoes.
// vanilla: ServerGamePacketListenerImpl.handleUseItemOn
func TestAClickOutsideTheWorldHeightSendsTheLimitLine(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")

	stone, _ := registry.ItemID("minecraft:stone")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	c.FacePlayer(6.5, 319, 10.5, 0, 0)
	c.PlaceBlock(6, 320, 8, protocol.FaceTop)
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySystemChat)
	if !ok || !bytes.Equal(data, buildLimitLine("build.tooHigh", "319")) {
		t.Errorf("line = % X (heard %v), want % X", data, ok, buildLimitLine("build.tooHigh", "319"))
	}
	c.Settle()
}

// TestAUsedBlockAtTheBuildLimitSendsNoLimitLine checks a block item clicked on
// the top of a crafting table at the world's top: the table's menu consumes
// the click, so the line stays unsent.
// vanilla: ServerGamePacketListenerImpl.handleUseItemOn
func TestAUsedBlockAtTheBuildLimitSendsNoLimitLine(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")

	stone, _ := registry.ItemID("minecraft:stone")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	table := registry.BlockStates["minecraft:crafting_table"]
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.SetBlock(6, 319, 8, table) || srv.BlockAt(6, 319, 8) == table })
	c.FacePlayer(6.5, 319, 10.5, 0, 0)
	c.PlaceBlock(6, 319, 8, protocol.FaceTop)
	opened, limited := false, false
	c.SettleAndCollect(func(id int32, _ []byte) {
		opened = opened || id == protocol.IDPlayOpenScreen
		limited = limited || id == protocol.IDPlaySystemChat
	})
	if !opened {
		t.Error("the crafting table did not open")
	}
	if limited {
		t.Error("a used block got the build-limit line")
	}
}

// TestASpectatorOpeningAMenuAtTheBuildLimitSendsNoLimitLine checks a spectator
// holding stone who clicks the top of a chest at the world's top: the menu
// opening answers CONSUME, so the line stays unsent.
// vanilla: ServerPlayerGameMode.useItemOn
func TestASpectatorOpeningAMenuAtTheBuildLimitSendsNoLimitLine(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BlockTestConfig(), "Ghost", servertest.OpsListing(t, "Ghost"))

	stone, _ := registry.ItemID("minecraft:stone")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	c.SendChangeGameMode(protocol.GameModeSpectator)
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.PlayerGameMode("Ghost") == protocol.GameModeSpectator })
	chest := registry.BlockStates["minecraft:chest"]
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.SetBlock(6, 319, 8, chest) || srv.BlockAt(6, 319, 8) == chest })
	c.FacePlayer(6.5, 319, 10.5, 0, 0)
	c.PlaceBlock(6, 319, 8, protocol.FaceTop)
	opened, limited := false, false
	c.SettleAndCollect(func(id int32, data []byte) {
		opened = opened || id == protocol.IDPlayOpenScreen
		limited = limited || (id == protocol.IDPlaySystemChat && bytes.Equal(data, buildLimitLine("build.tooHigh", "319")))
	})
	if !opened {
		t.Error("the chest did not open for the spectator")
	}
	if limited {
		t.Error("a spectator's menu got the build-limit line")
	}
}
