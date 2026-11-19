package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
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

// TestGameruleImmediateRespawnBroadcastsGameEvent checks that a set of
// immediate_respawn tells every player's client over the Game Event packet
// before it answers the runner, the order vanilla's GameRuleCommand.setRule
// sends them in.
func TestGameruleImmediateRespawnBroadcastsGameEvent(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("gamerule immediate_respawn true")
	// Reading the game event first fails if the feedback overtook it.
	// vanilla: GameRuleCommand.setRule
	event := c.ExpectPlay(protocol.IDPlayGameEvent)
	r := protocol.NewReader(event)
	if got := r.ReadU8(); got != protocol.GameEventImmediateRespawn {
		t.Errorf("game event after /gamerule immediate_respawn true = %d, want %d", got, protocol.GameEventImmediateRespawn)
	}
	if got := r.ReadF32(); got != 1 {
		t.Errorf("game event value = %v, want 1", got)
	}
	if !srv.GameRule("immediate_respawn") {
		t.Error("immediate_respawn after a set of true = false, want true")
	}
	c.ExpectCommandFeedback("commands.gamerule.set", "immediate_respawn", "true")

	// The way back broadcasts the other value, still ahead of its feedback.
	c.ChatCommand("gamerule immediate_respawn false")
	event = c.ExpectPlay(protocol.IDPlayGameEvent)
	r = protocol.NewReader(event)
	if got := r.ReadU8(); got != protocol.GameEventImmediateRespawn {
		t.Errorf("game event after /gamerule immediate_respawn false = %d, want %d", got, protocol.GameEventImmediateRespawn)
	}
	if got := r.ReadF32(); got != 0 {
		t.Errorf("game event value = %v, want 0", got)
	}
	c.ExpectCommandFeedback("commands.gamerule.set", "immediate_respawn", "false")

	c.Disconnect()
}

// TestGameruleReducedDebugSendsEntityEvent checks that a set of
// reduced_debug_info reaches every player as the entity event the vanilla
// server sends them: status 22 when the rule comes on, 23 when it goes off.
func TestGameruleReducedDebugSendsEntityEvent(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// The feedback and the entity event both ride the mailbox, the event
	// first; the feedback's wait keeps the event for the read after it.
	c.ChatCommand("gamerule reduced_debug_info true")
	servertest.AwaitCommandFeedback(t, c, "commands.gamerule.set", "reduced_debug_info", "true")
	if !srv.GameRule("reduced_debug_info") {
		t.Error("reduced_debug_info after a set of true = false, want true")
	}
	event := c.ExpectPlay(protocol.IDPlayEntityEvent)
	r := protocol.NewReader(event)
	if got := r.ReadI32(); got != c.PlayerEntityID {
		t.Errorf("entity event names entity %d, want the runner's %d", got, c.PlayerEntityID)
	}
	if got := r.ReadU8(); got != protocol.EntityStatusReducedDebugOn {
		t.Errorf("entity event status = %d, want %d", got, protocol.EntityStatusReducedDebugOn)
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

// The join carries the rules that shape the client's own screens: the
// vanilla join reads reduced_debug_info, immediate_respawn and
// limited_crafting out of the game rules as it builds the Login packet —
// the debug screen the first asks for, the death screen the second does
// away with, the crafting book's recipes the third locks — where the
// defaults leave all three off.
func TestTheJoinCarriesTheRuleToggles(t *testing.T) {
	t.Parallel()

	// The defaults: no reduced debug, the respawn screen shown, crafting
	// unlocked.
	defaults := config.Default()
	defaults.OnlineMode = false

	_, c := servertest.Dial(t, defaults)
	c.Login("Joiner")
	c.Configure()

	r := protocol.NewReader(c.Expect(protocol.IDPlayLogin))
	r.ReadI32()  // entity ID
	r.ReadBool() // hardcore
	count := r.ReadVarInt()
	if count < 1 {
		t.Fatalf("dimension count = %d, want the three", count)
	}
	for range count {
		r.ReadString(32767)
	}
	r.ReadVarInt() // max players
	r.ReadVarInt() // view distance
	r.ReadVarInt() // simulation distance
	if got := r.ReadBool(); got {
		t.Error("the join's reduced debug flag = true, want false under the defaults")
	}
	if got := r.ReadBool(); !got {
		t.Error("the join's respawn screen flag = false, want shown under the defaults")
	}
	if got := r.ReadBool(); got {
		t.Error("the join's limited crafting flag = true, want false under the defaults")
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reading the defaults' login: %v", err)
	}
	c.Disconnect()

	// The rules set: each toggle flips the flag it owns.
	set := config.Default()
	set.OnlineMode = false

	srv, connect := servertest.Start(t, set, func(s *server.Server) {
		s.SetGameRule("reduced_debug_info", true)
		s.SetGameRule("immediate_respawn", true)
		s.SetGameRule("limited_crafting", true)
	})
	joined := connect()
	joined.Login("Joiner")
	joined.Configure()

	r2 := protocol.NewReader(joined.Expect(protocol.IDPlayLogin))
	r2.ReadI32()
	r2.ReadBool()
	count2 := r2.ReadVarInt()
	if count2 < 1 {
		t.Fatalf("dimension count = %d, want the three", count2)
	}
	for range count2 {
		r2.ReadString(32767)
	}
	r2.ReadVarInt()
	r2.ReadVarInt()
	r2.ReadVarInt()
	if got := r2.ReadBool(); !got {
		t.Error("the join's reduced debug flag = false, want true with the rule on")
	}
	if got := r2.ReadBool(); got {
		t.Error("the join's respawn screen flag = true, want gone with immediate_respawn on")
	}
	if got := r2.ReadBool(); !got {
		t.Error("the join's limited crafting flag = false, want true with the rule on")
	}
	if err := r2.Err(); err != nil {
		t.Fatalf("reading the rules' login: %v", err)
	}
	joined.Disconnect()
	if got := srv.GameRule("reduced_debug_info"); !got {
		t.Error("reduced_debug_info after the setup = false, want true")
	}
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

// TestGameruleSetToCurrentValueIsRefused checks that setting a rule to the
// value it holds is refused, naming the rule and the value, and stores no
// success.
// vanilla: GameRuleCommand.setRule
func TestGameruleSetToCurrentValueIsRefused(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t)
	c.ChatCommand("gamerule keep_inventory false")
	servertest.AwaitCommandError(t, c, "commands.gamerule.not_set", "keep_inventory", "false")
	c.ChatCommand("gamerule random_tick_speed 3")
	servertest.AwaitCommandError(t, c, "commands.gamerule.not_set", "random_tick_speed", "3")
	expectStored := func(cmd, want string) {
		t.Helper()
		if got := storedSuccess(t, srv, c, cmd); got != want {
			t.Errorf("%s stored success %s, want %s", cmd, got, want)
		}
	}
	expectStored("gamerule keep_inventory false", "0")
	expectStored("gamerule keep_inventory true", "1")
	expectStored("gamerule keep_inventory true", "0")
}
