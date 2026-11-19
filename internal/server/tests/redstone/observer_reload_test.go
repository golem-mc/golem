package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestObserverPulseSurvivesFrozenJoin(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor",
		servertest.OpsListing(t, "Actor"), func(s *server.Server) {
			servertest.FreezeUntilHeld(t, s)
		})
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 6, 64, 5, "observer[facing=west] strict")
	servertest.Setblock(t, c, 5, 64, 5, "stone")
	for tick, want := range []bool{false, false, true, true, false, false} {
		if tick > 0 {
			servertest.RunTicks(t, srv, 1)
		}
		if observer, powered := servertest.ObserverPowerAt(srv, 6, 64, 5); !observer || powered != want {
			t.Errorf("tick %d: observer=%v powered=%v, want powered=%v", tick, observer, powered, want)
		}
	}
}
