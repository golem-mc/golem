package lifecycle_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestDemoWorldIntroAndLock joins a --demo player: the intro screen comes
// within 30 ticks, /seed answers "North Carolina".hashCode(), and once the
// world's game time is past 120500 a dig on an instantly breakable block
// leaves it standing and demo.reminder follows the attempts.
// vanilla: DemoMode, WorldOptions.DEMO_OPTIONS
func TestDemoWorldIntroAndLock(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.Demo = true
	srv, c := servertest.Join(t, cfg, "Demo", servertest.OpsListing(t, "Demo"))
	servertest.HoldTicks(t, srv)

	intro := false
	c.ReadTicks(srv, 30, func(id int32, data []byte) bool {
		if id != protocol.IDPlayGameEvent {
			return false
		}
		r := protocol.NewReader(data)
		intro = r.ReadU8() == protocol.GameEventDemoEvent && r.ReadF32() == 0
		return intro
	})
	if !intro {
		t.Fatal("no DEMO_EVENT 0 within 30 ticks of joining")
	}

	c.ChatCommand("seed")
	if got := c.ExpectSeed(); got != -343522682 {
		t.Fatalf("/seed = %d, want -343522682", got)
	}

	pos := c.Pos()
	x, y, z := int32(pos.X), int32(pos.Y), int32(pos.Z)+1
	torch := registry.BlockTorch.DefaultState()
	srv.SetBlock(x, y, z, torch)

	srv.SetWorldAge(120501)
	servertest.RunTicks(t, srv, 1)
	reminded := false
	for range 102 {
		c.StartDigging(x, y, z)
		c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
			if id == protocol.IDPlaySystemChat {
				text, _ := protocol.ReadText(protocol.NewReader(data))
				reminded = reminded || text.Translate == "demo.reminder"
			}
			return false
		})
	}
	if got := srv.BlockAt(x, y, z); got != torch {
		t.Fatalf("block after a refused dig is state %d, want the torch %d", got, torch)
	}
	if !reminded {
		t.Fatal("no demo.reminder after 102 refused attempts")
	}
}
