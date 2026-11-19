package commands_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestWorldborderFeedbackRoundsHalfUp checks that /worldborder prints its
// numbers as Java's String.format does: the shortest decimal form rounded
// half away from zero, not Go's half-even rounding of the binary value.
// vanilla: WorldBorderCommand.setSize, setDamageAmount, setCenter, getSize
func TestWorldborderFeedbackRoundsHalfUp(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	_, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.ChatCommand("worldborder set 10.25")
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.set.immediate", "10.3")
	c.ChatCommand("worldborder set 10.5")
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.set.immediate", "10.5")
	c.ChatCommand("worldborder get")
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.get", "11")
	c.ChatCommand("worldborder damage amount 0.125")
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.damage.amount.success", "0.13")
	c.ChatCommand("worldborder center 0.125 -0.125")
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.center.success", "0.13", "-0.13")
}
