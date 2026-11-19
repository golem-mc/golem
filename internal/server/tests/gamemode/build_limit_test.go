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
