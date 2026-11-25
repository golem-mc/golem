package mobs_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestFangsFindScaledPlayerOnlyInReachedSections: a scale-16 player's box
// reaches the fangs, but the query only visits the sections that the padded
// bite box touches, and the player is filed by the feet.
// vanilla: EvokerFangs.tick, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestFangsFindScaledPlayerOnlyInReachedSections(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Target", servertest.OpsListing(t, "Target"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(16.5, 64, 0.5, true)
	c.ChatCommand("attribute Target minecraft:scale base set 16")
	c.Settle()
	// The miss leaves the player at full health for the hit.
	for _, tc := range []struct {
		name string
		x    float64
		want float64
	}{
		{"section out of range", 13.5, 20},
		{"section in range", 14.5, 14},
	} {
		srv.SpawnFangs(tc.x, 64, 0.5, 0)
		servertest.RunTicks(t, srv, 8)
		if hp := srv.PlayerHealth("Target"); hp != tc.want {
			t.Errorf("%s: health = %v, want %v", tc.name, hp, tc.want)
		}
	}
}
