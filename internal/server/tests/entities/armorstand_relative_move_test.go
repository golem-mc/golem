package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A falling armor stand is told its ordinary steps by relative Move Entity
// Position packets, and its landing, which flips the ground flag, by a full
// position sync.
// vanilla: ServerEntity.sendChanges
func TestFallingArmorStandMovesRelativelyThenSyncsOnLanding(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon armor_stand 0.5 70 0.5 {Tags:[s]}`)
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	relative, syncs := 0, 0
	c.ReadTicks(srv, 60, func(pid int32, data []byte) bool {
		switch pid {
		case protocol.IDPlayEntityPosition:
			relative++
		case protocol.IDPlayTeleportEntity:
			syncs++
		}
		return false
	})
	if relative == 0 {
		t.Errorf("no relative position packet during the fall (%d syncs)", syncs)
	}
	if syncs == 0 {
		t.Errorf("no position sync when the stand's ground flag changed (%d relative)", relative)
	}
}
