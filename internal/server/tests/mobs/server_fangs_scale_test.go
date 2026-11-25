package mobs_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestFangsFindScaledPlayerOnlyInReachedSections: a scale-16 player's box
// reaches the fangs, but the query only visits the sections that the padded
// bite box touches, and the player is filed by the feet.
// vanilla: EvokerFangs.tick, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestFangsFindScaledPlayerOnlyInReachedSections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		x    float64
		want float64
	}{
		{"section out of range", 13.5, 20},
		{"section in range", 14.5, 14},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Target"))
			c := connect().Join("Target")
			servertest.HoldTicks(t, srv)
			c.MoveToGround(16.5, 64, 0.5, true)
			c.ChatCommand("attribute Target minecraft:scale base set 16")
			c.Settle()
			srv.SpawnFangs(tc.x, 64, 0.5, 0)
			servertest.RunTicks(t, srv, 8)
			if hp := srv.PlayerHealth("Target"); hp != tc.want {
				t.Errorf("health = %v, want %v", hp, tc.want)
			}
		})
	}
}
