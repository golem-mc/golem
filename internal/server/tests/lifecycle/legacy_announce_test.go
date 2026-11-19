package lifecycle

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestLegacyAnnouncePlayerAchievements checks announce-player-achievements=false
// turns the show_advancement_messages rule off at start.
// vanilla: DedicatedServer.initServer
func TestLegacyAnnouncePlayerAchievements(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LegacyAnnounceAchievements = "false"
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.ChatCommand("gamerule show_advancement_messages")
	c.ExpectCommandFeedback("commands.gamerule.query", "show_advancement_messages", "false")
}
