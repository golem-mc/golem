package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// TestJoinPlacementRunsTheSpawnSearch checks the join's own placement: a
// fresh player whose spawn block a body of water covers is carried out of it
// by the height fixup the search falls back to, the vanilla join putting a
// body on the water's surface rather than inside it, with the rule left at
// its default and the search's square a single candidate wide.
func TestJoinPlacementRunsTheSpawnSearch(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
		s.SetGameRuleValue("respawn_radius", 0)
		// The spawn block, two deep in water: the one candidate the
		// radius leaves the search fails on, and the fixup carries the
		// body up to the water's surface.
		water, _ := registry.FindState("minecraft:water", nil)
		s.SetLoadedBlock(0, 63, 0, water)
		s.SetLoadedBlock(0, 64, 0, water)
	})
	c := connect()
	c.Login("Testificate")
	c.Configure()
	pos := c.SpawnedAt()
	c.EnterWorld("Testificate")

	if pos.X != 0.5 || pos.Y != 65 || pos.Z != 0.5 {
		t.Errorf("join over a flooded spawn put the player at (%g, %g, %g), want (0.5, 65, 0.5)",
			pos.X, pos.Y, pos.Z)
	}
	if got := srv.GameRuleValue("respawn_radius"); got != 0 {
		t.Errorf("respawn_radius = %d, want the 0 the test set", got)
	}
	c.Disconnect()
}
