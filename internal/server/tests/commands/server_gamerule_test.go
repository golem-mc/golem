package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
)

// TestGameruleCommandQueriesAndSets checks /gamerule: a bare rule name reads
// the rule out, a value puts it there, the feedback carries the rule and the
// value the vanilla server's own does, and an integer beyond a rule's range
// is refused, the rule keeping the value it stood at.
func TestGameruleCommandQueriesAndSets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /gamerule is a gamemaster's command, so the runner is listed as an
	// operator.
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// The query carries the rule and the value it stands at.
	c.ChatCommand("gamerule keep_inventory")
	c.ExpectCommandFeedback("commands.gamerule.query", "keep_inventory", "false")

	// The set puts the rule there and says so; the query after it reads the
	// value the set left.
	c.ChatCommand("gamerule keep_inventory true")
	c.ExpectCommandFeedback("commands.gamerule.set", "keep_inventory", "true")
	if got := srv.GameRule("keep_inventory"); !got {
		t.Error("keep_inventory after /gamerule keep_inventory true = false, want true")
	}
	c.ChatCommand("gamerule keep_inventory")
	c.ExpectCommandFeedback("commands.gamerule.query", "keep_inventory", "true")

	// The tree answers the minecraft:-prefixed spelling as itself.
	c.ChatCommand("gamerule minecraft:keep_inventory false")
	c.ExpectCommandFeedback("commands.gamerule.set", "keep_inventory", "false")
	if got := srv.GameRule("keep_inventory"); got {
		t.Error("keep_inventory after the prefixed set = true, want false")
	}

	// An integer rule is held to the range its table row carries: a value
	// outside it is refused and the rule keeps the value it stood at.
	c.ChatCommand("gamerule max_snow_accumulation_height 99")
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.integer.big", "8", "99"),
		servertest.CommandErrorContextForTest("gamerule max_snow_accumulation_height 99",
			strings.Index("gamerule max_snow_accumulation_height 99", "99")))
	if got := srv.GameRuleValue("max_snow_accumulation_height"); got != 1 {
		t.Errorf("max_snow_accumulation_height after a refused set of 99 = %d, want the default 1", got)
	}
	c.ChatCommand("gamerule random_tick_speed 0")
	c.ExpectCommandFeedback("commands.gamerule.set", "random_tick_speed", "0")
	if got := srv.GameRuleValue("random_tick_speed"); got != 0 {
		t.Errorf("random_tick_speed after a set of 0 = %d, want 0", got)
	}

	// A quoted value reads as the text inside the quotes, which the walk
	// and the set both take, so the rule lands at the value it spells.
	c.ChatCommand(`gamerule keep_inventory "true"`)
	c.ExpectCommandFeedback("commands.gamerule.set", "keep_inventory", "true")
	if got := srv.GameRule("keep_inventory"); !got {
		t.Error(`keep_inventory after a set of "true" = false, want true`)
	}
	c.ChatCommand(`gamerule keep_inventory 'false'`)
	c.ExpectCommandFeedback("commands.gamerule.set", "keep_inventory", "false")
	if got := srv.GameRule("keep_inventory"); got {
		t.Error(`keep_inventory after a set of 'false' = true, want false`)
	}

	// A boolean value spelled anything else is refused, the way the
	// vanilla parser reads its word case-sensitively: no feedback, no
	// change.
	c.ChatCommand("gamerule keep_inventory True")
	if got := srv.GameRule("keep_inventory"); got {
		t.Error("keep_inventory after a set of True = true, want unchanged false")
	}

	c.Disconnect()
}

// TestGameruleSurvivesRestart checks that the rules a world sets are written
// to the game rule file and read back by the next server to open the world,
// so a /gamerule outlives a restart.
func TestGameruleSurvivesRestart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("gamerule keep_inventory true")
	c.ExpectCommandFeedback("commands.gamerule.set", "keep_inventory", "true")
	c.ChatCommand("gamerule random_tick_speed 7")
	c.ExpectCommandFeedback("commands.gamerule.set", "random_tick_speed", "7")
	srv.Shutdown()

	// A server of its own, as after a restart, pointed at the same world.
	restarted := *cfg
	restartedSrv, reconnect := servertest.Start(t, &restarted)
	if got := restartedSrv.GameRule("keep_inventory"); !got {
		t.Error("keep_inventory after restart = false, want true")
	}
	if got := restartedSrv.GameRuleValue("random_tick_speed"); got != 7 {
		t.Errorf("random_tick_speed after restart = %d, want 7", got)
	}
	_ = reconnect
}

// TestGameruleResultIsTheRuleValue checks that /execute store result takes a
// rule's value: the integer itself, 1 or 0 for a boolean.
// vanilla: GameRules.registerInteger, GameRules.registerBoolean
func TestGameruleResultIsTheRuleValue(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t)
	c.ChatCommand("gamerule spawn_patrols false")
	expectStored(t, srv, c, [][2]string{
		{"gamerule max_entity_cramming", "24"},
		{"gamerule max_entity_cramming 30", "30"},
		{"gamerule spawn_patrols", "0"},
		{"gamerule spawn_patrols true", "1"},
	})
}
