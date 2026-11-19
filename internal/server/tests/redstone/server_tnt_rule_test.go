package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// tntRuleScene is a stopped server with the actor attached and tnt_explodes
// set to allowed, and a TNT block at (6,65,0).
func tntRuleScene(t *testing.T, allowed bool) (*server.Server, *servertest.Client, uint16) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()
	srv.SetGameRule(world.RuleTNTExplodes.String(), allowed)
	tnt := servertest.BlockState(t, "minecraft:tnt")
	if !srv.SetBlock(6, 65, 0, tnt) {
		t.Fatal("could not place the TNT")
	}
	return srv, c, tnt
}

func expectTNTKept(t *testing.T, srv *server.Server, tnt uint16) {
	t.Helper()
	if got := srv.BlockAt(6, 65, 0); got != tnt {
		t.Errorf("the TNT = %d, want the %d tnt_explodes keeps", got, tnt)
	}
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 0 {
		t.Errorf("primed TNT = %d, want none", n)
	}
}

func expectTNTPrimed(t *testing.T, srv *server.Server) {
	t.Helper()
	if got := srv.BlockAt(6, 65, 0); got != 0 {
		t.Errorf("the TNT = %d, want air once primed", got)
	}
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 1 {
		t.Errorf("primed TNT = %d, want the one", n)
	}
}

func TestRedstonePrimesTNTOnlyWhereTntExplodes(t *testing.T) {
	t.Parallel()
	for _, allowed := range []bool{false, true} {
		srv, c, tnt := tntRuleScene(t, allowed)
		servertest.PlaceFrom(c, "minecraft:redstone_block", 0, 0, 270, 6, 65, 0, protocol.FaceEast)
		servertest.DriveTargetTicks(srv, 2)
		if allowed {
			expectTNTPrimed(t, srv)
		} else {
			expectTNTKept(t, srv, tnt)
		}
	}
}

func TestABlastPrimesTNTOnlyWhereTntExplodes(t *testing.T) {
	t.Parallel()
	for _, allowed := range []bool{false, true} {
		srv, _, tnt := tntRuleScene(t, allowed)
		srv.SpawnTNT(3.5, 65, 0.5, 1)
		servertest.DriveTargetTicks(srv, 3)
		wantBlock := tnt
		if allowed {
			wantBlock = 0
		}
		if got := srv.BlockAt(6, 65, 0); got != wantBlock {
			t.Errorf("allowed=%v: the caught TNT = %d, want %d", allowed, got, wantBlock)
		}
		want := 0
		if allowed {
			want = 1
		}
		if n := len(srv.TNTsInDimension(world.DimensionName)); n != want {
			t.Errorf("allowed=%v: primed TNT = %d, want %d", allowed, n, want)
		}
	}
}
