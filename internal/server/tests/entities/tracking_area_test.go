package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestTrackingAreaCrossingsKeepOtherWatchers pairs nine players, then takes
// one across a negative region boundary and back, in running and frozen worlds.
func TestTrackingAreaCrossingsKeepOtherWatchers(t *testing.T) {
	for _, frozen := range []bool{false, true} {
		t.Run(fmt.Sprintf("frozen=%v", frozen), func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			cfg.ViewDistance = 2
			cfg.SimulationDistance = 2
			srv, connect := servertest.Start(t, cfg)
			watchers := make([]*servertest.Client, 9)
			for i := range watchers {
				watchers[i] = connect().Join(fmt.Sprintf("Viewer%d", i))
			}
			for _, c := range watchers {
				c.Settle()
			}
			servertest.HoldTicks(t, srv)
			if frozen {
				srv.RunLine("tick freeze")
			}
			id := srv.SpawnMob("minecraft:cow", 20.5, 64, 5.5)
			srv.SetMobNoAI(id)
			for _, c := range watchers {
				got := protocol.NewReader(waitForMobSpawn(t, c, srv, 4)).ReadVarInt()
				if got != id {
					t.Fatalf("spawned %d, want %d", got, id)
				}
			}
			srv.RunLine("tp Viewer0 -256 64 0.5")
			servertest.AwaitRemovalsOfTicks(t, watchers[0], srv, 4, id)
			servertest.RunTicks(t, srv, 1)
			for _, c := range watchers[1:] {
				servertest.ExpectNoRemovalOfTicks(t, c, srv, 0, id)
			}
			srv.RunLine("tp Viewer0 -0.5 64 0.5")
			spawn := protocol.NewReader(waitForMobSpawn(t, watchers[0], srv, 4))
			if got := spawn.ReadVarInt(); got != id {
				t.Fatalf("return spawned %d, want %d", got, id)
			}
		})
	}
}
