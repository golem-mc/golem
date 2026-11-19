package world

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// readInitializeBorderAbsoluteMax joins a second client and reads the
// absolute maximum its Initialize World Border carries.
func readInitializeBorderAbsoluteMax(t *testing.T, connect func() *servertest.Client) int32 {
	t.Helper()
	c := connect()
	c.Login("Late")
	c.Configure()
	var packet []byte
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayInitializeBorder {
			packet = data
		}
	})
	if packet == nil {
		t.Fatal("no Initialize World Border on join")
	}
	r := protocol.NewReader(packet)
	r.ReadF64() // centre X
	r.ReadF64() // centre Z
	r.ReadF64() // old size
	r.ReadF64() // new size
	r.ReadVarLong()
	return r.ReadVarInt()
}

// TestADefaultConfigSendsTheWholeWorldAsAbsoluteMax checks the default
// max-world-size on the wire.
func TestADefaultConfigSendsTheWholeWorldAsAbsoluteMax(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())
	if got := readInitializeBorderAbsoluteMax(t, connect); got != 29999984 {
		t.Errorf("absolute max = %d, want 29999984", got)
	}
}

// TestMaxWorldSizeBoundsTheBorder checks that max-world-size clamps the
// border's edges: the join tells it, and a block past it is not broken.
// vanilla: MinecraftServer.createLevels, WorldBorder.setAbsoluteMaxSize
func TestMaxWorldSizeBoundsTheBorder(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.MaxWorldSize = 1000
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Warden"))
	if got := readInitializeBorderAbsoluteMax(t, connect); got != 1000 {
		t.Errorf("absolute max = %d, want 1000", got)
	}

	c := connect().Join("Warden")
	servertest.HoldTicks(t, srv)
	gold, _ := registry.FindState("minecraft:gold_block", nil)
	for _, tc := range []struct {
		x     int32
		stays bool
	}{
		{1010, true},
		{990, false},
	} {
		defer srv.HoldChunk(tc.x, 0)()
		if !srv.SetBlock(tc.x, 63, 0, gold) {
			t.Fatalf("chunk at (%d, 0) not loaded", tc.x)
		}
		c.ChatCommand(fmt.Sprintf("tp @s %d.5 64 0.5", tc.x))
		c.Settle()
		c.BreakBlock(tc.x, 63, 0)
		c.Settle()
		servertest.RunTicks(t, srv, 1)
		if got := srv.BlockAt(tc.x, 63, 0) == gold; got != tc.stays {
			t.Errorf("gold at (%d, 63, 0) stays = %v, want %v", tc.x, got, tc.stays)
		}
	}
}
